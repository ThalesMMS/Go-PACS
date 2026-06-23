package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyArchiveCleanArchivePasses(t *testing.T) {
	ctx := context.Background()
	sess := openTestSession(t)
	studyUID := "1.2.826.0.1.3680043.10.543.9101"
	writeVerifiedStudy(t, sess, studyUID)

	result, err := sess.VerifyArchive(ctx)
	if err != nil {
		t.Fatalf("VerifyArchive failed: %v", err)
	}
	if !result.OK {
		t.Fatalf("OK = false, errors = %#v", result.Errors)
	}
	if result.StudyCount != 1 {
		t.Fatalf("StudyCount = %d, want 1", result.StudyCount)
	}
	if result.InstanceCount != 1 {
		t.Fatalf("InstanceCount = %d, want 1", result.InstanceCount)
	}
	if result.ObjectCount != 1 {
		t.Fatalf("ObjectCount = %d, want 1", result.ObjectCount)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("Errors = %#v, want none", result.Errors)
	}
}

func TestVerifyArchiveReportsMissingStoredObject(t *testing.T) {
	ctx := context.Background()
	sess := openTestSession(t)
	studyUID := "1.2.826.0.1.3680043.10.543.9102"
	writeVerifiedStudy(t, sess, studyUID)

	instances, err := sess.Catalog().InstancesForStudy(ctx, studyUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("instances = %d, want 1", len(instances))
	}
	missingPath := instances[0].StoredPath
	if err := os.Remove(missingPath); err != nil {
		t.Fatal(err)
	}

	result, err := sess.VerifyArchive(ctx)
	if err != nil {
		t.Fatalf("VerifyArchive failed: %v", err)
	}
	if result.OK {
		t.Fatalf("OK = true, want false")
	}
	if !hasVerifyError(result.Errors, "storage", filepath.Base(missingPath)) {
		t.Fatalf("storage error for %q not found in %#v", missingPath, result.Errors)
	}
	if result.StudyCount != 1 || result.InstanceCount != 1 || result.ObjectCount != 1 {
		t.Fatalf("counts = studies:%d instances:%d objects:%d, want 1/1/1", result.StudyCount, result.InstanceCount, result.ObjectCount)
	}
}

func TestVerifyArchiveReportsCorruptJSONSidecar(t *testing.T) {
	sess := openTestSession(t)
	if err := os.WriteFile(sess.configPath, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := sess.VerifyArchive(context.Background())
	if err != nil {
		t.Fatalf("VerifyArchive failed: %v", err)
	}
	if result.OK {
		t.Fatalf("OK = true, want false")
	}
	if !hasVerifyError(result.Errors, "config", "config.json") {
		t.Fatalf("config error not found in %#v", result.Errors)
	}
}

func writeVerifiedStudy(t *testing.T, sess *Session, studyUID string) {
	t.Helper()
	sourceDir := t.TempDir()
	writeCorePart10(t, filepath.Join(sourceDir, "one.dcm"), studyUID, studyUID+".1", studyUID+".1.1")
	if _, err := sess.ImportPath(context.Background(), sourceDir, nil); err != nil {
		t.Fatal(err)
	}
}

func hasVerifyError(errors []VerifyError, category string, fragment string) bool {
	for _, err := range errors {
		if err.Category == category && strings.Contains(err.Message, fragment) {
			return true
		}
	}
	return false
}
