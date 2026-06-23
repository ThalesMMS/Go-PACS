package core

import (
	"path/filepath"
	"testing"

	"github.com/ThalesMMS/Go-PACS/internal/appconfig"
	"github.com/ThalesMMS/Go-PACS/internal/autoquery"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	ops "github.com/ThalesMMS/Go-PACS/internal/operations"
)

func openTestSession(t *testing.T) *Session {
	t.Helper()
	dir := t.TempDir()
	sess, err := Open(dir)
	if err != nil {
		t.Fatalf("Open(%q) failed: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := sess.Close(); err != nil {
			t.Errorf("Close failed: %v", err)
		}
	})
	return sess
}

func TestOpenPreparesStoresAndPaths(t *testing.T) {
	dir := t.TempDir()
	sess, err := Open(dir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer sess.Close()

	if sess.Catalog() == nil {
		t.Fatal("Catalog() is nil")
	}
	if sess.NodeStore() == nil {
		t.Fatal("NodeStore() is nil")
	}
	if sess.AutoQueryStore() == nil {
		t.Fatal("AutoQueryStore() is nil")
	}
	if got, want := sess.ArchiveDir(), dir; got != want {
		t.Errorf("ArchiveDir() = %q, want %q", got, want)
	}
	if got, want := sess.ConfigPath(), filepath.Join(dir, configFileName); got != want {
		t.Errorf("ConfigPath() = %q, want %q", got, want)
	}
	if got, want := sess.HistoryPath(), filepath.Join(dir, historyFileName); got != want {
		t.Errorf("HistoryPath() = %q, want %q", got, want)
	}
}

func TestCloseOnNilSessionIsSafe(t *testing.T) {
	var sess *Session
	if err := sess.Close(); err != nil {
		t.Fatalf("Close on nil Session = %v, want nil", err)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	sess := openTestSession(t)

	cfg := appconfig.Defaults()
	cfg.LocalAETitle = "GOPACSTEST"
	if err := sess.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}
	got, err := sess.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if got.LocalAETitle != "GOPACSTEST" {
		t.Errorf("LocalAETitle = %q, want %q", got.LocalAETitle, "GOPACSTEST")
	}
}

func TestHistoryRoundTrip(t *testing.T) {
	sess := openTestSession(t)

	if got, err := sess.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory on empty session failed: %v", err)
	} else if len(got) != 0 {
		t.Fatalf("LoadHistory on empty session = %d entries, want 0", len(got))
	}

	history := []ops.Summary{{Kind: ops.KindImport, Status: ops.StatusSuccess}}
	if err := sess.SaveHistory(history); err != nil {
		t.Fatalf("SaveHistory failed: %v", err)
	}
	got, err := sess.LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory failed: %v", err)
	}
	if len(got) != 1 || got[0].Kind != ops.KindImport {
		t.Errorf("LoadHistory = %+v, want single import summary", got)
	}
}

func TestNodesRoundTrip(t *testing.T) {
	sess := openTestSession(t)

	node, err := nodes.NewNode(nodes.Draft{
		Name:    "TEST-NODE",
		AETitle: "TESTAE",
		Host:    "127.0.0.1",
		Port:    11112,
	})
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	if err := sess.SaveNodes([]nodes.Node{node}); err != nil {
		t.Fatalf("SaveNodes failed: %v", err)
	}
	got, err := sess.ListNodes()
	if err != nil {
		t.Fatalf("ListNodes failed: %v", err)
	}
	if len(got) != 1 || got[0].AETitle != "TESTAE" {
		t.Errorf("ListNodes = %+v, want single node with AETitle TESTAE", got)
	}
}

func TestAutoQueryProfilesRoundTrip(t *testing.T) {
	sess := openTestSession(t)

	profile := autoquery.DefaultProfile()
	profile.Name = "Nightly"
	if err := sess.SaveAutoQueryProfiles([]autoquery.Profile{profile}); err != nil {
		t.Fatalf("SaveAutoQueryProfiles failed: %v", err)
	}
	got, err := sess.ListAutoQueryProfiles()
	if err != nil {
		t.Fatalf("ListAutoQueryProfiles failed: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Nightly" {
		t.Errorf("ListAutoQueryProfiles = %+v, want single profile named Nightly", got)
	}
}

func TestDefaultArchiveDirIsStable(t *testing.T) {
	if a, b := DefaultArchiveDir(), DefaultArchiveDir(); a != b {
		t.Errorf("DefaultArchiveDir not stable: %q vs %q", a, b)
	}
	if DefaultArchiveDir() == "" {
		t.Error("DefaultArchiveDir returned empty string")
	}
}
