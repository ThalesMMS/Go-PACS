package web

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
)

// handleArchiveExportDICOM streams the stored .dcm files for a study (or a
// single series) as a ZIP archive. It is dispatched from handleArchiveExport
// when the export kind is "dicom".
func (s *Server) handleArchiveExportDICOM(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cat := s.session.Catalog()

	studyUID := strings.TrimSpace(r.URL.Query().Get("studyUID"))
	seriesUID := strings.TrimSpace(r.URL.Query().Get("seriesUID"))

	if studyUID == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Error: "studyUID is required"})
		return
	}

	var (
		instances []archive.Instance
		err       error
	)
	if seriesUID != "" {
		instances, err = cat.InstancesForSeries(ctx, seriesUID)
	} else {
		instances, err = cat.InstancesForStudy(ctx, studyUID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(instances) == 0 {
		writeJSON(w, http.StatusNotFound, apiResponse{Error: "no instances found for export"})
		return
	}

	filename := sanitizeZipComponent(studyUID)
	if filename == "" {
		filename = "studies"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.dcm.zip", filename))

	_, _ = writeInstancesZip(w, instances)
}

// handleArchiveExportPath writes the stored .dcm files for a study (or a single
// series) as a ZIP archive to an absolute destination path on disk, chosen via
// the native macOS save panel. As this is a localhost desktop app, a
// server-side destination path is expected and the file is created/truncated
// there.
func (s *Server) handleArchiveExportPath(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StudyUID  string `json:"studyUID"`
		SeriesUID string `json:"seriesUID"`
		Dest      string `json:"dest"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	studyUID := strings.TrimSpace(req.StudyUID)
	seriesUID := strings.TrimSpace(req.SeriesUID)
	dest := strings.TrimSpace(req.Dest)
	if studyUID == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Error: "studyUID is required"})
		return
	}
	if dest == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Error: "dest is required"})
		return
	}

	ctx := r.Context()
	cat := s.session.Catalog()

	var (
		instances []archive.Instance
		err       error
	)
	if seriesUID != "" {
		instances, err = cat.InstancesForSeries(ctx, seriesUID)
	} else {
		instances, err = cat.InstancesForStudy(ctx, studyUID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(instances) == 0 {
		writeJSON(w, http.StatusNotFound, apiResponse{Error: "no instances found for export"})
		return
	}

	file, err := os.Create(dest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer file.Close()

	count, err := writeInstancesZip(file, instances)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, map[string]any{"path": dest, "count": count})
}

// writeInstancesZip wraps w in a zip.Writer and writes each instance's stored
// .dcm file into it, using a sanitized "<series>/<sop>.dcm" layout with
// de-duplicated entry names. It skips instances with no stored path and any
// file that fails to open, returning the count of files actually written. The
// zip.Writer is finalized (closed) before returning.
func writeInstancesZip(w io.Writer, instances []archive.Instance) (int, error) {
	zw := zip.NewWriter(w)
	used := map[string]bool{}
	count := 0
	for _, inst := range instances {
		if strings.TrimSpace(inst.StoredPath) == "" {
			continue
		}
		seriesFolder := sanitizeZipComponent(inst.SeriesNumber)
		if seriesFolder == "" {
			seriesFolder = sanitizeZipComponent(inst.SeriesInstanceUID)
		}
		if seriesFolder == "" {
			seriesFolder = "series"
		}
		base := sanitizeZipComponent(inst.SOPInstanceUID)
		if base == "" {
			base = sanitizeZipComponent(inst.SHA256)
		}
		if base == "" {
			base = "instance"
		}

		entryName := uniqueZipEntryName(used, seriesFolder+"/"+base+".dcm")
		if err := copyFileIntoZip(zw, entryName, inst.StoredPath); err != nil {
			// Skip individual files that fail to open and continue with the rest.
			continue
		}
		count++
	}
	if err := zw.Close(); err != nil {
		return count, err
	}
	return count, nil
}

func copyFileIntoZip(zw *zip.Writer, entryName string, storedPath string) error {
	file, err := os.Open(storedPath)
	if err != nil {
		return err
	}
	defer file.Close()

	entry, err := zw.Create(entryName)
	if err != nil {
		return err
	}
	if _, err := io.Copy(entry, file); err != nil {
		return err
	}
	return nil
}

// sanitizeZipComponent strips path separators and traversal sequences so an
// untrusted UID/number cannot escape the archive layout.
func sanitizeZipComponent(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\\", "_")
	value = strings.ReplaceAll(value, "/", "_")
	value = strings.ReplaceAll(value, "..", "_")
	return strings.Trim(value, " .")
}

// uniqueZipEntryName de-duplicates entry names so two instances mapping to the
// same folder/name (e.g. missing SOP UID) do not collide inside the archive.
func uniqueZipEntryName(used map[string]bool, name string) string {
	candidate := name
	for i := 1; used[candidate]; i++ {
		if dot := strings.LastIndex(name, "."); dot >= 0 {
			candidate = fmt.Sprintf("%s_%d%s", name[:dot], i, name[dot:])
		} else {
			candidate = fmt.Sprintf("%s_%d", name, i)
		}
	}
	used[candidate] = true
	return candidate
}
