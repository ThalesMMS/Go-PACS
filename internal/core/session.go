// Package core is the frontend-agnostic application layer for go-pacs.
//
// It owns the application's on-disk stores and the archive catalog and exposes
// them through a small, UI-free API so that any frontend — the Fyne desktop
// GUI, a future alternative frontend, or the headless receiver — can be built
// on top of the same backend without duplicating lifecycle, persistence, or
// (over time) workflow orchestration logic. No package under internal/ imports
// a UI toolkit; core keeps it that way and is the seam a second frontend plugs
// into.
package core

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/appconfig"
	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/autoquery"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	ops "github.com/ThalesMMS/Go-PACS/internal/operations"
	"github.com/ThalesMMS/Go-PACS/internal/receive"
	"github.com/ThalesMMS/Go-PACS/internal/tokens"
)

// File names for the per-archive stores, kept in one place so every frontend
// resolves the same paths under a given archive directory.
const (
	configFileName            = "config.json"
	historyFileName           = "tasks.json"
	nodesFileName             = "nodes.json"
	autoQueryProfilesFileName = "auto-query-profiles.json"
	tokensFileName            = "tokens.json"
)

// Session is an open application session. It holds the archive catalog plus the
// stores for configuration, DICOM nodes, auto-query profiles, and operation
// history. A Session carries no view or widget state, so the same Session API
// backs every frontend.
//
// Working data (the in-memory node list, the operation history slice, the live
// config) is intentionally not cached on the Session: callers own their working
// copies and round-trip them through the Load*/Save* helpers. That keeps the
// Session free of staleness concerns and makes the persistence boundary
// explicit.
type Session struct {
	archiveDir  string
	catalog     *archive.Catalog
	nodeStore   *nodes.Store
	autoQuery   *autoquery.Store
	tokenStore  *tokens.Store
	configPath  string
	historyPath string

	// Receiver lifecycle. The running DICOM listener is process-scoped
	// state shared by every frontend, guarded by receiverMu. See receiver.go.
	receiverMu        sync.Mutex
	receiver          *receive.Server
	receiverStartedAt time.Time
	receiverWarnings  []string

	// jobs is the registry of asynchronous operations (retrieve/send/import)
	// streamed to frontends. See jobs.go.
	jobs *jobManager

	// Auto-query scheduler (process-scoped, one at a time). See
	// autoquery_scheduler.go.
	autoQueryMu     sync.Mutex
	autoQueryCancel context.CancelFunc
	autoQueryStatus AutoQueryStatus
}

// Open opens (creating it if necessary) the archive catalog under archiveDir
// and prepares the configuration, node, auto-query, and history stores. The
// Open opens an archive at the given directory and returns a new Session with all stores and configuration paths initialized. It returns an error if the catalog cannot be opened.
func Open(archiveDir string) (*Session, error) {
	catalog, err := archive.Open(archiveDir)
	if err != nil {
		return nil, err
	}
	return &Session{
		archiveDir:  archiveDir,
		catalog:     catalog,
		nodeStore:   nodes.NewStore(filepath.Join(archiveDir, nodesFileName)),
		autoQuery:   autoquery.NewStore(filepath.Join(archiveDir, autoQueryProfilesFileName)),
		tokenStore:  tokens.NewStore(filepath.Join(archiveDir, tokensFileName)),
		configPath:  filepath.Join(archiveDir, configFileName),
		historyPath: filepath.Join(archiveDir, historyFileName),
		jobs:        newJobManager(),
	}, nil
}

// Close stops the receiver if running and releases the archive catalog. The
// Session must not be used afterward. Close is safe to call on a nil Session.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.autoQueryMu.Lock()
	if s.autoQueryCancel != nil {
		s.autoQueryCancel()
		s.autoQueryCancel = nil
	}
	s.autoQueryMu.Unlock()
	s.receiverMu.Lock()
	if s.receiver != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = s.receiver.Stop(ctx)
		cancel()
		s.receiver = nil
	}
	s.receiverMu.Unlock()
	if s.catalog == nil {
		return nil
	}
	return s.catalog.Close()
}

