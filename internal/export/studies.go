package export

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
)

func WriteStudiesCSV(w io.Writer, studies []archive.Study) error {
	csvw := csv.NewWriter(w)
	if err := csvw.Write([]string{
		"study_instance_uid",
		"patient_id",
		"patient_name",
		"study_date",
		"study_description",
		"modalities",
		"accession_number",
		"series_count",
		"instance_count",
		"imported_at",
	}); err != nil {
		return fmt.Errorf("write studies CSV header: %w", err)
	}
	for _, study := range studies {
		importedAt := ""
		if !study.ImportedAt.IsZero() {
			importedAt = study.ImportedAt.Format("2006-01-02T15:04:05.999999999Z07:00")
		}
		if err := csvw.Write([]string{
			study.StudyInstanceUID,
			study.PatientID,
			study.PatientName,
			study.StudyDate,
			study.StudyDescription,
			study.Modalities,
			study.AccessionNumber,
			strconv.Itoa(study.SeriesCount),
			strconv.Itoa(study.InstanceCount),
			importedAt,
		}); err != nil {
			return fmt.Errorf("write studies CSV row: %w", err)
		}
	}
	csvw.Flush()
	if err := csvw.Error(); err != nil {
		return fmt.Errorf("flush studies CSV: %w", err)
	}
	return nil
}

type studyRecord struct {
	StudyInstanceUID string `json:"study_instance_uid"`
	PatientID        string `json:"patient_id"`
	PatientName      string `json:"patient_name"`
	StudyDate        string `json:"study_date"`
	StudyDescription string `json:"study_description"`
	Modalities       string `json:"modalities"`
	AccessionNumber  string `json:"accession_number"`
	SeriesCount      int    `json:"series_count"`
	InstanceCount    int    `json:"instance_count"`
	ImportedAt       string `json:"imported_at"`
}

func WriteStudiesJSON(w io.Writer, studies []archive.Study) error {
	records := make([]studyRecord, 0, len(studies))
	for _, study := range studies {
		records = append(records, studyToRecord(study))
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(records); err != nil {
		return fmt.Errorf("write studies JSON: %w", err)
	}
	return nil
}

func studyToRecord(study archive.Study) studyRecord {
	importedAt := ""
	if !study.ImportedAt.IsZero() {
		importedAt = study.ImportedAt.Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	return studyRecord{
		StudyInstanceUID: study.StudyInstanceUID,
		PatientID:        study.PatientID,
		PatientName:      study.PatientName,
		StudyDate:        study.StudyDate,
		StudyDescription: study.StudyDescription,
		Modalities:       study.Modalities,
		AccessionNumber:  study.AccessionNumber,
		SeriesCount:      study.SeriesCount,
		InstanceCount:    study.InstanceCount,
		ImportedAt:       importedAt,
	}
}
