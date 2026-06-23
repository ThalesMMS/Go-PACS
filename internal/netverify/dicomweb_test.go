package netverify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ThalesMMS/Go-PACS/internal/nodes"
)

type staticCredentialResolver struct {
	credential Credential
	err        error
	ref        string
}

func (r *staticCredentialResolver) Resolve(ref string) (Credential, error) {
	r.ref = ref
	if r.err != nil {
		return Credential{}, r.err
	}
	return r.credential, nil
}

func TestVerifyDICOMwebSuccessfulQIDOProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dicom-web/qido-rs/studies" {
			t.Fatalf("path = %q, want /dicom-web/qido-rs/studies", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "1" {
			t.Fatalf("limit = %q, want 1", r.URL.Query().Get("limit"))
		}
		w.Header().Set("Content-Type", "application/dicom+json")
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()

	node := nodes.Node{
		Name:           "web",
		Protocol:       nodes.ProtocolDICOMweb,
		BaseURL:        server.URL + "/dicom-web",
		QIDOPathPrefix: "/qido-rs",
	}
	result, err := VerifyDICOMweb(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("Success = false, result = %+v", result)
	}
	if result.NodeName != "web" || result.StatusCode != http.StatusOK || result.URL == "" || result.StartedAt.IsZero() {
		t.Fatalf("result = %+v", result)
	}
}

func TestVerifyDICOMwebReportsAuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "good" || password != "secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	resolver := &staticCredentialResolver{credential: Credential{
		Type:     CredentialTypeBasic,
		Username: "bad",
		Password: "wrong",
	}}
	node := nodes.Node{
		Name:          "web",
		Protocol:      nodes.ProtocolDICOMweb,
		BaseURL:       server.URL,
		CredentialRef: "basic-ref",
	}
	result, err := VerifyDICOMweb(context.Background(), node, resolver)
	if err == nil {
		t.Fatal("VerifyDICOMweb succeeded with bad credentials")
	}
	if result.Success || result.StatusCode != http.StatusUnauthorized {
		t.Fatalf("result = %+v, want unsuccessful 401", result)
	}
	if resolver.ref != "basic-ref" {
		t.Fatalf("resolver ref = %q, want basic-ref", resolver.ref)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %q, want status code", err.Error())
	}
}

func TestVerifyDICOMwebHonorsContextTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	node := nodes.Node{Name: "slow", Protocol: nodes.ProtocolDICOMweb, BaseURL: server.URL}
	result, err := VerifyDICOMweb(ctx, node, nil)
	if err == nil {
		t.Fatal("VerifyDICOMweb succeeded after context timeout")
	}
	if result.Success {
		t.Fatalf("Success = true after timeout: %+v", result)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %q, want timeout detail", err.Error())
	}
}
