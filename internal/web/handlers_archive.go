package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/core"
	"github.com/ThalesMMS/Go-PACS/internal/export"
)

const (
	defaultArchiveStudyLimit = 100
	maxArchiveStudyLimit     = 500
)

// studyFiltersFromQuery builds archive.StudyFilters from URL query parameters so
// the web frontend drives the same filtering the Fyne UI does.
func studyFiltersFromQuery(r *http.Request) archive.StudyFilters {
	q := r.URL.Query()
	f := archive.StudyFilters{
		PatientName:      q.Get("patientName"),
		PatientID:        q.Get("patientID"),
		AccessionNumber:  q.Get("accession"),
		StudyDescription: q.Get("description"),
		StudyDateFrom:    q.Get("dateFrom"),
		StudyDateTo:      q.Get("dateTo"),
		ImportedAtFrom:   q.Get("importedFrom"),
		ImportedAtTo:     q.Get("importedTo"),
		SourcePath:       q.Get("sourcePath"),
		Status:           q.Get("status"),
		HasComments:      q.Get("hasComments") == "true",

		AllFields:           q.Get("q"),
		PatientBirthDate:    q.Get("patientBirthDate"),
		StudyID:             q.Get("studyID"),
		StudyInstanceUID:    q.Get("studyInstanceUID"),
		Comments:            q.Get("comments"),
		BodyPart:            q.Get("bodyPart"),
		Modality:            q.Get("modality"),
		ReferringPhysician:  q.Get("referringPhysician"),
		PerformingPhysician: q.Get("performingPhysician"),
	}
	if m := strings.TrimSpace(q.Get("modalities")); m != "" {
		for _, part := range strings.Split(m, ",") {
			if p := strings.TrimSpace(part); p != "" {
				f.Modalities = append(f.Modalities, p)
			}
		}
	}
	return f
}

func seriesFiltersFromQuery(r *http.Request) archive.SeriesFilters {
	q := r.URL.Query()
	return archive.SeriesFilters{
		Modality:          q.Get("seriesModality"),
		SeriesNumber:      q.Get("seriesNumber"),
		SeriesDescription: q.Get("seriesDescription"),
	}
}

func (s *Server) handleArchiveStudies(w http.ResponseWriter, r *http.Request) {
	if _, hasLimit := r.URL.Query()["limit"]; hasLimit || r.URL.Query().Has("offset") {
		opts, err := studyPageOptionsFromQuery(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		page, err := s.session.Catalog().StudiesPageWithFilters(r.Context(), studyFiltersFromQuery(r), opts)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeData(w, page)
		return
	}
	studies, err := s.session.Catalog().StudiesWithFilters(r.Context(), studyFiltersFromQuery(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, studies)
}

func studyPageOptionsFromQuery(r *http.Request) (archive.StudyPageOptions, error) {
	q := r.URL.Query()
	limit := defaultArchiveStudyLimit
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return archive.StudyPageOptions{}, fmt.Errorf("limit must be a positive integer")
		}
		limit = n
	}
	if limit > maxArchiveStudyLimit {
		limit = maxArchiveStudyLimit
	}
	offset := 0
	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return archive.StudyPageOptions{}, fmt.Errorf("offset must be zero or a positive integer")
		}
		offset = n
	}
	return archive.StudyPageOptions{Limit: limit, Offset: offset}, nil
}

