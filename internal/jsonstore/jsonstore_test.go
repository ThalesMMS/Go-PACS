package jsonstore_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ThalesMMS/Go-PACS/internal/jsonstore"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
)

func TestStoreLoadErrorReportsBackupAndRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nodes.json")
	store := nodes.NewStore(path)
	first, err := nodes.NewNode(nodes.Draft{Name: "NODE-ONE", AETitle: "NODEONE", Host: "127.0.0.1", Port: 11112})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save([]nodes.Node{first}); err != nil {
		t.Fatal(err)
	}
	second, err := nodes.NewNode(nodes.Draft{Name: "NODE-TWO", AETitle: "NODETWO", Host: "127.0.0.1", Port: 11113})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save([]nodes.Node{second}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jsonstore.BackupPath(path)); err != nil {
		t.Fatalf("backup file missing: %v", err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = store.List()
	var loadErr *jsonstore.LoadError
	if !errors.As(err, &loadErr) {
		t.Fatalf("List error = %v, want *jsonstore.LoadError", err)
	}
	if !loadErr.BackupExists {
		t.Fatalf("BackupExists = false, want true")
	}
	if err := store.RecoverFromBackup(); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 1 || recovered[0].AETitle != "NODEONE" {
		t.Fatalf("recovered nodes = %#v, want NODEONE from backup", recovered)
	}
}
