package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ThalesMMS/Go-PACS/internal/appconfig"
	"github.com/ThalesMMS/Go-PACS/internal/archive"
	dicomcore "github.com/ThalesMMS/dicom-go/core"
	"github.com/ThalesMMS/dicom-go/object"
)

var (
	errDICOMwebObjectUnavailable = errors.New("DICOM object unavailable")
	errDICOMwebUnsafeStoredPath  = errors.New("stored DICOM object path is invalid")
	errDICOMwebPartTooLarge      = errors.New("DICOMweb part exceeds max_file_import_bytes")
)

const (
	stowReferencedSOPSequence = "00081199"
	stowFailedSOPSequence     = "00081198"
	stowReferencedSOPClassUID = "00081150"
	stowReferencedSOPUID      = "00081155"
	stowFailureReason         = "00081197"

	stowFailureOutOfResources   = 0xA700
	stowFailureCannotUnderstand = 0xC000
)

var (
	tagDICOMwebMediaStorageSOPClassUID    = dicomcore.NewTag(0x0002, 0x0002)
	tagDICOMwebMediaStorageSOPInstanceUID = dicomcore.NewTag(0x0002, 0x0003)
	tagDICOMwebSOPClassUID                = dicomcore.NewTag(0x0008, 0x0016)
	tagDICOMwebSOPInstanceUID             = dicomcore.NewTag(0x0008, 0x0018)
)

type dicomJSONDataset map[string]dicomJSONValue

type dicomJSONValue struct {
	VR    string `json:"vr"`
	Value []any  `json:"Value,omitempty"`
}

type dicomwebStoreReference struct {
	SOPClassUID    string
	SOPInstanceUID string
}

type dicomwebStoreFailure struct {
	Reference dicomwebStoreReference
	Reason    int
}

type dicomwebStoreResult struct {
	Stored []dicomwebStoreReference
	Failed []dicomwebStoreFailure
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

func (s *Server) handleDICOMwebStoreStudies(w http.ResponseWriter, r *http.Request) {
	reader, err := dicomwebStoreMultipartReader(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg, err := s.session.LoadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	limits := dicomwebImportLimits(cfg)
	result := dicomwebStoreResult{}
	partCount := 0
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("read DICOMweb multipart part: %w", err))
			return
		}
		partCount++
		if limits.MaxImportTotalFiles > 0 && partCount > limits.MaxImportTotalFiles {
			_, _ = io.Copy(io.Discard, part)
			_ = part.Close()
			result.Failed = append(result.Failed, dicomwebStoreFailure{Reason: stowFailureOutOfResources})
			continue
		}
		ref, failure, err := s.importDICOMwebStorePart(r, part, partCount, limits)
		_ = part.Close()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if failure != nil {
			result.Failed = append(result.Failed, *failure)
			continue
		}
		result.Stored = append(result.Stored, ref)
	}
	if partCount == 0 {
		writeError(w, http.StatusBadRequest, errors.New("STOW-RS request contains no DICOM parts"))
		return
	}
	status := http.StatusOK
	if len(result.Failed) > 0 {
		status = http.StatusAccepted
	}
	writeDICOMJSONDataset(w, status, result.Dataset())
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

// WriteDICOMwebMetadata writes instance metadata as a DICOMweb JSON response. If no instances are present, it writes an HTTP 404 error response instead.
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
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		return "", errDICOMwebObjectUnavailable
	}
	objectsRoot, err = filepath.EvalSymlinks(objectsRoot)
	if err != nil {
		return "", errDICOMwebUnsafeStoredPath
	}
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", errDICOMwebUnsafeStoredPath
	}
	rel, err := filepath.Rel(objectsRoot, resolvedPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errDICOMwebUnsafeStoredPath
	}
	return resolvedPath, nil
}

// dicomwebStoreMultipartReader creates a multipart reader from an HTTP request for STOW-RS operations. It validates that the Content-Type header specifies multipart/related with a required boundary parameter and an optional application/dicom type parameter.
func dicomwebStoreMultipartReader(r *http.Request) (*multipart.Reader, error) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("STOW-RS Content-Type is invalid: %w", err)
	}
	if !strings.EqualFold(mediaType, "multipart/related") {
		return nil, fmt.Errorf("STOW-RS Content-Type must be multipart/related")
	}
	if params["boundary"] == "" {
		return nil, fmt.Errorf("STOW-RS multipart boundary is required")
	}
	if contentType := strings.TrimSpace(params["type"]); contentType != "" && !strings.EqualFold(contentType, "application/dicom") {
		return nil, fmt.Errorf("STOW-RS multipart type must be application/dicom")
	}
	return multipart.NewReader(r.Body, params["boundary"]), nil
}

