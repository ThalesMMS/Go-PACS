package autoquery

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestStoreListMissingFileReturnsDefaultProfile(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "auto-query-profiles.json"))

	profiles, err := store.List()
	if err != nil {
		t.Fatalf("List missing file: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("len(profiles) = %d, want 1", len(profiles))
	}
	if profiles[0].Name != DefaultProfileName {
		t.Fatalf("profile name = %q, want %q", profiles[0].Name, DefaultProfileName)
	}
	if !profiles[0].Settings.RequireConfirmation {
		t.Fatal("default profile should require confirmation")
	}
}

func TestStoreSaveListRoundTrip(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "auto-query-profiles.json"))
	profiles := []Profile{{
		Name:   DefaultProfileName,
		Locked: true,
		Settings: Settings{
			RetrieveLevel:       "Series",
			MaxMatches:          "10",
			DuplicatePolicy:     "Keep duplicate",
			RequireConfirmation: false,
		},
		Criteria: Criteria{
			SearchField: "Patient ID",
			SearchText:  "P123",
			DatePreset:  "Today",
			Modalities:  []string{"CT", "MR"},
			RefreshMode: "Refresh every 30 min",
		},
		Sources: []Source{
			{NodeID: "node-2", Name: "HOROS", Host: "10.0.0.2", Port: 105, Enabled: true},
			{NodeID: "node-1", Name: "RADIANT", Host: "10.0.0.1", Port: 104, Enabled: false},
		},
	}}

	if err := store.Save(profiles); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("len(loaded) = %d, want 1", len(loaded))
	}
	if !reflect.DeepEqual(loaded[0], profiles[0]) {
		t.Fatalf("loaded profile = %#v, want %#v", loaded[0], profiles[0])
	}
}
