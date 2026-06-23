package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ThalesMMS/Go-PACS/internal/tokens"
	dicomcore "github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/transfer"
)

func TestDICOMwebQIDORequiresBearerToken(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dicomweb/studies", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want 401", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/dicomweb/studies", nil)
	req.Header.Set("Authorization", "Bearer invalid")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token status = %d, want 401", rec.Code)
	}
}

func TestDICOMwebQIDOStudiesSeriesAndInstances(t *testing.T) {
	s := newTestServer(t)
	token := createDICOMwebToken(t, s, tokens.RoleRead)
	studyUID := "1.2.826.0.1.3680043.10.543.3501"
	seriesUID := studyUID + ".1"
	sopUID := seriesUID + ".1"
	importWebPart10(t, s, webPart10Options{
		StudyUID:     studyUID,
		SeriesUID:    seriesUID,
		SOPUID:       sopUID,
		PatientName:  "QIDO^PATIENT",
		PatientID:    "Q001",
		StudyDate:    "20260605",
		Modality:     "CT",
		Accession:    "ACC-QIDO",
		SeriesNumber: "7",
		InstanceNo:   "3",
	})

	studyRec := doDICOMwebGet(t, s, "/dicomweb/studies", token)
	if studyRec.Code != http.StatusOK {
		t.Fatalf("studies status = %d body=%s", studyRec.Code, studyRec.Body.String())
	}
	studies := decodeDICOMJSONDatasets(t, studyRec)
	if len(studies) != 1 {
		t.Fatalf("studies = %d, want 1", len(studies))
	}
	assertDICOMJSONValue(t, studies[0], "0020000D", studyUID)
	assertDICOMJSONValue(t, studies[0], "00100020", "Q001")
	assertDICOMJSONValue(t, studies[0], "00080061", "CT")
	assertDICOMJSONValue(t, studies[0], "00201208", "1")

	seriesRec := doDICOMwebGet(t, s, "/dicomweb/studies/"+studyUID+"/series", token)
	if seriesRec.Code != http.StatusOK {
		t.Fatalf("series status = %d body=%s", seriesRec.Code, seriesRec.Body.String())
	}
	series := decodeDICOMJSONDatasets(t, seriesRec)
	if len(series) != 1 {
		t.Fatalf("series = %d, want 1", len(series))
	}
	assertDICOMJSONValue(t, series[0], "0020000E", seriesUID)
	assertDICOMJSONValue(t, series[0], "00080060", "CT")
	assertDICOMJSONValue(t, series[0], "00200011", "7")

	instanceRec := doDICOMwebGet(t, s, "/dicomweb/studies/"+studyUID+"/series/"+seriesUID+"/instances", token)
	if instanceRec.Code != http.StatusOK {
		t.Fatalf("instances status = %d body=%s", instanceRec.Code, instanceRec.Body.String())
	}
	instances := decodeDICOMJSONDatasets(t, instanceRec)
	if len(instances) != 1 {
		t.Fatalf("instances = %d, want 1", len(instances))
	}
	assertDICOMJSONValue(t, instances[0], "00080018", sopUID)
	assertDICOMJSONValue(t, instances[0], "00080016", "1.2.840.10008.5.1.4.1.1.2")
	assertDICOMJSONValue(t, instances[0], "00200013", "3")
}

