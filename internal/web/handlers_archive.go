package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/export"
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
	studies, err := s.session.Catalog().StudiesWithFilters(r.Context(), studyFiltersFromQuery(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, studies)
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
	count, err := s.session.Catalog().DeleteStudy(r.Context(), r.PathValue("studyUID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, map[string]int{"deletedObjects": count})
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
