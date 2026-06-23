package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"net/textproto"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
)

var (
	errDICOMwebObjectUnavailable = errors.New("DICOM object unavailable")
	errDICOMwebUnsafeStoredPath  = errors.New("stored DICOM object path is invalid")
)

type dicomJSONDataset map[string]dicomJSONValue

type dicomJSONValue struct {
	VR    string `json:"vr"`
	Value []any  `json:"Value,omitempty"`
}

func (s *Server) handleDICOMwebStudies(w http.ResponseWriter, r *http.Request) {
	filters, window, err := qidoStudyFilters(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	studies, err := s.session.Catalog().StudiesWithFilters(r.Context(), filters)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	studies = applyWindow(studies, window)
	datasets := make([]dicomJSONDataset, 0, len(studies))
	for _, study := range studies {
		datasets = append(datasets, qidoStudyDataset(study))
	}
	writeDICOMJSON(w, datasets)
}

func (s *Server) handleDICOMwebSeries(w http.ResponseWriter, r *http.Request) {
	filters, seriesUID, window, err := qidoSeriesFilters(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	series, err := s.session.Catalog().SeriesForStudyWithFilters(r.Context(), r.PathValue("studyUID"), filters)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if seriesUID != "" {
		series = filterSlice(series, func(item archive.Series) bool {
			return item.SeriesInstanceUID == seriesUID
		})
	}
	series = applyWindow(series, window)
	datasets := make([]dicomJSONDataset, 0, len(series))
	for _, item := range series {
		datasets = append(datasets, qidoSeriesDataset(item))
	}
	writeDICOMJSON(w, datasets)
}

func (s *Server) handleDICOMwebInstances(w http.ResponseWriter, r *http.Request) {
	filters, window, err := qidoInstanceFilters(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	instances, err := s.session.Catalog().InstancesForSeries(r.Context(), r.PathValue("seriesUID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	studyUID := r.PathValue("studyUID")
	instances = filterSlice(instances, func(instance archive.Instance) bool {
		if instance.StudyInstanceUID != studyUID {
			return false
		}
		if filters.SOPInstanceUID != "" && instance.SOPInstanceUID != filters.SOPInstanceUID {
			return false
		}
		if filters.SOPClassUID != "" && instance.SOPClassUID != filters.SOPClassUID {
			return false
		}
		if filters.InstanceNumber != "" && instance.InstanceNumber != filters.InstanceNumber {
			return false
		}
		return true
	})
	instances = applyWindow(instances, window)
	datasets := make([]dicomJSONDataset, 0, len(instances))
	for _, instance := range instances {
		datasets = append(datasets, qidoInstanceDataset(instance))
	}
	writeDICOMJSON(w, datasets)
}

func (s *Server) handleDICOMwebStudyMetadata(w http.ResponseWriter, r *http.Request) {
	instances, err := s.session.Catalog().InstancesForStudy(r.Context(), r.PathValue("studyUID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeDICOMwebMetadata(w, instances)
}

func (s *Server) handleDICOMwebSeriesMetadata(w http.ResponseWriter, r *http.Request) {
	instances, err := s.session.Catalog().InstancesForSeries(r.Context(), r.PathValue("seriesUID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	studyUID := r.PathValue("studyUID")
	instances = filterSlice(instances, func(instance archive.Instance) bool {
		return instance.StudyInstanceUID == studyUID
	})
	writeDICOMwebMetadata(w, instances)
}

func (s *Server) handleDICOMwebInstanceMetadata(w http.ResponseWriter, r *http.Request) {
	instance, ok := s.dicomwebInstanceForPath(w, r)
	if !ok {
		return
	}
	writeDICOMJSON(w, []dicomJSONDataset{qidoInstanceDataset(instance)})
}

func (s *Server) handleDICOMwebStudyObjects(w http.ResponseWriter, r *http.Request) {
	instances, err := s.session.Catalog().InstancesForStudy(r.Context(), r.PathValue("studyUID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeDICOMwebMultipartObjects(w, instances)
}

func (s *Server) handleDICOMwebSeriesObjects(w http.ResponseWriter, r *http.Request) {
	instances, err := s.session.Catalog().InstancesForSeries(r.Context(), r.PathValue("seriesUID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	studyUID := r.PathValue("studyUID")
	instances = filterSlice(instances, func(instance archive.Instance) bool {
		return instance.StudyInstanceUID == studyUID
	})
	s.writeDICOMwebMultipartObjects(w, instances)
}

func (s *Server) handleDICOMwebInstanceObject(w http.ResponseWriter, r *http.Request) {
	instance, ok := s.dicomwebInstanceForPath(w, r)
	if !ok {
		return
	}
	path, err := s.safeStoredObjectPath(instance)
	if err != nil {
		writeDICOMwebObjectError(w, err)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeDICOMwebObjectError(w, errDICOMwebObjectUnavailable)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/dicom")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, file)
}

func writeDICOMwebMetadata(w http.ResponseWriter, instances []archive.Instance) {
	if len(instances) == 0 {
		writeJSON(w, http.StatusNotFound, apiResponse{Error: "DICOM resource not found"})
		return
	}
	datasets := make([]dicomJSONDataset, 0, len(instances))
	for _, instance := range instances {
		datasets = append(datasets, qidoInstanceDataset(instance))
	}
	writeDICOMJSON(w, datasets)
}

func (s *Server) writeDICOMwebMultipartObjects(w http.ResponseWriter, instances []archive.Instance) {
	if len(instances) == 0 {
		writeJSON(w, http.StatusNotFound, apiResponse{Error: "DICOM resource not found"})
		return
	}
	paths := make([]string, 0, len(instances))
	for _, instance := range instances {
		path, err := s.safeStoredObjectPath(instance)
		if err != nil {
			writeDICOMwebObjectError(w, err)
			return
		}
		paths = append(paths, path)
	}
	writer := multipart.NewWriter(w)
	w.Header().Set("Content-Type", `multipart/related; type="application/dicom"; boundary=`+writer.Boundary())
	w.WriteHeader(http.StatusOK)
	for _, path := range paths {
		header := textproto.MIMEHeader{}
		header.Set("Content-Type", "application/dicom")
		part, err := writer.CreatePart(header)
		if err != nil {
			return
		}
		file, err := os.Open(path)
		if err != nil {
			return
		}
		_, copyErr := io.Copy(part, file)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			return
		}
	}
	_ = writer.Close()
}

func (s *Server) dicomwebInstanceForPath(w http.ResponseWriter, r *http.Request) (archive.Instance, bool) {
	instance, err := s.session.Catalog().InstanceBySOPInstanceUID(r.Context(), r.PathValue("sopUID"))
	if err != nil || instance.StudyInstanceUID != r.PathValue("studyUID") || instance.SeriesInstanceUID != r.PathValue("seriesUID") {
		writeJSON(w, http.StatusNotFound, apiResponse{Error: "DICOM resource not found"})
		return archive.Instance{}, false
	}
	return instance, true
}

func (s *Server) safeStoredObjectPath(instance archive.Instance) (string, error) {
	path := strings.TrimSpace(instance.StoredPath)
	if path == "" {
		return "", errDICOMwebObjectUnavailable
	}
	objectsRoot, err := filepath.Abs(filepath.Join(s.session.ArchiveDir(), "objects"))
	if err != nil {
		return "", errDICOMwebUnsafeStoredPath
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", errDICOMwebUnsafeStoredPath
	}
	rel, err := filepath.Rel(objectsRoot, absPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errDICOMwebUnsafeStoredPath
	}
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		return "", errDICOMwebObjectUnavailable
	}
	return absPath, nil
}

func writeDICOMwebObjectError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errDICOMwebObjectUnavailable):
		writeJSON(w, http.StatusNotFound, apiResponse{Error: errDICOMwebObjectUnavailable.Error()})
	case errors.Is(err, errDICOMwebUnsafeStoredPath):
		writeJSON(w, http.StatusInternalServerError, apiResponse{Error: errDICOMwebUnsafeStoredPath.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, apiResponse{Error: "DICOM object retrieval failed"})
	}
}

type qidoWindow struct {
	Limit  int
	Offset int
}

type qidoInstanceFilter struct {
	SOPInstanceUID string
	SOPClassUID    string
	InstanceNumber string
}

func qidoStudyFilters(values url.Values) (archive.StudyFilters, qidoWindow, error) {
	allowed := map[string]bool{
		"PatientName": true, "PatientID": true, "PatientBirthDate": true,
		"StudyDate": true, "AccessionNumber": true, "StudyDescription": true,
		"ModalitiesInStudy": true, "Modality": true, "StudyInstanceUID": true,
		"StudyID": true, "ReferringPhysicianName": true, "BodyPartExamined": true,
		"limit": true, "offset": true,
	}
	if err := rejectUnsupportedQIDOParams(values, allowed); err != nil {
		return archive.StudyFilters{}, qidoWindow{}, err
	}
	window, err := qidoWindowFromParams(values)
	if err != nil {
		return archive.StudyFilters{}, qidoWindow{}, err
	}
	from, to, err := qidoDateRange(values.Get("StudyDate"))
	if err != nil {
		return archive.StudyFilters{}, qidoWindow{}, err
	}
	filters := archive.StudyFilters{
		PatientName:        values.Get("PatientName"),
		PatientID:          values.Get("PatientID"),
		PatientBirthDate:   values.Get("PatientBirthDate"),
		AccessionNumber:    values.Get("AccessionNumber"),
		StudyDescription:   values.Get("StudyDescription"),
		StudyDateFrom:      from,
		StudyDateTo:        to,
		Modalities:         qidoList(values.Get("ModalitiesInStudy")),
		Modality:           values.Get("Modality"),
		StudyInstanceUID:   values.Get("StudyInstanceUID"),
		StudyID:            values.Get("StudyID"),
		ReferringPhysician: values.Get("ReferringPhysicianName"),
		BodyPart:           values.Get("BodyPartExamined"),
	}
	return filters, window, nil
}

func qidoSeriesFilters(values url.Values) (archive.SeriesFilters, string, qidoWindow, error) {
	allowed := map[string]bool{
		"Modality": true, "SeriesNumber": true, "SeriesDescription": true,
		"SeriesInstanceUID": true, "limit": true, "offset": true,
	}
	if err := rejectUnsupportedQIDOParams(values, allowed); err != nil {
		return archive.SeriesFilters{}, "", qidoWindow{}, err
	}
	window, err := qidoWindowFromParams(values)
	if err != nil {
		return archive.SeriesFilters{}, "", qidoWindow{}, err
	}
	return archive.SeriesFilters{
		Modality:          values.Get("Modality"),
		SeriesNumber:      values.Get("SeriesNumber"),
		SeriesDescription: values.Get("SeriesDescription"),
	}, strings.TrimSpace(values.Get("SeriesInstanceUID")), window, nil
}

func qidoInstanceFilters(values url.Values) (qidoInstanceFilter, qidoWindow, error) {
	allowed := map[string]bool{
		"SOPInstanceUID": true, "SOPClassUID": true, "InstanceNumber": true,
		"limit": true, "offset": true,
	}
	if err := rejectUnsupportedQIDOParams(values, allowed); err != nil {
		return qidoInstanceFilter{}, qidoWindow{}, err
	}
	window, err := qidoWindowFromParams(values)
	if err != nil {
		return qidoInstanceFilter{}, qidoWindow{}, err
	}
	return qidoInstanceFilter{
		SOPInstanceUID: strings.TrimSpace(values.Get("SOPInstanceUID")),
		SOPClassUID:    strings.TrimSpace(values.Get("SOPClassUID")),
		InstanceNumber: strings.TrimSpace(values.Get("InstanceNumber")),
	}, window, nil
}

func rejectUnsupportedQIDOParams(values url.Values, allowed map[string]bool) error {
	for key := range values {
		if !allowed[key] {
			return fmt.Errorf("unsupported QIDO parameter %q", key)
		}
	}
	return nil
}

func qidoWindowFromParams(values url.Values) (qidoWindow, error) {
	var window qidoWindow
	var err error
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		window.Limit, err = strconv.Atoi(raw)
		if err != nil || window.Limit < 0 {
			return qidoWindow{}, fmt.Errorf("limit must be a non-negative integer")
		}
	}
	if raw := strings.TrimSpace(values.Get("offset")); raw != "" {
		window.Offset, err = strconv.Atoi(raw)
		if err != nil || window.Offset < 0 {
			return qidoWindow{}, fmt.Errorf("offset must be a non-negative integer")
		}
	}
	return window, nil
}

func qidoDateRange(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", nil
	}
	parts := strings.Split(value, "-")
	if len(parts) == 1 {
		return parts[0], parts[0], nil
	}
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
	}
	return "", "", fmt.Errorf("StudyDate must be a DICOM date or date range")
}

func qidoList(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == '\\' || r == ','
	})
}

func applyWindow[T any](items []T, window qidoWindow) []T {
	if window.Offset >= len(items) {
		return nil
	}
	if window.Offset > 0 {
		items = items[window.Offset:]
	}
	if window.Limit > 0 && window.Limit < len(items) {
		items = items[:window.Limit]
	}
	return items
}

func filterSlice[T any](items []T, keep func(T) bool) []T {
	out := make([]T, 0, len(items))
	for _, item := range items {
		if keep(item) {
			out = append(out, item)
		}
	}
	return out
}

func qidoStudyDataset(study archive.Study) dicomJSONDataset {
	return dicomJSONDataset{
		"00080020": qidoValue("DA", study.StudyDate),
		"00080030": qidoValue("TM", study.StudyTime),
		"00080050": qidoValue("SH", study.AccessionNumber),
		"00080061": qidoValues("CS", strings.FieldsFunc(study.Modalities, func(r rune) bool { return r == ',' || r == '\\' })),
		"00080080": qidoValue("LO", study.InstitutionName),
		"00080090": qidoValue("PN", study.ReferringPhysicianName),
		"00081030": qidoValue("LO", study.StudyDescription),
		"00100010": qidoValue("PN", study.PatientName),
		"00100020": qidoValue("LO", study.PatientID),
		"00100030": qidoValue("DA", study.PatientBirthDate),
		"0020000D": qidoValue("UI", study.StudyInstanceUID),
		"00200010": qidoValue("SH", study.StudyID),
		"00201206": qidoValue("IS", strconv.Itoa(study.SeriesCount)),
		"00201208": qidoValue("IS", strconv.Itoa(study.InstanceCount)),
	}
}

func qidoSeriesDataset(series archive.Series) dicomJSONDataset {
	return dicomJSONDataset{
		"00080021": qidoValue("DA", series.SeriesDate),
		"00080031": qidoValue("TM", series.SeriesTime),
		"00080060": qidoValue("CS", series.Modality),
		"0008103E": qidoValue("LO", series.SeriesDescription),
		"0020000D": qidoValue("UI", series.StudyInstanceUID),
		"0020000E": qidoValue("UI", series.SeriesInstanceUID),
		"00200011": qidoValue("IS", series.SeriesNumber),
		"00201209": qidoValue("IS", strconv.Itoa(series.InstanceCount)),
	}
}

func qidoInstanceDataset(instance archive.Instance) dicomJSONDataset {
	return dicomJSONDataset{
		"00020010": qidoValue("UI", instance.TransferSyntaxUID),
		"00080016": qidoValue("UI", instance.SOPClassUID),
		"00080018": qidoValue("UI", instance.SOPInstanceUID),
		"00080060": qidoValue("CS", instance.Modality),
		"0020000D": qidoValue("UI", instance.StudyInstanceUID),
		"0020000E": qidoValue("UI", instance.SeriesInstanceUID),
		"00200013": qidoValue("IS", instance.InstanceNumber),
	}
}

func qidoValue(vr string, value string) dicomJSONValue {
	if strings.TrimSpace(value) == "" {
		return dicomJSONValue{VR: vr}
	}
	return dicomJSONValue{VR: vr, Value: []any{value}}
}

func qidoValues(vr string, values []string) dicomJSONValue {
	out := make([]any, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return dicomJSONValue{VR: vr}
	}
	return dicomJSONValue{VR: vr, Value: out}
}

func writeDICOMJSON(w http.ResponseWriter, datasets []dicomJSONDataset) {
	if len(datasets) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/dicom+json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(datasets)
}
