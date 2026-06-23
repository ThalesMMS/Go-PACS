package autoquery

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ThalesMMS/Go-PACS/internal/jsonstore"
)

const DefaultProfileName = "Default Instance"

type Settings struct {
	AutoRetrieve        bool   `json:"autoRetrieve"`
	RetrieveLevel       string `json:"retrieveLevel"`
	MaxMatches          string `json:"maxMatches"`
	DuplicatePolicy     string `json:"duplicatePolicy"`
	RequireConfirmation bool   `json:"requireConfirmation"`
}

type Criteria struct {
	SearchField string   `json:"searchField,omitempty"`
	SearchText  string   `json:"searchText,omitempty"`
	DatePreset  string   `json:"datePreset,omitempty"`
	OnDate      string   `json:"onDate,omitempty"`
	LastHours   string   `json:"lastHours,omitempty"`
	Modalities  []string `json:"modalities,omitempty"`
	RefreshMode string   `json:"refreshMode,omitempty"`
}

type Source struct {
	NodeID  string `json:"nodeID,omitempty"`
	Name    string `json:"name,omitempty"`
	Host    string `json:"host,omitempty"`
	Port    uint16 `json:"port,omitempty"`
	Enabled bool   `json:"enabled"`
}

type Profile struct {
	Name     string   `json:"name"`
	Locked   bool     `json:"locked,omitempty"`
	Settings Settings `json:"settings"`
	Criteria Criteria `json:"criteria,omitempty"`
	Sources  []Source `json:"sources,omitempty"`
}

type Store struct {
	path string
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func DefaultProfile() Profile {
	return Profile{
		Name: DefaultProfileName,
		Settings: Settings{
			AutoRetrieve:        true,
			RetrieveLevel:       "Study",
			MaxMatches:          "25",
			DuplicatePolicy:     "Skip existing",
			RequireConfirmation: true,
		},
	}
}

func (s *Store) List() ([]Profile, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return []Profile{DefaultProfile()}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read auto query profiles: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return []Profile{DefaultProfile()}, nil
	}
	var profiles []Profile
	if err := json.Unmarshal(data, &profiles); err != nil {
		return nil, &jsonstore.LoadError{Err: fmt.Errorf("parse auto query profiles: %w", err), BackupExists: jsonstore.CheckBackupExists(s.path)}
	}
	if len(profiles) == 0 {
		return []Profile{DefaultProfile()}, nil
	}
	return profiles, nil
}

func (s *Store) Save(profiles []Profile) error {
	if len(profiles) == 0 {
		profiles = []Profile{DefaultProfile()}
	}
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return fmt.Errorf("encode auto query profiles: %w", err)
	}
	data = append(data, '\n')
	if err := jsonstore.WriteWithBackup(s.path, data); err != nil {
		return fmt.Errorf("write auto query profiles: %w", err)
	}
	return nil
}

func (s *Store) RecoverFromBackup() error {
	return jsonstore.RecoverFromBackup(s.path)
}
