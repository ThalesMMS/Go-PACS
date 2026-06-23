package dicominspect

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	dicom "github.com/ThalesMMS/dicom-go"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/object"
)

const (
	DefaultMaxTotalBytes    int64 = 512 << 20
	DefaultMaxElementBytes  int64 = 32 << 20
	DefaultMaxElements            = 200000
	DefaultMaxSequenceDepth       = 64
)

type Options struct {
	MaxTotalBytes    int64
	MaxElementBytes  int64
	MaxElements      int
	MaxSequenceDepth int
}

type Summary struct {
	FileName          string `json:"fileName,omitempty"`
	TransferSyntaxUID string `json:"transferSyntaxUID"`
	TransferSyntax    string `json:"transferSyntax"`
	PatientName       string `json:"patientName,omitempty"`
	PatientID         string `json:"patientID,omitempty"`
	PatientBirthDate  string `json:"patientBirthDate,omitempty"`
	InstitutionName   string `json:"institutionName,omitempty"`
	StudyDate         string `json:"studyDate,omitempty"`
	StudyTime         string `json:"studyTime,omitempty"`
	SeriesDate        string `json:"seriesDate,omitempty"`
	SeriesTime        string `json:"seriesTime,omitempty"`
	StudyDescription  string `json:"studyDescription,omitempty"`
	Modality          string `json:"modality,omitempty"`
	AccessionNumber   string `json:"accessionNumber,omitempty"`
	SeriesDescription string `json:"seriesDescription,omitempty"`
	StudyInstanceUID  string `json:"studyInstanceUID,omitempty"`
	SeriesInstanceUID string `json:"seriesInstanceUID,omitempty"`
	SeriesNumber      string `json:"seriesNumber,omitempty"`
	SOPClassUID       string `json:"sopClassUID,omitempty"`
	SOPInstanceUID    string `json:"sopInstanceUID,omitempty"`
	InstanceNumber    string `json:"instanceNumber,omitempty"`

	StudyID                 string `json:"studyID,omitempty"`
	BodyPartExamined        string `json:"bodyPartExamined,omitempty"`
	ReferringPhysicianName  string `json:"referringPhysicianName,omitempty"`
	PerformingPhysicianName string `json:"performingPhysicianName,omitempty"`

	ElementCount int              `json:"elementCount"`
	Elements     []ElementSummary `json:"elements"`
}

type ElementSummary struct {
	Source  string `json:"source"`
	Tag     string `json:"tag"`
	VR      string `json:"vr"`
	Keyword string `json:"keyword,omitempty"`
	Name    string `json:"name,omitempty"`
	Length  string `json:"length"`
	Value   string `json:"value,omitempty"`
	Private bool   `json:"private"`
}

func DefaultOptions() Options {
	return Options{
		MaxTotalBytes:    DefaultMaxTotalBytes,
		MaxElementBytes:  DefaultMaxElementBytes,
		MaxElements:      DefaultMaxElements,
		MaxSequenceDepth: DefaultMaxSequenceDepth,
	}
}

// InspectReader reads a DICOM file from a reader and returns a summary of its metadata and elements.
// Parsing limits specified in opts are applied; zero values are replaced with defaults.
func InspectReader(fileName string, r io.Reader, opts Options) (Summary, error) {
	opts = withDefaults(opts)
	file, err := dicom.ReadFileWithOptions(r, dicom.ReadFileOptions{
		MaxTotalBytes:      opts.MaxTotalBytes,
		MaxElementBytes:    opts.MaxElementBytes,
		MaxElements:        opts.MaxElements,
		MaxSequenceDepth:   opts.MaxSequenceDepth,
		SkipPixelData:      true,
		Dictionary:         std.Dictionary,
		FileMetaDictionary: std.Dictionary,
	})
	if err != nil {
		return Summary{}, fmt.Errorf("inspect DICOM file: %w", err)
	}
	defer file.Close()

	metadata := file.Metadata()
	summary := Summary{
		FileName:          fileName,
		TransferSyntaxUID: metadata.TransferSyntaxUID,
		TransferSyntax:    metadata.TransferSyntaxName,
		PatientName:       metadata.PatientName,
		PatientID:         metadata.PatientID,
		PatientBirthDate:  metadata.PatientBirthDate,
		InstitutionName:   metadata.InstitutionName,
		StudyDate:         metadata.StudyDate,
		StudyTime:         metadata.StudyTime,
		SeriesDate:        metadata.SeriesDate,
		SeriesTime:        metadata.SeriesTime,
		StudyDescription:  metadata.StudyDescription,
		Modality:          metadata.Modality,
		AccessionNumber:   metadata.AccessionNumber,
		SeriesDescription: metadata.SeriesDescription,
		StudyInstanceUID:  metadata.StudyInstanceUID,
		SeriesInstanceUID: metadata.SeriesInstanceUID,
		SeriesNumber:      metadata.SeriesNumber,
		SOPClassUID:       metadata.SOPClassUID,
		SOPInstanceUID:    metadata.SOPInstanceUID,
		InstanceNumber:    metadata.InstanceNumber,

		StudyID:                 metadata.StudyID,
		BodyPartExamined:        metadata.BodyPartExamined,
		ReferringPhysicianName:  metadata.ReferringPhysicianName,
		PerformingPhysicianName: metadata.PerformingPhysicianName,
	}
	summary.Elements = append(summary.Elements, summarizeObject("meta", file.Meta)...)
	summary.Elements = append(summary.Elements, summarizeObject("dataset", file.Dataset)...)
	summary.ElementCount = len(summary.Elements)
	return summary, nil
}

func InspectFile(path string, opts Options) (Summary, error) {
	file, err := os.Open(path)
	if err != nil {
		return Summary{}, fmt.Errorf("open DICOM file: %w", err)
	}
	defer file.Close()
	return InspectReader(filepath.Base(path), file, opts)
}

// withDefaults returns opts with zero-valued limits replaced by defaults from DefaultOptions.
func withDefaults(opts Options) Options {
	defaults := DefaultOptions()
	if opts.MaxTotalBytes == 0 {
		opts.MaxTotalBytes = defaults.MaxTotalBytes
	}
	if opts.MaxElementBytes == 0 {
		opts.MaxElementBytes = defaults.MaxElementBytes
	}
	if opts.MaxElements == 0 {
		opts.MaxElements = defaults.MaxElements
	}
	if opts.MaxSequenceDepth == 0 {
		opts.MaxSequenceDepth = defaults.MaxSequenceDepth
	}
	return opts
}

// summarizeObject converts DICOM elements from the given object into ElementSummary values with the specified source.
func summarizeObject(source string, obj *object.Object) []ElementSummary {
	rows := object.SummarizeElements(obj, object.SummaryOptions{
		Dictionary:     std.Dictionary,
		Source:         source,
		MaxValueLength: 160,
	})
	out := make([]ElementSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, ElementSummary{
			Source:  row.Source,
			Tag:     row.TagString(),
			VR:      row.VRString(),
			Keyword: row.Keyword,
			Name:    row.Name,
			Length:  row.LengthString(),
			Value:   row.Value,
			Private: row.Private,
		})
	}
	return out
}
