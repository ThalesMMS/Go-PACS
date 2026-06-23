package receive

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/netverify"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/send"
	"github.com/ThalesMMS/Go-PACS/internal/testutil"
	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/net/ul"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/pixeldata/codecfixture"
	"github.com/ThalesMMS/dicom-go/transfer"
)

const (
	testStorageSOPClassUID     = "1.2.840.10008.5.1.4.1.1.2"
	testStudyInstanceUID       = "1.2.826.0.1.3680043.10.543.100"
	testSeriesInstanceUID      = "1.2.826.0.1.3680043.10.543.101"
	testSOPInstanceUID         = "1.2.826.0.1.3680043.10.543.102"
	testReceiverCallingAETitle = "STORESCU"
	testReceiverCalledAETitle  = "RECVSCP"
)

func TestServerReceivesCStoreIntoArchive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog: catalog,
		Address: "127.0.0.1:0",
		AETitle: testReceiverCalledAETitle,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	source := filepath.Join(t.TempDir(), "incoming.dcm")
	if err := os.WriteFile(source, testPart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}

	node := receiverNode(t, server)
	outcome, err := send.SendFiles(ctx, node, []string{source}, testReceiverCallingAETitle)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Attempted != 1 || outcome.Sent != 1 || outcome.Failed != 0 {
		t.Fatalf("outcome = %#v, want one successful send", outcome)
	}

	studies, err := catalog.Studies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 {
		t.Fatalf("len(studies) = %d, want 1", len(studies))
	}
	if studies[0].StudyInstanceUID != testStudyInstanceUID {
		t.Fatalf("StudyInstanceUID = %q, want %q", studies[0].StudyInstanceUID, testStudyInstanceUID)
	}

	instances, err := catalog.InstancesForStudy(ctx, testStudyInstanceUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("len(instances) = %d, want 1", len(instances))
	}
	if instances[0].SourcePath != "dicom://"+testReceiverCallingAETitle+"/"+testSOPInstanceUID {
		t.Fatalf("SourcePath = %q", instances[0].SourcePath)
	}

	snapshot := server.Snapshot()
	if snapshot.Stored != 1 {
		t.Fatalf("Snapshot.Stored = %d, want 1", snapshot.Stored)
	}
	if snapshot.Failed != 0 {
		t.Fatalf("Snapshot.Failed = %d, want 0", snapshot.Failed)
	}
}

