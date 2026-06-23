package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreBackupRestoresIntoNewArchive(t *testing.T) {
	ctx := context.Background()
	sess := openTestSession(t)
	writeBackupSidecars(t, sess)
	studyUID := "1.2.826.0.1.3680043.10.543.9301"
	writeVerifiedStudy(t, sess, studyUID)
	backupDir := filepath.Join(t.TempDir(), "backup")
	if _, err := sess.BackupArchive(ctx, backupDir); err != nil {
		t.Fatalf("BackupArchive failed: %v", err)
	}

	restoreDir := filepath.Join(t.TempDir(), "restore")
	result, err := sess.RestoreBackup(ctx, backupDir, restoreDir, false)
	if err != nil {
		t.Fatalf("RestoreBackup failed: %v", err)
	}
	if result.DestDir == "" {
		t.Fatal("result DestDir is empty")
	}
	if !result.CatalogRestored {
		t.Fatal("CatalogRestored = false, want true")
	}
	if result.ObjectsRestored != 1 {
		t.Fatalf("ObjectsRestored = %d, want 1", result.ObjectsRestored)
	}
	if result.StoredPathsRebased != 1 {
		t.Fatalf("StoredPathsRebased = %d, want 1", result.StoredPathsRebased)
	}
	if len(result.SidecarsRestored) != 4 {
		t.Fatalf("SidecarsRestored = %#v, want four sidecars", result.SidecarsRestored)
	}
	if !result.VerificationPassed || result.Verification == nil || !result.Verification.OK {
		t.Fatalf("verification = %#v, passed=%v; want OK", result.Verification, result.VerificationPassed)
	}
	if result.OpenArchiveHint == "" {
		t.Fatal("OpenArchiveHint is empty")
	}

	restored, err := Open(restoreDir)
	if err != nil {
		t.Fatalf("open restored archive: %v", err)
	}
	defer restored.Close()
	studies, err := restored.Catalog().Studies(ctx)
	if err != nil {
		t.Fatalf("query restored archive: %v", err)
	}
	if len(studies) != 1 || studies[0].StudyInstanceUID != studyUID {
		t.Fatalf("restored studies = %#v, want study %q", studies, studyUID)
	}
}

func TestRestoreBackupRefusesCurrentArchiveWithoutOptIn(t *testing.T) {
	ctx := context.Background()
	sess := openTestSession(t)
	writeVerifiedStudy(t, sess, "1.2.826.0.1.3680043.10.543.9302")
	backupDir := filepath.Join(t.TempDir(), "backup")
	if _, err := sess.BackupArchive(ctx, backupDir); err != nil {
		t.Fatalf("BackupArchive failed: %v", err)
	}

	_, err := sess.RestoreBackup(ctx, backupDir, sess.ArchiveDir(), false)
	if !errors.Is(err, ErrOverwriteCurrentArchive) {
		t.Fatalf("RestoreBackup error = %v, want ErrOverwriteCurrentArchive", err)
	}
}

func TestRestoreBackupRejectsInvalidManifestBeforeWritingDestination(t *testing.T) {
	sess := openTestSession(t)
	backupDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(backupDir, backupManifestFileName), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	destDir := filepath.Join(t.TempDir(), "restore")

	_, err := sess.RestoreBackup(context.Background(), backupDir, destDir, false)
	if !errors.Is(err, ErrInvalidBackupManifest) {
		t.Fatalf("RestoreBackup error = %v, want ErrInvalidBackupManifest", err)
	}
	if _, err := os.Stat(destDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination was created for invalid backup: %v", err)
	}
}
