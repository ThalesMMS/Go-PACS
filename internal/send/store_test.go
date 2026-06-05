package send

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/net/ul"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
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

func serveSingleCStore(t *testing.T, ctx context.Context, listener *ul.Listener, done chan<- error) {
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
	req, err := dimse.ReceiveCStoreRequest(assoc, pc.ID)
	if err != nil {
		done <- err
		return
	}
	if req.MessageID != 1 {
		done <- errors.New("server received wrong C-STORE message ID")
		return
	}
	if req.AffectedSOPClassUID != testCTImageStorageSOPClassUID {
		done <- errors.New("server received wrong SOP Class UID")
		return
	}
	if req.AffectedSOPInstanceUID != testSOPInstanceUID {
		done <- errors.New("server received wrong SOP Instance UID")
		return
	}
	dataset, err := dimse.ReceiveDataSet(assoc, pc.ID, transfer.ExplicitVRLittleEndian)
	if err != nil {
		done <- err
		return
	}
	if got, ok := dataset.GetUID(core.NewTag(0x0008, 0x0018)); !ok || got != testSOPInstanceUID {
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
