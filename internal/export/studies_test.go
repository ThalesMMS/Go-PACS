package export

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
)

func TestWriteStudiesCSV(t *testing.T) {
	var buf bytes.Buffer
	err := WriteStudiesCSV(&buf, []archive.Study{{
		StudyInstanceUID: "1.2.3",
		PatientID:        "P001",
		PatientName:      "Last, \"First\"",
		StudyDate:        "20260604",
		StudyDescription: "Abdomen",
		Modalities:       "CT\\MR",
		AccessionNumber:  "ACC1",
		SeriesCount:      1,
		InstanceCount:    2,
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.HasPrefix(got, "study_instance_uid,patient_id,patient_name,study_date,study_description,modalities,accession_number,series_count,instance_count,imported_at\n") {
		t.Fatalf("CSV header mismatch:\n%s", got)
	}
	if !strings.Contains(got, `"Last, ""First"""`) {
		t.Fatalf("CSV did not escape quoted patient name:\n%s", got)
	}
	if !strings.Contains(got, "1.2.3") {
		t.Fatalf("CSV missing study UID:\n%s", got)
	}
}

func TestWriteStudiesJSON(t *testing.T) {
	importedAt := time.Date(2026, 6, 4, 12, 34, 56, 789, time.UTC)
	var buf bytes.Buffer
	err := WriteStudiesJSON(&buf, []archive.Study{{
		StudyInstanceUID: "1.2.3",
		PatientID:        "P001",
		PatientName:      "JSON^PATIENT",
		StudyDate:        "20260604",
		StudyDescription: "Chest",
		Modalities:       "CT",
		AccessionNumber:  "ACC2",
		SeriesCount:      3,
		InstanceCount:    4,
		ImportedAt:       importedAt,
	}})
	if err != nil {
		t.Fatal(err)
	}
	var records []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &records); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if len(records) != 1 {
		t.Fatalf("records len = %d, want 1", len(records))
	}
	record := records[0]
	if record["study_instance_uid"] != "1.2.3" || record["patient_name"] != "JSON^PATIENT" {
		t.Fatalf("record identity fields = %#v", record)
	}
	if record["series_count"] != float64(3) || record["instance_count"] != float64(4) {
		t.Fatalf("record counts = %#v", record)
	}
	if record["imported_at"] != "2026-06-04T12:34:56.000000789Z" {
		t.Fatalf("imported_at = %#v", record["imported_at"])
	}
}
