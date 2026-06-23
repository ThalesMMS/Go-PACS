package send

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/net/ul"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
)

const (
	testCTImageStorageSOPClassUID = "1.2.840.10008.5.1.4.1.1.2"
	testStudyInstanceUID          = "1.2.826.0.1.3680043.10.543.1"
	testSeriesInstanceUID         = "1.2.826.0.1.3680043.10.543.2"
	testOtherSeriesInstanceUID    = "1.2.826.0.1.3680043.10.543.20"
	testSOPInstanceUID            = "1.2.826.0.1.3680043.10.543.3"
	testOtherSOPInstanceUID       = "1.2.826.0.1.3680043.10.543.30"
)

func TestSendStudyStoresImportedInstanceAgainstLocalSCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	source := filepath.Join(t.TempDir(), "send.dcm")
	if err := os.WriteFile(source, testStoragePart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := catalog.ImportPath(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if report.StoredFiles != 1 {
		t.Fatalf("StoredFiles = %d, want 1", report.StoredFiles)
	}

	listener, err := ul.Listen(ul.ListenOptions{Address: "127.0.0.1:0", Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	done := make(chan error, 1)
	go serveSingleCStore(t, ctx, listener, done)

	node := nodes.Node{
		Name:    "storescp",
		AETitle: "STORESCP",
		Host:    "127.0.0.1",
		Port:    uint16(listener.Addr().(*net.TCPAddr).Port),
	}
	outcome, err := SendStudy(ctx, catalog, node, testStudyInstanceUID, "STORESCU")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Attempted != 1 {
		t.Fatalf("Attempted = %d, want 1", outcome.Attempted)
	}
	if outcome.Sent != 1 {
		t.Fatalf("Sent = %d, want 1", outcome.Sent)
	}
	if outcome.Warnings != 0 {
		t.Fatalf("Warnings = %d, want 0", outcome.Warnings)
	}
	if outcome.Failed != 0 {
		t.Fatalf("Failed = %d, want 0 (%v)", outcome.Failed, outcome.Failures)
	}
	if len(outcome.Results) != 1 {
		t.Fatalf("len(Results) = %d, want 1", len(outcome.Results))
	}
	if outcome.Results[0].SOPInstanceUID != testSOPInstanceUID {
		t.Fatalf("result SOPInstanceUID = %q, want %q", outcome.Results[0].SOPInstanceUID, testSOPInstanceUID)
	}
	if outcome.Results[0].Status != dimse.StatusSuccess {
		t.Fatalf("result Status = 0x%04X, want success", outcome.Results[0].Status)
	}
	if outcome.Results[0].RequestedTransferSyntaxUID != transfer.ExplicitVRLittleEndian.UID {
		t.Fatalf("result requested transfer syntax = %q", outcome.Results[0].RequestedTransferSyntaxUID)
	}
	if outcome.Results[0].NegotiatedTransferSyntaxUID != transfer.ExplicitVRLittleEndian.UID {
		t.Fatalf("result negotiated transfer syntax = %q", outcome.Results[0].NegotiatedTransferSyntaxUID)
	}
	if err := <-done; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestSendStudyWithNoInstancesIsNoOp(t *testing.T) {
	ctx := context.Background()
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	outcome, err := SendStudy(ctx, catalog, nodes.Node{}, "1.2.3.missing", "STORESCU")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Attempted != 0 || outcome.Sent != 0 || outcome.Warnings != 0 || outcome.Failed != 0 || len(outcome.Failures) != 0 || outcome.Duration != 0 {
		t.Fatalf("Outcome = %#v, want zero value", outcome)
	}
}

func TestSendFilesWithOptionsReportsProgressAfterEachFile(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tempDir := t.TempDir()
	first := filepath.Join(tempDir, "send-1.dcm")
	second := filepath.Join(tempDir, "send-2.dcm")
	if err := os.WriteFile(first, testStoragePart10FileWithUIDs(t, testStudyInstanceUID, testSeriesInstanceUID, testSOPInstanceUID), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, testStoragePart10FileWithUIDs(t, testStudyInstanceUID, testSeriesInstanceUID, testOtherSOPInstanceUID), 0o644); err != nil {
		t.Fatal(err)
	}

	listener, err := ul.Listen(ul.ListenOptions{Address: "127.0.0.1:0", Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	done := make(chan error, 1)
	go serveCStoreRequests(t, ctx, listener, done, []string{testSOPInstanceUID, testOtherSOPInstanceUID})

	node := nodes.Node{
		Name:    "storescp",
		AETitle: "STORESCP",
		Host:    "127.0.0.1",
		Port:    uint16(listener.Addr().(*net.TCPAddr).Port),
	}
	var updates []Progress
	outcome, err := SendFilesWithOptions(ctx, node, []string{first, second}, Options{
		CallingAETitle: "STORESCU",
		OnProgress: func(update Progress) {
			updates = append(updates, update)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Attempted != 2 || outcome.Sent != 2 || outcome.Failed != 0 {
		t.Fatalf("outcome = %#v, want two successful sends", outcome)
	}
	if len(updates) != 2 {
		t.Fatalf("progress updates = %d, want 2", len(updates))
	}
	if updates[0].Attempted != 1 || updates[0].Sent != 1 || updates[0].Total != 2 || updates[0].Path != first {
		t.Fatalf("first progress update = %#v", updates[0])
	}
	if updates[1].Attempted != 2 || updates[1].Sent != 2 || updates[1].Total != 2 || updates[1].Path != second {
		t.Fatalf("second progress update = %#v", updates[1])
	}
	if err := <-done; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestProposedTransferSyntaxesUseNodePreference(t *testing.T) {
	tests := []struct {
		name       string
		fileSyntax string
		preference string
		want       []string
	}{
		{
			name:       "auto keeps file syntax first",
			fileSyntax: transfer.ExplicitVRLittleEndian.UID,
			preference: nodes.SendTransferSyntaxAuto,
			want:       []string{transfer.ExplicitVRLittleEndian.UID, transfer.ImplicitVRLittleEndian.UID},
		},
		{
			name:       "explicit native first",
			fileSyntax: transfer.ImplicitVRLittleEndian.UID,
			preference: nodes.SendTransferSyntaxExplicitVRLittleEndian,
			want:       []string{transfer.ExplicitVRLittleEndian.UID, transfer.ImplicitVRLittleEndian.UID},
		},
		{
			name:       "implicit native first",
			fileSyntax: transfer.ExplicitVRLittleEndian.UID,
			preference: nodes.SendTransferSyntaxImplicitVRLittleEndian,
			want:       []string{transfer.ImplicitVRLittleEndian.UID, transfer.ExplicitVRLittleEndian.UID},
		},
		{
			name:       "compressed file keeps original syntax only",
			fileSyntax: transfer.JPEG2000.UID,
			preference: nodes.SendTransferSyntaxExplicitVRLittleEndian,
			want:       []string{transfer.JPEG2000.UID},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := proposedTransferSyntaxes(test.fileSyntax, test.preference)
			if len(got) != len(test.want) {
				t.Fatalf("len(got) = %d, want %d (%#v)", len(got), len(test.want), got)
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Fatalf("syntax order = %#v, want %#v", got, test.want)
				}
			}
		})
	}
}

func TestSendSeriesStoresOnlySelectedSeriesAgainstLocalSCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	baseDir := t.TempDir()
	catalog, err := archive.Open(filepath.Join(baseDir, "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	incomingDir := filepath.Join(baseDir, "incoming")
	if err := os.Mkdir(incomingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(incomingDir, "selected-series.dcm")
	if err := os.WriteFile(selected, testStoragePart10FileWithUIDs(t, testStudyInstanceUID, testSeriesInstanceUID, testSOPInstanceUID), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(incomingDir, "other-series.dcm")
	if err := os.WriteFile(other, testStoragePart10FileWithUIDs(t, testStudyInstanceUID, testOtherSeriesInstanceUID, testOtherSOPInstanceUID), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := catalog.ImportPath(ctx, incomingDir)
	if err != nil {
		t.Fatal(err)
	}
	if report.StoredFiles != 2 {
		t.Fatalf("StoredFiles = %d, want 2", report.StoredFiles)
	}

	listener, err := ul.Listen(ul.ListenOptions{Address: "127.0.0.1:0", Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	done := make(chan error, 1)
	go serveSingleCStore(t, ctx, listener, done)

	node := nodes.Node{
		Name:    "storescp",
		AETitle: "STORESCP",
		Host:    "127.0.0.1",
		Port:    uint16(listener.Addr().(*net.TCPAddr).Port),
	}
	outcome, err := SendSeries(ctx, catalog, node, testSeriesInstanceUID, "STORESCU")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Attempted != 1 {
		t.Fatalf("Attempted = %d, want 1", outcome.Attempted)
	}
	if outcome.Sent != 1 {
		t.Fatalf("Sent = %d, want 1", outcome.Sent)
	}
	if outcome.Warnings != 0 {
		t.Fatalf("Warnings = %d, want 0", outcome.Warnings)
	}
	if outcome.Failed != 0 {
		t.Fatalf("Failed = %d, want 0 (%v)", outcome.Failed, outcome.Failures)
	}
	if err := <-done; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestSendInstanceStoresOnlySelectedImageAgainstLocalSCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	baseDir := t.TempDir()
	catalog, err := archive.Open(filepath.Join(baseDir, "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	incomingDir := filepath.Join(baseDir, "incoming")
	if err := os.Mkdir(incomingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(incomingDir, "selected-image.dcm")
	if err := os.WriteFile(selected, testStoragePart10FileWithUIDs(t, testStudyInstanceUID, testSeriesInstanceUID, testSOPInstanceUID), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(incomingDir, "other-image.dcm")
	if err := os.WriteFile(other, testStoragePart10FileWithUIDs(t, testStudyInstanceUID, testSeriesInstanceUID, testOtherSOPInstanceUID), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := catalog.ImportPath(ctx, incomingDir)
	if err != nil {
		t.Fatal(err)
	}
	if report.StoredFiles != 2 {
		t.Fatalf("StoredFiles = %d, want 2", report.StoredFiles)
	}

	listener, err := ul.Listen(ul.ListenOptions{Address: "127.0.0.1:0", Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	done := make(chan error, 1)
	go serveSingleCStore(t, ctx, listener, done)

	node := nodes.Node{
		Name:    "storescp",
		AETitle: "STORESCP",
		Host:    "127.0.0.1",
		Port:    uint16(listener.Addr().(*net.TCPAddr).Port),
	}
	outcome, err := SendInstance(ctx, catalog, node, testSOPInstanceUID, "STORESCU")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Attempted != 1 {
		t.Fatalf("Attempted = %d, want 1", outcome.Attempted)
	}
	if outcome.Sent != 1 {
		t.Fatalf("Sent = %d, want 1", outcome.Sent)
	}
	if outcome.Warnings != 0 {
		t.Fatalf("Warnings = %d, want 0", outcome.Warnings)
	}
	if outcome.Failed != 0 {
		t.Fatalf("Failed = %d, want 0 (%v)", outcome.Failed, outcome.Failures)
	}
	if err := <-done; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestSendDICOMwebStudySeriesAndInstanceUseSTOW(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	catalog, err := archive.Open(filepath.Join(baseDir, "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	incomingDir := filepath.Join(baseDir, "incoming")
	if err := os.Mkdir(incomingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(incomingDir, "first.dcm")
	if err := os.WriteFile(first, testStoragePart10FileWithUIDs(t, testStudyInstanceUID, testSeriesInstanceUID, testSOPInstanceUID), 0o644); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(incomingDir, "second.dcm")
	if err := os.WriteFile(second, testStoragePart10FileWithUIDs(t, testStudyInstanceUID, testOtherSeriesInstanceUID, testOtherSOPInstanceUID), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := catalog.ImportPath(ctx, incomingDir)
	if err != nil {
		t.Fatal(err)
	}
	if report.StoredFiles != 2 {
		t.Fatalf("StoredFiles = %d, want 2", report.StoredFiles)
	}

	server, requests := newSTOWTestServer(t, http.StatusOK, nil)
	defer server.Close()
	node := stowTestNode(server.URL)

	studyOutcome, err := SendStudy(ctx, catalog, node, testStudyInstanceUID, "")
	if err != nil {
		t.Fatal(err)
	}
	if studyOutcome.Method != MethodSTOWRS || studyOutcome.Attempted != 2 || studyOutcome.Sent != 2 || studyOutcome.Failed != 0 {
		t.Fatalf("study outcome = %+v", studyOutcome)
	}

	seriesOutcome, err := SendSeries(ctx, catalog, node, testSeriesInstanceUID, "")
	if err != nil {
		t.Fatal(err)
	}
	if seriesOutcome.Method != MethodSTOWRS || seriesOutcome.Attempted != 1 || seriesOutcome.Sent != 1 || seriesOutcome.Failed != 0 {
		t.Fatalf("series outcome = %+v", seriesOutcome)
	}

	imageOutcome, err := SendInstance(ctx, catalog, node, testOtherSOPInstanceUID, "")
	if err != nil {
		t.Fatal(err)
	}
	if imageOutcome.Method != MethodSTOWRS || imageOutcome.Attempted != 1 || imageOutcome.Sent != 1 || imageOutcome.Failed != 0 {
		t.Fatalf("image outcome = %+v", imageOutcome)
	}

	if len(*requests) != 3 {
		t.Fatalf("STOW requests = %d, want 3", len(*requests))
	}
	if got := (*requests)[0].sopInstanceUIDs; !sameStrings(got, []string{testSOPInstanceUID, testOtherSOPInstanceUID}) {
		t.Fatalf("study STOW SOPs = %v", got)
	}
	if got := (*requests)[1].sopInstanceUIDs; !sameStrings(got, []string{testSOPInstanceUID}) {
		t.Fatalf("series STOW SOPs = %v", got)
	}
	if got := (*requests)[2].sopInstanceUIDs; !sameStrings(got, []string{testOtherSOPInstanceUID}) {
		t.Fatalf("image STOW SOPs = %v", got)
	}
}

func TestSendDICOMwebStudyWithNoInstancesIsNoOp(t *testing.T) {
	ctx := context.Background()
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected STOW-RS request to %s", r.URL.Path)
	}))
	defer server.Close()

	outcome, err := SendStudy(ctx, catalog, stowTestNode(server.URL), "1.2.3.missing", "")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Method != "" || outcome.Attempted != 0 || outcome.Sent != 0 || outcome.Failed != 0 {
		t.Fatalf("outcome = %+v, want no-op", outcome)
	}
}

func TestSendDICOMwebMissingStoredFileFailsClearly(t *testing.T) {
	ctx := context.Background()
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	source := filepath.Join(t.TempDir(), "send.dcm")
	if err := os.WriteFile(source, testStoragePart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, source); err != nil {
		t.Fatal(err)
	}
	instance, err := catalog.InstanceBySOPInstanceUID(ctx, testSOPInstanceUID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(instance.StoredPath); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected STOW-RS request after missing stored file")
	}))
	defer server.Close()

	outcome, err := SendInstance(ctx, catalog, stowTestNode(server.URL), testSOPInstanceUID, "")
	if err == nil {
		t.Fatal("SendInstance succeeded with missing stored file")
	}
	if outcome.Method != MethodSTOWRS || outcome.Attempted != 1 || outcome.Failed != 1 || !strings.Contains(err.Error(), "open") {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
}

func TestSendDICOMwebPartialSTOWFailureMapsOutcome(t *testing.T) {
	ctx := context.Background()
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	incomingDir := filepath.Join(t.TempDir(), "incoming")
	if err := os.Mkdir(incomingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(incomingDir, "first.dcm"), testStoragePart10FileWithUIDs(t, testStudyInstanceUID, testSeriesInstanceUID, testSOPInstanceUID), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(incomingDir, "second.dcm"), testStoragePart10FileWithUIDs(t, testStudyInstanceUID, testSeriesInstanceUID, testOtherSOPInstanceUID), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, incomingDir); err != nil {
		t.Fatal(err)
	}

	server, _ := newSTOWTestServer(t, http.StatusOK, map[string]uint16{testOtherSOPInstanceUID: 0x0110})
	defer server.Close()

	outcome, err := SendStudy(ctx, catalog, stowTestNode(server.URL), testStudyInstanceUID, "")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Attempted != 2 || outcome.Sent != 1 || outcome.Failed != 1 {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(outcome.Failures) != 1 || !strings.Contains(outcome.Failures[0], "0x0110") {
		t.Fatalf("failures = %+v", outcome.Failures)
	}
}

func TestSendDICOMwebHTTPAuthFailureFailsAllObjects(t *testing.T) {
	ctx := context.Background()
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	source := filepath.Join(t.TempDir(), "send.dcm")
	if err := os.WriteFile(source, testStoragePart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ImportPath(ctx, source); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	outcome, err := SendInstance(ctx, catalog, stowTestNode(server.URL), testSOPInstanceUID, "")
	if err == nil {
		t.Fatal("SendInstance succeeded after HTTP 401")
	}
	if outcome.Attempted != 1 || outcome.Sent != 0 || outcome.Failed != 1 || !strings.Contains(err.Error(), "401") {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
}

func serveSingleCStore(t *testing.T, ctx context.Context, listener *ul.Listener, done chan<- error) {
	t.Helper()
	serveCStoreRequests(t, ctx, listener, done, []string{testSOPInstanceUID})
}

func serveCStoreRequests(t *testing.T, ctx context.Context, listener *ul.Listener, done chan<- error, sopInstanceUIDs []string) {
	t.Helper()
	assoc, err := listener.AcceptAssociation(ul.AcceptOptions{
		AETitle:                   "STORESCP",
		Context:                   ctx,
		SupportedAbstractSyntaxes: []string{testCTImageStorageSOPClassUID},
		SupportedTransferSyntaxes: []string{transfer.ExplicitVRLittleEndian.UID},
	})
	if err != nil {
		done <- err
		return
	}
	defer assoc.Close()

	pc, err := dimse.AcceptedContextForSOPClass(assoc, testCTImageStorageSOPClassUID)
	if err != nil {
		done <- err
		return
	}
	for i, sopInstanceUID := range sopInstanceUIDs {
		req, err := dimse.ReceiveCStoreRequest(assoc, pc.ID)
		if err != nil {
			done <- err
			return
		}
		if req.MessageID != uint16(i+1) {
			done <- errors.New("server received wrong C-STORE message ID")
			return
		}
		if req.AffectedSOPClassUID != testCTImageStorageSOPClassUID {
			done <- errors.New("server received wrong SOP Class UID")
			return
		}
		if req.AffectedSOPInstanceUID != sopInstanceUID {
			done <- errors.New("server received wrong SOP Instance UID")
			return
		}
		dataset, err := dimse.ReceiveDataSet(assoc, pc.ID, transfer.ExplicitVRLittleEndian)
		if err != nil {
			done <- err
			return
		}
		if got, ok := dataset.GetUID(core.NewTag(0x0008, 0x0018)); !ok || got != sopInstanceUID {
			done <- errors.New("server received dataset with wrong SOP Instance UID")
			return
		}
		if err := dimse.SendCStoreResponse(assoc, pc.ID, dimse.CStoreResponse{
			AffectedSOPClassUID:       req.AffectedSOPClassUID,
			MessageIDBeingRespondedTo: req.MessageID,
			AffectedSOPInstanceUID:    req.AffectedSOPInstanceUID,
			Status:                    dimse.StatusSuccess,
		}); err != nil {
			done <- err
			return
		}
	}
	pdu, err := assoc.ReadPDU()
	if err != nil {
		done <- err
		return
	}
	if _, ok := pdu.(*ul.ReleaseRQ); !ok {
		done <- errors.New("server expected A-RELEASE-RQ")
		return
	}
	done <- assoc.WritePDU(&ul.ReleaseRP{})
}

func testStoragePart10File(t *testing.T) []byte {
	t.Helper()
	return testStoragePart10FileWithUIDs(t, testStudyInstanceUID, testSeriesInstanceUID, testSOPInstanceUID)
}

func testStoragePart10FileWithUIDs(t *testing.T, studyUID, seriesUID, sopUID string) []byte {
	t.Helper()
	dataset := []core.Element{
		testStringElement(core.NewTag(0x0008, 0x0016), core.VRUI, testCTImageStorageSOPClassUID),
		testStringElement(core.NewTag(0x0008, 0x0018), core.VRUI, sopUID),
		testStringElement(core.NewTag(0x0010, 0x0010), core.VRPN, "SEND^PATIENT"),
		testStringElement(core.NewTag(0x0010, 0x0020), core.VRLO, "S001"),
		testStringElement(core.NewTag(0x0008, 0x0020), core.VRDA, "20260604"),
		testStringElement(core.NewTag(0x0008, 0x0060), core.VRCS, "CT"),
		testStringElement(core.NewTag(0x0020, 0x000D), core.VRUI, studyUID),
		testStringElement(core.NewTag(0x0020, 0x000E), core.VRUI, seriesUID),
	}
	file := &object.File{
		Dataset:        object.FromElements(dataset, std.Dictionary),
		TransferSyntax: transfer.ExplicitVRLittleEndian,
	}
	var buf bytes.Buffer
	if err := object.WriteFile(&buf, file); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testStringElement(tag core.Tag, vr core.VR, value string) core.Element {
	return core.Element{
		Header: core.ElementHeader{Tag: tag, VR: vr},
		Value:  core.StringValue{value},
	}
}

type stowTestRequest struct {
	sopInstanceUIDs []string
}

func newSTOWTestServer(t *testing.T, statusCode int, failures map[string]uint16) (*httptest.Server, *[]stowTestRequest) {
	t.Helper()
	var requests []stowTestRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/dicom-web/stow/studies"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("parse Content-Type: %v", err)
		}
		if mediaType != "multipart/related" || params["type"] != "application/dicom" || params["boundary"] == "" {
			t.Fatalf("Content-Type = %q params=%v", mediaType, params)
		}
		reader := multipart.NewReader(r.Body, params["boundary"])
		var sops []string
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("NextPart: %v", err)
			}
			if got := part.Header.Get("Content-Type"); got != "application/dicom" {
				t.Fatalf("part Content-Type = %q, want application/dicom", got)
			}
			data, err := io.ReadAll(part)
			if err != nil {
				t.Fatalf("read STOW part: %v", err)
			}
			file, err := object.ReadFile(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("parse STOW part: %v", err)
			}
			sop, ok := file.Dataset.GetUID(tagSOPInstanceUID)
			if !ok || sop == "" {
				t.Fatalf("STOW part missing SOP Instance UID")
			}
			sops = append(sops, sop)
		}
		requests = append(requests, stowTestRequest{sopInstanceUIDs: sops})
		w.Header().Set("Content-Type", "application/dicom+json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(stowResponseJSON(sops, failures)))
	}))
	return server, &requests
}

func stowTestNode(baseURL string) nodes.Node {
	return nodes.Node{
		ID:             "stow-node",
		Name:           "stow-node",
		Protocol:       nodes.ProtocolDICOMweb,
		BaseURL:        baseURL + "/dicom-web",
		STOWPathPrefix: "/stow",
	}
}

func stowResponseJSON(sops []string, failures map[string]uint16) string {
	var stored []string
	var failed []string
	for _, sop := range sops {
		if reason, ok := failures[sop]; ok {
			failed = append(failed, fmt.Sprintf(`{"00081150":{"vr":"UI","Value":[%q]},"00081155":{"vr":"UI","Value":[%q]},"00081197":{"vr":"US","Value":[%d]}}`, testCTImageStorageSOPClassUID, sop, reason))
			continue
		}
		stored = append(stored, fmt.Sprintf(`{"00081150":{"vr":"UI","Value":[%q]},"00081155":{"vr":"UI","Value":[%q]}}`, testCTImageStorageSOPClassUID, sop))
	}
	return fmt.Sprintf(`{"00081199":{"vr":"SQ","Value":[%s]},"00081198":{"vr":"SQ","Value":[%s]}}`, strings.Join(stored, ","), strings.Join(failed, ","))
}

func sameStrings(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
