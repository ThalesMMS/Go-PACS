package qido

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/query"
	"github.com/ThalesMMS/dicom-go/dicomjson"
	"github.com/ThalesMMS/dicom-go/net/dicomweb"
	"github.com/ThalesMMS/dicom-go/net/dimse"
)

// StudyQuery performs a QIDO-RS study search and maps DICOM JSON results into
// the shared query.Match model used by the existing DIMSE query workflow.
func StudyQuery(ctx context.Context, node nodes.Node, criteria query.Criteria) (query.Result, error) {
	params := url.Values{}
	addParam(params, "PatientName", criteria.PatientName)
	addParam(params, "PatientID", criteria.PatientID)
	addParam(params, "PatientBirthDate", criteria.PatientBirthDate)
	addParam(params, "StudyDate", valueRange(criteria.StudyDateFrom, criteria.StudyDateTo))
	addParam(params, "StudyTime", valueRange(criteria.StudyTimeFrom, criteria.StudyTimeTo))
	addParam(params, "StudyDescription", criteria.StudyDescription)
	addParam(params, "AccessionNumber", criteria.AccessionNumber)
	addParam(params, "ReferringPhysicianName", criteria.ReferringPhysicianName)
	addParam(params, "InstitutionName", criteria.InstitutionName)
	addParam(params, "PatientComments", criteria.PatientComments)
	addParam(params, "StudyStatusID", criteria.StudyStatusID)
	addParam(params, "ModalitiesInStudy", criteria.Modality)
	addParam(params, "StudyInstanceUID", criteria.StudyInstanceUID)
	if strings.TrimSpace(criteria.CustomFieldValue) != "" && strings.TrimSpace(criteria.CustomFieldKeyword) == "" {
		return query.Result{}, fmt.Errorf("custom DICOM field keyword is required")
	}
	addParam(params, criteria.CustomFieldKeyword, criteria.CustomFieldValue)
	addLimit(params, criteria.MaxResults)

	return search(ctx, node, criteria.MaxResults, func(client dicomweb.Client) ([]dicomweb.Dataset, error) {
		return client.SearchStudies(ctx, params)
	}, studyMatch)
}

// SeriesQuery performs a QIDO-RS series search for one study.
func SeriesQuery(ctx context.Context, node nodes.Node, criteria query.SeriesCriteria) (query.Result, error) {
	studyUID := strings.TrimSpace(criteria.StudyInstanceUID)
	if studyUID == "" {
		return query.Result{}, fmt.Errorf("study instance UID is required for series query")
	}
	params := url.Values{}
	addParam(params, "PatientName", criteria.PatientName)
	addParam(params, "PatientID", criteria.PatientID)
	addParam(params, "StudyDate", valueRange(criteria.StudyDateFrom, criteria.StudyDateTo))
	addParam(params, "SeriesInstanceUID", criteria.SeriesInstanceUID)
	addParam(params, "Modality", criteria.Modality)
	addParam(params, "SeriesNumber", criteria.SeriesNumber)
	addParam(params, "SeriesDescription", criteria.SeriesDescription)
	addLimit(params, criteria.MaxResults)

	return search(ctx, node, criteria.MaxResults, func(client dicomweb.Client) ([]dicomweb.Dataset, error) {
		return client.SearchSeries(ctx, studyUID, params)
	}, seriesMatch)
}

// ImageQuery performs a QIDO-RS instance search for one series.
func ImageQuery(ctx context.Context, node nodes.Node, criteria query.ImageCriteria) (query.Result, error) {
	studyUID := strings.TrimSpace(criteria.StudyInstanceUID)
	if studyUID == "" {
		return query.Result{}, fmt.Errorf("study instance UID is required for image query")
	}
	seriesUID := strings.TrimSpace(criteria.SeriesInstanceUID)
	if seriesUID == "" {
		return query.Result{}, fmt.Errorf("series instance UID is required for image query")
	}
	params := url.Values{}
	addParam(params, "PatientName", criteria.PatientName)
	addParam(params, "PatientID", criteria.PatientID)
	addParam(params, "StudyDate", valueRange(criteria.StudyDateFrom, criteria.StudyDateTo))
	addParam(params, "SOPInstanceUID", criteria.SOPInstanceUID)
	addParam(params, "SOPClassUID", criteria.SOPClassUID)
	addParam(params, "Modality", criteria.Modality)
	addParam(params, "InstanceNumber", criteria.InstanceNumber)
	addLimit(params, criteria.MaxResults)

	return search(ctx, node, criteria.MaxResults, func(client dicomweb.Client) ([]dicomweb.Dataset, error) {
		return client.SearchInstances(ctx, studyUID, seriesUID, params)
	}, imageMatch)
}

