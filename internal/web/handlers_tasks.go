package web

import "net/http"

// handleTasks returns the persisted operation history (Tasks tab).
func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	history, err := s.session.LoadHistory()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, history)
}
