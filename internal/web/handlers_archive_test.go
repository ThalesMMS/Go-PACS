package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestArchiveStudiesEmpty(t *testing.T) {
	s := newTestServer(t)
	rec, env := do(t, s, http.MethodGet, "/api/archive/studies", "")
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("studies: code=%d env=%v", rec.Code, env)
	}
	if list, _ := env["data"].([]any); len(list) != 0 {
		t.Fatalf("empty archive should have 0 studies, got %d", len(list))
	}
}

func TestArchiveStudiesAcceptsFilters(t *testing.T) {
	s := newTestServer(t)
	rec, env := do(t, s, http.MethodGet, "/api/archive/studies?patientName=DOE&modalities=CT,MR&hasComments=true", "")
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("filtered studies: code=%d env=%v", rec.Code, env)
	}
}

func TestArchiveExportStudiesCSV(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/archive/export/studies?format=csv", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("export studies csv: code=%d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/csv" {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd == "" {
		t.Error("missing Content-Disposition for download")
	}
}

func TestArchiveExportUnknownKind(t *testing.T) {
	s := newTestServer(t)
	rec, _ := do(t, s, http.MethodGet, "/api/archive/export/bogus", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown export kind code = %d, want 404", rec.Code)
	}
}

func TestArchiveInspectUnknownInstance(t *testing.T) {
	s := newTestServer(t)
	rec, _ := do(t, s, http.MethodGet, "/api/archive/instances/1.2.3.4/inspect", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("inspect unknown instance code = %d, want 404", rec.Code)
	}
}

func TestArchiveDeleteUnknownStudy(t *testing.T) {
	s := newTestServer(t)
	// Deleting a non-existent study is a no-op delete of 0 objects (not an error).
	rec, env := do(t, s, http.MethodDelete, "/api/archive/studies/1.2.3.4", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete unknown study: code=%d env=%v", rec.Code, env)
	}
}
