package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ThalesMMS/Go-PACS/internal/nodes"
)

func TestRunNetworkDiagnosticsRecordsPerNodeFailureWithoutAbortingBatch(t *testing.T) {
	okServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dicom+json")
		_, _ = w.Write([]byte("[]"))
	}))
	defer okServer.Close()
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer failServer.Close()

	okNode, err := nodes.NewNode(nodes.Draft{Name: "ok-web", Protocol: nodes.ProtocolDICOMweb, BaseURL: okServer.URL, QIDOPathPrefix: "/"})
	if err != nil {
		t.Fatal(err)
	}
	failNode, err := nodes.NewNode(nodes.Draft{Name: "fail-web", Protocol: nodes.ProtocolDICOMweb, BaseURL: failServer.URL, QIDOPathPrefix: "/"})
	if err != nil {
		t.Fatal(err)
	}
	sess := openTestSession(t)
	if err := sess.SaveNodes([]nodes.Node{okNode, failNode}); err != nil {
		t.Fatal(err)
	}

	results, err := sess.RunNetworkDiagnostics(context.Background(), NetworkDiagnosticRequest{NodeIDs: []string{okNode.ID, failNode.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if len(results[0].Steps) != 1 || !results[0].Steps[0].Success {
		t.Fatalf("first result = %#v, want success", results[0])
	}
	if len(results[1].Steps) != 1 || results[1].Steps[0].Success || results[1].Steps[0].Error == "" {
		t.Fatalf("second result = %#v, want recorded failure", results[1])
	}
}

func TestRunNetworkDiagnosticsRequiresStudyUIDWhenIncludeCStoreTrue(t *testing.T) {
	sess := openTestSession(t)

	_, err := sess.RunNetworkDiagnostics(context.Background(), NetworkDiagnosticRequest{
		IncludeCStore: true,
		StudyUID:      "",
	})
	if err == nil {
		t.Fatal("RunNetworkDiagnostics should error when IncludeCStore=true and StudyUID is empty")
	}
	if !strings.Contains(err.Error(), "studyUID") {
		t.Fatalf("error = %q, want studyUID mention", err.Error())
	}
}

func TestRunNetworkDiagnosticsReturnsDICOMwebProtocolInResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dicom+json")
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()

	node, err := nodes.NewNode(nodes.Draft{
		Name:           "dicomweb-proto",
		Protocol:       nodes.ProtocolDICOMweb,
		BaseURL:        server.URL,
		QIDOPathPrefix: "/",
	})
	if err != nil {
		t.Fatal(err)
	}
	sess := openTestSession(t)
	if err := sess.SaveNodes([]nodes.Node{node}); err != nil {
		t.Fatal(err)
	}

	results, err := sess.RunNetworkDiagnostics(context.Background(), NetworkDiagnosticRequest{NodeIDs: []string{node.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results count = %d, want 1", len(results))
	}
	if results[0].Protocol != nodes.ProtocolDICOMweb {
		t.Fatalf("Protocol = %q, want %q", results[0].Protocol, nodes.ProtocolDICOMweb)
	}
}

func TestRunNetworkDiagnosticsIncludesFindStepWhenStudyUIDProvided(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dicom+json")
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()

	node, err := nodes.NewNode(nodes.Draft{
		Name:           "find-step",
		Protocol:       nodes.ProtocolDICOMweb,
		BaseURL:        server.URL,
		QIDOPathPrefix: "/",
	})
	if err != nil {
		t.Fatal(err)
	}
	sess := openTestSession(t)
	if err := sess.SaveNodes([]nodes.Node{node}); err != nil {
		t.Fatal(err)
	}

	results, err := sess.RunNetworkDiagnostics(context.Background(), NetworkDiagnosticRequest{
		NodeIDs:  []string{node.ID},
		StudyUID: "1.2.826.0.1.3680043.10.543.diag",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results count = %d, want 1", len(results))
	}
	// With StudyUID set, we expect both a verify step and a find step.
	if len(results[0].Steps) < 2 {
		t.Fatalf("steps count = %d, want >= 2 (verify + find)", len(results[0].Steps))
	}
	findStep := results[0].Steps[1]
	if findStep.Name != "qido-find" {
		t.Fatalf("second step name = %q, want qido-find", findStep.Name)
	}
}

func TestRunNetworkDiagnosticsUsesAllNodesWhenIDsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dicom+json")
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()

	nodeA, err := nodes.NewNode(nodes.Draft{Name: "all-nodes-a", Protocol: nodes.ProtocolDICOMweb, BaseURL: server.URL, QIDOPathPrefix: "/"})
	if err != nil {
		t.Fatal(err)
	}
	nodeB, err := nodes.NewNode(nodes.Draft{Name: "all-nodes-b", Protocol: nodes.ProtocolDICOMweb, BaseURL: server.URL, QIDOPathPrefix: "/"})
	if err != nil {
		t.Fatal(err)
	}
	sess := openTestSession(t)
	if err := sess.SaveNodes([]nodes.Node{nodeA, nodeB}); err != nil {
		t.Fatal(err)
	}

	// Empty NodeIDs slice — should target all configured nodes.
	results, err := sess.RunNetworkDiagnostics(context.Background(), NetworkDiagnosticRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results count = %d, want 2 (one per node)", len(results))
	}
}
