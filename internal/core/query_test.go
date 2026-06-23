package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/query"
)

func TestRunQueryAcrossSourcesAnnotatesAndPreservesOrder(t *testing.T) {
	sources := []nodes.Node{
		{ID: "node-1", Name: "first", Host: "10.0.0.1", Port: 104},
		{ID: "node-2", Name: "second", Host: "10.0.0.2", Port: 105},
	}
	var calls []string

	result, err := RunQueryAcrossSources(context.Background(), sources, func(_ context.Context, node nodes.Node) (query.Result, error) {
		calls = append(calls, node.Name)
		return query.Result{Matches: []query.Match{{PatientName: node.Name}}, FinalStatus: 0x0000}, nil
	}, QueryObserver(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Join(calls, "|") != "first|second" {
		t.Fatalf("calls = %#v, want first then second", calls)
	}
	if len(result.Matches) != 2 {
		t.Fatalf("len(matches) = %d, want 2", len(result.Matches))
	}
	if result.Matches[0].SourceNodeID != "node-1" || result.Matches[1].SourceNodeID != "node-2" {
		t.Fatalf("matches not annotated with source: %+v", result.Matches)
	}
}

func TestRunQueryAcrossSourcesContinuesAndReportsFailures(t *testing.T) {
	sources := []nodes.Node{
		{ID: "node-1", Name: "offline"},
		{ID: "node-2", Name: "online"},
	}
	var updates []QueryProgress

	result, err := RunQueryAcrossSources(context.Background(), sources, func(_ context.Context, node nodes.Node) (query.Result, error) {
		if node.Name == "offline" {
			return query.Result{}, errors.New("association failed")
		}
		return query.Result{Matches: []query.Match{{PatientName: "A"}, {PatientName: "B"}}}, nil
	}, QueryObserverFunc(func(p QueryProgress) {
		updates = append(updates, p)
	}))

	var failures *QuerySourceFailures
	if !errors.As(err, &failures) {
		t.Fatalf("error = %v, want *QuerySourceFailures", err)
	}
	if failures.Successes != 1 {
		t.Fatalf("Successes = %d, want 1", failures.Successes)
	}
	if !failures.Failed(sources[0]) || failures.Failed(sources[1]) {
		t.Fatalf("Failed() did not isolate the offline source")
	}
	if !strings.Contains(err.Error(), "offline") || !strings.Contains(err.Error(), "association failed") {
		t.Fatalf("error message = %q", err.Error())
	}
	if len(result.Matches) != 2 {
		t.Fatalf("partial matches = %d, want 2", len(result.Matches))
	}
	if len(updates) != 2 {
		t.Fatalf("progress updates = %#v, want 2", updates)
	}
	if updates[0] != (QueryProgress{Attempted: 1, Total: 2, Matches: 0, Failures: 1}) {
		t.Fatalf("first update = %#v", updates[0])
	}
	if updates[1] != (QueryProgress{Attempted: 2, Total: 2, Matches: 2, Failures: 1}) {
		t.Fatalf("second update = %#v", updates[1])
	}
}

func TestRunQueryAcrossSourcesAllSucceedNoError(t *testing.T) {
	sources := []nodes.Node{{Name: "only"}}
	_, err := RunQueryAcrossSources(context.Background(), sources, func(context.Context, nodes.Node) (query.Result, error) {
		return query.Result{}, nil
	}, QueryObserver(nil))
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
}

func TestAnnotateMatchesDoesNotMutateInput(t *testing.T) {
	in := []query.Match{{PatientName: "DOE^JANE"}}
	node := nodes.Node{ID: "n", Name: "src", AETitle: "AE", Host: "h", Port: 7}
	out := AnnotateMatches(in, node)
	if out[0].SourceNodeName != "src" || out[0].SourceAETitle != "AE" {
		t.Fatalf("annotation missing: %+v", out[0])
	}
	if in[0].SourceNodeName != "" {
		t.Fatalf("AnnotateMatches mutated the input slice")
	}
}
