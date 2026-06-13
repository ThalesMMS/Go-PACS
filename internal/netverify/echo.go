package netverify

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/nettimeout"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/net/ul"
)

const (
	DefaultCallingAETitle = "GOPACS"
	DefaultDialTimeout    = 10 * time.Second
	DefaultReleaseTimeout = 5 * time.Second
)

type EchoResult struct {
	NodeName  string
	Address   string
	Status    uint16
	Duration  time.Duration
	StartedAt time.Time
}

func Echo(ctx context.Context, node nodes.Node, callingAETitle string) (EchoResult, error) {
	if callingAETitle == "" {
		callingAETitle = DefaultCallingAETitle
	}
	callingAETitle = nodes.NormalizeAETitle(callingAETitle)
	if err := nodes.ValidateAETitle(callingAETitle); err != nil {
		return EchoResult{}, fmt.Errorf("calling AE title: %w", err)
	}
	address := net.JoinHostPort(node.Host, strconv.Itoa(int(node.Port)))
	started := time.Now()
	tlsConfig, err := TLSConfigForNode(node)
	if err != nil {
		return EchoResult{}, err
	}

	dialCtx, cancelDial := nettimeout.WithDefault(ctx, DefaultDialTimeout)
	defer cancelDial()
	assoc, err := ul.DialContext(dialCtx, address, ul.DialOptions{
		CalledAETitle:  node.AETitle,
		CallingAETitle: callingAETitle,
		Contexts: []ul.PresentationContext{{
			AbstractSyntaxUID:  dimse.VerificationSOPClassUID,
			TransferSyntaxUIDs: []string{ul.ImplicitVRLittleEndian},
		}},
		TLSConfig: tlsConfig,
	})
	if err != nil {
		return EchoResult{}, fmt.Errorf("associate with %s (%s): %w", node.Name, address, err)
	}
	released := false
	defer func() {
		if !released {
			_ = assoc.Close()
		}
	}()

	pc, ok := dimse.AcceptedVerificationContext(assoc)
	if !ok {
		return EchoResult{}, fmt.Errorf("verification presentation context was not accepted by %s", node.Name)
	}
	response, err := dimse.SendCEcho(assoc, pc.ID, 1)
	if err != nil {
		return EchoResult{}, fmt.Errorf("send C-ECHO to %s (%s): %w", node.Name, address, err)
	}

	releaseCtx, cancelRelease := context.WithTimeout(ctx, DefaultReleaseTimeout)
	defer cancelRelease()
	if err := assoc.Release(releaseCtx); err != nil {
		return EchoResult{}, fmt.Errorf("release association with %s (%s): %w", node.Name, address, err)
	}
	released = true

	return EchoResult{
		NodeName:  node.Name,
		Address:   address,
		Status:    response.Status,
		Duration:  time.Since(started),
		StartedAt: started.UTC(),
	}, nil
}

func TLSConfigForNode(node nodes.Node) (*tls.Config, error) {
	if !node.UseTLS {
		return nil, nil
	}
	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: node.TLSSkipVerify,
		ServerName:         strings.TrimSpace(node.TLSServerName),
	}
	if caFile := strings.TrimSpace(node.TLSCAFile); caFile != "" {
		caPEM, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read TLS CA file: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("TLS CA file %q does not contain a PEM certificate", caFile)
		}
		cfg.RootCAs = roots
	}
	certFile := strings.TrimSpace(node.TLSCertFile)
	keyFile := strings.TrimSpace(node.TLSKeyFile)
	if (certFile == "") != (keyFile == "") {
		return nil, fmt.Errorf("TLS client certificate and key must be provided together")
	}
	if certFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load TLS client certificate: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}