func (s *Server) handleArchiveSeries(w http.ResponseWriter, r *http.Request) {
	series, err := s.session.Catalog().SeriesForStudyWithFilters(r.Context(), r.PathValue("studyUID"), seriesFiltersFromQuery(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, series)
}

func (s *Server) handleArchiveSeriesInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := s.session.Catalog().InstancesForSeries(r.Context(), r.PathValue("seriesUID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, instances)
}

func (s *Server) handleArchiveStudyInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := s.session.Catalog().InstancesForStudy(r.Context(), r.PathValue("studyUID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, instances)
}

func (s *Server) handleArchiveInstance(w http.ResponseWriter, r *http.Request) {
	inst, err := s.session.Catalog().InstanceBySOPInstanceUID(r.Context(), r.PathValue("sopUID"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeData(w, inst)
}

func (s *Server) handleArchiveInspect(w http.ResponseWriter, r *http.Request) {
	summary, err := s.session.InspectInstance(r.Context(), r.PathValue("sopUID"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeData(w, summary)
}

func (s *Server) handleArchivePreview(w http.ResponseWriter, r *http.Request) {
	size := strings.TrimSpace(r.URL.Query().Get("size"))
	if size != "" && size != "thumb" && size != "large" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Error: "size must be thumb or large"})
		return
	}
	data, err := s.session.PreviewInstancePNG(r.Context(), r.PathValue("sopUID"), size)
	if err != nil {
		status := http.StatusNotFound
		if errors.Is(err, core.ErrPreviewUnsupported) {
			status = http.StatusUnsupportedMediaType
		}
		writeJSON(w, status, apiResponse{Error: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handleArchiveGetMetadata(w http.ResponseWriter, r *http.Request) {
	md, err := s.session.Catalog().StudyMetadata(r.Context(), r.PathValue("studyUID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, md)
}

func (s *Server) handleArchiveSetMetadata(w http.ResponseWriter, r *http.Request) {
	var md archive.StudyMetadata
	if err := decodeJSON(r, &md); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.session.Catalog().SetStudyMetadata(r.Context(), r.PathValue("studyUID"), md); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, md)
}

func (s *Server) handleArchiveAnonymizeStudy(w http.ResponseWriter, r *http.Request) {
	studyUID := strings.TrimSpace(r.PathValue("studyUID"))
	if studyUID == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Error: "study UID is required"})
		return
	}
	job := s.session.StartAnonymizeStudyJob(studyUID)
	writeData(w, map[string]string{"jobID": job.ID, "kind": job.Kind})
}

func (s *Server) handleArchiveDecompressStudy(w http.ResponseWriter, r *http.Request) {
	studyUID := strings.TrimSpace(r.PathValue("studyUID"))
	if studyUID == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Error: "study UID is required"})
		return
	}
	report, err := s.session.Catalog().DecompressStudy(r.Context(), studyUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, report)
}

func (s *Server) handleArchiveDeleteStudy(w http.ResponseWriter, r *http.Request) {
	count, err := s.session.Catalog().TrashStudy(r.Context(), r.PathValue("studyUID"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, archive.ErrStudyNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}
	_, _ = s.session.PurgeExpiredTrash(r.Context())
	writeData(w, map[string]int{"trashedObjects": count})
}

func (s *Server) handleArchiveVerify(w http.ResponseWriter, r *http.Request) {
	result, err := s.session.VerifyArchive(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, result)
}

func (s *Server) handleArchiveTrashList(w http.ResponseWriter, r *http.Request) {
	entries, err := s.session.Catalog().ListTrash(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, entries)
}

func (s *Server) handleArchiveTrashRestore(w http.ResponseWriter, r *http.Request) {
	report, err := s.session.Catalog().RestoreStudy(r.Context(), r.PathValue("studyUID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	_, _ = s.session.PurgeExpiredTrash(r.Context())
	writeData(w, report)
}

func (s *Server) handleArchiveTrashPurge(w http.ResponseWriter, r *http.Request) {
	if err := s.session.Catalog().PurgeStudy(r.Context(), r.PathValue("studyUID")); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	_, _ = s.session.PurgeExpiredTrash(r.Context())
	writeData(w, map[string]bool{"purged": true})
}

func (s *Server) handleArchiveTrashPurgeExpired(w http.ResponseWriter, r *http.Request) {
	report, err := s.session.PurgeExpiredTrash(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, report)
}

func (s *Server) handleArchiveStorage(w http.ResponseWriter, r *http.Request) {
	status, err := s.session.StorageStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, status)
}

func (s *Server) handleArchiveSetStoragePolicy(w http.ResponseWriter, r *http.Request) {
	var policy core.StoragePolicy
	if err := decodeJSON(r, &policy); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	policy, err := s.session.SaveStoragePolicy(policy)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeData(w, policy)
}

func (s *Server) handleArchiveBackupPath(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DestPath string `json:"destPath"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	req.DestPath = strings.TrimSpace(req.DestPath)
	if req.DestPath == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Error: "destPath is required"})
		return
	}
	result, err := s.session.BackupArchive(r.Context(), req.DestPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, result)
}

func (s *Server) handleArchiveRestorePath(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BackupPath     string `json:"backupPath"`
		DestPath       string `json:"destPath"`
		AllowOverwrite bool   `json:"allowOverwrite"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	req.BackupPath = strings.TrimSpace(req.BackupPath)
	req.DestPath = strings.TrimSpace(req.DestPath)
	if req.BackupPath == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Error: "backupPath is required"})
		return
	}
	if req.DestPath == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Error: "destPath is required"})
		return
	}
	result, err := s.session.RestoreBackup(r.Context(), req.BackupPath, req.DestPath, req.AllowOverwrite)
	if err != nil {
		writeError(w, restoreErrorStatus(err), err)
		return
	}
	writeData(w, result)
}

