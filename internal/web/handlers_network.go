package web

import (
	"net/http"
	"strings"

	"github.com/ThalesMMS/Go-PACS/internal/core"
)

func (s *Server) handleNetworkDiagnostics(w http.ResponseWriter, r *http.Request) {
	var req core.NetworkDiagnosticRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.IncludeCStore && strings.TrimSpace(req.StudyUID) == "" {
		writeJSON(w, http.StatusBadRequest, apiResponse{Error: "studyUID is required when includeCStore is true"})
		return
	}
	results, err := s.session.RunNetworkDiagnostics(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, results)
}
