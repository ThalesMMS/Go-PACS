package web

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	ops "github.com/ThalesMMS/Go-PACS/internal/operations"
)

func TestTaskCanRetryEndpointReportsEligibility(t *testing.T) {
	s := newTestServer(t)
	sourceDir := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	summary := ops.ImportSummary(archive.ImportReport{
		ScannedFiles: 1,
		InvalidFiles: 1,
	}, time.Millisecond, sourceDir)
	if err := s.session.SaveHistory([]ops.Summary{summary}); err != nil {
		t.Fatal(err)
	}

	rec, env := do(t, s, http.MethodGet, "/api/tasks/0/can-retry", "")
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("can-retry: code=%d env=%v", rec.Code, env)
	}
	data, _ := env["data"].(map[string]any)
	if data["canRetry"] != true {
		t.Fatalf("canRetry = %v, want true (data=%v)", data["canRetry"], data)
	}
}

func TestTaskRetryEndpointRejectsNonRetryableHistory(t *testing.T) {
	s := newTestServer(t)
	if err := s.session.SaveHistory([]ops.Summary{{
		Version: ops.SummaryVersion,
		Kind:    ops.KindQueryFind,
		Status:  ops.StatusSuccess,
	}}); err != nil {
		t.Fatal(err)
	}

	rec, env := do(t, s, http.MethodPost, "/api/tasks/0/retry", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("retry: code=%d env=%v, want 400", rec.Code, env)
	}
	if env["ok"] == true || env["error"] == "" {
		t.Fatalf("retry error envelope = %v", env)
	}
}
