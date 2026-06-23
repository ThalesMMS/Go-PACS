package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ThalesMMS/Go-PACS/internal/core"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	sess, err := core.Open(t.TempDir())
	if err != nil {
		t.Fatalf("core.Open: %v", err)
	}
	t.Cleanup(func() { sess.Close() })
	return NewServer(sess)
}

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode JSON envelope: %v (body=%q)", err, rec.Body.String())
	}
	return env
}

func TestServeIndex(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "go-pacs") {
		t.Errorf("index page did not contain expected content")
	}
}

func TestNodesEndpointReturnsSavedNodes(t *testing.T) {
	s := newTestServer(t)
	node, err := nodes.NewNode(nodes.Draft{Name: "RADIANT", AETitle: "RADIANT", Host: "127.0.0.1", Port: 11112})
	if err != nil {
		t.Fatalf("NewNode: %v", err)
	}
	if err := s.session.SaveNodes([]nodes.Node{node}); err != nil {
		t.Fatalf("SaveNodes: %v", err)
	}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/nodes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/nodes status = %d, want 200", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if env["ok"] != true {
		t.Fatalf("ok = %v, want true", env["ok"])
	}
	list, _ := env["data"].([]any)
	if len(list) != 1 {
		t.Fatalf("data length = %d, want 1", len(list))
	}
}

func TestConfigEndpoint(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/config status = %d, want 200", rec.Code)
	}
	if decodeEnvelope(t, rec)["ok"] != true {
		t.Errorf("config envelope ok != true")
	}
}

func TestArchiveVerifyEndpointReturnsResult(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/archive/verify", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/archive/verify status = %d, want 200", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if env["ok"] != true {
		t.Fatalf("ok = %v, want true", env["ok"])
	}
	data, _ := env["data"].(map[string]any)
	if data["ok"] != true {
		t.Fatalf("verify data ok = %v, want true", data["ok"])
	}
}

func TestArchiveRestoreEndpointRestoresBackup(t *testing.T) {
	s := newTestServer(t)
	backupDir := filepath.Join(t.TempDir(), "backup")
	if _, err := s.session.BackupArchive(context.Background(), backupDir); err != nil {
		t.Fatalf("BackupArchive: %v", err)
	}
	restoreDir := filepath.Join(t.TempDir(), "restore")
	body := fmt.Sprintf(`{"backupPath":%q,"destPath":%q}`, backupDir, restoreDir)
	rec, env := do(t, s, http.MethodPost, "/api/archive/restore-path", body)
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("restore: code=%d env=%v", rec.Code, env)
	}
	data, _ := env["data"].(map[string]any)
	if data["verificationPassed"] != true {
		t.Fatalf("verificationPassed = %v, want true", data["verificationPassed"])
	}
	if _, err := os.Stat(filepath.Join(restoreDir, "catalog.db")); err != nil {
		t.Fatalf("restored catalog missing: %v", err)
	}
}

func TestEchoUnknownNodeReturns404(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/echo", strings.NewReader(`{"nodeID":"ghost"}`))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST /api/echo unknown node status = %d, want 404", rec.Code)
	}
	if decodeEnvelope(t, rec)["error"] != "node not found" {
		t.Errorf("unexpected error message: %v", decodeEnvelope(t, rec)["error"])
	}
}

func TestEchoVerifiesDICOMwebNode(t *testing.T) {
	dicomwebServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dicom-web/qido-rs/studies" {
			t.Fatalf("path = %q, want /dicom-web/qido-rs/studies", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/dicom+json")
		_, _ = w.Write([]byte("[]"))
	}))
	defer dicomwebServer.Close()

	s := newTestServer(t)
	node, err := nodes.NewNode(nodes.Draft{
		Name:           "web",
		Protocol:       nodes.ProtocolDICOMweb,
		BaseURL:        dicomwebServer.URL + "/dicom-web",
		QIDOPathPrefix: "/qido-rs",
	})
	if err != nil {
		t.Fatalf("NewNode: %v", err)
	}
	if err := s.session.SaveNodes([]nodes.Node{node}); err != nil {
		t.Fatalf("SaveNodes: %v", err)
	}

	rec, env := do(t, s, http.MethodPost, "/api/echo", `{"nodeID":"web"}`)
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("DICOMweb echo: code=%d env=%v", rec.Code, env)
	}
	data, _ := env["data"].(map[string]any)
	if data["statusCode"].(float64) != http.StatusOK {
		t.Fatalf("statusCode = %v, want 200", data["statusCode"])
	}
	if data["success"] != true {
		t.Fatalf("success = %v, want true", data["success"])
	}
}

func TestEchoRequiresPost(t *testing.T) {
	// Echo is registered POST-only; a GET must not perform an echo. With the
	// method-routed mux it falls through to the JSON /api 404 handler.
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/echo", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/echo status = %d, want 404", rec.Code)
	}
}
