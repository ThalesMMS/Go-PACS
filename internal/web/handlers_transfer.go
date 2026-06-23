package web

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
)

var (
	errNoFiles = errors.New("no files uploaded")
	errNoFlush = errors.New("streaming unsupported")
)

// handleArchiveSend transmits an archived study/series/image to a node via
// C-STORE.
func (s *Server) handleArchiveSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID    string `json:"nodeID"`
		Level     string `json:"level"`
		StudyUID  string `json:"studyUID"`
		SeriesUID string `json:"seriesUID"`
		SopUID    string `json:"sopUID"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	node, found, err := s.findNode(req.NodeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, apiResponse{Error: "node not found"})
		return
	}
	// Send runs asynchronously; the client streams progress over
	// /api/jobs/{id}/events.
	job := s.session.StartSendJob(node, req.Level, req.StudyUID, req.SeriesUID, req.SopUID)
	writeData(w, map[string]string{"jobID": job.ID, "kind": job.Kind})
}

// handleArchiveImport accepts multipart-uploaded DICOM files (or a .zip), streams
// them to a temporary directory, and imports them into the local archive. The
// catalog auto-detects .zip archives.
func (s *Server) handleArchiveImport(w http.ResponseWriter, r *http.Request) {
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	tempDir, err := os.MkdirTemp("", "gopacs-import-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer os.RemoveAll(tempDir)

	count := 0
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		name := filepath.Base(part.FileName()) // strip any client path components
		if name == "" || name == "." || name == string(filepath.Separator) {
			part.Close()
			continue
		}
		dst, err := os.Create(filepath.Join(tempDir, name))
		if err != nil {
			part.Close()
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if _, err := io.Copy(dst, part); err != nil {
			dst.Close()
			part.Close()
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		dst.Close()
		part.Close()
		count++
	}
	if count == 0 {
		writeError(w, http.StatusBadRequest, errNoFiles)
		return
	}

	report, err := s.session.ImportPath(r.Context(), tempDir, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, apiResponse{OK: false, Data: report, Error: err.Error()})
		return
	}
	writeData(w, report)
}

// handleArchiveImportPath imports one or more absolute server-side paths
// (files, directories, or .zip archives) chosen via the native macOS open
// panel. Each path is imported through session.ImportPath and the per-path
// reports are accumulated into a single combined report. A path that fails to
// import is recorded as a rejection but does not abort the remaining imports.
func (s *Server) handleArchiveImportPath(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Paths) == 0 {
		writeError(w, http.StatusBadRequest, errNoFiles)
		return
	}

	var combined archive.ImportReport
	for _, path := range req.Paths {
		report, err := s.session.ImportPath(r.Context(), path, nil)
		combined.ScannedFiles += report.ScannedFiles
		combined.StoredFiles += report.StoredFiles
		combined.Duplicates += report.Duplicates
		combined.InvalidFiles += report.InvalidFiles
		combined.Rejections = append(combined.Rejections, report.Rejections...)
		if err != nil {
			// Record the failure and continue with the remaining paths.
			combined.Rejections = append(combined.Rejections, archive.Rejection{
				Path:   path,
				Reason: err.Error(),
			})
		}
	}
	writeData(w, combined)
}
