package wadors

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/testutil"
	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/net/dicomweb"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
)

const (
	wadoStudyUID  = "1.2.826.0.1.3680043.10.543.3301"
	wadoSeriesUID = "1.2.826.0.1.3680043.10.543.3301.1"
	wadoSOPUID    = "1.2.826.0.1.3680043.10.543.3301.1.1"
)

func TestRetrieveInstanceStoresAndReportsDuplicate(t *testing.T) {
	part10 := wadoPart10(t, wadoStudyUID, wadoSeriesUID, wadoSOPUID)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dicom-web/wado-rs/studies/"+wadoStudyUID+"/series/"+wadoSeriesUID+"/instances/"+wadoSOPUID {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/dicom")
		_, _ = w.Write(part10)
	}))
	defer server.Close()

	catalog := newWADOCatalog(t)
	client := ClientForNode(wadoNode(server.URL), nil)
	ref := dicomweb.InstanceRef{StudyInstanceUID: wadoStudyUID, SeriesInstanceUID: wadoSeriesUID, SOPInstanceUID: wadoSOPUID}
	outcome, err := RetrieveInstance(context.Background(), catalog, client, ref, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Method != MethodWADORS || outcome.Requested != 1 || outcome.Stored != 1 || outcome.Duplicates != 0 || outcome.Failed != 0 || outcome.Rejected != 0 {
		t.Fatalf("outcome = %+v", outcome)
	}
	if _, err := catalog.InstanceBySOPInstanceUID(context.Background(), wadoSOPUID); err != nil {
		t.Fatalf("stored instance not found: %v", err)
	}

	duplicate, err := RetrieveInstance(context.Background(), catalog, client, ref, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.Stored != 0 || duplicate.Duplicates != 1 || duplicate.Requested != 1 {
		t.Fatalf("duplicate outcome = %+v", duplicate)
	}
}

func TestRetrieveSeriesEnumeratesMetadataAndStoresInstances(t *testing.T) {
	firstSOP := wadoSOPUID
	secondSOP := "1.2.826.0.1.3680043.10.543.3301.1.2"
	part10BySOP := map[string][]byte{
		firstSOP:  wadoPart10(t, wadoStudyUID, wadoSeriesUID, firstSOP),
		secondSOP: wadoPart10(t, wadoStudyUID, wadoSeriesUID, secondSOP),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/dicom-web/wado-rs/studies/"+wadoStudyUID+"/series/"+wadoSeriesUID+"/metadata":
			w.Header().Set("Content-Type", "application/dicom+json")
			_, _ = w.Write([]byte(fmt.Sprintf(`[
				{"0020000D":{"vr":"UI","Value":[%q]},"0020000E":{"vr":"UI","Value":[%q]},"00080018":{"vr":"UI","Value":[%q]}},
				{"0020000D":{"vr":"UI","Value":[%q]},"0020000E":{"vr":"UI","Value":[%q]},"00080018":{"vr":"UI","Value":[%q]}}
			]`, wadoStudyUID, wadoSeriesUID, firstSOP, wadoStudyUID, wadoSeriesUID, secondSOP)))
		case strings.Contains(r.URL.Path, "/instances/"):
			sop := pathTail(r.URL.Path)
			w.Header().Set("Content-Type", "application/dicom")
			_, _ = w.Write(part10BySOP[sop])
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	var progress []Progress
	catalog := newWADOCatalog(t)
	outcome, err := RetrieveSeries(context.Background(), catalog, ClientForNode(wadoNode(server.URL), nil), wadoStudyUID, wadoSeriesUID, Options{
		OnProgress: func(p Progress) { progress = append(progress, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Requested != 2 || outcome.Stored != 2 || outcome.Failed != 0 {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(progress) != 2 || progress[1].Completed != 2 {
		t.Fatalf("progress = %+v", progress)
	}
	instances, err := catalog.InstancesForSeries(context.Background(), wadoSeriesUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 2 {
		t.Fatalf("instances = %d, want 2", len(instances))
	}
}

func TestRetrieveStudyEnumeratesMetadataAndStoresInstances(t *testing.T) {
	firstSeriesUID := wadoSeriesUID
	secondSeriesUID := "1.2.826.0.1.3680043.10.543.3301.2"
	firstSOP := wadoSOPUID
	secondSOP := secondSeriesUID + ".1"
	part10BySOP := map[string][]byte{
		firstSOP:  wadoPart10(t, wadoStudyUID, firstSeriesUID, firstSOP),
		secondSOP: wadoPart10(t, wadoStudyUID, secondSeriesUID, secondSOP),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/dicom-web/wado-rs/studies/"+wadoStudyUID+"/metadata":
			w.Header().Set("Content-Type", "application/dicom+json")
			_, _ = w.Write([]byte(fmt.Sprintf(`[
				{"0020000D":{"vr":"UI","Value":[%q]},"0020000E":{"vr":"UI","Value":[%q]},"00080018":{"vr":"UI","Value":[%q]}},
				{"0020000D":{"vr":"UI","Value":[%q]},"0020000E":{"vr":"UI","Value":[%q]},"00080018":{"vr":"UI","Value":[%q]}}
			]`, wadoStudyUID, firstSeriesUID, firstSOP, wadoStudyUID, secondSeriesUID, secondSOP)))
		case strings.Contains(r.URL.Path, "/instances/"):
			sop := pathTail(r.URL.Path)
			w.Header().Set("Content-Type", "application/dicom")
			_, _ = w.Write(part10BySOP[sop])
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	catalog := newWADOCatalog(t)
	outcome, err := RetrieveStudy(context.Background(), catalog, ClientForNode(wadoNode(server.URL), nil), wadoStudyUID, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Requested != 2 || outcome.Stored != 2 || outcome.Failed != 0 {
		t.Fatalf("outcome = %+v", outcome)
	}
	firstInstances, err := catalog.InstancesForSeries(context.Background(), firstSeriesUID)
	if err != nil {
		t.Fatal(err)
	}
	secondInstances, err := catalog.InstancesForSeries(context.Background(), secondSeriesUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstInstances) != 1 || len(secondInstances) != 1 {
		t.Fatalf("instances by series = %d/%d, want 1/1", len(firstInstances), len(secondInstances))
	}
}

func TestRetrieveInstanceSupportsMultipartDICOMAndCountsBadPart(t *testing.T) {
	valid := wadoPart10(t, wadoStudyUID, wadoSeriesUID, wadoSOPUID)
	body, contentType := multipartDICOMBody(t, [][]byte{valid, []byte("not dicom")})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	catalog := newWADOCatalog(t)
	outcome, err := RetrieveInstance(context.Background(), catalog, ClientForNode(wadoNode(server.URL), nil), dicomweb.InstanceRef{StudyInstanceUID: wadoStudyUID, SeriesInstanceUID: wadoSeriesUID, SOPInstanceUID: wadoSOPUID}, Options{})
	if err == nil {
		t.Fatal("RetrieveInstance succeeded with malformed multipart part")
	}
	if outcome.Stored != 1 || outcome.Failed != 1 || outcome.Requested != 1 {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestRetrieveInstanceRejectsMismatchedObjectUIDs(t *testing.T) {
	mismatchedSOP := "1.2.826.0.1.3680043.10.543.3301.1.99"
	part10 := wadoPart10(t, wadoStudyUID, wadoSeriesUID, mismatchedSOP)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dicom")
		_, _ = w.Write(part10)
	}))
	defer server.Close()

	catalog := newWADOCatalog(t)
	outcome, err := RetrieveInstance(context.Background(), catalog, ClientForNode(wadoNode(server.URL), nil), dicomweb.InstanceRef{StudyInstanceUID: wadoStudyUID, SeriesInstanceUID: wadoSeriesUID, SOPInstanceUID: wadoSOPUID}, Options{})
	if err == nil {
		t.Fatal("RetrieveInstance accepted object with mismatched SOP Instance UID")
	}
	if outcome.Stored != 0 || outcome.Failed != 1 {
		t.Fatalf("outcome = %+v, want one failed and no stored objects", outcome)
	}
	if _, err := catalog.InstanceBySOPInstanceUID(context.Background(), mismatchedSOP); err == nil {
		t.Fatal("mismatched object was imported")
	}
}

func TestRetrieveInstanceRejectsOversizedResponse(t *testing.T) {
	part10 := wadoPart10(t, wadoStudyUID, wadoSeriesUID, wadoSOPUID)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dicom")
		_, _ = w.Write(part10)
	}))
	defer server.Close()

	catalog := newWADOCatalog(t)
	outcome, err := RetrieveInstance(context.Background(), catalog, ClientForNode(wadoNode(server.URL), nil), dicomweb.InstanceRef{StudyInstanceUID: wadoStudyUID, SeriesInstanceUID: wadoSeriesUID, SOPInstanceUID: wadoSOPUID}, Options{MaxObjectBytes: 16})
	if err == nil {
		t.Fatal("RetrieveInstance accepted oversized response")
	}
	if outcome.Failed != 1 || outcome.Stored != 0 {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestRetrieveInstanceReportsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer server.Close()

	catalog := newWADOCatalog(t)
	outcome, err := RetrieveInstance(context.Background(), catalog, ClientForNode(wadoNode(server.URL), nil), dicomweb.InstanceRef{StudyInstanceUID: wadoStudyUID, SeriesInstanceUID: wadoSeriesUID, SOPInstanceUID: wadoSOPUID}, Options{})
	if err == nil {
		t.Fatal("RetrieveInstance succeeded after HTTP 404")
	}
	if outcome.Failed != 1 || !strings.Contains(err.Error(), "404") {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
}

func newWADOCatalog(t *testing.T) *archive.Catalog {
	t.Helper()
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close() })
	return catalog
}

func wadoNode(baseURL string) nodes.Node {
	return nodes.Node{
		ID:             "web",
		Name:           "web",
		Protocol:       nodes.ProtocolDICOMweb,
		BaseURL:        baseURL + "/dicom-web",
		WADOPathPrefix: "/wado-rs",
	}
}

func wadoPart10(t *testing.T, studyUID string, seriesUID string, sopUID string) []byte {
	t.Helper()
	file := &object.File{
		Dataset: object.FromElements([]core.Element{
			testutil.StringElement(core.NewTag(0x0008, 0x0016), core.VRUI, "1.2.840.10008.5.1.4.1.1.2"),
			testutil.StringElement(core.NewTag(0x0008, 0x0018), core.VRUI, sopUID),
			testutil.StringElement(core.NewTag(0x0010, 0x0010), core.VRPN, "WADO^PATIENT"),
			testutil.StringElement(core.NewTag(0x0010, 0x0020), core.VRLO, "W001"),
			testutil.StringElement(core.NewTag(0x0008, 0x0020), core.VRDA, "20260604"),
			testutil.StringElement(core.NewTag(0x0008, 0x0060), core.VRCS, "CT"),
			testutil.StringElement(core.NewTag(0x0020, 0x000D), core.VRUI, studyUID),
			testutil.StringElement(core.NewTag(0x0020, 0x000E), core.VRUI, seriesUID),
		}, std.Dictionary),
		TransferSyntax: transfer.ExplicitVRLittleEndian,
	}
	var buf bytes.Buffer
	if err := object.WriteFile(&buf, file); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func multipartDICOMBody(t *testing.T, parts [][]byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for _, data := range parts {
		part, err := writer.CreatePart(textproto.MIMEHeader{"Content-Type": []string{"application/dicom"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), fmt.Sprintf("multipart/related; type=\"application/dicom\"; boundary=%s", writer.Boundary())
}

func pathTail(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}
