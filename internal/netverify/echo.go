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

	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/dicom-go/net/dimse"
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

// Echo performs a DIMSE C-ECHO verification to a target node.
// If callingAETitle is empty, DefaultCallingAETitle is used.
// It returns an EchoResult containing the verification outcome and any error encountered.
func Echo(ctx context.Context, node nodes.Node, callingAETitle string) (EchoResult, error) {
	if callingAETitle == "" {
		callingAETitle = DefaultCallingAETitle
	}
	callingAETitle = nodes.NormalizeAETitle(callingAETitle)
	if err := nodes.ValidateAETitle(callingAETitle); err != nil {
		return EchoResult{}, fmt.Errorf("calling AE title: %w", err)
	}
	address := net.JoinHostPort(node.Host, strconv.Itoa(int(node.Port)))
	tlsConfig, err := TLSConfigForNode(node)
	if err != nil {
		return EchoResult{}, err
	}

	result, err := dimse.VerifyCEcho(ctx, dimse.CEchoVerificationOptions{
		Address:        address,
		CalledAETitle:  node.AETitle,
		CallingAETitle: callingAETitle,
		TLSConfig:      tlsConfig,
		DialTimeout:    DefaultDialTimeout,
		ReleaseTimeout: DefaultReleaseTimeout,
		MessageID:      1,
	})
	if err != nil {
		return EchoResult{}, fmt.Errorf("verify C-ECHO with %s (%s): %w", node.Name, address, err)
	}

	return EchoResult{
		NodeName:  node.Name,
		Address:   address,
		Status:    result.Status,
		Duration:  result.Duration,
		StartedAt: result.StartedAt,
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
