package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestArchiveSendUnknownNode(t *testing.T) {
	s := newTestServer(t)
	rec, _ := do(t, s, http.MethodPost, "/api/archive/send", `{"nodeID":"ghost","level":"STUDY","studyUID":"1.2.3"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("send unknown node code = %d, want 404", rec.Code)
	}
}

func TestArchiveImportNoFiles(t *testing.T) {
	s := newTestServer(t)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.Close() // no parts
	req := httptest.NewRequest(http.MethodPost, "/api/archive/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("import no files code = %d, want 400", rec.Code)
	}
}

func TestArchiveImportNonDicomReports(t *testing.T) {
	s := newTestServer(t)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("files", "junk.txt")
	fw.Write([]byte("not a dicom file"))
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/archive/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	// Import handles the request (200); the file is reported invalid, not an HTTP error.
	if rec.Code != http.StatusOK {
		t.Fatalf("import junk code = %d, want 200", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	data, _ := env["data"].(map[string]any)
	if data == nil {
		t.Fatalf("import response missing report: %v", env)
	}
}

func TestArchiveImportMultipleParts(t *testing.T) {
	s := newTestServer(t)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		fw, _ := mw.CreateFormFile("files", name)
		fw.Write([]byte("junk-" + name))
	}
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/archive/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("import code = %d, want 200", rec.Code)
	}
	data, _ := decodeEnvelope(t, rec)["data"].(map[string]any)
	if sc, _ := data["ScannedFiles"].(float64); int(sc) != 3 {
		t.Fatalf("ScannedFiles = %v, want 3 (handler dropped multipart parts)", data["ScannedFiles"])
	}
}
