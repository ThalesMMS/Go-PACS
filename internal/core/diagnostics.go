package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/netverify"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/query"
	"github.com/ThalesMMS/Go-PACS/internal/send"
)

type NetworkDiagnosticRequest struct {
	NodeIDs       []string `json:"nodeIDs"`
	StudyUID      string   `json:"studyUID"`
	IncludeCStore bool     `json:"includeCStore"`
}

type NetworkDiagnosticResult struct {
	NodeID   string                  `json:"nodeID"`
	NodeName string                  `json:"nodeName"`
	Protocol string                  `json:"protocol"`
	Steps    []NetworkDiagnosticStep `json:"steps"`
}

type NetworkDiagnosticStep struct {
	Name                         string   `json:"name"`
	Status                       string   `json:"status"`
	Success                      bool     `json:"success"`
	DurationMS                   int64    `json:"durationMs,omitempty"`
	Error                        string   `json:"error,omitempty"`
	StatusCode                   string   `json:"statusCode,omitempty"`
	Count                        int      `json:"count,omitempty"`
	Attempted                    int      `json:"attempted,omitempty"`
	Sent                         int      `json:"sent,omitempty"`
	Warnings                     int      `json:"warnings,omitempty"`
	Failed                       int      `json:"failed,omitempty"`
	RequestedTransferSyntaxUIDs  []string `json:"requestedTransferSyntaxUIDs,omitempty"`
	NegotiatedTransferSyntaxUIDs []string `json:"negotiatedTransferSyntaxUIDs,omitempty"`
}

func (s *Session) RunNetworkDiagnostics(ctx context.Context, req NetworkDiagnosticRequest) ([]NetworkDiagnosticResult, error) {
	req.StudyUID = strings.TrimSpace(req.StudyUID)
	if req.IncludeCStore && req.StudyUID == "" {
		return nil, fmt.Errorf("studyUID is required when includeCStore is true")
	}
	targets, err := s.diagnosticNodes(req.NodeIDs)
	if err != nil {
		return nil, err
	}
	results := make([]NetworkDiagnosticResult, 0, len(targets))
	for _, node := range targets {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		result := NetworkDiagnosticResult{
			NodeID:   node.ID,
			NodeName: node.Name,
			Protocol: node.ProtocolOrDefault(),
		}
		if node.IsDICOMweb() {
			result.Steps = append(result.Steps, s.diagnosticDICOMwebVerify(ctx, node))
		} else {
			result.Steps = append(result.Steps, s.diagnosticDIMSEEcho(ctx, node))
		}
		if req.StudyUID != "" {
			result.Steps = append(result.Steps, s.diagnosticFind(ctx, node, req.StudyUID))
		}
		if req.IncludeCStore {
			result.Steps = append(result.Steps, s.diagnosticStore(ctx, node, req.StudyUID))
		}
		results = append(results, result)
	}
	return results, nil
}

func (s *Session) diagnosticNodes(ids []string) ([]nodes.Node, error) {
	all, err := s.ListNodes()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return all, nil
	}
	index := map[string]nodes.Node{}
	for _, node := range all {
		index[node.ID] = node
		index[node.Name] = node
	}
	out := make([]nodes.Node, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		node, ok := index[id]
		if !ok {
			out = append(out, nodes.Node{ID: id, Name: id})
			continue
		}
		out = append(out, node)
	}
	return out, nil
}

func (s *Session) diagnosticDIMSEEcho(ctx context.Context, node nodes.Node) NetworkDiagnosticStep {
	cfg, err := s.LoadConfig()
	if err != nil {
		return failedStep("c-echo", err)
	}
	result, err := netverify.Echo(ctx, node, cfg.LocalAETitle)
	step := NetworkDiagnosticStep{
		Name:       "c-echo",
		Status:     "ok",
		Success:    err == nil && result.Status == 0,
		DurationMS: durationMS(result.Duration),
		StatusCode: fmt.Sprintf("0x%04X", result.Status),
	}
	if err != nil {
		step.Status = "failed"
		step.Error = err.Error()
	}
	return step
}

