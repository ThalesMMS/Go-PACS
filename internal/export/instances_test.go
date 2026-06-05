package export

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
)

func TestWriteInstancesCSV(t *testing.T) {
	var buf bytes.Buffer
	err := WriteInstancesCSV(&buf, []archive.Instance{{
		SHA256:            "abc123",
		SourcePath:        "source, one.dcm",
		FileSize:          42,
		PatientName:       "INSTANCE^PATIENT",
		PatientID:         "I001",
		StudyDate:         "20260604",
		StudyDescription:  "Brain",
		Modality:          "CT",
		AccessionNumber:   "ACC3",
		StudyInstanceUID:  "1.2.3",
		SeriesInstanceUID: "1.2.3.4",
		SeriesNumber:      "2",
		SeriesDescription: "Axial",
		SOPClassUID:       "1.2.840.10008.5.1.4.1.1.2",
		SOPInstanceUID:    "1.2.3.4.5",
		InstanceNumber:    "8",
		TransferSyntaxUID: "1.2.840.10008.1.2.1",
		TransferSyntax:    "Explicit VR Little Endian",
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.HasPrefix(got, "sha256,source_path,file_size,patient_id,patient_name,study_date,study_description,modality,accession_number,study_instance_uid,series_instance_uid,series_number,series_description,sop_class_uid,sop_instance_uid,instance_number,transfer_syntax_uid,transfer_syntax,imported_at\n") {
		t.Fatalf("CSV header mismatch:\n%s", got)
	}
	if !strings.Contains(got, `"source, one.dcm"`) {
		t.Fatalf("CSV did not escape source path:\n%s", got)
	}
	if !strings.Contains(got, "1.2.3.4.5") {
		t.Fatalf("CSV missing SOP Instance UID:\n%s", got)
	}
}

func TestWriteInstancesJSON(t *testing.T) {
	importedAt := time.Date(2026, 6, 4, 12, 34, 56, 789, time.UTC)
	var buf bytes.Buffer
	err := WriteInstancesJSON(&buf, []archive.Instance{{
		SHA256:            "def456",
		SourcePath:        "source.dcm",
		FileSize:          64,
		PatientName:       "JSON^INSTANCE",
		PatientID:         "I002",
		StudyDate:         "20260604",
		StudyDescription:  "Chest",
		Modality:          "MR",
		AccessionNumber:   "ACC4",
		StudyInstanceUID:  "1.2.3",
		SeriesInstanceUID: "1.2.3.4",
		SeriesNumber:      "3",
		SeriesDescription: "Coronal",
		SOPClassUID:       "1.2.840.10008.5.1.4.1.1.4",
		SOPInstanceUID:    "1.2.3.4.6",
		InstanceNumber:    "9",
		TransferSyntaxUID: "1.2.840.10008.1.2.1",
		TransferSyntax:    "Explicit VR Little Endian",
		ImportedAt:        importedAt,
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
	if record["sop_instance_uid"] != "1.2.3.4.6" || record["transfer_syntax"] != "Explicit VR Little Endian" {
		t.Fatalf("record identity fields = %#v", record)
	}
	if record["file_size"] != float64(64) {
		t.Fatalf("file_size = %#v", record["file_size"])
	}
	if record["imported_at"] != "2026-06-04T12:34:56.000000789Z" {
		t.Fatalf("imported_at = %#v", record["imported_at"])
	}
}
