// Package web is a frontend for go-pacs built on the same scheme as the
// TaskForge desktop app: a small net/http server exposes the backend over a
// JSON API and serves an embedded static web UI, which a launcher renders in a
// native window (or the system browser).
//
// The server holds only a *core.Session — the frontend-agnostic backend — so it
// shares the exact same backend as the Fyne desktop frontend (cmd/pacs-gui).
// Handlers are deliberately thin: decode the request, call core, encode a JSON
// envelope. No DICOM or persistence logic lives here. Handlers are grouped into
// handlers_*.go files by domain area.
package web

import (
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"

	"github.com/ThalesMMS/Go-PACS/internal/core"
)

//go:embed static
var staticFS embed.FS

// Server exposes a *core.Session over HTTP/JSON plus the embedded web UI.
type Server struct {
	session *core.Session
	mux     *http.ServeMux
}

// NewServer builds a Server backed by session and registers its routes.
func NewServer(session *core.Session) *Server {
	s := &Server{session: session, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler returns the http.Handler serving the API and the static UI.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	// Config
	s.mux.HandleFunc("GET /api/config", s.handleGetConfig)
	s.mux.HandleFunc("PUT /api/config", s.handlePutConfig)

	// DICOM nodes + verify
	s.mux.HandleFunc("GET /api/nodes", s.handleListNodes)
	s.mux.HandleFunc("POST /api/nodes", s.handleCreateNode)
	s.mux.HandleFunc("PUT /api/nodes/{id}", s.handleUpdateNode)
	s.mux.HandleFunc("DELETE /api/nodes/{id}", s.handleDeleteNode)
	s.mux.HandleFunc("POST /api/echo", s.handleEcho)

	// Operation history (Tasks tab)
	s.mux.HandleFunc("GET /api/tasks", s.handleTasks)

	// Local archive browse / metadata / export (Archive tab)
	s.mux.HandleFunc("GET /api/archive/studies", s.handleArchiveStudies)
	s.mux.HandleFunc("GET /api/archive/studies/{studyUID}/series", s.handleArchiveSeries)
	s.mux.HandleFunc("GET /api/archive/studies/{studyUID}/instances", s.handleArchiveStudyInstances)
	s.mux.HandleFunc("GET /api/archive/series/{seriesUID}/instances", s.handleArchiveSeriesInstances)
	s.mux.HandleFunc("GET /api/archive/instances/{sopUID}", s.handleArchiveInstance)
	s.mux.HandleFunc("GET /api/archive/instances/{sopUID}/inspect", s.handleArchiveInspect)
	s.mux.HandleFunc("GET /api/archive/studies/{studyUID}/metadata", s.handleArchiveGetMetadata)
	s.mux.HandleFunc("PUT /api/archive/studies/{studyUID}/metadata", s.handleArchiveSetMetadata)
	s.mux.HandleFunc("POST /api/archive/studies/{studyUID}/anonymize", s.handleArchiveAnonymizeStudy)
	s.mux.HandleFunc("POST /api/archive/studies/{studyUID}/decompress", s.handleArchiveDecompressStudy)
	s.mux.HandleFunc("DELETE /api/archive/studies/{studyUID}", s.handleArchiveDeleteStudy)
	s.mux.HandleFunc("GET /api/archive/export/{kind}", s.handleArchiveExport)
	s.mux.HandleFunc("POST /api/archive/send", s.handleArchiveSend)
	s.mux.HandleFunc("POST /api/archive/import", s.handleArchiveImport)
	s.mux.HandleFunc("POST /api/archive/import-path", s.handleArchiveImportPath)
	s.mux.HandleFunc("POST /api/archive/export-path", s.handleArchiveExportPath)

	// Async job progress (SSE) + cancel
	s.mux.HandleFunc("GET /api/jobs/{id}/events", s.handleJobEvents)
	s.mux.HandleFunc("POST /api/jobs/{id}/cancel", s.handleJobCancel)

	// Auto query (Auto Query tab)
	s.mux.HandleFunc("GET /api/autoquery/profiles", s.handleAutoQueryProfiles)
	s.mux.HandleFunc("PUT /api/autoquery/profiles", s.handleAutoQuerySaveProfiles)
	s.mux.HandleFunc("POST /api/autoquery/run", s.handleAutoQueryRun)
	s.mux.HandleFunc("GET /api/autoquery/status", s.handleAutoQueryStatus)
	s.mux.HandleFunc("POST /api/autoquery/start", s.handleAutoQueryStart)
	s.mux.HandleFunc("POST /api/autoquery/stop", s.handleAutoQueryStop)

	// Query / retrieve (Query tab)
	s.mux.HandleFunc("GET /api/query/sources", s.handleQuerySources)
	s.mux.HandleFunc("POST /api/query/study", s.handleQueryStudy)
	s.mux.HandleFunc("POST /api/query/series", s.handleQuerySeries)
	s.mux.HandleFunc("POST /api/query/image", s.handleQueryImage)
	s.mux.HandleFunc("POST /api/query/retrieve", s.handleQueryRetrieve)

	// Storage SCP receiver / listener
	s.mux.HandleFunc("GET /api/receiver/status", s.handleReceiverStatus)
	s.mux.HandleFunc("GET /api/receiver/config", s.handleReceiverConfig)
	s.mux.HandleFunc("POST /api/receiver/start", s.handleReceiverStart)
	s.mux.HandleFunc("POST /api/receiver/stop", s.handleReceiverStop)
	s.mux.HandleFunc("POST /api/receiver/restart", s.handleReceiverRestart)

	// Unknown /api/* routes get a JSON 404 instead of the static 404 page.
	s.mux.HandleFunc("/api/", s.handleAPINotFound)

	// Static UI (catch-all; ServeMux longest-prefix keeps /api/* above "/").
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // embedded FS is built into the binary; this cannot fail.
	}
	s.mux.Handle("/", http.FileServer(http.FS(sub)))
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotFound, apiResponse{Error: "unknown API route: " + r.Method + " " + r.URL.Path})
}

// apiResponse is the uniform JSON envelope returned by every /api endpoint.
type apiResponse struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, resp apiResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

func writeData(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, apiResponse{OK: true, Data: data})
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, apiResponse{Error: err.Error()})
}

// decodeJSON decodes the request body into v, rejecting unknown fields.
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	return dec.Decode(v)
}