func (s *Session) diagnosticDICOMwebVerify(ctx context.Context, node nodes.Node) NetworkDiagnosticStep {
	result, err := netverify.VerifyDICOMweb(ctx, node, netverify.NoOpCredentialResolver{})
	step := NetworkDiagnosticStep{
		Name:       "dicomweb-verify",
		Status:     "ok",
		Success:    err == nil && result.Success,
		DurationMS: durationMS(result.Duration),
		StatusCode: fmt.Sprintf("%d", result.StatusCode),
	}
	if err != nil {
		step.Status = "failed"
		step.Error = err.Error()
	}
	return step
}

func (s *Session) diagnosticFind(ctx context.Context, node nodes.Node, studyUID string) NetworkDiagnosticStep {
	start := time.Now()
	result, err := QueryStudySource(ctx, node, query.Criteria{StudyInstanceUID: studyUID, MaxResults: 25}, s.callingAETitle())
	step := NetworkDiagnosticStep{
		Name:       "c-find",
		Status:     "ok",
		Success:    err == nil && result.FinalStatus == 0,
		DurationMS: durationMS(result.Duration),
		StatusCode: fmt.Sprintf("0x%04X", result.FinalStatus),
		Count:      len(result.Matches),
	}
	if step.DurationMS == 0 {
		step.DurationMS = durationMS(time.Since(start))
	}
	if err != nil {
		step.Status = "failed"
		step.Error = err.Error()
	}
	if node.IsDICOMweb() {
		step.Name = "qido-find"
	}
	return step
}

func (s *Session) diagnosticStore(ctx context.Context, node nodes.Node, studyUID string) NetworkDiagnosticStep {
	if !node.SendEnabled() {
		return NetworkDiagnosticStep{Name: "c-store", Status: "skipped", Error: "node send is disabled"}
	}
	outcome, err := s.Send(ctx, node, "STUDY", studyUID, "", "", nil)
	step := NetworkDiagnosticStep{
		Name:                         "c-store",
		Status:                       "ok",
		Success:                      err == nil && outcome.Failed == 0,
		DurationMS:                   durationMS(outcome.Duration),
		Attempted:                    outcome.Attempted,
		Sent:                         outcome.Sent,
		Warnings:                     outcome.Warnings,
		Failed:                       outcome.Failed,
		RequestedTransferSyntaxUIDs:  transferSyntaxUIDs(outcome.Results, true),
		NegotiatedTransferSyntaxUIDs: transferSyntaxUIDs(outcome.Results, false),
	}
	if node.IsDICOMweb() {
		step.Name = "stow-rs"
	}
	if err != nil {
		step.Status = "failed"
		step.Error = err.Error()
	}
	return step
}

// failedStep creates a diagnostic step with the given name and failure status, using the provided error's message if the error is non-nil.
func failedStep(name string, err error) NetworkDiagnosticStep {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return NetworkDiagnosticStep{Name: name, Status: "failed", Error: msg}
}

// durationMS converts a duration to milliseconds, returning zero for durations less than or equal to zero.
func durationMS(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return int64(duration / time.Millisecond)
}

// transferSyntaxUIDs collects and deduplicates transfer syntax UIDs from send results.
// If requested is true, it extracts RequestedTransferSyntaxUID; otherwise it extracts
// NegotiatedTransferSyntaxUID. Duplicates and empty strings are excluded.
func transferSyntaxUIDs(results []send.Result, requested bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, result := range results {
		uid := result.NegotiatedTransferSyntaxUID
		if requested {
			uid = result.RequestedTransferSyntaxUID
		}
		uid = strings.TrimSpace(uid)
		if uid == "" || seen[uid] {
			continue
		}
		seen[uid] = true
		out = append(out, uid)
	}
	return out
}
