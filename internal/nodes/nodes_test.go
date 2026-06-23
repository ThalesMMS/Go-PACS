package nodes

import (
	"errors"
	"net"
	"path/filepath"
	"slices"
	"strings"
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

func TestRemoteHostAllowlistResolvesHostnameNodes(t *testing.T) {
	nodeList := []Node{
		{Name: "literal", Host: "192.0.2.10"},
		{Name: "hostname", Host: "pacs.example.test"},
		{Name: "duplicate", Host: "192.0.2.10"},
	}

	got := remoteHostAllowlist(nodeList, func(host string) ([]net.IP, error) {
		if host != "pacs.example.test" {
			t.Fatalf("resolver called for %q, want hostname only", host)
		}
		return []net.IP{net.ParseIP("192.0.2.20"), net.ParseIP("2001:db8::20")}, nil
	})
	want := []string{"192.0.2.10", "192.0.2.20", "2001:db8::20"}
	if !slices.Equal(got.Hosts, want) {
		t.Fatalf("RemoteHostAllowlist hosts = %#v, want %#v", got.Hosts, want)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("RemoteHostAllowlist warnings = %#v, want none", got.Warnings)
	}
}

func TestRemoteHostAllowlistSkipsUnresolvedHostname(t *testing.T) {
	nodeList := []Node{
		{Name: "literal", Host: "192.0.2.10"},
		{Name: "hostname", Host: "missing.example.test"},
	}
	got := remoteHostAllowlist(nodeList, func(string) ([]net.IP, error) {
		return nil, errors.New("no such host")
	})
	want := []string{"192.0.2.10"}
	if !slices.Equal(got.Hosts, want) {
		t.Fatalf("RemoteHostAllowlist hosts = %#v, want %#v", got.Hosts, want)
	}
	if len(got.Warnings) != 1 {
		t.Fatalf("RemoteHostAllowlist warnings = %#v, want one warning", got.Warnings)
	}
	if !strings.Contains(got.Warnings[0], "missing.example.test") || !strings.Contains(got.Warnings[0], "no such host") {
		t.Fatalf("RemoteHostAllowlist warning = %q, want hostname and resolver error", got.Warnings[0])
	}
}

func TestNewNodeStoresTLSSettings(t *testing.T) {
	node, err := NewNode(Draft{
		Name:          "pacs",
		AETitle:       "PACS",
		Host:          "localhost",
		Port:          11112,
		UseTLS:        true,
		TLSSkipVerify: true,
		TLSServerName: " pacs.local ",
		TLSCAFile:     " /tmp/ca.pem ",
		TLSCertFile:   " /tmp/client.pem ",
		TLSKeyFile:    " /tmp/client.key ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !node.UseTLS {
		t.Fatal("UseTLS = false, want true")
	}
	if !node.TLSSkipVerify {
		t.Fatal("TLSSkipVerify = false, want true")
	}
	if node.TLSServerName != "pacs.local" || node.TLSCAFile != "/tmp/ca.pem" || node.TLSCertFile != "/tmp/client.pem" || node.TLSKeyFile != "/tmp/client.key" {
		t.Fatalf("TLS fields = %+v", node)
	}
}

func TestNewNodeRejectsPartialTLSClientCertificate(t *testing.T) {
	if _, err := NewNode(Draft{Name: "pacs", AETitle: "PACS", Host: "localhost", Port: 11112, UseTLS: true, TLSCertFile: "/tmp/client.pem"}); err == nil {
		t.Fatal("NewNode accepted TLS cert without key")
	}
	if _, err := NewNode(Draft{Name: "pacs", AETitle: "PACS", Host: "localhost", Port: 11112, UseTLS: true, TLSKeyFile: "/tmp/client.key"}); err == nil {
		t.Fatal("NewNode accepted TLS key without cert")
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

func TestFindByAETitleReturnsFirstExactMatch(t *testing.T) {
	nodeList := []Node{
		{Name: "first", AETitle: "DESTAE", Host: "127.0.0.1", Port: 11112},
		{Name: "second", AETitle: "DESTAE", Host: "127.0.0.2", Port: 11113},
		{Name: "lower", AETitle: "destae", Host: "127.0.0.3", Port: 11114},
	}

	got, ok := FindByAETitle(nodeList, "DESTAE")
	if !ok {
		t.Fatal("FindByAETitle did not find DESTAE")
	}
	if got.Name != "first" {
		t.Fatalf("FindByAETitle returned %q, want first match", got.Name)
	}

	if _, ok := FindByAETitle(nodeList, "destae"); !ok {
		t.Fatal("FindByAETitle should use exact AE title matching")
	}
	if _, ok := FindByAETitle(nodeList, "MISSING"); ok {
		t.Fatal("FindByAETitle found missing AE title")
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

func TestNewNodeNormalizesSendTransferSyntaxPreference(t *testing.T) {
	node, err := NewNode(Draft{Name: "pacs", AETitle: "PACS", Host: "localhost", Port: 11112, SendTransferSyntax: SendTransferSyntaxExplicitVRLittleEndian})
	if err != nil {
		t.Fatal(err)
	}
	if node.SendTransferSyntax != SendTransferSyntaxExplicitVRLittleEndian {
		t.Fatalf("SendTransferSyntax = %q, want %q", node.SendTransferSyntax, SendTransferSyntaxExplicitVRLittleEndian)
	}
	if node.SendTransferSyntaxOrDefault() != SendTransferSyntaxExplicitVRLittleEndian {
		t.Fatalf("SendTransferSyntaxOrDefault = %q, want %q", node.SendTransferSyntaxOrDefault(), SendTransferSyntaxExplicitVRLittleEndian)
	}
}

func TestNewNodeDefaultsSendTransferSyntaxToAuto(t *testing.T) {
	node, err := NewNode(Draft{Name: "pacs", AETitle: "PACS", Host: "localhost", Port: 11112})
	if err != nil {
		t.Fatal(err)
	}
	if node.SendTransferSyntax != "" {
		t.Fatalf("SendTransferSyntax = %q, want empty persisted default", node.SendTransferSyntax)
	}
	if node.SendTransferSyntaxOrDefault() != SendTransferSyntaxAuto {
		t.Fatalf("SendTransferSyntaxOrDefault = %q, want %q", node.SendTransferSyntaxOrDefault(), SendTransferSyntaxAuto)
	}
}

func TestNewNodeRejectsInvalidSendTransferSyntax(t *testing.T) {
	if _, err := NewNode(Draft{Name: "pacs", AETitle: "PACS", Host: "localhost", Port: 11112, SendTransferSyntax: "1.2.3"}); err == nil {
		t.Fatal("NewNode accepted invalid send transfer syntax")
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

func TestKeyPrefersIDThenEndpoint(t *testing.T) {
	withID := Node{ID: "abc", Name: "n", Host: "h", Port: 1}
	if got, want := withID.Key(), "id:abc"; got != want {
		t.Errorf("Key() with ID = %q, want %q", got, want)
	}
	noID := Node{Name: "RADIANT", Host: "10.0.0.1", Port: 104}
	if got, want := noID.Key(), "endpoint:RADIANT:10.0.0.1:104"; got != want {
		t.Errorf("Key() without ID = %q, want %q", got, want)
	}
	// Distinct endpoints yield distinct keys.
	other := Node{Name: "RADIANT", Host: "10.0.0.2", Port: 104}
	if noID.Key() == other.Key() {
		t.Errorf("distinct endpoints produced same key %q", noID.Key())
	}
}
