package receive

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/dictionary/std"
	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/object"
)

var (
	findTagPatientName                    = core.NewTag(0x0010, 0x0010)
	findTagPatientID                      = core.NewTag(0x0010, 0x0020)
	findTagPatientBirthDate               = core.NewTag(0x0010, 0x0030)
	findTagStudyDate                      = core.NewTag(0x0008, 0x0020)
	findTagStudyTime                      = core.NewTag(0x0008, 0x0030)
	findTagStudyDescription               = core.NewTag(0x0008, 0x1030)
	findTagAccessionNumber                = core.NewTag(0x0008, 0x0050)
	findTagModality                       = core.NewTag(0x0008, 0x0060)
	findTagModalitiesInStudy              = core.NewTag(0x0008, 0x0061)
	findTagStudyInstanceUID               = core.NewTag(0x0020, 0x000D)
	findTagSeriesInstanceUID              = core.NewTag(0x0020, 0x000E)
	findTagSeriesNumber                   = core.NewTag(0x0020, 0x0011)
	findTagSeriesDescription              = core.NewTag(0x0008, 0x103E)
	findTagSOPClassUID                    = core.NewTag(0x0008, 0x0016)
	findTagSOPInstanceUID                 = core.NewTag(0x0008, 0x0018)
	findTagInstanceNumber                 = core.NewTag(0x0020, 0x0013)
	findTagNumberOfStudyRelatedSeries     = core.NewTag(0x0020, 0x1206)
	findTagNumberOfStudyRelatedInstances  = core.NewTag(0x0020, 0x1208)
	findTagNumberOfSeriesRelatedInstances = core.NewTag(0x0020, 0x1209)
)

func (s *Server) findCFind(ctx context.Context, req dimse.CFindRequestContext) ([]*object.Object, error) {
	switch req.QueryRetrieveLevel {
	case dimse.QueryRetrieveLevelStudy:
		return s.findStudies(ctx, req.Identifier)
	case dimse.QueryRetrieveLevelSeries:
		return s.findSeries(ctx, req.Identifier)
	case dimse.QueryRetrieveLevelImage:
		return s.findImages(ctx, req.Identifier)
	default:
		return nil, fmt.Errorf("unsupported QueryRetrieveLevel %q", req.QueryRetrieveLevel)
	}
}

func (s *Server) findStudies(ctx context.Context, identifier *object.Object) ([]*object.Object, error) {
	studies, err := s.catalog.StudiesWithFilters(ctx, studyFindFilters(identifier))
	if err != nil {
		return nil, err
	}
	out := make([]*object.Object, 0, len(studies))
	for _, study := range studies {
		out = append(out, studyFindObject(study))
	}
	return out, nil
}

func (s *Server) findSeries(ctx context.Context, identifier *object.Object) ([]*object.Object, error) {
	studyUID := findCriterion(identifier, findTagStudyInstanceUID)
	if studyUID == "" {
		return nil, fmt.Errorf("StudyInstanceUID is required for SERIES C-FIND")
	}
	studies, err := s.catalog.StudiesWithFilters(ctx, studyFindFilters(identifier))
	if err != nil {
		return nil, err
	}
	if len(studies) == 0 {
		return nil, nil
	}
	series, err := s.catalog.SeriesForStudyWithFilters(ctx, studyUID, archive.SeriesFilters{
		Modality:          findCriterion(identifier, findTagModality),
		SeriesNumber:      findCriterion(identifier, findTagSeriesNumber),
		SeriesDescription: findCriterion(identifier, findTagSeriesDescription),
	})
	if err != nil {
		return nil, err
	}
	seriesUID := findCriterion(identifier, findTagSeriesInstanceUID)
	out := make([]*object.Object, 0, len(series))
	for _, item := range series {
		if !findMatches(item.SeriesInstanceUID, seriesUID) {
			continue
		}
		out = append(out, seriesFindObject(item, studies[0]))
	}
	return out, nil
}

func (s *Server) findImages(ctx context.Context, identifier *object.Object) ([]*object.Object, error) {
	studyUID := findCriterion(identifier, findTagStudyInstanceUID)
	seriesUID := findCriterion(identifier, findTagSeriesInstanceUID)
	var (
		instances []archive.Instance
		err       error
	)
	switch {
	case seriesUID != "":
		instances, err = s.catalog.InstancesForSeries(ctx, seriesUID)
	case studyUID != "":
		instances, err = s.catalog.InstancesForStudy(ctx, studyUID)
	default:
		return nil, fmt.Errorf("SeriesInstanceUID or StudyInstanceUID is required for IMAGE C-FIND")
	}
	if err != nil {
		return nil, err
	}

	out := make([]*object.Object, 0, len(instances))
	for _, inst := range instances {
		if !instanceMatchesFind(inst, identifier) {
			continue
		}
		out = append(out, instanceFindObject(inst))
	}
	return out, nil
}

