package web

import (
	"net/http"
	"testing"
)

func TestQuerySourcesEmpty(t *testing.T) {
	s := newTestServer(t)
	rec, env := do(t, s, http.MethodGet, "/api/query/sources", "")
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("query sources: code=%d env=%v", rec.Code, env)
	}
}

func TestQueryStudyNoSourcesReturnsEmpty(t *testing.T) {
	// With no query-enabled nodes, the multi-source query has nothing to do and
	// returns an empty match set without contacting the network.
	s := newTestServer(t)
	rec, env := do(t, s, http.MethodPost, "/api/query/study", `{"criteria":{"patientName":"DOE"},"sourceIDs":[]}`)
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("query study: code=%d env=%v", rec.Code, env)
	}
	data, _ := env["data"].(map[string]any)
	if matches, _ := data["matches"].([]any); len(matches) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matches))
	}
}

func TestRetrieveUnknownNode(t *testing.T) {
	s := newTestServer(t)
	rec, env := do(t, s, http.MethodPost, "/api/query/retrieve", `{"nodeID":"ghost","level":"STUDY","studyUID":"1.2.3"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("retrieve unknown node: code=%d env=%v", rec.Code, env)
	}
}
