package nodes

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Node struct {
	ID                       string `json:"id"`
	Name                     string `json:"name"`
	AETitle                  string `json:"aeTitle"`
	Host                     string `json:"host"`
	Port                     uint16 `json:"port"`
	Disabled                 bool   `json:"disabled,omitempty"`
	QueryDisabled            bool   `json:"queryDisabled,omitempty"`
	SendDisabled             bool   `json:"sendDisabled,omitempty"`
	RetrieveMethod           string `json:"retrieveMethod,omitempty"`
	PreferredMoveDestination string `json:"preferredMoveDestination,omitempty"`
	Notes                    string `json:"notes,omitempty"`
	CreatedAt                string `json:"createdAt"`
	UpdatedAt                string `json:"updatedAt"`
}

type Draft struct {
	Name                     string
	AETitle                  string
	Host                     string
	Port                     uint16
	Disabled                 bool
	QueryDisabled            bool
	SendDisabled             bool
	RetrieveMethod           string
	PreferredMoveDestination string
	Notes                    string
}

const (
	RetrieveMethodAuto = "Auto"
	RetrieveMethodMove = "C-MOVE"
	RetrieveMethodGet  = "C-GET"
)

type Store struct {
	path string
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) List() ([]Node, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read nodes config: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var nodes []Node
	if err := json.Unmarshal(data, &nodes); err != nil {
		return nil, fmt.Errorf("parse nodes config: %w", err)
	}
	return nodes, nil
}

func (s *Store) Add(draft Draft) (Node, error) {
	node, err := NewNode(draft)
	if err != nil {
		return Node{}, err
	}
	existing, err := s.List()
	if err != nil {
		return Node{}, err
	}
	for _, item := range existing {
		if strings.EqualFold(item.Name, node.Name) {
			return Node{}, fmt.Errorf("node name %q already exists", node.Name)
		}
	}
	existing = append(existing, node)
	if err := s.Save(existing); err != nil {
		return Node{}, err
	}
	return node, nil
}

func (s *Store) Update(id string, draft Draft) (Node, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Node{}, errors.New("node ID cannot be empty")
	}
	node, err := NewNode(draft)
	if err != nil {
		return Node{}, err
	}
	existing, err := s.List()
	if err != nil {
		return Node{}, err
	}
	index := -1
	for i, item := range existing {
		if item.ID == id {
			index = i
			continue
		}
		if strings.EqualFold(item.Name, node.Name) {
			return Node{}, fmt.Errorf("node name %q already exists", node.Name)
		}
	}
	if index < 0 {
		return Node{}, fmt.Errorf("node ID %q not found", id)
	}
	node.ID = existing[index].ID
	node.CreatedAt = existing[index].CreatedAt
	node.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	existing[index] = node
	if err := s.Save(existing); err != nil {
		return Node{}, err
	}
	return node, nil
}

func (s *Store) Delete(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("node ID cannot be empty")
	}
	existing, err := s.List()
	if err != nil {
		return err
	}
	next := make([]Node, 0, len(existing))
	found := false
	for _, node := range existing {
		if node.ID == id {
			found = true
			continue
		}
		next = append(next, node)
	}
	if !found {
		return fmt.Errorf("node ID %q not found", id)
	}
	return s.Save(next)
}

func (s *Store) Save(nodes []Node) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(nodes, "", "  ")
	if err != nil {
		return fmt.Errorf("encode nodes config: %w", err)
	}
	data = append(data, '\n')
	temp := s.path + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return fmt.Errorf("write nodes config: %w", err)
	}
	if err := os.Rename(temp, s.path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("replace nodes config: %w", err)
	}
	return nil
}

func NewNode(draft Draft) (Node, error) {
	name := NormalizeNodeName(draft.Name)
	aeTitle := NormalizeAETitle(draft.AETitle)
	host := strings.TrimSpace(draft.Host)
	moveDestination := NormalizeAETitle(draft.PreferredMoveDestination)
	retrieveMethod, err := NormalizeRetrieveMethod(draft.RetrieveMethod)
	if err != nil {
		return Node{}, err
	}

	if name == "" {
		return Node{}, errors.New("node name cannot be empty")
	}
	if err := ValidateAETitle(aeTitle); err != nil {
		return Node{}, err
	}
	if host == "" {
		return Node{}, errors.New("node host cannot be empty")
	}
	if draft.Port == 0 {
		return Node{}, errors.New("node port must be between 1 and 65535")
	}
	if moveDestination != "" {
		if err := ValidateAETitle(moveDestination); err != nil {
			return Node{}, fmt.Errorf("move destination: %w", err)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return Node{
		ID:                       uuid.NewString(),
		Name:                     name,
		AETitle:                  aeTitle,
		Host:                     host,
		Port:                     draft.Port,
		Disabled:                 draft.Disabled,
		QueryDisabled:            draft.QueryDisabled,
		SendDisabled:             draft.SendDisabled,
		RetrieveMethod:           retrieveMethod,
		PreferredMoveDestination: moveDestination,
		Notes:                    strings.TrimSpace(draft.Notes),
		CreatedAt:                now,
		UpdatedAt:                now,
	}, nil
}

func (n Node) Enabled() bool {
	return !n.Disabled
}

func (n Node) QueryEnabled() bool {
	return !n.QueryDisabled
}

func (n Node) SendEnabled() bool {
	return !n.SendDisabled
}

func (n Node) RetrieveMethodOrDefault() string {
	method, err := NormalizeRetrieveMethod(n.RetrieveMethod)
	if err != nil || method == "" {
		return RetrieveMethodAuto
	}
	return method
}

func NormalizeNodeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func NormalizeRetrieveMethod(method string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "", "AUTO":
		return "", nil
	case "C-MOVE", "MOVE":
		return RetrieveMethodMove, nil
	case "C-GET", "GET":
		return RetrieveMethodGet, nil
	default:
		return "", fmt.Errorf("retrieve method must be %s, %s, or %s", RetrieveMethodAuto, RetrieveMethodMove, RetrieveMethodGet)
	}
}

func NormalizeAETitle(aeTitle string) string {
	return strings.ToUpper(strings.TrimSpace(aeTitle))
}

func ValidateAETitle(aeTitle string) error {
	trimmed := strings.TrimSpace(aeTitle)
	if trimmed == "" {
		return errors.New("AE title cannot be empty")
	}
	if aeTitle != trimmed {
		return errors.New("AE title cannot have leading or trailing whitespace")
	}
	if len([]rune(aeTitle)) > 16 {
		return errors.New("AE title must be at most 16 characters")
	}
	for _, r := range aeTitle {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' {
			continue
		}
		return fmt.Errorf("AE title contains invalid character %q; allowed: A-Z, 0-9, space", r)
	}
	return nil
}