// studyFindFilters builds study filter criteria from a C-FIND query identifier.
func studyFindFilters(identifier *object.Object) archive.StudyFilters {
	from, to := findDateRange(findString(identifier, findTagStudyDate))
	return archive.StudyFilters{
		PatientName:      findCriterion(identifier, findTagPatientName),
		PatientID:        findCriterion(identifier, findTagPatientID),
		PatientBirthDate: findCriterion(identifier, findTagPatientBirthDate),
		AccessionNumber:  findCriterion(identifier, findTagAccessionNumber),
		StudyDateFrom:    from,
		StudyDateTo:      to,
		Modalities:       findModalities(identifier),
		StudyInstanceUID: findCriterion(identifier, findTagStudyInstanceUID),
	}
}

// instanceMatchesFind reports whether an instance matches all criteria in a C-FIND query identifier.
func instanceMatchesFind(inst archive.Instance, identifier *object.Object) bool {
	return findMatches(inst.PatientName, findString(identifier, findTagPatientName)) &&
		findMatches(inst.PatientID, findString(identifier, findTagPatientID)) &&
		findDateMatches(inst.StudyDate, findString(identifier, findTagStudyDate)) &&
		findMatches(inst.AccessionNumber, findString(identifier, findTagAccessionNumber)) &&
		findMatches(inst.StudyInstanceUID, findString(identifier, findTagStudyInstanceUID)) &&
		findMatches(inst.SeriesInstanceUID, findString(identifier, findTagSeriesInstanceUID)) &&
		findMatches(inst.SOPClassUID, findString(identifier, findTagSOPClassUID)) &&
		findMatches(inst.SOPInstanceUID, findString(identifier, findTagSOPInstanceUID)) &&
		findMatches(inst.Modality, findString(identifier, findTagModality)) &&
		findMatches(inst.InstanceNumber, findString(identifier, findTagInstanceNumber))
}