func restoreErrorStatus(err error) int {
	switch {
	case errors.Is(err, core.ErrInvalidBackupManifest), errors.Is(err, core.ErrMissingBackupEntry):
		return http.StatusBadRequest
	case errors.Is(err, core.ErrOverwriteCurrentArchive):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// handleArchiveExport streams studies/series/instances as CSV or JSON, re-applying
// the same filters so the download matches what the user sees.
func (s *Server) handleArchiveExport(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}
	ctx := r.Context()
	cat := s.session.Catalog()

	if kind == "dicom" {
		s.handleArchiveExportDICOM(w, r)
		return
	}

	var (
		emitCSV  func(http.ResponseWriter) error
		emitJSON func(http.ResponseWriter) error
	)
	switch kind {
	case "studies":
		studies, err := cat.StudiesWithFilters(ctx, studyFiltersFromQuery(r))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		emitCSV = func(w http.ResponseWriter) error { return export.WriteStudiesCSV(w, studies) }
		emitJSON = func(w http.ResponseWriter) error { return export.WriteStudiesJSON(w, studies) }
	case "series":
		studyUID := r.URL.Query().Get("studyUID")
		series, err := cat.SeriesForStudyWithFilters(ctx, studyUID, seriesFiltersFromQuery(r))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		emitCSV = func(w http.ResponseWriter) error { return export.WriteSeriesCSV(w, series) }
		emitJSON = func(w http.ResponseWriter) error { return export.WriteSeriesJSON(w, series) }
	case "instances":
		instances, err := s.instancesForExport(r)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		emitCSV = func(w http.ResponseWriter) error { return export.WriteInstancesCSV(w, instances) }
		emitJSON = func(w http.ResponseWriter) error { return export.WriteInstancesJSON(w, instances) }
	default:
		writeJSON(w, http.StatusNotFound, apiResponse{Error: "unknown export kind: " + kind})
		return
	}

	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.json", kind))
		_ = emitJSON(w)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", kind))
	_ = emitCSV(w)
}

func (s *Server) instancesForExport(r *http.Request) ([]archive.Instance, error) {
	ctx := r.Context()
	cat := s.session.Catalog()
	if seriesUID := r.URL.Query().Get("seriesUID"); seriesUID != "" {
		return cat.InstancesForSeries(ctx, seriesUID)
	}
	return cat.InstancesForStudy(ctx, r.URL.Query().Get("studyUID"))
}
