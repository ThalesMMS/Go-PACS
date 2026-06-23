package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func do(t *testing.T, s *Server, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	return rec, decodeEnvelope(t, rec)
}

func TestNodeCRUDOverHTTP(t *testing.T) {
	s := newTestServer(t)

	// Create
	rec, env := do(t, s, http.MethodPost, "/api/nodes",
		`{"name":"RADIANT","aeTitle":"RADIANT","host":"127.0.0.1","port":4006}`)
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("create node: code=%d env=%v", rec.Code, env)
	}
	node, _ := env["data"].(map[string]any)
	id, _ := node["id"].(string)
	if id == "" {
		t.Fatalf("created node has no id: %v", node)
	}

	// Update
	rec, env = do(t, s, http.MethodPut, "/api/nodes/"+id,
		`{"name":"RADIANT2","aeTitle":"RADIANT","host":"127.0.0.1","port":4007}`)
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("update node: code=%d env=%v", rec.Code, env)
	}

	// List reflects the update
	_, env = do(t, s, http.MethodGet, "/api/nodes", "")
	list, _ := env["data"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 node, got %d", len(list))
	}
	first, _ := list[0].(map[string]any)
	if name, _ := first["name"].(string); !strings.EqualFold(name, "RADIANT2") {
		t.Errorf("node name = %v, want RADIANT2 (case-insensitive)", first["name"])
	}

	// Delete
	rec, env = do(t, s, http.MethodDelete, "/api/nodes/"+id, "")
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("delete node: code=%d env=%v", rec.Code, env)
	}
	_, env = do(t, s, http.MethodGet, "/api/nodes", "")
	if list, _ := env["data"].([]any); len(list) != 0 {
		t.Fatalf("expected 0 nodes after delete, got %d", len(list))
	}
}

func TestCreateNodeValidationError(t *testing.T) {
	s := newTestServer(t)
	rec, env := do(t, s, http.MethodPost, "/api/nodes", `{"name":"","aeTitle":"","host":"","port":0}`)
	if rec.Code != http.StatusBadRequest || env["ok"] == true {
		t.Fatalf("expected 400 for invalid node, got code=%d env=%v", rec.Code, env)
	}
}

func TestConfigPutRoundTrip(t *testing.T) {
	s := newTestServer(t)
	rec, env := do(t, s, http.MethodPut, "/api/config",
		`{"localAETitle":"WEBAE","receiverAddress":"127.0.0.1:11112","receivePreferredTransferSyntax":"auto","dicomCommunicationTimeoutSeconds":40,"dicomConnectionTimeoutSeconds":10}`)
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("put config: code=%d env=%v", rec.Code, env)
	}
	_, env = do(t, s, http.MethodGet, "/api/config", "")
	cfg, _ := env["data"].(map[string]any)
	if cfg["localAETitle"] != "WEBAE" {
		t.Errorf("localAETitle = %v, want WEBAE", cfg["localAETitle"])
	}
}

func TestReceiverLifecycleOverHTTP(t *testing.T) {
	s := newTestServer(t)
	// Configure an ephemeral receiver port.
	do(t, s, http.MethodPut, "/api/config",
		`{"localAETitle":"WEBRX","receiverAddress":"127.0.0.1:0","receivePreferredTransferSyntax":"auto","dicomCommunicationTimeoutSeconds":40,"dicomConnectionTimeoutSeconds":10}`)

	// Initially stopped.
	_, env := do(t, s, http.MethodGet, "/api/receiver/status", "")
	st, _ := env["data"].(map[string]any)
	if st["running"] != false {
		t.Fatalf("receiver should start stopped: %v", st)
	}

	// Start.
	rec, env := do(t, s, http.MethodPost, "/api/receiver/start", "")
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("start receiver: code=%d env=%v", rec.Code, env)
	}
	st, _ = env["data"].(map[string]any)
	if st["running"] != true {
		t.Fatalf("receiver should be running after start: %v", st)
	}

	// Double start -> 409.
	rec, _ = do(t, s, http.MethodPost, "/api/receiver/start", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("double start code = %d, want 409", rec.Code)
	}

	// Stop.
	rec, env = do(t, s, http.MethodPost, "/api/receiver/stop", "")
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("stop receiver: code=%d env=%v", rec.Code, env)
	}

	// A receiver run is recorded in tasks history.
	_, env = do(t, s, http.MethodGet, "/api/tasks", "")
	if hist, _ := env["data"].([]any); len(hist) != 1 {
		t.Fatalf("expected 1 task after receiver run, got %d", len(hist))
	}
}