func TestServerDecompressesIncomingCompressedImages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog:          catalog,
		Address:          "127.0.0.1:0",
		AETitle:          testReceiverCalledAETitle,
		DecompressImages: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	data, wantPixels := testCompressedPart10File(t)
	source := filepath.Join(t.TempDir(), "compressed.dcm")
	if err := os.WriteFile(source, data, 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := send.SendFiles(ctx, receiverNode(t, server), []string{source}, testReceiverCallingAETitle)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Sent != 1 || outcome.Failed != 0 {
		t.Fatalf("outcome = %#v, want one successful compressed send", outcome)
	}

	instances, err := catalog.InstancesForStudy(ctx, testStudyInstanceUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("len(instances) = %d, want 1", len(instances))
	}
	if instances[0].TransferSyntaxUID != transfer.ExplicitVRLittleEndian.UID {
		t.Fatalf("TransferSyntaxUID = %q, want %q", instances[0].TransferSyntaxUID, transfer.ExplicitVRLittleEndian.UID)
	}
	storedFile, err := os.Open(instances[0].StoredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer storedFile.Close()
	stored, err := object.ReadFile(storedFile)
	if err != nil {
		t.Fatal(err)
	}
	if raw, ok := stored.GetRaw(core.TagPixelData); !ok || !bytes.Equal(raw, wantPixels) {
		t.Fatalf("stored PixelData = %v, want %v", raw, wantPixels)
	}
}

func TestServerRejectsOversizedCStoreObject(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog:             catalog,
		Address:             "127.0.0.1:0",
		AETitle:             testReceiverCalledAETitle,
		MaxStoreObjectBytes: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	source := filepath.Join(t.TempDir(), "incoming.dcm")
	if err := os.WriteFile(source, testPart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := send.SendFiles(ctx, receiverNode(t, server), []string{source}, testReceiverCallingAETitle)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Sent != 0 || outcome.Failed != 1 {
		t.Fatalf("outcome = %#v, want one failed send", outcome)
	}

	studies, err := catalog.Studies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 0 {
		t.Fatalf("len(studies) = %d, want 0", len(studies))
	}

	snapshot := server.Snapshot()
	if snapshot.Stored != 0 {
		t.Fatalf("Snapshot.Stored = %d, want 0", snapshot.Stored)
	}
	if snapshot.Failed != 1 {
		t.Fatalf("Snapshot.Failed = %d, want 1", snapshot.Failed)
	}
}

func TestServerDrainsMalformedCStoreDataSetBeforeNextCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog: catalog,
		Address: "127.0.0.1:0",
		AETitle: testReceiverCalledAETitle,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	assoc, pc, err := dialStorageAssociation(ctx, server)
	if err != nil {
		t.Fatal(err)
	}
	defer assoc.Close()

	if err := dimse.SendCStoreRequest(assoc, pc.ID, dimse.CStoreRequest{
		AffectedSOPClassUID:    testStorageSOPClassUID,
		MessageID:              1,
		Priority:               dimse.PriorityMedium,
		AffectedSOPInstanceUID: "1.2.826.0.1.3680043.10.543.199",
	}); err != nil {
		t.Fatalf("SendCStoreRequest(malformed) error = %v", err)
	}
	if err := sendMalformedExplicitVRDataSet(assoc, pc.ID); err != nil {
		t.Fatalf("sendMalformedExplicitVRDataSet() error = %v", err)
	}
	malformedRsp, err := dimse.ReceiveCStoreResponse(assoc, pc.ID)
	if err != nil {
		t.Fatalf("ReceiveCStoreResponse(malformed) error = %v", err)
	}
	if malformedRsp.Status != dimse.StatusCStoreCannotUnderstand {
		t.Fatalf("malformed C-STORE status = 0x%04X, want 0x%04X", malformedRsp.Status, dimse.StatusCStoreCannotUnderstand)
	}

	validFile, err := object.ReadFile(bytes.NewReader(testPart10File(t)))
	if err != nil {
		t.Fatalf("ReadFile(valid) error = %v", err)
	}
	if err := dimse.SendCStoreRequest(assoc, pc.ID, dimse.CStoreRequest{
		AffectedSOPClassUID:    testStorageSOPClassUID,
		MessageID:              2,
		Priority:               dimse.PriorityMedium,
		AffectedSOPInstanceUID: testSOPInstanceUID,
	}); err != nil {
		t.Fatalf("SendCStoreRequest(valid) error = %v", err)
	}
	if err := dimse.SendDataSet(assoc, pc.ID, validFile.Dataset, transfer.ExplicitVRLittleEndian); err != nil {
		t.Fatalf("SendDataSet(valid) error = %v", err)
	}
	validRsp, err := dimse.ReceiveCStoreResponse(assoc, pc.ID)
	if err != nil {
		t.Fatalf("ReceiveCStoreResponse(valid) error = %v", err)
	}
	if validRsp.Status != dimse.StatusSuccess {
		t.Fatalf("valid C-STORE status = 0x%04X, want success", validRsp.Status)
	}

	snapshot := server.Snapshot()
	if snapshot.Stored != 1 {
		t.Fatalf("Snapshot.Stored = %d, want 1", snapshot.Stored)
	}
	if snapshot.Failed != 1 {
		t.Fatalf("Snapshot.Failed = %d, want 1", snapshot.Failed)
	}
}

func TestServerCountsMalformedCStoreFailureOnceAfterAssociationClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog: catalog,
		Address: "127.0.0.1:0",
		AETitle: testReceiverCalledAETitle,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	assoc, pc, err := dialStorageAssociation(ctx, server)
	if err != nil {
		t.Fatal(err)
	}
	if err := dimse.SendCStoreRequest(assoc, pc.ID, dimse.CStoreRequest{
		AffectedSOPClassUID:    testStorageSOPClassUID,
		MessageID:              1,
		Priority:               dimse.PriorityMedium,
		AffectedSOPInstanceUID: "1.2.826.0.1.3680043.10.543.200",
	}); err != nil {
		t.Fatalf("SendCStoreRequest(malformed) error = %v", err)
	}
	if err := sendMalformedExplicitVRDataSet(assoc, pc.ID); err != nil {
		t.Fatalf("sendMalformedExplicitVRDataSet() error = %v", err)
	}
	rsp, err := dimse.ReceiveCStoreResponse(assoc, pc.ID)
	if err != nil {
		t.Fatalf("ReceiveCStoreResponse(malformed) error = %v", err)
	}
	if rsp.Status != dimse.StatusCStoreCannotUnderstand {
		t.Fatalf("malformed C-STORE status = 0x%04X, want 0x%04X", rsp.Status, dimse.StatusCStoreCannotUnderstand)
	}
	if err := assoc.Close(); err != nil {
		t.Fatalf("close association: %v", err)
	}
	waitForAssociationHandlers(t, server)

	snapshot := server.Snapshot()
	if snapshot.Stored != 0 {
		t.Fatalf("Snapshot.Stored = %d, want 0", snapshot.Stored)
	}
	if snapshot.Failed != 1 {
		t.Fatalf("Snapshot.Failed = %d, want 1", snapshot.Failed)
	}
}

