package export

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
)

func WriteSeriesCSV(w io.Writer, series []archive.Series) error {
	csvw := csv.NewWriter(w)
	if err := csvw.Write([]string{
		"study_instance_uid",
		"series_instance_uid",
		"modality",
		"series_number",
		"series_description",
		"instance_count",
		"imported_at",
	}); err != nil {
		return fmt.Errorf("write series CSV header: %w", err)
	}
	for _, item := range series {
		record := seriesToRecord(item)
		if err := csvw.Write([]string{
			record.StudyInstanceUID,
			record.SeriesInstanceUID,
			record.Modality,
			record.SeriesNumber,
			record.SeriesDescription,
			strconv.Itoa(record.InstanceCount),
			record.ImportedAt,
		}); err != nil {
			return fmt.Errorf("write series CSV row: %w", err)
		}
	}
	csvw.Flush()
	if err := csvw.Error(); err != nil {
		return fmt.Errorf("flush series CSV: %w", err)
	}
	return nil
}

type seriesRecord struct {
	StudyInstanceUID  string `json:"study_instance_uid"`
	SeriesInstanceUID string `json:"series_instance_uid"`
	Modality          string `json:"modality"`
	SeriesNumber      string `json:"series_number"`
	SeriesDescription string `json:"series_description"`
	InstanceCount     int    `json:"instance_count"`
	ImportedAt        string `json:"imported_at"`
}

func WriteSeriesJSON(w io.Writer, series []archive.Series) error {
	records := make([]seriesRecord, 0, len(series))
	for _, item := range series {
		records = append(records, seriesToRecord(item))
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(records); err != nil {
		return fmt.Errorf("write series JSON: %w", err)
	}
	return nil
}

func seriesToRecord(series archive.Series) seriesRecord {
	importedAt := ""
	if !series.ImportedAt.IsZero() {
		importedAt = series.ImportedAt.Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	return seriesRecord{
		StudyInstanceUID:  series.StudyInstanceUID,
		SeriesInstanceUID: series.SeriesInstanceUID,
		Modality:          series.Modality,
		SeriesNumber:      series.SeriesNumber,
		SeriesDescription: series.SeriesDescription,
		InstanceCount:     series.InstanceCount,
		ImportedAt:        importedAt,
	}
}
