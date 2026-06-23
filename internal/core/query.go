package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/query"
)

// QueryProgress reports incremental progress while a query fans out across
// multiple sources. It is plain data so any frontend can render it.
type QueryProgress struct {
	Attempted int
	Total     int
	Matches   int
	Failures  int
}

// QuerySourceFunc runs a query against a single node and returns its result.
type QuerySourceFunc func(context.Context, nodes.Node) (query.Result, error)

// QueryObserver receives progress from a multi-source query, after each source
// completes. Methods are called from the goroutine driving the query, so a UI
// frontend marshals to its own thread and a web frontend streams to the client.
// A nil QueryObserver means progress is ignored.
type QueryObserver interface {
	QueryProgress(QueryProgress)
}

// QueryObserverFunc adapts a plain function to a QueryObserver.
type QueryObserverFunc func(QueryProgress)

// QueryProgress implements QueryObserver. A nil func value is a no-op.
func (f QueryObserverFunc) QueryProgress(p QueryProgress) {
	if f != nil {
		f(p)
	}
}

// QuerySourceFailures aggregates per-source failures from a multi-source query.
// It is returned as the error from RunQueryAcrossSources when at least one
// source failed; callers can still use any partial results that came back.
type QuerySourceFailures struct {
	// Successes counts sources that returned without error.
	Successes int
	// Messages holds one "<source>: <error>" string per failed source.
	Messages []string
	// FailedKeys is the set of nodes.Node.Key() values that failed, so callers
	// can map a failure back to a specific source.
	FailedKeys map[string]bool
}

// Error implements error.
func (e *QuerySourceFailures) Error() string {
	return strings.Join(e.Messages, "; ")
}

// Failed reports whether the given node was among the failed sources.
func (e *QuerySourceFailures) Failed(node nodes.Node) bool {
	return e != nil && e.FailedKeys[node.Key()]
}

// AnnotateMatches returns a copy of matches with each match tagged with the
// identity of the source node it came from. The input slice is not mutated.
func AnnotateMatches(matches []query.Match, node nodes.Node) []query.Match {
	out := make([]query.Match, len(matches))
	for i, match := range matches {
		match.SourceNodeID = node.ID
		match.SourceNodeName = node.Name
		match.SourceAETitle = node.AETitle
		match.SourceHost = node.Host
		match.SourcePort = node.Port
		out[i] = match
	}
	return out
}

// RunQueryAcrossSources runs run against each source in order, merging the
// matches (each annotated with its source) into a single query.Result and
// reporting progress after every source to obs (which may be nil).
//
// A failing source does not abort the fan-out: remaining sources are still
// queried. If any source failed, the returned error is a *QuerySourceFailures
// alongside whatever partial result was gathered; otherwise the error is nil.
// This is the canonical multi-source query workflow shared by every frontend.
func RunQueryAcrossSources(ctx context.Context, sources []nodes.Node, run QuerySourceFunc, obs QueryObserver) (query.Result, error) {
	var merged query.Result
	var messages []string
	failedKeys := map[string]bool{}
	successes := 0
	for i, source := range sources {
		result, err := run(ctx, source)
		if err != nil {
			messages = append(messages, fmt.Sprintf("%s: %s", sourceLabel(source), err.Error()))
			failedKeys[source.Key()] = true
			reportQueryProgress(obs, i+1, len(sources), len(merged.Matches), len(messages))
			continue
		}
		successes++
		merged.Matches = append(merged.Matches, AnnotateMatches(result.Matches, source)...)
		merged.FinalStatus = result.FinalStatus
		merged.Duration += result.Duration
		reportQueryProgress(obs, i+1, len(sources), len(merged.Matches), len(messages))
	}
	if len(messages) > 0 {
		return merged, &QuerySourceFailures{Successes: successes, Messages: messages, FailedKeys: failedKeys}
	}
	return merged, nil
}

func reportQueryProgress(obs QueryObserver, attempted, total, matches, failures int) {
	if obs == nil {
		return
	}
	obs.QueryProgress(QueryProgress{Attempted: attempted, Total: total, Matches: matches, Failures: failures})
}

func sourceLabel(node nodes.Node) string {
	if name := strings.TrimSpace(node.Name); name != "" {
		return name
	}
	return "-"
}