// ArchiveDir reports the root directory backing this session.
func (s *Session) ArchiveDir() string { return s.archiveDir }

// Catalog returns the archive catalog handle.
func (s *Session) Catalog() *archive.Catalog { return s.catalog }

// NodeStore returns the DICOM node store.
func (s *Session) NodeStore() *nodes.Store { return s.nodeStore }

// AutoQueryStore returns the auto-query profile store.
func (s *Session) AutoQueryStore() *autoquery.Store { return s.autoQuery }

// TokenStore returns the inbound DICOMweb service-token store.
func (s *Session) TokenStore() *tokens.Store { return s.tokenStore }

// ConfigPath reports the on-disk location of the configuration file.
func (s *Session) ConfigPath() string { return s.configPath }

// HistoryPath reports the on-disk location of the operation history file.
func (s *Session) HistoryPath() string { return s.historyPath }

// --- configuration ---

// LoadConfig reads and normalizes the persisted configuration.
func (s *Session) LoadConfig() (appconfig.Config, error) {
	return appconfig.Load(s.configPath)
}

// SaveConfig persists cfg to the configuration file.
func (s *Session) SaveConfig(cfg appconfig.Config) error {
	return appconfig.Save(s.configPath, cfg)
}

func (s *Session) RecoverConfigFromBackup() error {
	return appconfig.RecoverFromBackup(s.configPath)
}

// --- operation history ---

// LoadHistory reads the persisted operation history.
func (s *Session) LoadHistory() ([]ops.Summary, error) {
	return ops.LoadHistory(s.historyPath)
}

// SaveHistory persists the operation history.
func (s *Session) SaveHistory(history []ops.Summary) error {
	return ops.SaveHistory(s.historyPath, history)
}

func (s *Session) RecoverHistoryFromBackup() error {
	return ops.RecoverFromBackup(s.historyPath)
}

// --- DICOM nodes ---

// ListNodes returns the configured DICOM nodes.
func (s *Session) ListNodes() ([]nodes.Node, error) {
	return s.nodeStore.List()
}

// SaveNodes persists the supplied node list.
func (s *Session) SaveNodes(list []nodes.Node) error {
	return s.nodeStore.Save(list)
}

func (s *Session) RecoverNodesFromBackup() error {
	return s.nodeStore.RecoverFromBackup()
}

// AddNode validates and persists a new DICOM node.
func (s *Session) AddNode(draft nodes.Draft) (nodes.Node, error) {
	return s.nodeStore.Add(draft)
}

// UpdateNode validates and persists changes to the node with the given id.
func (s *Session) UpdateNode(id string, draft nodes.Draft) (nodes.Node, error) {
	return s.nodeStore.Update(id, draft)
}

// DeleteNode removes the node with the given id.
func (s *Session) DeleteNode(id string) error {
	return s.nodeStore.Delete(id)
}

// --- auto-query profiles ---

// ListAutoQueryProfiles returns the persisted auto-query profiles.
func (s *Session) ListAutoQueryProfiles() ([]autoquery.Profile, error) {
	return s.autoQuery.List()
}

// SaveAutoQueryProfiles persists the supplied auto-query profiles.
func (s *Session) SaveAutoQueryProfiles(profiles []autoquery.Profile) error {
	return s.autoQuery.Save(profiles)
}

func (s *Session) RecoverAutoQueryProfilesFromBackup() error {
	return s.autoQuery.RecoverFromBackup()
}

// DefaultArchiveDir returns the platform-appropriate default location for the
// DefaultArchiveDir returns the default archive directory path, preferring the user's system config directory with a fallback to ./.go-pacs.
func DefaultArchiveDir() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return filepath.Join(".", ".go-pacs")
	}
	return filepath.Join(dir, "go-pacs")
}
