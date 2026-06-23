package web

import (
	"net/http"

	"github.com/ThalesMMS/Go-PACS/internal/appconfig"
)

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.session.LoadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, cfg)
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var cfg appconfig.Config
	if err := decodeJSON(r, &cfg); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	normalized, err := appconfig.Normalize(cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.session.SaveConfig(normalized); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, normalized)
}
