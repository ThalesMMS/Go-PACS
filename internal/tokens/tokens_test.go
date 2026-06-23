package tokens

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateTokenReturnsBase64URLToken(t *testing.T) {
	token, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 43 {
		t.Fatalf("len(token) = %d, want 43", len(token))
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("token is not base64url: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("decoded length = %d, want 32", len(decoded))
	}
}

func TestHashTokenIsDeterministic(t *testing.T) {
	first := HashToken("secret")
	second := HashToken("secret")
	if first != second {
		t.Fatalf("hashes differ: %q != %q", first, second)
	}
	if first == "secret" || len(first) != 64 {
		t.Fatalf("hash = %q, want 64-char hex digest", first)
	}
}

func TestStoreCreatePersistsOnlyHashAndRevokes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	store := NewStore(path)
	token, plaintext, err := store.Create(Draft{Name: "dicomweb client", Role: RoleWrite})
	if err != nil {
		t.Fatal(err)
	}
	if plaintext == "" {
		t.Fatal("plaintext token is empty")
	}
	if token.TokenHash == "" || token.TokenHash == plaintext {
		t.Fatalf("stored hash = %q, plaintext = %q", token.TokenHash, plaintext)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), plaintext) {
		t.Fatalf("token store contains plaintext token: %s", data)
	}
	if mode := fileMode(t, path); mode != 0o600 {
		t.Fatalf("mode = %#o, want 0600", mode)
	}

	found, err := store.GetByHash(HashToken(plaintext))
	if err != nil {
		t.Fatal(err)
	}
	if found.ID != token.ID || found.Role != RoleWrite {
		t.Fatalf("found token = %#v, want ID %q role %q", found, token.ID, RoleWrite)
	}
	if _, err := store.GetByHash(HashToken("unknown")); err != ErrTokenNotFound {
		t.Fatalf("GetByHash unknown err = %v, want ErrTokenNotFound", err)
	}

	if err := store.Revoke(token.ID); err != nil {
		t.Fatal(err)
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
	if list[0].RevokedAt == "" {
		t.Fatalf("RevokedAt is empty after revoke: %#v", list[0])
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary file remains after writes: %v", err)
	}
}

func TestStoreRejectsInvalidDrafts(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "tokens.json"))
	if _, _, err := store.Create(Draft{Name: "", Role: RoleRead}); err == nil {
		t.Fatal("Create with empty name succeeded")
	}
	if _, _, err := store.Create(Draft{Name: "bad", Role: "admin"}); err == nil {
		t.Fatal("Create with invalid role succeeded")
	}
	if err := store.Revoke(""); err == nil {
		t.Fatal("Revoke with empty ID succeeded")
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
