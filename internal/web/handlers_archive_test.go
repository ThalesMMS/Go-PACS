package web

import (
	"bytes"
	"context"
	"encoding/binary"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	dicomcore "github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
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

func TestArchiveStudiesPaginatedResponseShape(t *testing.T) {
	s := newTestServer(t)
	importWebPart10(t, s, webPart10Options{StudyUID: "1.2.826.0.1.3680043.10.543.9901", SeriesUID: "1.2.826.0.1.3680043.10.543.9901.1", SOPUID: "1.2.826.0.1.3680043.10.543.9901.1.1", PatientName: "PAGE^ONE", PatientID: "P1", StudyDate: "20260604", Modality: "CT"})
	importWebPart10(t, s, webPart10Options{StudyUID: "1.2.826.0.1.3680043.10.543.9902", SeriesUID: "1.2.826.0.1.3680043.10.543.9902.1", SOPUID: "1.2.826.0.1.3680043.10.543.9902.1.1", PatientName: "PAGE^TWO", PatientID: "P2", StudyDate: "20260604", Modality: "MR"})

	rec, env := do(t, s, http.MethodGet, "/api/archive/studies?limit=1&offset=0&patientName=PAGE", "")
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("paginated studies: code=%d env=%v", rec.Code, env)
	}
	data, _ := env["data"].(map[string]any)
	if data == nil {
		t.Fatalf("page data = %#v, want object", env["data"])
	}
	if data["total"].(float64) != 2 || data["limit"].(float64) != 1 || data["offset"].(float64) != 0 {
		t.Fatalf("page data = %#v, want total=2 limit=1 offset=0", data)
	}
	items, _ := data["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("page items = %d, want 1", len(items))
	}

	rec, env = do(t, s, http.MethodGet, "/api/archive/studies", "")
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("legacy studies: code=%d env=%v", rec.Code, env)
	}
	if _, ok := env["data"].([]any); !ok {
		t.Fatalf("legacy studies data = %#v, want JSON array", env["data"])
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

func TestArchivePreviewPNGAndFailureStatuses(t *testing.T) {
	s := newTestServer(t)
	studyUID := "1.2.826.0.1.3680043.10.543.9910"
	sopUID := studyUID + ".1.1"
	source := filepath.Join(t.TempDir(), "pixel.dcm")
	if err := os.WriteFile(source, webPixelPart10(t, studyUID, studyUID+".1", sopUID), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.session.Catalog().ImportPath(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/archive/instances/"+sopUID+"/preview?size=thumb", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("preview code = %d, body=%q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("preview Content-Type = %q, want image/png", ct)
	}
	if _, err := png.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
		t.Fatalf("preview is not PNG: %v", err)
	}

	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/archive/instances/1.2.3.missing/preview", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing preview code = %d, want 404", rec.Code)
	}

	unsupportedUID := "1.2.826.0.1.3680043.10.543.9911"
	importWebPart10(t, s, webPart10Options{StudyUID: unsupportedUID, SeriesUID: unsupportedUID + ".1", SOPUID: unsupportedUID + ".1.1", PatientName: "NOPIXEL^PATIENT", PatientID: "NP1", StudyDate: "20260604", Modality: "CT"})
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/archive/instances/"+unsupportedUID+".1.1/preview", nil))
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("unsupported preview code = %d, want 415", rec.Code)
	}
}

func TestArchiveStoragePolicyAndPurgeEndpoints(t *testing.T) {
	s := newTestServer(t)
	rec, env := do(t, s, http.MethodGet, "/api/archive/storage", "")
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("storage: code=%d env=%v", rec.Code, env)
	}
	data, _ := env["data"].(map[string]any)
	policy, _ := data["policy"].(map[string]any)
	if policy["trashAutoPurgeDays"].(float64) != 90 {
		t.Fatalf("policy = %#v, want trashAutoPurgeDays 90", policy)
	}

	rec, env = do(t, s, http.MethodPut, "/api/archive/storage/policy", `{"trashAutoPurgeDays":0}`)
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("put policy: code=%d env=%v", rec.Code, env)
	}
	rec, env = do(t, s, http.MethodPost, "/api/archive/trash/purge-expired", "")
	if rec.Code != http.StatusOK || env["ok"] != true {
		t.Fatalf("purge expired: code=%d env=%v", rec.Code, env)
	}
}

func TestArchiveDeleteUnknownStudy(t *testing.T) {
	s := newTestServer(t)
	rec, env := do(t, s, http.MethodDelete, "/api/archive/studies/1.2.3.4", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete unknown study: code=%d env=%v, want 404", rec.Code, env)
	}
}

func webPixelPart10(t *testing.T, studyUID, seriesUID, sopUID string) []byte {
	t.Helper()
	elements := []dicomcore.Element{
		webStringElement(dicomcore.NewTag(0x0008, 0x0016), dicomcore.VRUI, "1.2.840.10008.5.1.4.1.1.2"),
		webStringElement(dicomcore.NewTag(0x0008, 0x0018), dicomcore.VRUI, sopUID),
		webStringElement(dicomcore.NewTag(0x0010, 0x0010), dicomcore.VRPN, "PIXEL^PATIENT"),
		webStringElement(dicomcore.NewTag(0x0010, 0x0020), dicomcore.VRLO, "PX1"),
		webStringElement(dicomcore.NewTag(0x0008, 0x0020), dicomcore.VRDA, "20260604"),
		webStringElement(dicomcore.NewTag(0x0008, 0x0060), dicomcore.VRCS, "CT"),
		webStringElement(dicomcore.NewTag(0x0020, 0x000D), dicomcore.VRUI, studyUID),
		webStringElement(dicomcore.NewTag(0x0020, 0x000E), dicomcore.VRUI, seriesUID),
		webUSElement(dicomcore.NewTag(0x0028, 0x0002), 1),
		webStringElement(dicomcore.NewTag(0x0028, 0x0004), dicomcore.VRCS, "MONOCHROME2"),
		webUSElement(dicomcore.NewTag(0x0028, 0x0010), 2),
		webUSElement(dicomcore.NewTag(0x0028, 0x0011), 2),
		webUSElement(dicomcore.NewTag(0x0028, 0x0100), 8),
		webUSElement(dicomcore.NewTag(0x0028, 0x0101), 8),
		webUSElement(dicomcore.NewTag(0x0028, 0x0102), 7),
		webUSElement(dicomcore.NewTag(0x0028, 0x0103), 0),
		dicomcore.NewRawElement(dicomcore.TagPixelData, dicomcore.VROB, []byte{0, 64, 128, 255}),
	}
	file := &object.File{
		Dataset:        object.FromElements(elements, std.Dictionary),
		TransferSyntax: transfer.ExplicitVRLittleEndian,
	}
	var buf bytes.Buffer
	if err := object.WriteFile(&buf, file); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func webUSElement(tag dicomcore.Tag, value uint16) dicomcore.Element {
	var raw [2]byte
	binary.LittleEndian.PutUint16(raw[:], value)
	return dicomcore.NewRawElement(tag, dicomcore.VRUS, raw[:])
}
