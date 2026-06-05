package receive

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/netverify"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/send"
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

func testPart10File(t *testing.T) []byte {
	t.Helper()
	dataset := []core.Element{
		stringElement(core.NewTag(0x0008, 0x0016), core.VRUI, testStorageSOPClassUID),
		stringElement(core.NewTag(0x0008, 0x0018), core.VRUI, testSOPInstanceUID),
		stringElement(core.NewTag(0x0010, 0x0010), core.VRPN, "RECEIVE^PATIENT"),
		stringElement(core.NewTag(0x0010, 0x0020), core.VRLO, "R001"),
		stringElement(core.NewTag(0x0008, 0x0020), core.VRDA, "20260604"),
		stringElement(core.NewTag(0x0008, 0x0060), core.VRCS, "CT"),
		stringElement(core.NewTag(0x0020, 0x000D), core.VRUI, testStudyInstanceUID),
		stringElement(core.NewTag(0x0020, 0x000E), core.VRUI, testSeriesInstanceUID),
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

func stringElement(tag core.Tag, vr core.VR, value string) core.Element {
	return core.Element{
		Header: core.ElementHeader{Tag: tag, VR: vr},
		Value:  core.StringValue{value},
	}
}
