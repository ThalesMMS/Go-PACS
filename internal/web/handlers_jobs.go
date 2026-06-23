package web

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// handleJobEvents streams a job's lifecycle events as Server-Sent Events. The
// stream replays buffered events, then live progress, and ends when the job
// finishes (channel close) or the client disconnects.
func (s *Server) handleJobEvents(w http.ResponseWriter, r *http.Request) {
	job, ok := s.session.Job(r.PathValue("id"))
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errNoFlush)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	ch := job.Subscribe()
	for {
		select {
		case ev, open := <-ch:
			if !open {
				return
			}
			data, _ := json.Marshal(ev)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// handleJobCancel requests cancellation of a running job.
func (s *Server) handleJobCancel(w http.ResponseWriter, r *http.Request) {
	if s.session.CancelJob(r.PathValue("id")) {
		writeData(w, map[string]bool{"cancelled": true})
		return
	}
	writeJSON(w, http.StatusNotFound, apiResponse{Error: "job not found"})
}
