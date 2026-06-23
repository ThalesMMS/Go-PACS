package web

import (
	"errors"
	"net/http"

	"github.com/ThalesMMS/Go-PACS/internal/autoquery"
	"github.com/ThalesMMS/Go-PACS/internal/core"
)

func (s *Server) handleAutoQueryProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := s.session.ListAutoQueryProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, profiles)
}

// handleAutoQuerySaveProfiles persists the full profile list (the editor sends
// the whole set).
func (s *Server) handleAutoQuerySaveProfiles(w http.ResponseWriter, r *http.Request) {
	var profiles []autoquery.Profile
	if err := decodeJSON(r, &profiles); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.session.SaveAutoQueryProfiles(profiles); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeData(w, profiles)
}

func (s *Server) handleAutoQueryStatus(w http.ResponseWriter, r *http.Request) {
	writeData(w, s.session.AutoQueryStatus())
}

func (s *Server) handleAutoQueryStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Profile         autoquery.Profile `json:"profile"`
		IntervalSeconds int               `json:"intervalSeconds"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	status, err := s.session.StartAutoQuery(req.Profile, req.IntervalSeconds)
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, core.ErrAutoQueryRunning) {
			code = http.StatusConflict
		}
		writeJSON(w, code, apiResponse{OK: false, Data: status, Error: err.Error()})
		return
	}
	writeData(w, status)
}

func (s *Server) handleAutoQueryStop(w http.ResponseWriter, r *http.Request) {
	status, err := s.session.StopAutoQuery()
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, core.ErrAutoQueryNotRunning) {
			code = http.StatusConflict
		}
		writeJSON(w, code, apiResponse{OK: false, Data: status, Error: err.Error()})
		return
	}
	writeData(w, status)
}

// handleAutoQueryRun runs a profile's query now and returns the matches.
func (s *Server) handleAutoQueryRun(w http.ResponseWriter, r *http.Request) {
	var profile autoquery.Profile
	if err := decodeJSON(r, &profile); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, qerr := s.session.RunAutoQuery(r.Context(), profile, nil)
	resp := queryResponse{Matches: result.Matches}
	var failures *core.QuerySourceFailures
	if errors.As(qerr, &failures) {
		resp.Failures = failures.Messages
	} else if qerr != nil {
		writeError(w, http.StatusInternalServerError, qerr)
		return
	}
	writeData(w, resp)
}
