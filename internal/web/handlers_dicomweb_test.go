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
	"net/textproto"
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

func TestDICOMwebProtectedRoutesRequireBearerToken(t *testing.T) {
	s := newTestServer(t)
	for _, tt := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "qido studies", method: http.MethodGet, path: "/dicomweb/studies"},
		{name: "qido series", method: http.MethodGet, path: "/dicomweb/studies/1.2.3/series"},
		{name: "qido instances", method: http.MethodGet, path: "/dicomweb/studies/1.2.3/series/4.5.6/instances"},
		{name: "wado study metadata", method: http.MethodGet, path: "/dicomweb/studies/1.2.3/metadata"},
		{name: "wado series metadata", method: http.MethodGet, path: "/dicomweb/studies/1.2.3/series/4.5.6/metadata"},
		{name: "wado instance metadata", method: http.MethodGet, path: "/dicomweb/studies/1.2.3/series/4.5.6/instances/7.8.9/metadata"},
		{name: "wado study object", method: http.MethodGet, path: "/dicomweb/studies/1.2.3"},
		{name: "wado series objects", method: http.MethodGet, path: "/dicomweb/studies/1.2.3/series/4.5.6"},
		{name: "wado instance object", method: http.MethodGet, path: "/dicomweb/studies/1.2.3/series/4.5.6/instances/7.8.9"},
		{name: "stow studies", method: http.MethodPost, path: "/dicomweb/studies"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s missing token status = %d, want 401", tt.method, tt.path, rec.Code)
			}
		})
	}

	readToken := createDICOMwebToken(t, s, tokens.RoleRead)
	req := httptest.NewRequest(http.MethodPost, "/dicomweb/studies", nil)
	req.Header.Set("Authorization", "Bearer "+readToken)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("STOW read-token status = %d, want 403", rec.Code)
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

func TestDICOMwebCapabilitiesAdvertisesSupportedSurface(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dicomweb/capabilities", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("capabilities status = %d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode capabilities: %v body=%s", err, rec.Body.String())
	}
	if body["basePath"] != "/dicomweb" {
		t.Fatalf("basePath = %v, want /dicomweb", body["basePath"])
	}
	auth, ok := body["authentication"].(map[string]any)
	if !ok {
		t.Fatalf("authentication missing in %#v", body)
	}
	if auth["required"] != true || auth["scheme"] != "Bearer" || auth["capabilitiesEndpointRequiresAuth"] != false {
		t.Fatalf("authentication = %#v", auth)
	}
	transactions := capabilitiesStrings(t, body, "transactions", "name")
	for _, want := range []string{"QIDO-RS", "WADO-RS metadata", "WADO-RS retrieval", "STOW-RS"} {
		if !containsString(transactions, want) {
			t.Fatalf("transactions = %v, missing %q", transactions, want)
		}
	}
	unsupported := capabilitiesStrings(t, body, "unsupported", "")
	for _, want := range []string{"WADO-URI", "WADO-WS", "UPS-RS", "rendered JPEG/PNG retrieval", "enterprise IAM integration"} {
		if !containsString(unsupported, want) {
			t.Fatalf("unsupported = %v, missing %q", unsupported, want)
		}
	}
	mediaTypes, ok := body["mediaTypes"].(map[string]any)
	if !ok {
		t.Fatalf("mediaTypes missing in %#v", body)
	}
	if mediaTypes["dicomJSON"] != "application/dicom+json" || mediaTypes["multipartDICOM"] != `multipart/related; type="application/dicom"` {
		t.Fatalf("mediaTypes = %#v", mediaTypes)
	}
	limits, ok := body["limits"].(map[string]any)
	if !ok || limits["maxFileImportBytes"] == nil || limits["maxStoreObjectBytes"] == nil {
		t.Fatalf("limits = %#v", body["limits"])
	}
}

func TestDICOMwebSTOWRequiresWriteToken(t *testing.T) {
	s := newTestServer(t)
	readToken := createDICOMwebToken(t, s, tokens.RoleRead)
	validPart := webPart10(t, webPart10Options{
		StudyUID:  "1.2.826.0.1.3680043.10.543.3701",
		SeriesUID: "1.2.826.0.1.3680043.10.543.3701.1",
		SOPUID:    "1.2.826.0.1.3680043.10.543.3701.1.1",
		PatientID: "STOW-AUTH",
		StudyDate: "20260611",
		Modality:  "CT",
	})

	rec := doDICOMwebSTOW(t, s, "", dicomwebStorePart{ContentType: "application/dicom", Data: validPart})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want 401", rec.Code)
	}

	rec = doDICOMwebSTOW(t, s, "invalid", dicomwebStorePart{ContentType: "application/dicom", Data: validPart})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token status = %d, want 401", rec.Code)
	}

	rec = doDICOMwebSTOW(t, s, readToken, dicomwebStorePart{ContentType: "application/dicom", Data: validPart})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read token status = %d, want 403", rec.Code)
	}
}

