package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	ops "github.com/ThalesMMS/Go-PACS/internal/operations"
	"github.com/ThalesMMS/Go-PACS/internal/retrieve"
	"github.com/ThalesMMS/Go-PACS/internal/send"
)

func TestRetrieveRequiresRunningReceiverForMove(t *testing.T) {
	sess := openTestSession(t)

	_, err := sess.Retrieve(context.Background(), nodes.Node{
		Name:           "remote",
		AETitle:        "REMOTE",
		Host:           "127.0.0.1",
		Port:           11112,
		RetrieveMethod: nodes.RetrieveMethodMove,
	}, "STUDY", "1.2.3", "", "", nil)

	if !errors.Is(err, retrieve.ErrReceiverRequired) {
		t.Fatalf("Retrieve() error = %v, want ErrReceiverRequired", err)
	}
}

func TestRetrieveDICOMwebImageUsesWADORSAndRecordsHistory(t *testing.T) {
	studyUID := "1.2.826.0.1.3680043.10.543.3302"
	seriesUID := studyUID + ".1"
	sopUID := seriesUID + ".1"
	part10Path := filepath.Join(t.TempDir(), "one.dcm")
	writeCorePart10(t, part10Path, studyUID, seriesUID, sopUID)
	part10, err := os.ReadFile(part10Path)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/dicom-web/wado-rs/studies/" + studyUID + "/series/" + seriesUID + "/instances/" + sopUID
		if r.URL.Path != wantPath {
			t.Fatalf("path = %q, want %q", r.URL.Path, wantPath)
		}
		w.Header().Set("Content-Type", "application/dicom")
		_, _ = w.Write(part10)
	}))
	defer server.Close()

	sess := openTestSession(t)
	outcome, err := sess.Retrieve(context.Background(), nodes.Node{
		ID:             "web-node",
		Name:           "web-node",
		Protocol:       nodes.ProtocolDICOMweb,
		BaseURL:        server.URL + "/dicom-web",
		WADOPathPrefix: "/wado-rs",
	}, "IMAGE", studyUID, seriesUID, sopUID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Method != "WADO-RS" || outcome.Stored != 1 || outcome.Completed != 1 || outcome.Failed != 0 {
		t.Fatalf("outcome = %+v", outcome)
	}
	if _, err := sess.Catalog().InstanceBySOPInstanceUID(context.Background(), sopUID); err != nil {
		t.Fatalf("stored instance not found: %v", err)
	}
	history, err := sess.LoadHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Kind != ops.KindRetrieveWADORS {
		t.Fatalf("history = %+v, want one WADO-RS retrieve entry", history)
	}
	if history[0].Counts.Stored == nil || *history[0].Counts.Stored != 1 {
		t.Fatalf("history stored count = %#v, want 1", history[0].Counts.Stored)
	}
}

func TestSendDICOMwebImageUsesSTOWAndRecordsHistory(t *testing.T) {
	studyUID := "1.2.826.0.1.3680043.10.543.3303"
	seriesUID := studyUID + ".1"
	sopUID := seriesUID + ".1"

	sess := openTestSession(t)
	source := filepath.Join(t.TempDir(), "send.dcm")
	writeCorePart10(t, source, studyUID, seriesUID, sopUID)
	report, err := sess.Catalog().ImportPath(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if report.StoredFiles != 1 {
		t.Fatalf("StoredFiles = %d, want 1", report.StoredFiles)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/dicom-web/stow/studies"
		if r.URL.Path != wantPath {
			t.Fatalf("path = %q, want %q", r.URL.Path, wantPath)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/dicom+json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	outcome, err := sess.Send(context.Background(), nodes.Node{
		ID:             "stow-node",
		Name:           "stow-node",
		Protocol:       nodes.ProtocolDICOMweb,
		BaseURL:        server.URL + "/dicom-web",
		STOWPathPrefix: "/stow",
	}, "IMAGE", studyUID, seriesUID, sopUID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Method != send.MethodSTOWRS || outcome.Attempted != 1 || outcome.Sent != 1 || outcome.Failed != 0 {
		t.Fatalf("outcome = %+v", outcome)
	}
	history, err := sess.LoadHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Kind != ops.KindSendStore || history[0].Method != send.MethodSTOWRS {
		t.Fatalf("history = %+v, want one STOW-RS send entry", history)
	}
}