// FindModalities extracts modality criteria from a query identifier.
// It reads ModalitiesInStudy values, falling back to Modality if none are found.
// Values are split on backslash separators, and wildcard characters are removed.
func findModalities(identifier *object.Object) []string {
	values := findStrings(identifier, findTagModalitiesInStudy)
	if len(values) == 0 {
		values = findStrings(identifier, findTagModality)
	}
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, "\\") {
			if part = findCriterionText(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// findDateRange parses a date criterion into a start and end date.
// If the criterion is empty, both return values are empty. If it contains
// no hyphen, both values are the criterion. If it contains a hyphen, it is
// split into start and end parts, each trimmed of whitespace.
func findDateRange(value string) (string, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	from, to, ok := strings.Cut(value, "-")
	if !ok {
		return value, value
	}
	return strings.TrimSpace(from), strings.TrimSpace(to)
}

// findMatches determines if a value matches a query criterion. Wildcard characters * and ? use shell-style matching with case-insensitive comparison. An empty criterion matches any value.
func findMatches(value, criterion string) bool {
	criterion = strings.TrimSpace(criterion)
	if criterion == "" {
		return true
	}
	if strings.ContainsAny(criterion, "*?") {
		ok, err := path.Match(strings.ToUpper(criterion), strings.ToUpper(value))
		return err == nil && ok
	}
	return strings.EqualFold(value, criterion)
}

// findDateMatches reports whether a date value falls within the range specified by a criterion.
// The criterion can specify an exact date, a range in the form from-to, or be empty for no constraint.
func findDateMatches(value, criterion string) bool {
	from, to := findDateRange(criterion)
	if from != "" && value < from {
		return false
	}
	if to != "" && value > to {
		return false
	}
	return true
}

// StudyFindObject converts a study record into a DICOM C-FIND response dataset.
func studyFindObject(study archive.Study) *object.Object {
	return object.FromElements([]core.Element{
		findElement(findTagPatientName, core.VRPN, study.PatientName),
		findElement(findTagPatientID, core.VRLO, study.PatientID),
		findElement(findTagPatientBirthDate, core.VRDA, study.PatientBirthDate),
		findElement(findTagStudyDate, core.VRDA, study.StudyDate),
		findElement(findTagStudyTime, core.VRTM, study.StudyTime),
		findElement(findTagStudyDescription, core.VRLO, study.StudyDescription),
		findElement(findTagAccessionNumber, core.VRSH, study.AccessionNumber),
		findElement(findTagStudyInstanceUID, core.VRUI, study.StudyInstanceUID),
		findElement(findTagModalitiesInStudy, core.VRCS, strings.ReplaceAll(study.Modalities, ",", "\\")),
		findElement(findTagNumberOfStudyRelatedSeries, core.VRIS, strconv.Itoa(study.SeriesCount)),
		findElement(findTagNumberOfStudyRelatedInstances, core.VRIS, strconv.Itoa(study.InstanceCount)),
	}, std.Dictionary)
}

// seriesFindObject creates a DICOM dataset for C-FIND series responses, combining patient and study information with series-specific fields.
func seriesFindObject(series archive.Series, study archive.Study) *object.Object {
	return object.FromElements([]core.Element{
		findElement(findTagPatientName, core.VRPN, study.PatientName),
		findElement(findTagPatientID, core.VRLO, study.PatientID),
		findElement(findTagPatientBirthDate, core.VRDA, study.PatientBirthDate),
		findElement(findTagStudyDate, core.VRDA, study.StudyDate),
		findElement(findTagStudyTime, core.VRTM, study.StudyTime),
		findElement(findTagStudyInstanceUID, core.VRUI, series.StudyInstanceUID),
		findElement(findTagSeriesInstanceUID, core.VRUI, series.SeriesInstanceUID),
		findElement(findTagModality, core.VRCS, series.Modality),
		findElement(findTagSeriesNumber, core.VRIS, series.SeriesNumber),
		findElement(findTagSeriesDescription, core.VRLO, series.SeriesDescription),
		findElement(findTagNumberOfSeriesRelatedInstances, core.VRIS, strconv.Itoa(series.InstanceCount)),
	}, std.Dictionary)
}

// instanceFindObject constructs a DICOM dataset from an instance record for C-FIND responses.
func instanceFindObject(inst archive.Instance) *object.Object {
	return object.FromElements([]core.Element{
		findElement(findTagPatientName, core.VRPN, inst.PatientName),
		findElement(findTagPatientID, core.VRLO, inst.PatientID),
		findElement(findTagPatientBirthDate, core.VRDA, inst.PatientBirthDate),
		findElement(findTagStudyDate, core.VRDA, inst.StudyDate),
		findElement(findTagStudyTime, core.VRTM, inst.StudyTime),
		findElement(findTagStudyInstanceUID, core.VRUI, inst.StudyInstanceUID),
		findElement(findTagSeriesInstanceUID, core.VRUI, inst.SeriesInstanceUID),
		findElement(findTagSOPClassUID, core.VRUI, inst.SOPClassUID),
		findElement(findTagSOPInstanceUID, core.VRUI, inst.SOPInstanceUID),
		findElement(findTagModality, core.VRCS, inst.Modality),
		findElement(findTagInstanceNumber, core.VRIS, inst.InstanceNumber),
	}, std.Dictionary)
}

// findElement constructs a core.Element with the given tag, VR, and string value.
func findElement(tag core.Tag, vr core.VR, value string) core.Element {
	return core.Element{Header: core.ElementHeader{Tag: tag, VR: vr}, Value: core.StringValue{value}}
}

// findCriterion extracts a DICOM query criterion from the given tag in the object, removing wildcard characters and trimming whitespace.
func findCriterion(obj *object.Object, tag core.Tag) string {
	return findCriterionText(findString(obj, tag))
}

// findCriterionText removes wildcard characters and trims whitespace from a criterion string.
func findCriterionText(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "*", "")
	value = strings.ReplaceAll(value, "?", "")
	return strings.TrimSpace(value)
}

// findString returns the string value associated with a tag from a DICOM object, or an empty string if the value is not present.
func findString(obj *object.Object, tag core.Tag) string {
	value, ok := obj.GetString(tag)
	if !ok {
		return ""
	}
	return value
}

// FindStrings retrieves string values from the given DICOM tag, returning a slice containing those values or a single string value if only a scalar is present.
func findStrings(obj *object.Object, tag core.Tag) []string {
	values, ok := obj.GetStrings(tag)
	if ok {
		return values
	}
	if value := findString(obj, tag); value != "" {
		return []string{value}
	}
	return nil
}
