package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/query"
)

func TestSessionQueryStudiesDispatchesDICOMwebSourceToQIDO(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dicom-web/qido-rs/studies" {
			t.Fatalf("path = %q, want DICOMweb study search", r.URL.Path)
		}
		if got := r.URL.Query().Get("PatientID"); got != "P001" {
			t.Fatalf("PatientID query = %q, want P001", got)
		}
		w.Header().Set("Content-Type", "application/dicom+json")
		_, _ = w.Write([]byte(`[{
			"00100010":{"vr":"PN","Value":[{"Alphabetic":"DOE^JANE"}]},
			"00100020":{"vr":"LO","Value":["P001"]},
			"0020000D":{"vr":"UI","Value":["1.2.3.study"]}
		}]`))
	}))
	defer server.Close()

	sess := newQueryTestSession(t)
	source := dicomwebQueryNode(server.URL, "web-1")
	result, err := sess.QueryStudies(context.Background(), []nodes.Node{source}, query.Criteria{PatientID: "P001"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(result.Matches))
	}
	match := result.Matches[0]
	if match.PatientName != "DOE^JANE" || match.StudyInstanceUID != "1.2.3.study" {
		t.Fatalf("match = %+v", match)
	}
	if match.SourceNodeID != "web-1" || match.SourceNodeName != "web" {
		t.Fatalf("source annotation = %+v", match)
	}
}

func TestSessionQuerySeriesAndImagesDispatchDICOMwebSourceToQIDO(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dicom+json")
		switch r.URL.Path {
		case "/dicom-web/qido-rs/studies/1.2.3.study/series":
			_, _ = w.Write([]byte(`[{
				"0020000D":{"vr":"UI","Value":["1.2.3.study"]},
				"0020000E":{"vr":"UI","Value":["1.2.3.series"]},
				"00080060":{"vr":"CS","Value":["CT"]}
			}]`))
		case "/dicom-web/qido-rs/studies/1.2.3.study/series/1.2.3.series/instances":
			_, _ = w.Write([]byte(`[{
				"0020000D":{"vr":"UI","Value":["1.2.3.study"]},
				"0020000E":{"vr":"UI","Value":["1.2.3.series"]},
				"00080018":{"vr":"UI","Value":["1.2.3.instance"]}
			}]`))
		default:
			t.Fatalf("unexpected QIDO path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	sess := newQueryTestSession(t)
	source := dicomwebQueryNode(server.URL, "web-1")
	series, err := sess.QuerySeries(context.Background(), []nodes.Node{source}, query.SeriesCriteria{StudyInstanceUID: "1.2.3.study"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Matches) != 1 || series.Matches[0].SeriesInstanceUID != "1.2.3.series" || series.Matches[0].SourceNodeID != "web-1" {
		t.Fatalf("series result = %+v", series.Matches)
	}

	images, err := sess.QueryImages(context.Background(), []nodes.Node{source}, query.ImageCriteria{StudyInstanceUID: "1.2.3.study", SeriesInstanceUID: "1.2.3.series"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(images.Matches) != 1 || images.Matches[0].SOPInstanceUID != "1.2.3.instance" || images.Matches[0].SourceNodeID != "web-1" {
		t.Fatalf("image result = %+v", images.Matches)
	}
}

func TestSessionQueryStudiesKeepsMixedSourcePartialFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dicom+json")
		_, _ = w.Write([]byte(`[{
			"00100010":{"vr":"PN","Value":[{"Alphabetic":"WEB^PATIENT"}]},
			"0020000D":{"vr":"UI","Value":["1.2.3.web"]}
		}]`))
	}))
	defer server.Close()

	sess := newQueryTestSession(t)
	sources := []nodes.Node{
		dicomwebQueryNode(server.URL, "web-1"),
		{ID: "dimse-1", Name: "dimse-down", AETitle: "DOWN", Host: "127.0.0.1", Port: 1},
	}
	var progress []QueryProgress
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := sess.QueryStudies(ctx, sources, query.Criteria{}, QueryObserverFunc(func(p QueryProgress) {
		progress = append(progress, p)
	}))

	var failures *QuerySourceFailures
	if !errors.As(err, &failures) {
		t.Fatalf("error = %v, want QuerySourceFailures", err)
	}
	if len(result.Matches) != 1 || result.Matches[0].PatientName != "WEB^PATIENT" {
		t.Fatalf("partial result = %+v", result.Matches)
	}
	if !failures.Failed(sources[1]) || failures.Failed(sources[0]) {
		t.Fatalf("failures = %+v", failures)
	}
	if !strings.Contains(err.Error(), "dimse-down") {
		t.Fatalf("error = %q, want failed source name", err.Error())
	}
	if len(progress) != 2 {
		t.Fatalf("progress updates = %+v, want 2", progress)
	}
	if progress[0] != (QueryProgress{Attempted: 1, Total: 2, Matches: 1, Failures: 0}) {
		t.Fatalf("first progress = %+v", progress[0])
	}
	if progress[1].Attempted != 2 || progress[1].Total != 2 || progress[1].Matches != 1 || progress[1].Failures != 1 {
		t.Fatalf("second progress = %+v", progress[1])
	}
}

func newQueryTestSession(t *testing.T) *Session {
	t.Helper()
	sess, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

func dicomwebQueryNode(baseURL string, id string) nodes.Node {
	return nodes.Node{
		ID:             id,
		Name:           "web",
		Protocol:       nodes.ProtocolDICOMweb,
		BaseURL:        baseURL + "/dicom-web",
		QIDOPathPrefix: "/qido-rs",
	}
}
