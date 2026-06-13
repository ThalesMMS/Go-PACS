package netverify

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
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

func TestEchoUsesNodeTLSConfig(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	listener, err := ul.Listen(ul.ListenOptions{Address: "127.0.0.1:0", Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverTLS := testServerTLSConfig(t)
	done := make(chan error, 1)
	go func() {
		assoc, err := listener.AcceptAssociation(ul.AcceptOptions{
			AETitle:                   "ECHOSCP",
			Context:                   ctx,
			SupportedAbstractSyntaxes: []string{dimse.VerificationSOPClassUID},
			SupportedTransferSyntaxes: []string{ul.ImplicitVRLittleEndian},
			TLSConfig:                 serverTLS,
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
		Name:          "test",
		AETitle:       "ECHOSCP",
		Host:          "127.0.0.1",
		Port:          uint16(listener.Addr().(*net.TCPAddr).Port),
		UseTLS:        true,
		TLSSkipVerify: true,
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

func testServerTLSConfig(t *testing.T) *tls.Config {
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
