package tokens

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	RoleRead  = "read"
	RoleWrite = "write"
)

var ErrTokenNotFound = errors.New("token not found")

type Token struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	TokenHash string `json:"tokenHash"`
	Role      string `json:"role"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	RevokedAt string `json:"revokedAt,omitempty"`
}

type Draft struct {
	Name string
	Role string
}

type Store struct {
	path string
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func GenerateToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func HashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

func (s *Store) List() ([]Token, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read tokens: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var out []Token
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse tokens: %w", err)
	}
	return out, nil
}

func (s *Store) Create(draft Draft) (Token, string, error) {
	name := strings.TrimSpace(draft.Name)
	if name == "" {
		return Token{}, "", errors.New("token name cannot be empty")
	}
	role, err := NormalizeRole(draft.Role)
	if err != nil {
		return Token{}, "", err
	}
	plaintext, err := GenerateToken()
	if err != nil {
		return Token{}, "", err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	token := Token{
		ID:        uuid.NewString(),
		Name:      name,
		TokenHash: HashToken(plaintext),
		Role:      role,
		CreatedAt: now,
		UpdatedAt: now,
	}
	existing, err := s.List()
	if err != nil {
		return Token{}, "", err
	}
	existing = append(existing, token)
	if err := s.save(existing); err != nil {
		return Token{}, "", err
	}
	return token, plaintext, nil
}

func (s *Store) GetByHash(hash string) (Token, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return Token{}, ErrTokenNotFound
	}
	existing, err := s.List()
	if err != nil {
		return Token{}, err
	}
	for _, token := range existing {
		if token.TokenHash == hash {
			return token, nil
		}
	}
	return Token{}, ErrTokenNotFound
}

func (s *Store) Revoke(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("token ID cannot be empty")
	}
	existing, err := s.List()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i := range existing {
		if existing[i].ID != id {
			continue
		}
		if existing[i].RevokedAt == "" {
			existing[i].RevokedAt = now
		}
		existing[i].UpdatedAt = now
		return s.save(existing)
	}
	return ErrTokenNotFound
}

func NormalizeRole(role string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case RoleRead:
		return RoleRead, nil
	case RoleWrite:
		return RoleWrite, nil
	default:
		return "", fmt.Errorf("token role must be %q or %q", RoleRead, RoleWrite)
	}
}

func RoleAllows(actual string, required string) bool {
	actual, actualErr := NormalizeRole(actual)
	required, requiredErr := NormalizeRole(required)
	if actualErr != nil || requiredErr != nil {
		return false
	}
	if actual == RoleWrite {
		return true
	}
	return actual == required
}

func (s *Store) save(tokens []Token) error {
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("encode tokens: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create token store directory: %w", err)
	}
	tmp := s.path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create temporary token store: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write temporary token store: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close temporary token store: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace token store: %w", err)
	}
	if err := os.Chmod(s.path, 0o600); err != nil {
		return fmt.Errorf("secure token store permissions: %w", err)
	}
	return nil
}
