package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/appconfig"
	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/autoquery"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
)

func TestBackupArchiveCreatesManifestAndCopiesFiles(t *testing.T) {
	ctx := context.Background()
	sess := openTestSession(t)
	writeBackupSidecars(t, sess)
	studyUID := "1.2.826.0.1.3680043.10.543.9201"
	writeVerifiedStudy(t, sess, studyUID)

	backupDir := filepath.Join(t.TempDir(), "backup")
	result, err := sess.BackupArchive(ctx, backupDir)
	if err != nil {
		t.Fatalf("BackupArchive failed: %v", err)
	}
	if result.Verification == nil || !result.Verification.OK {
		t.Fatalf("verification = %#v, want OK", result.Verification)
	}

	manifest := readBackupManifest(t, filepath.Join(backupDir, backupManifestFileName))
	if manifest.Timestamp == "" {
		t.Fatal("manifest timestamp is empty")
	}
	if _, err := time.Parse(time.RFC3339Nano, manifest.Timestamp); err != nil {
		t.Fatalf("manifest timestamp is not RFC3339Nano: %v", err)
	}
	sourceAbs, err := filepath.Abs(sess.ArchiveDir())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SourceArchivePath != sourceAbs {
		t.Fatalf("SourceArchivePath = %q, want %q", manifest.SourceArchivePath, sourceAbs)
	}
	if manifest.CatalogSchemaVersion != archive.CatalogSchemaVersion {
		t.Fatalf("CatalogSchemaVersion = %d, want %d", manifest.CatalogSchemaVersion, archive.CatalogSchemaVersion)
	}
	if manifest.ObjectCount != 1 {
		t.Fatalf("ObjectCount = %d, want 1", manifest.ObjectCount)
	}
	if manifest.ObjectBytes <= 0 {
		t.Fatalf("ObjectBytes = %d, want > 0", manifest.ObjectBytes)
	}
	if manifest.TotalBytes < manifest.ObjectBytes {
		t.Fatalf("TotalBytes = %d, want >= ObjectBytes %d", manifest.TotalBytes, manifest.ObjectBytes)
	}
	if !reflect.DeepEqual(result.Manifest, manifest) {
		t.Fatalf("result manifest = %#v, file manifest = %#v", result.Manifest, manifest)
	}

	if _, err := os.Stat(filepath.Join(backupDir, "catalog.db")); err != nil {
		t.Fatalf("backup catalog missing: %v", err)
	}
	copiedCatalog, err := archive.Open(backupDir)
	if err != nil {
		t.Fatalf("open copied catalog: %v", err)
	}
	defer copiedCatalog.Close()
	studies, err := copiedCatalog.Studies(ctx)
	if err != nil {
		t.Fatalf("query copied catalog: %v", err)
	}
	if len(studies) != 1 || studies[0].StudyInstanceUID != studyUID {
		t.Fatalf("copied studies = %#v, want study %q", studies, studyUID)
	}

	assertObjectsCopied(t, filepath.Join(sess.ArchiveDir(), "objects"), filepath.Join(backupDir, "objects"))
	for _, name := range []string{configFileName, nodesFileName, autoQueryProfilesFileName, historyFileName} {
		if _, err := os.Stat(filepath.Join(sess.ArchiveDir(), name)); err != nil {
			t.Fatalf("source sidecar %s missing before assertion: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(backupDir, name)); err != nil {
			t.Fatalf("backup sidecar %s missing: %v", name, err)
		}
	}
}

func TestBackupArchiveRejectsFailedVerification(t *testing.T) {
	ctx := context.Background()
	sess := openTestSession(t)
	studyUID := "1.2.826.0.1.3680043.10.543.9202"
	writeVerifiedStudy(t, sess, studyUID)
	instances, err := sess.Catalog().InstancesForStudy(ctx, studyUID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(instances[0].StoredPath); err != nil {
		t.Fatal(err)
	}

	result, err := sess.BackupArchive(ctx, filepath.Join(t.TempDir(), "backup"))
	if err == nil {
		t.Fatal("BackupArchive succeeded, want verification error")
	}
	if result == nil || result.Verification == nil || result.Verification.OK {
		t.Fatalf("result verification = %#v, want failed verification", result)
	}
}

func writeBackupSidecars(t *testing.T, sess *Session) {
	t.Helper()
	if err := sess.SaveConfig(appconfig.Defaults()); err != nil {
		t.Fatal(err)
	}
	node, err := nodes.NewNode(nodes.Draft{Name: "BACKUP-NODE", AETitle: "BACKUPAE", Host: "127.0.0.1", Port: 11112})
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.SaveNodes([]nodes.Node{node}); err != nil {
		t.Fatal(err)
	}
	profile := autoquery.DefaultProfile()
	profile.Name = "Backup"
	if err := sess.SaveAutoQueryProfiles([]autoquery.Profile{profile}); err != nil {
		t.Fatal(err)
	}
}

func readBackupManifest(t *testing.T, path string) BackupManifest {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest BackupManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	return manifest
}

func assertObjectsCopied(t *testing.T, sourceDir string, backupDir string) {
	t.Helper()
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("source objects directory is empty")
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(backupDir, entry.Name())); err != nil {
			t.Fatalf("backup object %s missing: %v", entry.Name(), err)
		}
	}
}
