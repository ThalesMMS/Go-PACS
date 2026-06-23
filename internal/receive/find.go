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

func findElement(tag core.Tag, vr core.VR, value string) core.Element {
	return core.Element{Header: core.ElementHeader{Tag: tag, VR: vr}, Value: core.StringValue{value}}
}

func findCriterion(obj *object.Object, tag core.Tag) string {
	return findCriterionText(findString(obj, tag))
}

func findCriterionText(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "*", "")
	value = strings.ReplaceAll(value, "?", "")
	return strings.TrimSpace(value)
}

func findString(obj *object.Object, tag core.Tag) string {
	value, ok := obj.GetString(tag)
	if !ok {
		return ""
	}
	return value
}

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
