package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	ops "github.com/ThalesMMS/Go-PACS/internal/operations"
)

func TestRetryFailedImportAppendsNewSummary(t *testing.T) {
	ctx := context.Background()
	archiveDir := filepath.Join(t.TempDir(), "archive")
	sourceDir := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "bad.txt"), []byte("not dicom"), 0o644); err != nil {
		t.Fatal(err)
	}

	sess, err := Open(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.ImportPath(ctx, sourceDir, nil); err != nil {
		t.Fatal(err)
	}
	if err := sess.Close(); err != nil {
		t.Fatal(err)
	}

	sess, err = Open(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	history, err := sess.LoadHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("len(history) = %d, want 1", len(history))
	}
	if history[0].Status != ops.StatusFailure {
		t.Fatalf("Status = %q, want %q", history[0].Status, ops.StatusFailure)
	}
	if history[0].RetryInput == nil || history[0].RetryInput.Path != sourceDir {
		t.Fatalf("RetryInput = %#v, want source path %q", history[0].RetryInput, sourceDir)
	}

	if err := sess.RetryTask(ctx, 0); err != nil {
		t.Fatal(err)
	}
	history, err = sess.LoadHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("len(history) = %d, want 2", len(history))
	}
	if history[0].Kind != ops.KindImport || history[0].Status != ops.StatusFailure {
		t.Fatalf("new history entry = %#v, want failed import", history[0])
	}
	if history[0].RetryInput == nil || history[0].RetryInput.Path != sourceDir {
		t.Fatalf("new RetryInput = %#v, want source path %q", history[0].RetryInput, sourceDir)
	}
}
