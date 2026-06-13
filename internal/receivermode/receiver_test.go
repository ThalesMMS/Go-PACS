package receivermode

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ThalesMMS/Go-PACS/internal/appconfig"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
)

func TestPlanFromArchiveDirLoadsConfigAndNodeAllowlists(t *testing.T) {
	archiveDir := t.TempDir()
	maxStoreObjectBytes := int64(789)
	if err := appconfig.Save(filepath.Join(archiveDir, "config.json"), appconfig.Config{
		LocalAETitle:        "local",
		ReceiverAddress:     "127.0.0.1:12345",
		AdditionalAETitles:  []string{"alias"},
		MaxStoreObjectBytes: &maxStoreObjectBytes,
	}); err != nil {
		t.Fatal(err)
	}
	store := nodes.NewStore(filepath.Join(archiveDir, "nodes.json"))
	if _, err := store.Add(nodes.Draft{Name: "pacs", AETitle: "remote", Host: "192.0.2.1", Port: 104}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add(nodes.Draft{Name: "hosted", AETitle: "remote2", Host: "localhost", Port: 104}); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanFromArchiveDir(Options{ArchiveDir: archiveDir})
	if err != nil {
		t.Fatal(err)
	}
	if plan.AETitle != "LOCAL" {
		t.Fatalf("AETitle = %q, want LOCAL", plan.AETitle)
	}
	if plan.Address != "127.0.0.1:12345" {
		t.Fatalf("Address = %q, want 127.0.0.1:12345", plan.Address)
	}
	if got, want := plan.AllowedCalledAETitles, []string{"ALIAS"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("AllowedCalledAETitles = %#v, want %#v", got, want)
	}
	if plan.MaxStoreObjectBytes != 789 {
		t.Fatalf("MaxStoreObjectBytes = %d, want 789", plan.MaxStoreObjectBytes)
	}
	if got, want := len(plan.AllowedCallingAETitles), 2; got != want {
		t.Fatalf("len(AllowedCallingAETitles) = %d, want %d", got, want)
	}
	if !containsString(plan.AllowedRemoteHosts, "192.0.2.1") {
		t.Fatalf("AllowedRemoteHosts = %#v, want literal IP", plan.AllowedRemoteHosts)
	}
	if !containsString(plan.AllowedRemoteHosts, "127.0.0.1") {
		t.Fatalf("AllowedRemoteHosts = %#v, want resolved localhost IPv4", plan.AllowedRemoteHosts)
	}
}

func TestPlanFromArchiveDirSkipsUnresolvedNodeHost(t *testing.T) {
	archiveDir := t.TempDir()
	store := nodes.NewStore(filepath.Join(archiveDir, "nodes.json"))
	if _, err := store.Add(nodes.Draft{Name: "hosted", AETitle: "remote", Host: "missing.invalid", Port: 104}); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanFromArchiveDir(Options{ArchiveDir: archiveDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.AllowedRemoteHosts) != 0 {
		t.Fatalf("AllowedRemoteHosts = %#v, want none for unresolved host", plan.AllowedRemoteHosts)
	}
	if len(plan.AllowlistWarnings) != 1 {
		t.Fatalf("AllowlistWarnings = %#v, want one warning", plan.AllowlistWarnings)
	}
	if !strings.Contains(plan.AllowlistWarnings[0], "missing.invalid") {
		t.Fatalf("AllowlistWarnings[0] = %q, want unresolved hostname context", plan.AllowlistWarnings[0])
	}
}

func TestPlanFromArchiveDirAppliesOverrides(t *testing.T) {
	archiveDir := t.TempDir()

	plan, err := PlanFromArchiveDir(Options{
		ArchiveDir:      archiveDir,
		AddressOverride: "127.0.0.1:22222",
		AETitleOverride: "serve",
		NoAllowlist:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.AETitle != "SERVE" {
		t.Fatalf("AETitle = %q, want SERVE", plan.AETitle)
	}
	if plan.Address != "127.0.0.1:22222" {
		t.Fatalf("Address = %q, want 127.0.0.1:22222", plan.Address)
	}
	if len(plan.AllowedCallingAETitles) != 0 || len(plan.AllowedRemoteHosts) != 0 {
		t.Fatalf("allowlists = %#v/%#v, want empty", plan.AllowedCallingAETitles, plan.AllowedRemoteHosts)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
