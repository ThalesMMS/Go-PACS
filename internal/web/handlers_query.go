package web

import (
	"errors"
	"net/http"

	"github.com/ThalesMMS/Go-PACS/internal/core"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/query"
)

// handleQuerySources returns the nodes eligible to be queried, for source
// selection in the Query tab.
func (s *Server) handleQuerySources(w http.ResponseWriter, r *http.Request) {
	list, err := s.session.QueryEnabledNodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, list)
}

// resolveSources maps the requested node ids/names to nodes, defaulting to all
// query-enabled nodes when none are specified.
func (s *Server) resolveSources(ids []string) ([]nodes.Node, error) {
	if len(ids) == 0 {
		return s.session.QueryEnabledNodes()
	}
	all, err := s.session.ListNodes()
	if err != nil {
		return nil, err
	}
	index := map[string]nodes.Node{}
	for _, n := range all {
		index[n.ID] = n
		index[n.Name] = n
	}
	var out []nodes.Node
	for _, id := range ids {
		if n, ok := index[id]; ok {
			out = append(out, n)
		}
	}
	return out, nil
}

// queryResponse is the JSON shape returned by the query endpoints: the merged
// matches plus per-source failure information.
type queryResponse struct {
	Matches  []query.Match `json:"matches"`
	Failures []string      `json:"failures,omitempty"`
}

func buildQueryResponse(result query.Result, err error) (queryResponse, error) {
	resp := queryResponse{Matches: result.Matches}
	var failures *core.QuerySourceFailures
	if errors.As(err, &failures) {
		resp.Failures = failures.Messages
		return resp, nil // partial success: matches + failures, not a hard error
	}
	return resp, err
}

func (s *Server) handleQueryStudy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Criteria  query.Criteria `json:"criteria"`
		SourceIDs []string       `json:"sourceIDs"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	sources, err := s.resolveSources(req.SourceIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	result, qerr := s.session.QueryStudies(r.Context(), sources, req.Criteria, nil)
	resp, err := buildQueryResponse(result, qerr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, resp)
}

func (s *Server) handleQuerySeries(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Criteria  query.SeriesCriteria `json:"criteria"`
		SourceIDs []string             `json:"sourceIDs"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	sources, err := s.resolveSources(req.SourceIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	result, qerr := s.session.QuerySeries(r.Context(), sources, req.Criteria, nil)
	resp, err := buildQueryResponse(result, qerr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, resp)
}

func (s *Server) handleQueryImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Criteria  query.ImageCriteria `json:"criteria"`
		SourceIDs []string            `json:"sourceIDs"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	sources, err := s.resolveSources(req.SourceIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	result, qerr := s.session.QueryImages(r.Context(), sources, req.Criteria, nil)
	resp, err := buildQueryResponse(result, qerr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeData(w, resp)
}

// handleQueryRetrieve retrieves a match (by level + UIDs) from a source node into
// the local archive.
func (s *Server) handleQueryRetrieve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID    string `json:"nodeID"`
		Level     string `json:"level"`
		StudyUID  string `json:"studyUID"`
		SeriesUID string `json:"seriesUID"`
		SopUID    string `json:"sopUID"`
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
	// Retrieve runs asynchronously; the client streams progress over
	// /api/jobs/{id}/events and may cancel via /api/jobs/{id}/cancel.
	job := s.session.StartRetrieveJob(node, req.Level, req.StudyUID, req.SeriesUID, req.SopUID)
	writeData(w, map[string]string{"jobID": job.ID, "kind": job.Kind})
}
