package web

import (
	"net/http"
	"testing"
)

func TestAutoQueryProfilesRoundTrip(t *testing.T) {
	s := newTestServer(t)
	rec, env := do(t, s, http.MethodPut, "/api/autoquery/profiles",
		`[{"name":"Nightly","settings":{"autoRetrieve":true,"retrieveLevel":"STUDY"},"criteria":{"searchField":"PatientName","onDate":"20260101"}}]`)
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("save profiles: code=%d env=%v", rec.Code, env)
	}
	_, env = do(t, s, http.MethodGet, "/api/autoquery/profiles", "")
	list, _ := env["data"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(list))
	}
}

func TestAutoQueryRunNoSources(t *testing.T) {
	s := newTestServer(t)
	rec, env := do(t, s, http.MethodPost, "/api/autoquery/run",
		`{"name":"P","criteria":{"searchText":"DOE"},"sources":[]}`)
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("run autoquery: code=%d env=%v", rec.Code, env)
	}
}
