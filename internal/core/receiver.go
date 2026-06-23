package core

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/appconfig"
	"github.com/ThalesMMS/Go-PACS/internal/netverify"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	ops "github.com/ThalesMMS/Go-PACS/internal/operations"
	"github.com/ThalesMMS/Go-PACS/internal/receive"
)

// Errors returned by the receiver lifecycle methods.
var (
	ErrReceiverRunning    = errors.New("receiver is already running")
	ErrReceiverNotRunning = errors.New("receiver is not running")
)

// ReceiverStatus describes the DICOM listener for a frontend to render.
// When stopped, Address/AETitle reflect the configured (effective) identity.
type ReceiverStatus struct {
	Running   bool              `json:"running"`
	Address   string            `json:"address"`
	AETitle   string            `json:"aeTitle"`
	StartedAt time.Time         `json:"startedAt,omitempty"`
	Snapshot  *receive.Snapshot `json:"snapshot,omitempty"`
	Warnings  []string          `json:"warnings,omitempty"`
}

// ReceiverStatus reports the current listener state and live counters.
func (s *Session) ReceiverStatus() ReceiverStatus {
	s.receiverMu.Lock()
	defer s.receiverMu.Unlock()
	return s.receiverStatusLocked()
}

// StartReceiver assembles the listener configuration from the current config and
// nodes (Called/Calling-AE and remote-host allowlists, optional TLS) and starts
// the DICOM listener. The listener is process-scoped and outlives the calling
// request, so it is started with a background context, not ctx. Returns
// ErrReceiverRunning if already started.
func (s *Session) StartReceiver(ctx context.Context) (ReceiverStatus, error) {
	s.receiverMu.Lock()
	defer s.receiverMu.Unlock()
	if s.receiver != nil {
		return s.receiverStatusLocked(), ErrReceiverRunning
	}
	cfg, err := s.LoadConfig()
	if err != nil {
		return ReceiverStatus{}, err
	}
	nodeList, err := s.ListNodes()
	if err != nil {
		return ReceiverStatus{}, err
	}
	rcfg, warnings, err := s.buildReceiveConfig(cfg, nodeList)
	if err != nil {
		return ReceiverStatus{}, err
	}
	server, err := receive.Start(context.Background(), rcfg)
	if err != nil {
		return ReceiverStatus{}, err
	}
	s.receiver = server
	s.receiverStartedAt = time.Now()
	s.receiverWarnings = warnings
	return s.receiverStatusLocked(), nil
}

// StopReceiver stops the listener, records a receiver run summary in the
// operation history, and returns the final status. Returns ErrReceiverNotRunning
// if the listener is not running.
func (s *Session) StopReceiver(ctx context.Context) (ReceiverStatus, error) {
	s.receiverMu.Lock()
	defer s.receiverMu.Unlock()
	if s.receiver == nil {
		return s.receiverStatusLocked(), ErrReceiverNotRunning
	}
	snapshot := s.receiver.Snapshot()
	duration := time.Since(s.receiverStartedAt)
	stopErr := s.receiver.Stop(ctx)
	s.receiver = nil
	s.receiverWarnings = nil

	summary := ops.ReceiverSummary(snapshot, duration)
	if history, err := s.LoadHistory(); err == nil {
		_ = s.SaveHistory(ops.Prepend(history, summary))
	}
	return s.receiverStatusLocked(), stopErr
}

// RestartReceiver stops (if running) then starts the listener so that changed
// listener settings take effect. Mirrors the Fyne "restart to apply" guidance.
func (s *Session) RestartReceiver(ctx context.Context) (ReceiverStatus, error) {
	if s.ReceiverStatus().Running {
		if _, err := s.StopReceiver(ctx); err != nil {
			return ReceiverStatus{}, err
		}
	}
	return s.StartReceiver(ctx)
}

// receiverStatusLocked must be called with receiverMu held.
func (s *Session) receiverStatusLocked() ReceiverStatus {
	if s.receiver != nil {
		snapshot := s.receiver.Snapshot()
		return ReceiverStatus{
			Running:   true,
			Address:   s.receiver.Addr(),
			AETitle:   s.receiver.AETitle(),
			StartedAt: s.receiverStartedAt,
			Snapshot:  &snapshot,
			Warnings:  s.receiverWarnings,
		}
	}
	cfg, _ := s.LoadConfig()
	return ReceiverStatus{
		Running: false,
		Address: cfg.ReceiverAddress,
		AETitle: effectiveAETitle(cfg.LocalAETitle),
	}
}

func (s *Session) buildReceiveConfig(cfg appconfig.Config, nodeList []nodes.Node) (receive.Config, []string, error) {
	rcfg := receive.Config{
		Catalog:                 s.catalog,
		Address:                 cfg.ReceiverAddress,
		AETitle:                 cfg.LocalAETitle,
		AllowedCalledAETitles:   append([]string(nil), cfg.AdditionalAETitles...),
		AllowedCallingAETitles:  nodes.CallingAETitles(nodeList),
		PreferredTransferSyntax: cfg.ReceivePreferredTransferSyntax,
		DecompressImages:        cfg.ReceiveDecompressImages,
		NodeLookup: func(aeTitle string) (nodes.Node, bool) {
			current, err := s.nodeStore.List()
			if err != nil {
				return nodes.Node{}, false
			}
			return nodes.FindByAETitle(current, aeTitle)
		},
	}
	if cfg.MaxStoreObjectBytes != nil {
		rcfg.MaxStoreObjectBytes = *cfg.MaxStoreObjectBytes
	}
	allow := nodes.RemoteHostAllowlist(nodeList)
	rcfg.AllowedRemoteHosts = allow.Hosts

	if cfg.ReceiverUseTLS {
		tlsConfig, err := receiverTLSConfig(cfg)
		if err != nil {
			return receive.Config{}, nil, err
		}
		rcfg.TLSConfig = tlsConfig
	}
	return rcfg, allow.Warnings, nil
}

func receiverTLSConfig(cfg appconfig.Config) (*tls.Config, error) {
	if cfg.ReceiverTLSCertFile == "" || cfg.ReceiverTLSKeyFile == "" {
		return nil, fmt.Errorf("receiver TLS requires both a certificate and key file")
	}
	cert, err := tls.LoadX509KeyPair(cfg.ReceiverTLSCertFile, cfg.ReceiverTLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load receiver TLS key pair: %w", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
}

// effectiveAETitle returns the configured AE title or the default fallback used
// across the app when none is set.
func effectiveAETitle(aeTitle string) string {
	if aeTitle == "" {
		return netverify.DefaultCallingAETitle
	}
	return aeTitle
}
