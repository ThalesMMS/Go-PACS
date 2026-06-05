package nodes

import (
	"path/filepath"
	"testing"
)

func TestNewNodeNormalizesAndValidates(t *testing.T) {
	node, err := NewNode(Draft{
		Name:                     "  PACS ",
		AETitle:                  " remote ",
		Host:                     " 127.0.0.1 ",
		Port:                     104,
		PreferredMoveDestination: " local ",
		Notes:                    " test ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if node.Name != "pacs" {
		t.Fatalf("Name = %q, want pacs", node.Name)
	}
	if node.AETitle != "REMOTE" {
		t.Fatalf("AETitle = %q, want REMOTE", node.AETitle)
	}
	if node.PreferredMoveDestination != "LOCAL" {
		t.Fatalf("PreferredMoveDestination = %q, want LOCAL", node.PreferredMoveDestination)
	}
	if !node.Enabled() {
		t.Fatal("new node should default to enabled")
	}
	if !node.QueryEnabled() {
		t.Fatal("new node should default query to enabled")
	}
	if !node.SendEnabled() {
		t.Fatal("new node should default send to enabled")
	}
}

func TestNewNodeCanBeDisabled(t *testing.T) {
	node, err := NewNode(Draft{Name: "pacs", AETitle: "PACS", Host: "localhost", Port: 11112, Disabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if node.Enabled() {
		t.Fatal("disabled draft produced enabled node")
	}
}

func TestNewNodeCanDisableQueryAndSend(t *testing.T) {
	node, err := NewNode(Draft{Name: "pacs", AETitle: "PACS", Host: "localhost", Port: 11112, QueryDisabled: true, SendDisabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if node.QueryEnabled() {
		t.Fatal("query-disabled draft produced query-enabled node")
	}
	if node.SendEnabled() {
		t.Fatal("send-disabled draft produced send-enabled node")
	}
}

func TestQueryAndSendFlagsAreIndependentFromMasterEnabledState(t *testing.T) {
	node := Node{Disabled: true}

	if !node.QueryEnabled() {
		t.Fatal("master-disabled node should preserve query checkbox state")
	}
	if !node.SendEnabled() {
		t.Fatal("master-disabled node should preserve send checkbox state")
	}
}

func TestNewNodeNormalizesRetrieveMethodPreference(t *testing.T) {
	node, err := NewNode(Draft{Name: "pacs", AETitle: "PACS", Host: "localhost", Port: 11112, RetrieveMethod: " c-get "})
	if err != nil {
		t.Fatal(err)
	}
	if node.RetrieveMethod != RetrieveMethodGet {
		t.Fatalf("RetrieveMethod = %q, want %q", node.RetrieveMethod, RetrieveMethodGet)
	}
	if node.RetrieveMethodOrDefault() != RetrieveMethodGet {
		t.Fatalf("RetrieveMethodOrDefault = %q, want %q", node.RetrieveMethodOrDefault(), RetrieveMethodGet)
	}
}

func TestNewNodeDefaultsRetrieveMethodToAuto(t *testing.T) {
	node, err := NewNode(Draft{Name: "pacs", AETitle: "PACS", Host: "localhost", Port: 11112})
	if err != nil {
		t.Fatal(err)
	}
	if node.RetrieveMethod != "" {
		t.Fatalf("RetrieveMethod = %q, want empty persisted default", node.RetrieveMethod)
	}
	if node.RetrieveMethodOrDefault() != RetrieveMethodAuto {
		t.Fatalf("RetrieveMethodOrDefault = %q, want %q", node.RetrieveMethodOrDefault(), RetrieveMethodAuto)
	}
}

func TestNewNodeRejectsInvalidRetrieveMethod(t *testing.T) {
	if _, err := NewNode(Draft{Name: "pacs", AETitle: "PACS", Host: "localhost", Port: 11112, RetrieveMethod: "WADO"}); err == nil {
		t.Fatal("NewNode accepted invalid retrieve method")
	}
}

func TestValidateAETitleRejectsInvalidCharacters(t *testing.T) {
	if err := ValidateAETitle("PACS_AE"); err == nil {
		t.Fatal("ValidateAETitle accepted underscore")
	}
	if err := ValidateAETitle("pacs"); err == nil {
		t.Fatal("ValidateAETitle accepted lowercase")
	}
}

func TestStoreAddListRoundTrip(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "nodes.json"))
	added, err := store.Add(Draft{Name: "pacs", AETitle: "PACS", Host: "localhost", Port: 11112, Disabled: true})
	if err != nil {
		t.Fatal(err)
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
	if list[0].ID != added.ID || list[0].Name != "pacs" {
		t.Fatalf("round-trip node = %+v, added %+v", list[0], added)
	}
	if list[0].Enabled() {
		t.Fatalf("round-trip node should be disabled: %+v", list[0])
	}
	if _, err := store.Add(Draft{Name: "PACS", AETitle: "OTHER", Host: "localhost", Port: 11113}); err == nil {
		t.Fatal("Store.Add accepted duplicate name")
	}
}

func TestStoreUpdatePreservesIDAndRejectsDuplicateName(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "nodes.json"))
	first, err := store.Add(Draft{Name: "pacs", AETitle: "PACS", Host: "localhost", Port: 11112})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Add(Draft{Name: "backup", AETitle: "BACKUP", Host: "backup.local", Port: 11113})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := store.Update(first.ID, Draft{
		Name:                     "renamed",
		AETitle:                  "remote",
		Host:                     "127.0.0.1",
		Port:                     104,
		PreferredMoveDestination: "local",
		Notes:                    "edited",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != first.ID {
		t.Fatalf("ID = %q, want %q", updated.ID, first.ID)
	}
	if updated.CreatedAt != first.CreatedAt {
		t.Fatalf("CreatedAt = %q, want %q", updated.CreatedAt, first.CreatedAt)
	}
	if updated.Name != "renamed" || updated.AETitle != "REMOTE" || updated.PreferredMoveDestination != "LOCAL" {
		t.Fatalf("updated node = %+v", updated)
	}

	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != first.ID || list[0].Name != "renamed" || list[1].ID != second.ID {
		t.Fatalf("list = %+v", list)
	}

	if _, err := store.Update(first.ID, Draft{Name: "backup", AETitle: "OTHER", Host: "localhost", Port: 11114}); err == nil {
		t.Fatal("Store.Update accepted duplicate name")
	}
	if _, err := store.Update("missing", Draft{Name: "missing", AETitle: "MISS", Host: "localhost", Port: 11115}); err == nil {
		t.Fatal("Store.Update accepted missing ID")
	}
}

func TestStoreDeleteRemovesNode(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "nodes.json"))
	first, err := store.Add(Draft{Name: "pacs", AETitle: "PACS", Host: "localhost", Port: 11112})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Add(Draft{Name: "backup", AETitle: "BACKUP", Host: "backup.local", Port: 11113})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Delete(first.ID); err != nil {
		t.Fatal(err)
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != second.ID {
		t.Fatalf("list = %+v, want only second node", list)
	}
	if err := store.Delete("missing"); err == nil {
		t.Fatal("Store.Delete accepted missing ID")
	}
}
