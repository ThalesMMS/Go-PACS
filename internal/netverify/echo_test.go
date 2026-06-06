package netverify

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/net/ul"
)

func TestEchoAgainstLocalVerificationSCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	listener, err := ul.Listen(ul.ListenOptions{Address: "127.0.0.1:0", Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	done := make(chan error, 1)
	go func() {
		assoc, err := listener.AcceptAssociation(ul.AcceptOptions{
			AETitle:                   "ECHOSCP",
			Context:                   ctx,
			SupportedAbstractSyntaxes: []string{dimse.VerificationSOPClassUID},
			SupportedTransferSyntaxes: []string{ul.ImplicitVRLittleEndian},
		})
		if err != nil {
			done <- err
			return
		}
		if err := dimse.HandleCEcho(assoc); err != nil {
			done <- err
			return
		}
		pdu, err := assoc.ReadPDU()
		if err != nil {
			done <- err
			return
		}
		if _, ok := pdu.(*ul.ReleaseRQ); !ok {
			done <- fmt.Errorf("got %T while waiting for A-RELEASE-RQ", pdu)
			return
		}
		done <- assoc.WritePDU(&ul.ReleaseRP{})
	}()

	node := nodes.Node{
		Name:    "test",
		AETitle: "ECHOSCP",
		Host:    "127.0.0.1",
		Port:    uint16(listener.Addr().(*net.TCPAddr).Port),
	}
	result, err := Echo(ctx, node, "ECHOSCU")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != dimse.StatusSuccess {
		t.Fatalf("Status = 0x%04X, want success", result.Status)
	}
	if err := <-done; err != nil {
		t.Fatalf("server error = %v", err)
	}
}
