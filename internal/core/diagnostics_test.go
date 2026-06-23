package core

import (
	"context"
	"net/http"
	"net/http/httptest"
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