func TestServerAnswersCEcho(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog: catalog,
		Address: "127.0.0.1:0",
		AETitle: testReceiverCalledAETitle,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	result, err := netverify.Echo(ctx, receiverNode(t, server), testReceiverCallingAETitle)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != 0 {
		t.Fatalf("Status = 0x%04X, want success", result.Status)
	}
}

func TestServerAnswersCEchoOverTLS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog:   catalog,
		Address:   "127.0.0.1:0",
		AETitle:   testReceiverCalledAETitle,
		TLSConfig: testReceiveServerTLSConfig(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	node := receiverNode(t, server)
	node.UseTLS = true
	node.TLSSkipVerify = true
	result, err := netverify.Echo(ctx, node, testReceiverCallingAETitle)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != 0 {
		t.Fatalf("Status = 0x%04X, want success", result.Status)
	}
}

func TestStartRejectsNegativeMaxAssociations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog:         catalog,
		Address:         "127.0.0.1:0",
		AETitle:         testReceiverCalledAETitle,
		MaxAssociations: -1,
	})
	if err == nil {
		stopServer(t, server)
		t.Fatal("Start succeeded with negative MaxAssociations")
	}
}

func TestServerLimitsConcurrentAssociations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog:         catalog,
		Address:         "127.0.0.1:0",
		AETitle:         testReceiverCalledAETitle,
		MaxAssociations: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	first, _, err := dialVerificationAssociation(ctx, server)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	secondCtx, cancelSecond := context.WithTimeout(ctx, time.Second)
	second, secondPC, err := dialVerificationAssociation(secondCtx, server)
	cancelSecond()
	if err == nil {
		_, echoErr := dimse.SendCEcho(second, secondPC.ID, 1)
		_ = second.Close()
		if echoErr == nil {
			t.Fatal("second association processed C-ECHO while max associations slot was full")
		}
	}
	waitForRejectedAssociations(t, server, 1)

	releaseCtx, cancelRelease := context.WithTimeout(ctx, time.Second)
	defer cancelRelease()
	if err := first.Release(releaseCtx); err != nil {
		t.Fatal(err)
	}

	result, err := netverify.Echo(ctx, receiverNode(t, server), testReceiverCallingAETitle)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != 0 {
		t.Fatalf("Status after releasing slot = 0x%04X, want success", result.Status)
	}
}

