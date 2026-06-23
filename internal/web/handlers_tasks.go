package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/ThalesMMS/Go-PACS/internal/core"
)

// handleTasks returns the persisted operation history (Tasks tab).
func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	history, err := s.session.LoadHistory()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, history)
}

func (s *Server) handleTaskCanRetry(w http.ResponseWriter, r *http.Request) {
	index, ok := taskIndexFromRequest(w, r)
	if !ok {
		return
	}
	eligibility, err := s.session.CanRetryTask(r.Context(), index)
	if err != nil {
		writeTaskRetryError(w, err)
		return
	}
	writeData(w, eligibility)
}

func (s *Server) handleTaskRetry(w http.ResponseWriter, r *http.Request) {
	index, ok := taskIndexFromRequest(w, r)
	if !ok {
		return
	}
	eligibility, err := s.session.CanRetryTask(r.Context(), index)
	if err != nil {
		writeTaskRetryError(w, err)
		return
	}
	if !eligibility.CanRetry {
		writeJSON(w, http.StatusBadRequest, apiResponse{Error: eligibility.Reason})
		return
	}
	job := s.session.StartRetryJob(index)
	writeData(w, map[string]string{"jobID": job.ID, "kind": job.Kind})
}

func taskIndexFromRequest(w http.ResponseWriter, r *http.Request) (int, bool) {
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || index < 0 {
		writeJSON(w, http.StatusBadRequest, apiResponse{Error: "invalid task index"})
		return 0, false
	}
	return index, true
}

func writeTaskRetryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, core.ErrTaskIndexOutOfRange):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}
