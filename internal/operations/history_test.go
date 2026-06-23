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

func TestPrependInsertsNewestFirstAndCaps(t *testing.T) {
	var history []Summary
	for i := 0; i < MaxHistoryEntries; i++ {
		history = append(history, Summary{DurationMS: uint64(i)})
	}
	original := append([]Summary(nil), history...)

	got := Prepend(history, Summary{DurationMS: 9999})

	if len(got) != MaxHistoryEntries {
		t.Fatalf("len after Prepend = %d, want %d", len(got), MaxHistoryEntries)
	}
	if got[0].DurationMS != 9999 {
		t.Fatalf("newest entry DurationMS = %d, want 9999", got[0].DurationMS)
	}
	if len(history) != MaxHistoryEntries || history[0].DurationMS != original[0].DurationMS {
		t.Fatalf("Prepend mutated the input slice")
	}
}

func TestRemoveAt(t *testing.T) {
	history := []Summary{{DurationMS: 0}, {DurationMS: 1}, {DurationMS: 2}}

	got, ok := RemoveAt(history, 1)
	if !ok {
		t.Fatal("RemoveAt(1) ok = false, want true")
	}
	if len(got) != 2 || got[0].DurationMS != 0 || got[1].DurationMS != 2 {
		t.Fatalf("RemoveAt(1) = %+v, want entries 0 and 2", got)
	}
	if len(history) != 3 {
		t.Fatal("RemoveAt mutated the input slice")
	}

	if _, ok := RemoveAt(history, -1); ok {
		t.Error("RemoveAt(-1) ok = true, want false")
	}
	if _, ok := RemoveAt(history, 3); ok {
		t.Error("RemoveAt(out of range) ok = true, want false")
	}
}