func (s *Server) importDICOMwebStorePart(r *http.Request, part *multipart.Part, index int, limits archive.ImportLimits) (dicomwebStoreReference, *dicomwebStoreFailure, error) {
	mediaType, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/dicom") {
		_, _ = io.Copy(io.Discard, part)
		return dicomwebStoreReference{}, &dicomwebStoreFailure{Reason: stowFailureCannotUnderstand}, nil
	}
	temp, err := os.CreateTemp(s.session.ArchiveDir(), ".stow-*.dcm")
	if err != nil {
		return dicomwebStoreReference{}, nil, fmt.Errorf("create STOW-RS temp file: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()

	_, copyErr := copyDICOMwebStorePart(temp, part, limits.MaxFileImportBytes)
	closeErr := temp.Close()
	if copyErr != nil && !errors.Is(copyErr, errDICOMwebPartTooLarge) {
		return dicomwebStoreReference{}, nil, fmt.Errorf("copy STOW-RS DICOM part: %w", copyErr)
	}
	if closeErr != nil {
		return dicomwebStoreReference{}, nil, fmt.Errorf("close STOW-RS temp file: %w", closeErr)
	}

	ref, _ := dicomwebStoreReferenceFromFile(tempPath)
	if errors.Is(copyErr, errDICOMwebPartTooLarge) {
		return ref, &dicomwebStoreFailure{Reference: ref, Reason: stowFailureOutOfResources}, nil
	}

	report, err := s.session.Catalog().ImportPart10FileWithOptions(r.Context(), tempPath, fmt.Sprintf("stow-rs://part/%d", index), archive.ImportOptions{Limits: limits})
	if err != nil {
		return ref, &dicomwebStoreFailure{Reference: ref, Reason: stowFailureCannotUnderstand}, nil
	}
	if report.StoredFiles > 0 || report.Duplicates > 0 {
		return ref, nil, nil
	}
	return ref, &dicomwebStoreFailure{Reference: ref, Reason: stowFailureReasonForReport(report)}, nil
}

// CopyDICOMwebStorePart copies data from src to dst, optionally enforcing a maximum size limit. If maxBytes is less than or equal to zero, all data is copied without restriction. If data exceeds maxBytes, the remaining source is drained and errDICOMwebPartTooLarge is returned. It returns the number of bytes copied and any error encountered.
func copyDICOMwebStorePart(dst io.Writer, src io.Reader, maxBytes int64) (int64, error) {
	if maxBytes <= 0 {
		return io.Copy(dst, src)
	}
	limit := maxBytes + 1
	copied, err := io.Copy(dst, io.LimitReader(src, limit))
	if err != nil {
		return copied, err
	}
	if copied > maxBytes {
		_, _ = io.Copy(io.Discard, src)
		return copied, errDICOMwebPartTooLarge
	}
	return copied, nil
}

// dicomwebStoreReferenceFromFile extracts the SOP Class and Instance UIDs from the DICOM file at the given path.
func dicomwebStoreReferenceFromFile(path string) (dicomwebStoreReference, error) {
	file, err := os.Open(path)
	if err != nil {
		return dicomwebStoreReference{}, err
	}
	defer file.Close()
	dicomFile, err := object.ReadFile(file)
	if err != nil {
		return dicomwebStoreReference{}, err
	}
	defer dicomFile.Close()
	return dicomwebStoreReference{
		SOPClassUID:    dicomwebFileUID(dicomFile, tagDICOMwebMediaStorageSOPClassUID, tagDICOMwebSOPClassUID),
		SOPInstanceUID: dicomwebFileUID(dicomFile, tagDICOMwebMediaStorageSOPInstanceUID, tagDICOMwebSOPInstanceUID),
	}, nil
}

// dicomwebFileUID returns a UID from the file, using the preferred tag if found, otherwise using the fallback tag.
func dicomwebFileUID(file *object.File, preferred dicomcore.Tag, fallback dicomcore.Tag) string {
	if value, ok := file.GetUID(preferred); ok {
		return value
	}
	if value, ok := file.GetUID(fallback); ok {
		return value
	}
	return ""
}

// stowFailureReasonForReport returns the STOW-RS failure reason code based on the import report's rejections.
func stowFailureReasonForReport(report archive.ImportReport) int {
	for _, rejection := range report.Rejections {
		if strings.Contains(rejection.Reason, "max_file_import_bytes") {
			return stowFailureOutOfResources
		}
	}
	return stowFailureCannotUnderstand
}

// dicomwebImportLimits converts server configuration to import limits, treating absent values as zero.
func dicomwebImportLimits(cfg appconfig.Config) archive.ImportLimits {
	d64 := func(p *int64) int64 {
		if p != nil {
			return *p
		}
		return 0
	}
	di := func(p *int) int {
		if p != nil {
			return *p
		}
		return 0
	}
	return archive.ImportLimits{
		MaxFileImportBytes:      d64(cfg.MaxFileImportBytes),
		MaxZipEntryBytes:        d64(cfg.MaxZipEntryBytes),
		MaxZipTotalBytes:        d64(cfg.MaxZipTotalBytes),
		MaxZipEntryCount:        di(cfg.MaxZipEntryCount),
		MaxImportTotalFiles:     di(cfg.MaxImportTotalFiles),
		MaxImportPathLength:     di(cfg.MaxImportPathLength),
		MaxImportDirectoryDepth: di(cfg.MaxImportDirectoryDepth),
	}
}

func (r dicomwebStoreResult) Dataset() dicomJSONDataset {
	dataset := dicomJSONDataset{}
	if len(r.Stored) > 0 {
		items := make([]dicomJSONDataset, 0, len(r.Stored))
		for _, ref := range r.Stored {
			items = append(items, dicomwebReferencedSOPItem(ref))
		}
		dataset[stowReferencedSOPSequence] = dicomwebSequenceValue(items)
	}
	if len(r.Failed) > 0 {
		items := make([]dicomJSONDataset, 0, len(r.Failed))
		for _, failure := range r.Failed {
			item := dicomwebReferencedSOPItem(failure.Reference)
			item[stowFailureReason] = dicomJSONValue{VR: "US", Value: []any{failure.Reason}}
			items = append(items, item)
		}
		dataset[stowFailedSOPSequence] = dicomwebSequenceValue(items)
	}
	return dataset
}

// DicomwebReferencedSOPItem builds a DICOM JSON dataset item containing the referenced SOP Class and Instance UIDs from a store reference.
func dicomwebReferencedSOPItem(ref dicomwebStoreReference) dicomJSONDataset {
	item := dicomJSONDataset{}
	if strings.TrimSpace(ref.SOPClassUID) != "" {
		item[stowReferencedSOPClassUID] = qidoValue("UI", ref.SOPClassUID)
	}
	if strings.TrimSpace(ref.SOPInstanceUID) != "" {
		item[stowReferencedSOPUID] = qidoValue("UI", ref.SOPInstanceUID)
	}
	return item
}

// dicomwebSequenceValue creates a DICOM JSON sequence value from a list of datasets.
func dicomwebSequenceValue(items []dicomJSONDataset) dicomJSONValue {
	values := make([]any, 0, len(items))
	for _, item := range items {
		values = append(values, item)
	}
	return dicomJSONValue{VR: "SQ", Value: values}
}

// writeDICOMwebObjectError writes a JSON-formatted HTTP error response with a status code appropriate to the error.
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

// qidoStudyFilters parses study-level filters and pagination parameters from QIDO query values.
// It returns the filters, pagination window, and any error if parameters are invalid.
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

// QidoSeriesFilters parses series-level QIDO-RS query filters and pagination parameters from URL query values. It rejects unsupported parameters and returns the parsed filters, the series instance UID, pagination window, and any parsing or validation error.
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

// qidoInstanceFilters parses instance-level QIDO query parameters, validates that only supported parameters are present, and returns the extracted filters and pagination window.
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

// rejectUnsupportedQIDOParams validates that all query parameter keys are in the allowed set, returning an error if any unsupported parameters are found.
func rejectUnsupportedQIDOParams(values url.Values, allowed map[string]bool) error {
	for key := range values {
		if !allowed[key] {
			return fmt.Errorf("unsupported QIDO parameter %q", key)
		}
	}
	return nil
}

// qidoWindowFromParams parses limit and offset pagination parameters from URL query values. Empty or missing values default to zero. It returns an error if either parameter cannot be parsed as a non-negative integer.
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

// qidoDateRange parses a DICOM date or date range string for QIDO queries. An empty string returns zero values. A single date returns that date as both the start and end. A date range is specified as two dates separated by a hyphen.
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

// qidoList splits value by commas and backslashes.
func qidoList(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == '\\' || r == ','
	})
}