func TestDICOMwebQIDOFiltersAndRejectsUnsupportedParams(t *testing.T) {
	s := newTestServer(t)
	token := createDICOMwebToken(t, s, tokens.RoleRead)
	importWebPart10(t, s, webPart10Options{
		StudyUID:    "1.2.826.0.1.3680043.10.543.3502",
		SeriesUID:   "1.2.826.0.1.3680043.10.543.3502.1",
		SOPUID:      "1.2.826.0.1.3680043.10.543.3502.1.1",
		PatientName: "FILTER^MATCH",
		PatientID:   "MATCH",
		StudyDate:   "20260606",
		Modality:    "MR",
	})
	importWebPart10(t, s, webPart10Options{
		StudyUID:    "1.2.826.0.1.3680043.10.543.3503",
		SeriesUID:   "1.2.826.0.1.3680043.10.543.3503.1",
		SOPUID:      "1.2.826.0.1.3680043.10.543.3503.1.1",
		PatientName: "FILTER^MISS",
		PatientID:   "MISS",
		StudyDate:   "20260607",
		Modality:    "CT",
	})

	rec := doDICOMwebGet(t, s, "/dicomweb/studies?PatientID=MATCH&StudyDate=20260606-20260606&ModalitiesInStudy=MR", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("filtered status = %d body=%s", rec.Code, rec.Body.String())
	}
	studies := decodeDICOMJSONDatasets(t, rec)
	if len(studies) != 1 {
		t.Fatalf("filtered studies = %d, want 1", len(studies))
	}
	assertDICOMJSONValue(t, studies[0], "00100020", "MATCH")

	rec = doDICOMwebGet(t, s, "/dicomweb/studies?IssuerOfPatientID=unsupported", token)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unsupported QIDO parameter") {
		t.Fatalf("unsupported status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDICOMwebQIDOEmptyResultsUseNoContent(t *testing.T) {
	s := newTestServer(t)
	token := createDICOMwebToken(t, s, tokens.RoleRead)
	rec := doDICOMwebGet(t, s, "/dicomweb/studies?PatientID=NOPE", token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("empty status = %d body=%q, want 204", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "" {
		t.Fatalf("204 body = %q, want empty", rec.Body.String())
	}
}

func TestDICOMwebWADORequiresBearerToken(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dicomweb/studies/1.2.3/metadata", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want 401", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/dicomweb/studies/1.2.3/metadata", nil)
	req.Header.Set("Authorization", "Bearer invalid")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token status = %d, want 401", rec.Code)
	}
}

func TestDICOMwebWADOMetadataWithoutPixelData(t *testing.T) {
	s := newTestServer(t)
	token := createDICOMwebToken(t, s, tokens.RoleRead)
	studyUID := "1.2.826.0.1.3680043.10.543.3601"
	seriesUID := studyUID + ".1"
	sopUID := seriesUID + ".1"
	importWebPart10(t, s, webPart10Options{
		StudyUID:  studyUID,
		SeriesUID: seriesUID,
		SOPUID:    sopUID,
		PatientID: "WADO-META",
		StudyDate: "20260608",
		Modality:  "CT",
	})

	for _, path := range []string{
		"/dicomweb/studies/" + studyUID + "/metadata",
		"/dicomweb/studies/" + studyUID + "/series/" + seriesUID + "/metadata",
		"/dicomweb/studies/" + studyUID + "/series/" + seriesUID + "/instances/" + sopUID + "/metadata",
	} {
		rec := doDICOMwebGet(t, s, path, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
		datasets := decodeDICOMJSONDatasets(t, rec)
		if len(datasets) != 1 {
			t.Fatalf("%s datasets = %d, want 1", path, len(datasets))
		}
		assertDICOMJSONValue(t, datasets[0], "00080018", sopUID)
		if _, ok := datasets[0]["7FE00010"]; ok {
			t.Fatalf("%s metadata contains Pixel Data: %#v", path, datasets[0]["7FE00010"])
		}
	}
}

func TestDICOMwebWADORetrieveStudySeriesAndInstanceObjects(t *testing.T) {
	s := newTestServer(t)
	token := createDICOMwebToken(t, s, tokens.RoleRead)
	studyUID := "1.2.826.0.1.3680043.10.543.3602"
	firstSeriesUID := studyUID + ".1"
	secondSeriesUID := studyUID + ".2"
	firstSOPUID := firstSeriesUID + ".1"
	secondSOPUID := secondSeriesUID + ".1"
	importWebPart10(t, s, webPart10Options{StudyUID: studyUID, SeriesUID: firstSeriesUID, SOPUID: firstSOPUID, PatientID: "WADO-OBJ", StudyDate: "20260609", Modality: "CT"})
	importWebPart10(t, s, webPart10Options{StudyUID: studyUID, SeriesUID: secondSeriesUID, SOPUID: secondSOPUID, PatientID: "WADO-OBJ", StudyDate: "20260609", Modality: "MR"})

	studyRec := doDICOMwebGet(t, s, "/dicomweb/studies/"+studyUID, token)
	if studyRec.Code != http.StatusOK {
		t.Fatalf("study retrieve status = %d body=%s", studyRec.Code, studyRec.Body.String())
	}
	studyParts := decodeDICOMMultipart(t, studyRec)
	if !sameStrings(studyParts, []string{firstSOPUID, secondSOPUID}) {
		t.Fatalf("study multipart SOPs = %v", studyParts)
	}

	seriesRec := doDICOMwebGet(t, s, "/dicomweb/studies/"+studyUID+"/series/"+firstSeriesUID, token)
	if seriesRec.Code != http.StatusOK {
		t.Fatalf("series retrieve status = %d body=%s", seriesRec.Code, seriesRec.Body.String())
	}
	seriesParts := decodeDICOMMultipart(t, seriesRec)
	if !sameStrings(seriesParts, []string{firstSOPUID}) {
		t.Fatalf("series multipart SOPs = %v", seriesParts)
	}

	instanceRec := doDICOMwebGet(t, s, "/dicomweb/studies/"+studyUID+"/series/"+secondSeriesUID+"/instances/"+secondSOPUID, token)
	if instanceRec.Code != http.StatusOK {
		t.Fatalf("instance retrieve status = %d body=%s", instanceRec.Code, instanceRec.Body.String())
	}
	if ct := instanceRec.Header().Get("Content-Type"); ct != "application/dicom" {
		t.Fatalf("instance Content-Type = %q, want application/dicom", ct)
	}
	file, err := object.ReadFile(bytes.NewReader(instanceRec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if sop, ok := file.Dataset.GetUID(dicomcore.NewTag(0x0008, 0x0018)); !ok || sop != secondSOPUID {
		t.Fatalf("instance response SOP = %q ok=%v", sop, ok)
	}
}

func TestDICOMwebWADOMissingStoredFileDoesNotLeakPath(t *testing.T) {
	s := newTestServer(t)
	token := createDICOMwebToken(t, s, tokens.RoleRead)
	studyUID := "1.2.826.0.1.3680043.10.543.3603"
	seriesUID := studyUID + ".1"
	sopUID := seriesUID + ".1"
	importWebPart10(t, s, webPart10Options{StudyUID: studyUID, SeriesUID: seriesUID, SOPUID: sopUID, PatientID: "WADO-MISSING", StudyDate: "20260610", Modality: "CT"})
	instance, err := s.session.Catalog().InstanceBySOPInstanceUID(context.Background(), sopUID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(instance.StoredPath); err != nil {
		t.Fatal(err)
	}

	rec := doDICOMwebGet(t, s, "/dicomweb/studies/"+studyUID+"/series/"+seriesUID+"/instances/"+sopUID, token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing file status = %d body=%s, want 404", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), instance.StoredPath) {
		t.Fatalf("missing file response leaked path %q: %s", instance.StoredPath, rec.Body.String())
	}
}

func createDICOMwebToken(t *testing.T, s *Server, role string) string {
	t.Helper()
	_, plaintext, err := s.session.TokenStore().Create(tokens.Draft{Name: "qido-test", Role: role})
	if err != nil {
		t.Fatal(err)
	}
	return plaintext
}

func doDICOMwebGet(t *testing.T, s *Server, path string, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func decodeDICOMJSONDatasets(t *testing.T, rec *httptest.ResponseRecorder) []map[string]dicomJSONElement {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/dicom+json") {
		t.Fatalf("Content-Type = %q, want application/dicom+json", ct)
	}
	var datasets []map[string]dicomJSONElement
	if err := json.Unmarshal(rec.Body.Bytes(), &datasets); err != nil {
		t.Fatalf("decode DICOM JSON: %v body=%s", err, rec.Body.String())
	}
	return datasets
}

type dicomJSONElement struct {
	VR    string `json:"vr"`
	Value []any  `json:"Value,omitempty"`
}

func assertDICOMJSONValue(t *testing.T, dataset map[string]dicomJSONElement, tag string, want string) {
	t.Helper()
	element, ok := dataset[tag]
	if !ok || len(element.Value) == 0 {
		t.Fatalf("tag %s missing in %#v", tag, dataset)
	}
	if got, _ := element.Value[0].(string); got != want {
		t.Fatalf("tag %s value = %v, want %q", tag, element.Value, want)
	}
}

func decodeDICOMMultipart(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(rec.Header().Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse multipart Content-Type: %v", err)
	}
	if mediaType != "multipart/related" || params["type"] != "application/dicom" || params["boundary"] == "" {
		t.Fatalf("Content-Type = %q params=%v", mediaType, params)
	}
	reader := multipart.NewReader(bytes.NewReader(rec.Body.Bytes()), params["boundary"])
	var sops []string
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("NextPart: %v", err)
		}
		if got := part.Header.Get("Content-Type"); got != "application/dicom" {
			t.Fatalf("part Content-Type = %q, want application/dicom", got)
		}
		data, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		file, err := object.ReadFile(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		sop, ok := file.Dataset.GetUID(dicomcore.NewTag(0x0008, 0x0018))
		if !ok || sop == "" {
			t.Fatalf("multipart part missing SOP Instance UID")
		}
		sops = append(sops, sop)
	}
	return sops
}

func sameStrings(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

type webPart10Options struct {
	StudyUID     string
	SeriesUID    string
	SOPUID       string
	PatientName  string
	PatientID    string
	StudyDate    string
	Modality     string
	Accession    string
	SeriesNumber string
	InstanceNo   string
}

func importWebPart10(t *testing.T, s *Server, opts webPart10Options) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "qido.dcm")
	if err := os.WriteFile(path, webPart10(t, opts), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := s.session.Catalog().ImportPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if report.StoredFiles != 1 {
		t.Fatalf("StoredFiles = %d, want 1", report.StoredFiles)
	}
}

func webPart10(t *testing.T, opts webPart10Options) []byte {
	t.Helper()
	if opts.Accession == "" {
		opts.Accession = "ACC-WEB"
	}
	if opts.SeriesNumber == "" {
		opts.SeriesNumber = "1"
	}
	if opts.InstanceNo == "" {
		opts.InstanceNo = "1"
	}
	file := &object.File{
		Dataset: object.FromElements([]dicomcore.Element{
			webStringElement(dicomcore.NewTag(0x0008, 0x0016), dicomcore.VRUI, "1.2.840.10008.5.1.4.1.1.2"),
			webStringElement(dicomcore.NewTag(0x0008, 0x0018), dicomcore.VRUI, opts.SOPUID),
			webStringElement(dicomcore.NewTag(0x0010, 0x0010), dicomcore.VRPN, opts.PatientName),
			webStringElement(dicomcore.NewTag(0x0010, 0x0020), dicomcore.VRLO, opts.PatientID),
			webStringElement(dicomcore.NewTag(0x0008, 0x0020), dicomcore.VRDA, opts.StudyDate),
			webStringElement(dicomcore.NewTag(0x0008, 0x0050), dicomcore.VRSH, opts.Accession),
			webStringElement(dicomcore.NewTag(0x0008, 0x0060), dicomcore.VRCS, opts.Modality),
			webStringElement(dicomcore.NewTag(0x0020, 0x000D), dicomcore.VRUI, opts.StudyUID),
			webStringElement(dicomcore.NewTag(0x0020, 0x000E), dicomcore.VRUI, opts.SeriesUID),
			webStringElement(dicomcore.NewTag(0x0020, 0x0011), dicomcore.VRIS, opts.SeriesNumber),
			webStringElement(dicomcore.NewTag(0x0020, 0x0013), dicomcore.VRIS, opts.InstanceNo),
		}, std.Dictionary),
		TransferSyntax: transfer.ExplicitVRLittleEndian,
	}
	var buf bytes.Buffer
	if err := object.WriteFile(&buf, file); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func webStringElement(tag dicomcore.Tag, vr dicomcore.VR, value string) dicomcore.Element {
	return dicomcore.Element{
		Header: dicomcore.ElementHeader{Tag: tag, VR: vr},
		Value:  dicomcore.StringValue{value},
	}
}
