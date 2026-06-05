package operations

import (
	"path/filepath"
	"testing"
)

func TestLoadHistoryMissingFileReturnsEmpty(t *testing.T) {
	history, err := LoadHistory(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 {
		t.Fatalf("len(history) = %d, want 0", len(history))
	}
}

func TestSaveAndLoadHistoryRoundTripCapsEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	var history []Summary
	for i := 0; i < MaxHistoryEntries+2; i++ {
		history = append(history, Summary{
			Version:    SummaryVersion,
			Kind:       KindImport,
			Status:     StatusSuccess,
			DurationMS: uint64(i),
		})
	}

	if err := SaveHistory(path, history); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != MaxHistoryEntries {
		t.Fatalf("len(loaded) = %d, want %d", len(loaded), MaxHistoryEntries)
	}
	if loaded[0].DurationMS != 0 {
		t.Fatalf("first DurationMS = %d, want 0", loaded[0].DurationMS)
	}
	if loaded[len(loaded)-1].DurationMS != uint64(MaxHistoryEntries-1) {
		t.Fatalf("last DurationMS = %d, want %d", loaded[len(loaded)-1].DurationMS, MaxHistoryEntries-1)
	}
}
