package dicominspect

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	dicom "github.com/ThalesMMS/dicom-go"
	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/object"
)

const (
	DefaultMaxTotalBytes    int64 = 512 << 20
	DefaultMaxElementBytes  int64 = 32 << 20
	DefaultMaxElements            = 200000
	DefaultMaxSequenceDepth       = 64
)

var (
	tagPatientName       = core.NewTag(0x0010, 0x0010)
	tagPatientID         = core.NewTag(0x0010, 0x0020)
	tagPatientBirthDate  = core.NewTag(0x0010, 0x0030)
	tagInstitutionName   = core.NewTag(0x0008, 0x0080)
	tagStudyDate         = core.NewTag(0x0008, 0x0020)
	tagSeriesDate        = core.NewTag(0x0008, 0x0021)
	tagStudyTime         = core.NewTag(0x0008, 0x0030)
	tagSeriesTime        = core.NewTag(0x0008, 0x0031)
	tagStudyDescription  = core.NewTag(0x0008, 0x1030)
	tagModality          = core.NewTag(0x0008, 0x0060)
	tagAccessionNumber   = core.NewTag(0x0008, 0x0050)
	tagSeriesDescription = core.NewTag(0x0008, 0x103E)
	tagStudyInstanceUID  = core.NewTag(0x0020, 0x000D)
	tagSeriesUID         = core.NewTag(0x0020, 0x000E)
	tagSeriesNumber      = core.NewTag(0x0020, 0x0011)
	tagSOPClassUID       = core.NewTag(0x0008, 0x0016)
	tagSOPInstanceUID    = core.NewTag(0x0008, 0x0018)
	tagInstanceNumber    = core.NewTag(0x0020, 0x0013)
)

type Options struct {
	MaxTotalBytes    int64
	MaxElementBytes  int64
	MaxElements      int
	MaxSequenceDepth int
}

type Summary struct {
	FileName          string           `json:"fileName,omitempty"`
	TransferSyntaxUID string           `json:"transferSyntaxUID"`
	TransferSyntax    string           `json:"transferSyntax"`
	PatientName       string           `json:"patientName,omitempty"`
	PatientID         string           `json:"patientID,omitempty"`
	PatientBirthDate  string           `json:"patientBirthDate,omitempty"`
	InstitutionName   string           `json:"institutionName,omitempty"`
	StudyDate         string           `json:"studyDate,omitempty"`
	StudyTime         string           `json:"studyTime,omitempty"`
	SeriesDate        string           `json:"seriesDate,omitempty"`
	SeriesTime        string           `json:"seriesTime,omitempty"`
	StudyDescription  string           `json:"studyDescription,omitempty"`
	Modality          string           `json:"modality,omitempty"`
	AccessionNumber   string           `json:"accessionNumber,omitempty"`
	SeriesDescription string           `json:"seriesDescription,omitempty"`
	StudyInstanceUID  string           `json:"studyInstanceUID,omitempty"`
	SeriesInstanceUID string           `json:"seriesInstanceUID,omitempty"`
	SeriesNumber      string           `json:"seriesNumber,omitempty"`
	SOPClassUID       string           `json:"sopClassUID,omitempty"`
	SOPInstanceUID    string           `json:"sopInstanceUID,omitempty"`
	InstanceNumber    string           `json:"instanceNumber,omitempty"`
	ElementCount      int              `json:"elementCount"`
	Elements          []ElementSummary `json:"elements"`
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

	summary := Summary{
		FileName:          fileName,
		TransferSyntaxUID: file.TransferSyntax.UID,
		TransferSyntax:    file.TransferSyntax.Name,
		PatientName:       getString(file, tagPatientName),
		PatientID:         getString(file, tagPatientID),
		PatientBirthDate:  getString(file, tagPatientBirthDate),
		InstitutionName:   getString(file, tagInstitutionName),
		StudyDate:         getString(file, tagStudyDate),
		StudyTime:         getString(file, tagStudyTime),
		SeriesDate:        getString(file, tagSeriesDate),
		SeriesTime:        getString(file, tagSeriesTime),
		StudyDescription:  getString(file, tagStudyDescription),
		Modality:          getString(file, tagModality),
		AccessionNumber:   getString(file, tagAccessionNumber),
		SeriesDescription: getString(file, tagSeriesDescription),
		StudyInstanceUID:  getString(file, tagStudyInstanceUID),
		SeriesInstanceUID: getString(file, tagSeriesUID),
		SeriesNumber:      getString(file, tagSeriesNumber),
		SOPClassUID:       getString(file, tagSOPClassUID),
		SOPInstanceUID:    getString(file, tagSOPInstanceUID),
		InstanceNumber:    getString(file, tagInstanceNumber),
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

func getString(file *dicom.File, tag core.Tag) string {
	value, ok := file.GetString(tag)
	if !ok {
		return ""
	}
	return value
}

func summarizeObject(source string, obj *object.Object) []ElementSummary {
	if obj == nil {
		return nil
	}
	elements := obj.SortedElements()
	out := make([]ElementSummary, 0, len(elements))
	for _, elem := range elements {
		entry, hasEntry := std.Dictionary.ByTag(elem.Tag())
		item := ElementSummary{
			Source:  source,
			Tag:     elem.Tag().String(),
			VR:      elem.VR().String(),
			Length:  elem.Length().String(),
			Value:   previewValue(elem),
			Private: elem.Tag().IsPrivate(),
		}
		if hasEntry {
			item.Keyword = entry.Keyword
			item.Name = entry.Name
		}
		out = append(out, item)
	}
	return out
}

func previewValue(elem core.Element) string {
	if elem.Value == nil {
		if elem.Tag() == core.TagPixelData {
			return "<pixel data skipped>"
		}
		return "<value skipped>"
	}
	switch value := elem.Value.(type) {
	case core.SequenceValue:
		return fmt.Sprintf("%d item(s)", len(value.Items))
	case core.FragmentSequence:
		return fmt.Sprintf("%d fragment(s)", len(value.Fragments))
	case core.BulkDataValue:
		return value.URI
	}
	if !elem.VR().IsStringLike() {
		return ""
	}
	values := elem.StringValues()
	if len(values) == 0 {
		return ""
	}
	return truncate(strings.Join(values, "\\"), 160)
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	if max <= 1 {
		return value[:max]
	}
	return value[:max-1] + "..."
}