func search(ctx context.Context, node nodes.Node, maxResults int, run func(dicomweb.Client) ([]dicomweb.Dataset, error), mapMatch func(dicomweb.Dataset) query.Match) (query.Result, error) {
	if !node.IsDICOMweb() {
		return query.Result{}, fmt.Errorf("node %q is not a DICOMweb profile", node.Name)
	}
	client := dicomweb.Client{
		Endpoint: dicomweb.Endpoint{
			BaseURL:  strings.TrimSpace(node.BaseURL),
			QIDOPath: nodes.NormalizeDICOMwebPathPrefix(node.QIDOPathPrefix),
			WADOPath: nodes.NormalizeDICOMwebPathPrefix(node.WADOPathPrefix),
			STOWPath: nodes.NormalizeDICOMwebPathPrefix(node.STOWPathPrefix),
		},
		Options: dicomweb.Options{Timeout: query.DefaultTimeout},
	}
	start := time.Now()
	datasets, err := run(client)
	if err != nil {
		return query.Result{}, err
	}
	out := query.Result{FinalStatus: dimse.StatusSuccess, Duration: time.Since(start)}
	for _, dataset := range datasets {
		if maxResults > 0 && len(out.Matches) >= maxResults {
			break
		}
		out.Matches = append(out.Matches, mapMatch(dataset))
	}
	return out, nil
}

func studyMatch(dataset dicomweb.Dataset) query.Match {
	return query.Match{
		QueryRetrieveLevel:     dimse.QueryRetrieveLevelStudy,
		PatientName:            elementString(dataset, "00100010"),
		PatientID:              elementString(dataset, "00100020"),
		PatientBirthDate:       elementString(dataset, "00100030"),
		StudyDate:              elementString(dataset, "00080020"),
		StudyTime:              elementString(dataset, "00080030"),
		ImageCount:             elementString(dataset, "00201208"),
		StudyDescription:       elementString(dataset, "00081030"),
		AccessionNumber:        elementString(dataset, "00080050"),
		ReferringPhysicianName: elementString(dataset, "00080090"),
		InstitutionName:        elementString(dataset, "00080080"),
		PatientComments:        elementString(dataset, "00104000"),
		StudyStatusID:          elementString(dataset, "0032000A"),
		StudyInstanceUID:       elementString(dataset, "0020000D"),
		Modalities:             strings.Join(elementStrings(dataset, "00080061"), "\\"),
		Status:                 dimse.StatusSuccess,
	}
}

func seriesMatch(dataset dicomweb.Dataset) query.Match {
	return query.Match{
		QueryRetrieveLevel:     dimse.QueryRetrieveLevelSeries,
		PatientName:            elementString(dataset, "00100010"),
		PatientID:              elementString(dataset, "00100020"),
		PatientBirthDate:       elementString(dataset, "00100030"),
		StudyDate:              elementString(dataset, "00080020"),
		StudyTime:              elementString(dataset, "00080030"),
		ImageCount:             elementString(dataset, "00201209"),
		ReferringPhysicianName: elementString(dataset, "00080090"),
		InstitutionName:        elementString(dataset, "00080080"),
		StudyInstanceUID:       elementString(dataset, "0020000D"),
		SeriesInstanceUID:      elementString(dataset, "0020000E"),
		Modality:               elementString(dataset, "00080060"),
		SeriesNumber:           elementString(dataset, "00200011"),
		SeriesDescription:      elementString(dataset, "0008103E"),
		Status:                 dimse.StatusSuccess,
	}
}

func imageMatch(dataset dicomweb.Dataset) query.Match {
	return query.Match{
		QueryRetrieveLevel:     dimse.QueryRetrieveLevelImage,
		PatientName:            elementString(dataset, "00100010"),
		PatientID:              elementString(dataset, "00100020"),
		PatientBirthDate:       elementString(dataset, "00100030"),
		StudyDate:              elementString(dataset, "00080020"),
		StudyTime:              elementString(dataset, "00080030"),
		ReferringPhysicianName: elementString(dataset, "00080090"),
		InstitutionName:        elementString(dataset, "00080080"),
		StudyInstanceUID:       elementString(dataset, "0020000D"),
		SeriesInstanceUID:      elementString(dataset, "0020000E"),
		Modality:               elementString(dataset, "00080060"),
		SOPClassUID:            elementString(dataset, "00080016"),
		SOPInstanceUID:         elementString(dataset, "00080018"),
		InstanceNumber:         elementString(dataset, "00200013"),
		Status:                 dimse.StatusSuccess,
	}
}

func addParam(params url.Values, key string, value string) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return
	}
	params.Set(key, value)
}

func addLimit(params url.Values, maxResults int) {
	if maxResults > 0 {
		params.Set("limit", strconv.Itoa(maxResults))
	}
}

func valueRange(from, to string) string {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	switch {
	case from == "" && to == "":
		return ""
	case from == to:
		return from
	default:
		return from + "-" + to
	}
}

func elementString(dataset dicomweb.Dataset, tag string) string {
	return dicomjson.ElementString(dataset, tag)
}

func elementStrings(dataset dicomweb.Dataset, tag string) []string {
	return dicomjson.ElementStrings(dataset, tag)
}
