package export

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
)

func TestWriteSeriesCSV(t *testing.T) {
	var buf bytes.Buffer
	err := WriteSeriesCSV(&buf, []archive.Series{{
		StudyInstanceUID:  "1.2.3",
		SeriesInstanceUID: "1.2.3.4",
		Modality:          "CT",
		SeriesNumber:      "7",
		SeriesDescription: "Head, contrast",
		InstanceCount:     9,
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.HasPrefix(got, "study_instance_uid,series_instance_uid,modality,series_number,series_description,instance_count,imported_at\n") {
		t.Fatalf("CSV header mismatch:\n%s", got)
	}
	if !strings.Contains(got, `"Head, contrast"`) {
		t.Fatalf("CSV did not escape description:\n%s", got)
	}
	if !strings.Contains(got, "1.2.3.4") {
		t.Fatalf("CSV missing series UID:\n%s", got)
	}
}

func TestWriteSeriesJSON(t *testing.T) {
	importedAt := time.Date(2026, 6, 4, 12, 34, 56, 789, time.UTC)
	var buf bytes.Buffer
	err := WriteSeriesJSON(&buf, []archive.Series{{
		StudyInstanceUID:  "1.2.3",
		SeriesInstanceUID: "1.2.3.4",
		Modality:          "MR",
		SeriesNumber:      "11",
		SeriesDescription: "Spine",
		InstanceCount:     12,
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
	if record["series_instance_uid"] != "1.2.3.4" || record["series_description"] != "Spine" {
		t.Fatalf("record identity fields = %#v", record)
	}
	if record["instance_count"] != float64(12) {
		t.Fatalf("instance_count = %#v", record["instance_count"])
	}
	if record["imported_at"] != "2026-06-04T12:34:56.000000789Z" {
		t.Fatalf("imported_at = %#v", record["imported_at"])
	}
}