func TestDICOMwebSTOWStoresValidAndDuplicateObjects(t *testing.T) {
	s := newTestServer(t)
	token := createDICOMwebToken(t, s, tokens.RoleWrite)
	studyUID := "1.2.826.0.1.3680043.10.543.3702"
	seriesUID := studyUID + ".1"
	sopUID := seriesUID + ".1"
	data := webPart10(t, webPart10Options{
		StudyUID:  studyUID,
		SeriesUID: seriesUID,
		SOPUID:    sopUID,
		PatientID: "STOW-STORE",
		StudyDate: "20260612",
		Modality:  "CT",
	})

	rec := doDICOMwebSTOW(t, s, token, dicomwebStorePart{ContentType: "application/dicom", Data: data})
	if rec.Code != http.StatusOK {
		t.Fatalf("store status = %d body=%s", rec.Code, rec.Body.String())
	}
	dataset := decodeSingleDICOMJSONDataset(t, rec)
	assertDICOMJSONSequenceLen(t, dataset, "00081199", 1)
	assertDICOMJSONSequenceUID(t, dataset, "00081199", 0, "00081155", sopUID)
	if _, ok := dataset["00081198"]; ok {
		t.Fatalf("unexpected failed sequence: %#v", dataset["00081198"])
	}
	if _, err := s.session.Catalog().InstanceBySOPInstanceUID(context.Background(), sopUID); err != nil {
		t.Fatalf("stored instance lookup: %v", err)
	}

	rec = doDICOMwebSTOW(t, s, token, dicomwebStorePart{ContentType: "application/dicom", Data: data})
	if rec.Code != http.StatusOK {
		t.Fatalf("duplicate status = %d body=%s", rec.Code, rec.Body.String())
	}
	dataset = decodeSingleDICOMJSONDataset(t, rec)
	assertDICOMJSONSequenceLen(t, dataset, "00081199", 1)
	assertDICOMJSONSequenceUID(t, dataset, "00081199", 0, "00081155", sopUID)
	if _, ok := dataset["00081198"]; ok {
		t.Fatalf("duplicate reported failed sequence: %#v", dataset["00081198"])
	}
}

func TestDICOMwebSTOWReportsFailedItems(t *testing.T) {
	s := newTestServer(t)
	token := createDICOMwebToken(t, s, tokens.RoleWrite)
	studyUID := "1.2.826.0.1.3680043.10.543.3703"
	seriesUID := studyUID + ".1"
	sopUID := seriesUID + ".1"

	rec := doDICOMwebSTOW(t, s, token,
		dicomwebStorePart{ContentType: "application/dicom", Data: webPart10(t, webPart10Options{
			StudyUID:  studyUID,
			SeriesUID: seriesUID,
			SOPUID:    sopUID,
			PatientID: "STOW-PARTIAL",
			StudyDate: "20260613",
			Modality:  "CT",
		})},
		dicomwebStorePart{ContentType: "application/dicom", Data: []byte("not dicom")},
	)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("partial status = %d body=%s, want 202", rec.Code, rec.Body.String())
	}
	dataset := decodeSingleDICOMJSONDataset(t, rec)
	assertDICOMJSONSequenceLen(t, dataset, "00081199", 1)
	assertDICOMJSONSequenceUID(t, dataset, "00081199", 0, "00081155", sopUID)
	assertDICOMJSONSequenceLen(t, dataset, "00081198", 1)
	assertDICOMJSONSequenceTag(t, dataset, "00081198", 0, "00081197")
}

func TestDICOMwebSTOWReportsOversizedObjects(t *testing.T) {
	s := newTestServer(t)
	token := createDICOMwebToken(t, s, tokens.RoleWrite)
	sopUID := "1.2.826.0.1.3680043.10.543.3704.1.1"
	data := webPart10(t, webPart10Options{
		StudyUID:  "1.2.826.0.1.3680043.10.543.3704",
		SeriesUID: "1.2.826.0.1.3680043.10.543.3704.1",
		SOPUID:    sopUID,
		PatientID: "STOW-SIZE",
		StudyDate: "20260614",
		Modality:  "CT",
	})
	cfg, err := s.session.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	maxBytes := int64(len(data) - 1)
	cfg.MaxFileImportBytes = &maxBytes
	if err := s.session.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	rec := doDICOMwebSTOW(t, s, token, dicomwebStorePart{ContentType: "application/dicom", Data: data})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("oversized status = %d body=%s, want 202", rec.Code, rec.Body.String())
	}
	dataset := decodeSingleDICOMJSONDataset(t, rec)
	assertDICOMJSONSequenceLen(t, dataset, "00081198", 1)
	assertDICOMJSONSequenceUID(t, dataset, "00081198", 0, "00081155", sopUID)
	assertDICOMJSONSequenceTag(t, dataset, "00081198", 0, "00081197")
	if _, err := s.session.Catalog().InstanceBySOPInstanceUID(context.Background(), sopUID); err == nil {
		t.Fatalf("oversized object was stored")
	}
}