func TestServerRejectsCStoreFromDisallowedCallingAE(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog:                catalog,
		Address:                "127.0.0.1:0",
		AETitle:                testReceiverCalledAETitle,
		AllowedCallingAETitles: []string{"TRUSTED"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	source := filepath.Join(t.TempDir(), "incoming.dcm")
	if err := os.WriteFile(source, testPart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := send.SendFiles(ctx, receiverNode(t, server), []string{source}, "BLOCKED")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Sent != 0 {
		t.Fatalf("Sent = %d, want 0", outcome.Sent)
	}
	if outcome.Failed != 1 {
		t.Fatalf("Failed = %d, want 1 (%v)", outcome.Failed, outcome.Failures)
	}

	studies, err := catalog.Studies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 0 {
		t.Fatalf("len(studies) = %d, want 0", len(studies))
	}
	snapshot := server.Snapshot()
	if snapshot.Rejected != 1 {
		t.Fatalf("Snapshot.Rejected = %d, want 1", snapshot.Rejected)
	}
	if snapshot.Stored != 0 {
		t.Fatalf("Snapshot.Stored = %d, want 0", snapshot.Stored)
	}
}

func TestServerRejectsCStoreFromDisallowedRemoteHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog:            catalog,
		Address:            "127.0.0.1:0",
		AETitle:            testReceiverCalledAETitle,
		AllowedRemoteHosts: []string{"192.0.2.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	source := filepath.Join(t.TempDir(), "incoming.dcm")
	if err := os.WriteFile(source, testPart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := send.SendFiles(ctx, receiverNode(t, server), []string{source}, testReceiverCallingAETitle)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Sent != 0 {
		t.Fatalf("Sent = %d, want 0", outcome.Sent)
	}
	if outcome.Failed != 1 {
		t.Fatalf("Failed = %d, want 1 (%v)", outcome.Failed, outcome.Failures)
	}
	snapshot := server.Snapshot()
	if snapshot.Rejected != 1 {
		t.Fatalf("Snapshot.Rejected = %d, want 1", snapshot.Rejected)
	}
}

func TestServerAcceptsConfiguredCalledAEAlias(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog:               catalog,
		Address:               "127.0.0.1:0",
		AETitle:               testReceiverCalledAETitle,
		AllowedCalledAETitles: []string{testReceiverCalledAETitle, "ALIAS"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	source := filepath.Join(t.TempDir(), "incoming.dcm")
	if err := os.WriteFile(source, testPart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}

	node := receiverNode(t, server)
	node.AETitle = "ALIAS"
	outcome, err := send.SendFiles(ctx, node, []string{source}, testReceiverCallingAETitle)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Sent != 1 || outcome.Failed != 0 {
		t.Fatalf("outcome = %#v, want one successful send", outcome)
	}
	snapshot := server.Snapshot()
	if snapshot.Stored != 1 {
		t.Fatalf("Snapshot.Stored = %d, want 1", snapshot.Stored)
	}
}

func TestServerRejectsUnlistedCalledAE(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	server, err := Start(ctx, Config{
		Catalog:               catalog,
		Address:               "127.0.0.1:0",
		AETitle:               testReceiverCalledAETitle,
		AllowedCalledAETitles: []string{testReceiverCalledAETitle},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, server)

	source := filepath.Join(t.TempDir(), "incoming.dcm")
	if err := os.WriteFile(source, testPart10File(t), 0o644); err != nil {
		t.Fatal(err)
	}

	node := receiverNode(t, server)
	node.AETitle = "OTHER"
	outcome, err := send.SendFiles(ctx, node, []string{source}, testReceiverCallingAETitle)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Sent != 0 || outcome.Failed != 1 {
		t.Fatalf("outcome = %#v, want one failed send", outcome)
	}
	snapshot := server.Snapshot()
	if snapshot.Rejected != 1 {
		t.Fatalf("Snapshot.Rejected = %d, want 1", snapshot.Rejected)
	}
}

func TestTransferSyntaxUIDsForPreferenceOrdersSupportedSyntaxes(t *testing.T) {
	tests := []struct {
		name       string
		preference string
		want       []string
	}{
		{
			name:       "auto keeps implicit first",
			preference: PreferredTransferSyntaxAuto,
			want:       []string{transfer.ImplicitVRLittleEndian.UID, transfer.ExplicitVRLittleEndian.UID},
		},
		{
			name:       "explicit first",
			preference: PreferredTransferSyntaxExplicitVRLittleEndian,
			want:       []string{transfer.ExplicitVRLittleEndian.UID, transfer.ImplicitVRLittleEndian.UID},
		},
		{
			name:       "implicit first",
			preference: PreferredTransferSyntaxImplicitVRLittleEndian,
			want:       []string{transfer.ImplicitVRLittleEndian.UID, transfer.ExplicitVRLittleEndian.UID},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := TransferSyntaxUIDsForPreference(test.preference)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) < len(test.want) {
				t.Fatalf("len(got) = %d, want at least %d (%#v)", len(got), len(test.want), got)
			}
			for i := range test.want {
				if got[i] != test.want[i] {
					t.Fatalf("syntax prefix = %#v, want %#v", got[:len(test.want)], test.want)
				}
			}
		})
	}
}

func TestTransferSyntaxUIDsForPreferenceRejectsUnsupportedSyntax(t *testing.T) {
	if _, err := TransferSyntaxUIDsForPreference(transfer.JPEG2000.UID); err == nil {
		t.Fatal("raw transfer syntax UID should not be accepted as a preference")
	}
}

func receiverNode(t *testing.T, server *Server) nodes.Node {
	t.Helper()
	_, portText, err := net.SplitHostPort(server.Addr())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return nodes.Node{
		Name:    "receiver",
		AETitle: server.AETitle(),
		Host:    "127.0.0.1",
		Port:    uint16(port),
	}
}

func stopServer(t *testing.T, server *Server) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func dialVerificationAssociation(ctx context.Context, server *Server) (*ul.Association, ul.AcceptedContext, error) {
	assoc, err := ul.DialContext(ctx, server.Addr(), ul.DialOptions{
		CalledAETitle:  server.AETitle(),
		CallingAETitle: testReceiverCallingAETitle,
		Contexts: []ul.PresentationContext{{
			AbstractSyntaxUID:  dimse.VerificationSOPClassUID,
			TransferSyntaxUIDs: []string{ul.ImplicitVRLittleEndian},
		}},
	})
	if err != nil {
		return nil, ul.AcceptedContext{}, err
	}
	pc, ok := dimse.AcceptedVerificationContext(assoc)
	if !ok {
		_ = assoc.Close()
		return nil, ul.AcceptedContext{}, errors.New("verification presentation context was not accepted")
	}
	return assoc, pc, nil
}

func dialStorageAssociation(ctx context.Context, server *Server) (*ul.Association, ul.AcceptedContext, error) {
	assoc, err := ul.DialContext(ctx, server.Addr(), ul.DialOptions{
		CalledAETitle:  server.AETitle(),
		CallingAETitle: testReceiverCallingAETitle,
		Contexts: []ul.PresentationContext{{
			AbstractSyntaxUID:  testStorageSOPClassUID,
			TransferSyntaxUIDs: []string{transfer.ExplicitVRLittleEndian.UID},
		}},
	})
	if err != nil {
		return nil, ul.AcceptedContext{}, err
	}
	pc, err := dimse.AcceptedContextForSOPClass(assoc, testStorageSOPClassUID)
	if err != nil {
		_ = assoc.Close()
		return nil, ul.AcceptedContext{}, err
	}
	return assoc, pc, nil
}

func sendMalformedExplicitVRDataSet(assoc *ul.Association, pcID byte) error {
	firstPDV := []byte{
		0x10, 0x00, 0x10, 0x00,
		'Z', 'Z',
		0x04, 0x00,
		'T', 'E', 'S', 'T',
	}
	if err := assoc.WritePDU(&ul.PDataTF{Values: []ul.PDataValue{{
		PresentationContextID: pcID,
		IsCommand:             false,
		IsLast:                false,
		Data:                  firstPDV,
	}}}); err != nil {
		return err
	}
	return assoc.WritePDU(&ul.PDataTF{Values: []ul.PDataValue{{
		PresentationContextID: pcID,
		IsCommand:             false,
		IsLast:                true,
		Data:                  []byte{0xde, 0xad, 0xbe, 0xef},
	}}})
}

func waitForRejectedAssociations(t *testing.T, server *Server, want int64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := server.Snapshot().Rejected; got >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Snapshot.Rejected = %d, want at least %d", server.Snapshot().Rejected, want)
}

func waitForAssociationHandlers(t *testing.T, server *Server) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		server.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for association handlers")
	}
}

func testReceiveServerTLSConfig(t *testing.T) *tls.Config {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("rand.Int() error = %v", err)
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey() error = %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair() error = %v", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}}
}

func testPart10File(t *testing.T) []byte {
	t.Helper()
	dataset := []core.Element{
		testutil.StringElement(core.NewTag(0x0008, 0x0016), core.VRUI, testStorageSOPClassUID),
		testutil.StringElement(core.NewTag(0x0008, 0x0018), core.VRUI, testSOPInstanceUID),
		testutil.StringElement(core.NewTag(0x0010, 0x0010), core.VRPN, "RECEIVE^PATIENT"),
		testutil.StringElement(core.NewTag(0x0010, 0x0020), core.VRLO, "R001"),
		testutil.StringElement(core.NewTag(0x0008, 0x0020), core.VRDA, "20260604"),
		testutil.StringElement(core.NewTag(0x0008, 0x0060), core.VRCS, "CT"),
		testutil.StringElement(core.NewTag(0x0020, 0x000D), core.VRUI, testStudyInstanceUID),
		testutil.StringElement(core.NewTag(0x0020, 0x000E), core.VRUI, testSeriesInstanceUID),
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

func testCompressedPart10File(t *testing.T) ([]byte, []byte) {
	t.Helper()
	tc := codecfixture.JPEGLosslessSmall()
	replaced := map[core.Tag]bool{
		core.NewTag(0x0008, 0x0016): true,
		core.NewTag(0x0008, 0x0018): true,
		core.NewTag(0x0010, 0x0010): true,
		core.NewTag(0x0010, 0x0020): true,
		core.NewTag(0x0008, 0x0020): true,
		core.NewTag(0x0008, 0x0060): true,
		core.NewTag(0x0020, 0x000D): true,
		core.NewTag(0x0020, 0x000E): true,
	}
	elements := make([]core.Element, 0, len(tc.Elements)+len(replaced))
	for _, element := range tc.Elements {
		if !replaced[element.Tag()] {
			elements = append(elements, element)
		}
	}
	elements = append(elements,
		testutil.StringElement(core.NewTag(0x0008, 0x0016), core.VRUI, testStorageSOPClassUID),
		testutil.StringElement(core.NewTag(0x0008, 0x0018), core.VRUI, testSOPInstanceUID),
		testutil.StringElement(core.NewTag(0x0010, 0x0010), core.VRPN, "RECEIVE^COMPRESSED"),
		testutil.StringElement(core.NewTag(0x0010, 0x0020), core.VRLO, "RC001"),
		testutil.StringElement(core.NewTag(0x0008, 0x0020), core.VRDA, "20260604"),
		testutil.StringElement(core.NewTag(0x0008, 0x0060), core.VRCS, "CT"),
		testutil.StringElement(core.NewTag(0x0020, 0x000D), core.VRUI, testStudyInstanceUID),
		testutil.StringElement(core.NewTag(0x0020, 0x000E), core.VRUI, testSeriesInstanceUID),
	)
	file := &object.File{
		Dataset:        object.FromElements(elements, std.Dictionary),
		TransferSyntax: tc.Syntax,
	}
	var buf bytes.Buffer
	if err := object.WriteFile(&buf, file); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), tc.ExpectedFrames[0]
}
