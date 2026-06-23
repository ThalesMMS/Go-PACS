package qido

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/query"
	"github.com/ThalesMMS/dicom-go/net/dimse"
)

func TestStudyQueryMapsDICOMJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dicom-web/qido-rs/studies" {
			t.Fatalf("path = %q, want /dicom-web/qido-rs/studies", r.URL.Path)
		}
		if got := r.URL.Query().Get("PatientID"); got != "P001" {
			t.Fatalf("PatientID query = %q, want P001", got)
		}
		if got := r.URL.Query().Get("StudyDate"); got != "20260101-20260131" {
			t.Fatalf("StudyDate query = %q, want range", got)
		}
		if got := r.URL.Query().Get("ModalitiesInStudy"); got != "CT" {
			t.Fatalf("ModalitiesInStudy query = %q, want CT", got)
		}
		if got := r.URL.Query().Get("limit"); got != "10" {
			t.Fatalf("limit query = %q, want 10", got)
		}
		if got := r.URL.Query().Get("StudyID"); got != "SID-1" {
			t.Fatalf("StudyID query = %q, want SID-1", got)
		}
		w.Header().Set("Content-Type", "application/dicom+json")
		_, _ = w.Write([]byte(`[{
			"00100010":{"vr":"PN","Value":[{"Alphabetic":"DOE^JANE"}]},
			"00100020":{"vr":"LO","Value":["P001"]},
			"00100030":{"vr":"DA","Value":["19700101"]},
			"00080020":{"vr":"DA","Value":["20260115"]},
			"00080030":{"vr":"TM","Value":["101112"]},
			"00201208":{"vr":"IS","Value":[12]},
			"00081030":{"vr":"LO","Value":["CT chest"]},
			"00080050":{"vr":"SH","Value":["ACC-1"]},
			"00080090":{"vr":"PN","Value":[{"Alphabetic":"REF^DOC"}]},
			"00080080":{"vr":"LO","Value":["General Hospital"]},
			"00104000":{"vr":"LT","Value":["follow-up"]},
			"0032000A":{"vr":"CS","Value":["VERIFIED"]},
			"0020000D":{"vr":"UI","Value":["1.2.3.study"]},
			"00080061":{"vr":"CS","Value":["CT","MR"]}
		}]`))
	}))
	defer server.Close()

	node := testDICOMwebNode(server.URL)
	result, err := StudyQuery(context.Background(), node, query.Criteria{
		PatientID:          "P001",
		StudyDateFrom:      "20260101",
		StudyDateTo:        "20260131",
		Modality:           "CT",
		CustomFieldKeyword: "StudyID",
		CustomFieldValue:   "SID-1",
		MaxResults:         10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalStatus != dimse.StatusSuccess {
		t.Fatalf("FinalStatus = 0x%04X, want success", result.FinalStatus)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(result.Matches))
	}
	match := result.Matches[0]
	if match.QueryRetrieveLevel != dimse.QueryRetrieveLevelStudy || match.PatientName != "DOE^JANE" || match.PatientID != "P001" || match.PatientBirthDate != "19700101" {
		t.Fatalf("patient/study basics = %+v", match)
	}
	if match.StudyDate != "20260115" || match.StudyTime != "101112" || match.ImageCount != "12" || match.StudyDescription != "CT chest" {
		t.Fatalf("study display fields = %+v", match)
	}
	if match.AccessionNumber != "ACC-1" || match.ReferringPhysicianName != "REF^DOC" || match.InstitutionName != "General Hospital" || match.PatientComments != "follow-up" || match.StudyStatusID != "VERIFIED" {
		t.Fatalf("study metadata fields = %+v", match)
	}
	if match.StudyInstanceUID != "1.2.3.study" || match.Modalities != "CT\\MR" {
		t.Fatalf("study identity fields = %+v", match)
	}
}

func TestSeriesQueryMapsDICOMJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dicom-web/qido-rs/studies/1.2.3.study/series" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("Modality"); got != "CT" {
			t.Fatalf("Modality query = %q, want CT", got)
		}
		w.Header().Set("Content-Type", "application/dicom+json")
		_, _ = w.Write([]byte(`[{
			"00100010":{"vr":"PN","Value":[{"Alphabetic":"DOE^JANE"}]},
			"00100020":{"vr":"LO","Value":["P001"]},
			"00080020":{"vr":"DA","Value":["20260115"]},
			"0020000D":{"vr":"UI","Value":["1.2.3.study"]},
			"0020000E":{"vr":"UI","Value":["1.2.3.series"]},
			"00080060":{"vr":"CS","Value":["CT"]},
			"00200011":{"vr":"IS","Value":[7]},
			"0008103E":{"vr":"LO","Value":["Axial"]},
			"00201209":{"vr":"IS","Value":[42]}
		}]`))
	}))
	defer server.Close()

	result, err := SeriesQuery(context.Background(), testDICOMwebNode(server.URL), query.SeriesCriteria{
		StudyInstanceUID: "1.2.3.study",
		Modality:         "CT",
	})
	if err != nil {
		t.Fatal(err)
	}
	match := result.Matches[0]
	if match.QueryRetrieveLevel != dimse.QueryRetrieveLevelSeries || match.SeriesInstanceUID != "1.2.3.series" || match.Modality != "CT" || match.SeriesNumber != "7" || match.SeriesDescription != "Axial" || match.ImageCount != "42" {
		t.Fatalf("series match = %+v", match)
	}
}

func TestImageQueryMapsDICOMJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dicom-web/qido-rs/studies/1.2.3.study/series/1.2.3.series/instances" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/dicom+json")
		_, _ = w.Write([]byte(`[{
			"00100010":{"vr":"PN","Value":[{"Alphabetic":"DOE^JANE"}]},
			"00100020":{"vr":"LO","Value":["P001"]},
			"00080020":{"vr":"DA","Value":["20260115"]},
			"0020000D":{"vr":"UI","Value":["1.2.3.study"]},
			"0020000E":{"vr":"UI","Value":["1.2.3.series"]},
			"00080060":{"vr":"CS","Value":["CT"]},
			"00080016":{"vr":"UI","Value":["1.2.840.10008.5.1.4.1.1.2"]},
			"00080018":{"vr":"UI","Value":["1.2.3.instance"]},
			"00200013":{"vr":"IS","Value":[12]}
		}]`))
	}))
	defer server.Close()

	result, err := ImageQuery(context.Background(), testDICOMwebNode(server.URL), query.ImageCriteria{
		StudyInstanceUID:  "1.2.3.study",
		SeriesInstanceUID: "1.2.3.series",
		SOPInstanceUID:    "1.2.3.instance",
	})
	if err != nil {
		t.Fatal(err)
	}
	match := result.Matches[0]
	if match.QueryRetrieveLevel != dimse.QueryRetrieveLevelImage || match.SOPClassUID != "1.2.840.10008.5.1.4.1.1.2" || match.SOPInstanceUID != "1.2.3.instance" || match.InstanceNumber != "12" {
		t.Fatalf("image match = %+v", match)
	}
}

func TestStudyQueryReportsMalformedDICOMJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dicom+json")
		_, _ = w.Write([]byte(`{"not":"an array"}`))
	}))
	defer server.Close()

	_, err := StudyQuery(context.Background(), testDICOMwebNode(server.URL), query.Criteria{})
	if err == nil {
		t.Fatal("StudyQuery accepted malformed DICOM JSON")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Fatalf("error = %q, want decode detail", err.Error())
	}
}

func TestStudyQueryReportsHTTPAuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := StudyQuery(context.Background(), testDICOMwebNode(server.URL), query.Criteria{})
	if err == nil {
		t.Fatal("StudyQuery succeeded after HTTP 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %q, want HTTP status", err.Error())
	}
}

func TestStudyQueryReportsHTTPServerFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := StudyQuery(context.Background(), testDICOMwebNode(server.URL), query.Criteria{})
	if err == nil {
		t.Fatal("StudyQuery succeeded after HTTP 503")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Fatalf("error = %q, want HTTP status", err.Error())
	}
}

func testDICOMwebNode(baseURL string) nodes.Node {
	return nodes.Node{
		ID:             "web-1",
		Name:           "web",
		Protocol:       nodes.ProtocolDICOMweb,
		BaseURL:        baseURL + "/dicom-web",
		QIDOPathPrefix: "/qido-rs",
	}
}
