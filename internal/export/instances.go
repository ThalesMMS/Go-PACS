package export

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
)

func WriteInstancesCSV(w io.Writer, instances []archive.Instance) error {
	csvw := csv.NewWriter(w)
	if err := csvw.Write([]string{
		"sha256",
		"source_path",
		"file_size",
		"patient_id",
		"patient_name",
		"study_date",
		"study_description",
		"modality",
		"accession_number",
		"study_instance_uid",
		"series_instance_uid",
		"series_number",
		"series_description",
		"sop_class_uid",
		"sop_instance_uid",
		"instance_number",
		"transfer_syntax_uid",
		"transfer_syntax",
		"imported_at",
	}); err != nil {
		return fmt.Errorf("write instances CSV header: %w", err)
	}
	for _, instance := range instances {
		record := instanceToRecord(instance)
		if err := csvw.Write([]string{
			record.SHA256,
			record.SourcePath,
			strconv.FormatInt(record.FileSize, 10),
			record.PatientID,
			record.PatientName,
			record.StudyDate,
			record.StudyDescription,
			record.Modality,
			record.AccessionNumber,
			record.StudyInstanceUID,
			record.SeriesInstanceUID,
			record.SeriesNumber,
			record.SeriesDescription,
			record.SOPClassUID,
			record.SOPInstanceUID,
			record.InstanceNumber,
			record.TransferSyntaxUID,
			record.TransferSyntax,
			record.ImportedAt,
		}); err != nil {
			return fmt.Errorf("write instances CSV row: %w", err)
		}
	}
	csvw.Flush()
	if err := csvw.Error(); err != nil {
		return fmt.Errorf("flush instances CSV: %w", err)
	}
	return nil
}

type instanceRecord struct {
	SHA256            string `json:"sha256"`
	SourcePath        string `json:"source_path"`
	FileSize          int64  `json:"file_size"`
	PatientID         string `json:"patient_id"`
	PatientName       string `json:"patient_name"`
	StudyDate         string `json:"study_date"`
	StudyDescription  string `json:"study_description"`
	Modality          string `json:"modality"`
	AccessionNumber   string `json:"accession_number"`
	StudyInstanceUID  string `json:"study_instance_uid"`
	SeriesInstanceUID string `json:"series_instance_uid"`
	SeriesNumber      string `json:"series_number"`
	SeriesDescription string `json:"series_description"`
	SOPClassUID       string `json:"sop_class_uid"`
	SOPInstanceUID    string `json:"sop_instance_uid"`
	InstanceNumber    string `json:"instance_number"`
	TransferSyntaxUID string `json:"transfer_syntax_uid"`
	TransferSyntax    string `json:"transfer_syntax"`
	ImportedAt        string `json:"imported_at"`
}

func WriteInstancesJSON(w io.Writer, instances []archive.Instance) error {
	records := make([]instanceRecord, 0, len(instances))
	for _, instance := range instances {
		records = append(records, instanceToRecord(instance))
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(records); err != nil {
		return fmt.Errorf("write instances JSON: %w", err)
	}
	return nil
}

func instanceToRecord(instance archive.Instance) instanceRecord {
	importedAt := ""
	if !instance.ImportedAt.IsZero() {
		importedAt = instance.ImportedAt.Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	return instanceRecord{
		SHA256:            instance.SHA256,
		SourcePath:        instance.SourcePath,
		FileSize:          instance.FileSize,
		PatientID:         instance.PatientID,
		PatientName:       instance.PatientName,
		StudyDate:         instance.StudyDate,
		StudyDescription:  instance.StudyDescription,
		Modality:          instance.Modality,
		AccessionNumber:   instance.AccessionNumber,
		StudyInstanceUID:  instance.StudyInstanceUID,
		SeriesInstanceUID: instance.SeriesInstanceUID,
		SeriesNumber:      instance.SeriesNumber,
		SeriesDescription: instance.SeriesDescription,
		SOPClassUID:       instance.SOPClassUID,
		SOPInstanceUID:    instance.SOPInstanceUID,
		InstanceNumber:    instance.InstanceNumber,
		TransferSyntaxUID: instance.TransferSyntaxUID,
		TransferSyntax:    instance.TransferSyntax,
		ImportedAt:        importedAt,
	}
}
