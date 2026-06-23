package web

import (
	"net/http"
	"testing"
)

func TestNetworkDiagnosticsRequiresStudyUIDForCStore(t *testing.T) {
	s := newTestServer(t)
	rec, _ := do(t, s, http.MethodPost, "/api/network/diagnostics", `{"nodeIDs":["node-1"],"includeCStore":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("diagnostics code = %d, want 400", rec.Code)
	}
}