// applyWindow returns items with pagination applied according to the offset and limit in window. If offset exceeds len(items), applyWindow returns nil.
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

// filterSlice returns a new slice containing only the elements of items for which keep returns true.
func filterSlice[T any](items []T, keep func(T) bool) []T {
	out := make([]T, 0, len(items))
	for _, item := range items {
		if keep(item) {
			out = append(out, item)
		}
	}
	return out
}

// QidoStudyDataset converts a study record to a DICOM JSON dataset with study and patient attributes mapped to standard DICOM tags.
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

// qidoSeriesDataset encodes a series as a DICOM JSON dataset.
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

// QidoInstanceDataset converts an instance to a DICOM JSON dataset.
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

// qidoValue constructs a DICOM JSON value with the specified value representation and the given value if it is non-empty.
func qidoValue(vr string, value string) dicomJSONValue {
	if strings.TrimSpace(value) == "" {
		return dicomJSONValue{VR: vr}
	}
	return dicomJSONValue{VR: vr, Value: []any{value}}
}

// qidoValues creates a DICOM JSON value from a list of string values, excluding empty or whitespace-only strings.
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

// writeDICOMJSONDataset writes a single DICOM JSON dataset to the HTTP response with the specified status code.
func writeDICOMJSONDataset(w http.ResponseWriter, status int, dataset dicomJSONDataset) {
	w.Header().Set("Content-Type", "application/dicom+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(dataset)
}

// writeDICOMJSON writes datasets as DICOM JSON to w, with HTTP 204 No Content if datasets is empty.
func writeDICOMJSON(w http.ResponseWriter, datasets []dicomJSONDataset) {
	if len(datasets) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/dicom+json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(datasets)
}
