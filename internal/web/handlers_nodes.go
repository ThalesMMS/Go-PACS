package web

import (
	"net/http"

	"github.com/ThalesMMS/Go-PACS/internal/netverify"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
)

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	list, err := s.session.ListNodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, list)
}

func (s *Server) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	var draft nodes.Draft
	if err := decodeJSON(r, &draft); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	node, err := s.session.AddNode(draft)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeData(w, node)
}

func (s *Server) handleUpdateNode(w http.ResponseWriter, r *http.Request) {
	var draft nodes.Draft
	if err := decodeJSON(r, &draft); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	node, err := s.session.UpdateNode(r.PathValue("id"), draft)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeData(w, node)
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	if err := s.session.DeleteNode(r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeData(w, map[string]string{"deleted": r.PathValue("id")})
}

// handleEcho performs a C-ECHO against a configured node, proving a real DICOM
// operation runs entirely through core from a non-Fyne frontend.
func (s *Server) handleEcho(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID string `json:"nodeID"`
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
	cfg, err := s.session.LoadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	result, err := netverify.Echo(r.Context(), node, cfg.LocalAETitle)
	if err != nil {
		// The request was handled; the DICOM operation itself failed.
		writeJSON(w, http.StatusOK, apiResponse{OK: false, Error: err.Error()})
		return
	}
	writeData(w, result)
}

func (s *Server) findNode(idOrName string) (nodes.Node, bool, error) {
	list, err := s.session.ListNodes()
	if err != nil {
		return nodes.Node{}, false, err
	}
	for _, n := range list {
		if n.ID == idOrName || n.Name == idOrName {
			return n, true, nil
		}
	}
	return nodes.Node{}, false, nil
}
