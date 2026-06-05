package receivermode

import (
	"path/filepath"
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
	if _, err := store.Add(nodes.Draft{Name: "pacs", AETitle: "remote", Host: "127.0.0.1", Port: 104}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add(nodes.Draft{Name: "hosted", AETitle: "remote2", Host: "pacs.example.test", Port: 104}); err != nil {
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
	if got, want := plan.AllowedRemoteHosts, []string{"127.0.0.1"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("AllowedRemoteHosts = %#v, want %#v", got, want)
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
