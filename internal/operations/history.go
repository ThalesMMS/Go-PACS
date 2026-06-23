package operations

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const MaxHistoryEntries = 200

func LoadHistory(path string) ([]Summary, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read task history: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var history []Summary
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("parse task history: %w", err)
	}
	return trimHistory(history), nil
}

func SaveHistory(path string, history []Summary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create task history directory: %w", err)
	}
	history = trimHistory(history)
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("encode task history: %w", err)
	}
	data = append(data, '\n')
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return fmt.Errorf("write task history: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("replace task history: %w", err)
	}
	return nil
}

func trimHistory(history []Summary) []Summary {
	if len(history) <= MaxHistoryEntries {
		return history
	}
	return history[:MaxHistoryEntries]
}

// Prepend returns history with entry inserted as the most recent item, trimmed
// to MaxHistoryEntries. The input slice is not mutated. This is the canonical
// "record a new operation" policy shared by every frontend.
func Prepend(history []Summary, entry Summary) []Summary {
	updated := make([]Summary, 0, len(history)+1)
	updated = append(updated, entry)
	updated = append(updated, history...)
	return trimHistory(updated)
}

// RemoveAt returns history with the entry at index removed, plus whether the
// index was in range. When index is out of range the original slice is returned
// unchanged and ok is false. The input slice is not mutated.
func RemoveAt(history []Summary, index int) (result []Summary, ok bool) {
	if index < 0 || index >= len(history) {
		return history, false
	}
	updated := make([]Summary, 0, len(history)-1)
	updated = append(updated, history[:index]...)
	updated = append(updated, history[index+1:]...)
	return updated, true
}
