package web

import (
	"errors"
	"net/http"

	"github.com/ThalesMMS/Go-PACS/internal/core"
)

func (s *Server) handleReceiverStatus(w http.ResponseWriter, r *http.Request) {
	writeData(w, s.session.ReceiverStatus())
}

// handleReceiverConfig returns the listener-relevant configuration plus a
// preview of the node-derived allowlists, so the Network tab can render the
// listener settings and what will be allowed.
func (s *Server) handleReceiverConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.session.LoadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, cfg)
}

func (s *Server) handleReceiverStart(w http.ResponseWriter, r *http.Request) {
	status, err := s.session.StartReceiver(r.Context())
	if err != nil {
		s.writeReceiverError(w, status, err)
		return
	}
	writeData(w, status)
}

func (s *Server) handleReceiverStop(w http.ResponseWriter, r *http.Request) {
	status, err := s.session.StopReceiver(r.Context())
	if err != nil {
		s.writeReceiverError(w, status, err)
		return
	}
	writeData(w, status)
}

func (s *Server) handleReceiverRestart(w http.ResponseWriter, r *http.Request) {
	status, err := s.session.RestartReceiver(r.Context())
	if err != nil {
		s.writeReceiverError(w, status, err)
		return
	}
	writeData(w, status)
}

// writeReceiverError maps lifecycle conflicts to 409 and other failures to 500,
// returning the current status as data so the UI can still update.
func (s *Server) writeReceiverError(w http.ResponseWriter, status core.ReceiverStatus, err error) {
	code := http.StatusInternalServerError
	if errors.Is(err, core.ErrReceiverRunning) || errors.Is(err, core.ErrReceiverNotRunning) {
		code = http.StatusConflict
	}
	writeJSON(w, code, apiResponse{OK: false, Data: status, Error: err.Error()})
}
