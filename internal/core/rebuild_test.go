package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRebuildArchiveCatalogVerifiesResult(t *testing.T) {
	ctx := context.Background()
	archiveDir := filepath.Join(t.TempDir(), "archive")
	sess, err := Open(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	writeVerifiedStudy(t, sess, "1.2.826.0.1.3680043.10.543.9501")
	if err := sess.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(archiveDir, "catalog.db")); err != nil {
		t.Fatal(err)
	}

	report, err := RebuildArchiveCatalog(ctx, archiveDir)
	if err != nil {
		t.Fatalf("RebuildArchiveCatalog failed: %v", err)
	}
	if !report.VerificationPassed {
		t.Fatalf("VerificationPassed = false, report = %#v", report)
	}
}