func TestDICOMwebSTOWRejectsMalformedMultipart(t *testing.T) {
	s := newTestServer(t)
	token := createDICOMwebToken(t, s, tokens.RoleWrite)
	req := httptest.NewRequest(http.MethodPost, "/dicomweb/studies", strings.NewReader("not multipart"))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", `multipart/related; type="application/dicom"`)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed multipart status = %d body=%s, want 400", rec.Code, rec.Body.String())
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

func capabilitiesStrings(t *testing.T, body map[string]any, key string, itemKey string) []string {
	t.Helper()
	values, ok := body[key].([]any)
	if !ok {
		t.Fatalf("%s missing in %#v", key, body)
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if itemKey == "" {
			text, ok := value.(string)
			if !ok {
				t.Fatalf("%s item = %#v, want string", key, value)
			}
			out = append(out, text)
			continue
		}
		item, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s item = %#v, want object", key, value)
		}
		text, ok := item[itemKey].(string)
		if !ok {
			t.Fatalf("%s item %s = %#v, want string", key, itemKey, item[itemKey])
		}
		out = append(out, text)
	}
	return out
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type dicomwebStorePart struct {
	ContentType string
	Data        []byte
}

func doDICOMwebSTOW(t *testing.T, s *Server, token string, parts ...dicomwebStorePart) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, item := range parts {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Type", item.ContentType)
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(item.Data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/dicomweb/studies", &body)
	req.Header.Set("Content-Type", `multipart/related; type="application/dicom"; boundary=`+writer.Boundary())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
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

func decodeSingleDICOMJSONDataset(t *testing.T, rec *httptest.ResponseRecorder) map[string]dicomJSONElement {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/dicom+json") {
		t.Fatalf("Content-Type = %q, want application/dicom+json", ct)
	}
	var dataset map[string]dicomJSONElement
	if err := json.Unmarshal(rec.Body.Bytes(), &dataset); err != nil {
		t.Fatalf("decode DICOM JSON dataset: %v body=%s", err, rec.Body.String())
	}
	return dataset
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

func assertDICOMJSONSequenceLen(t *testing.T, dataset map[string]dicomJSONElement, tag string, want int) {
	t.Helper()
	element, ok := dataset[tag]
	if !ok {
		t.Fatalf("sequence %s missing in %#v", tag, dataset)
	}
	if element.VR != "SQ" {
		t.Fatalf("sequence %s VR = %q, want SQ", tag, element.VR)
	}
	if len(element.Value) != want {
		t.Fatalf("sequence %s length = %d, want %d", tag, len(element.Value), want)
	}
}

func assertDICOMJSONSequenceUID(t *testing.T, dataset map[string]dicomJSONElement, sequenceTag string, index int, tag string, want string) {
	t.Helper()
	value := dicomJSONSequenceItemTag(t, dataset, sequenceTag, index, tag)
	values, _ := value["Value"].([]any)
	if len(values) == 0 {
		t.Fatalf("sequence %s[%d] tag %s has no Value: %#v", sequenceTag, index, tag, value)
	}
	if got, _ := values[0].(string); got != want {
		t.Fatalf("sequence %s[%d] tag %s = %v, want %q", sequenceTag, index, tag, values, want)
	}
}

func assertDICOMJSONSequenceTag(t *testing.T, dataset map[string]dicomJSONElement, sequenceTag string, index int, tag string) {
	t.Helper()
	_ = dicomJSONSequenceItemTag(t, dataset, sequenceTag, index, tag)
}

func dicomJSONSequenceItemTag(t *testing.T, dataset map[string]dicomJSONElement, sequenceTag string, index int, tag string) map[string]any {
	t.Helper()
	element, ok := dataset[sequenceTag]
	if !ok || index < 0 || index >= len(element.Value) {
		t.Fatalf("sequence %s[%d] unavailable in %#v", sequenceTag, index, dataset)
	}
	item, ok := element.Value[index].(map[string]any)
	if !ok {
		t.Fatalf("sequence %s[%d] = %#v, want object", sequenceTag, index, element.Value[index])
	}
	value, ok := item[tag].(map[string]any)
	if !ok {
		t.Fatalf("sequence %s[%d] tag %s missing in %#v", sequenceTag, index, tag, item)
	}
	return value
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
