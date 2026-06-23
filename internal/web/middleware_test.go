package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/tokens"
)

func TestBearerAuthEnforcesTokensAndRoles(t *testing.T) {
	store := tokens.NewStore(filepath.Join(t.TempDir(), "tokens.json"))
	readToken, readPlaintext, err := store.Create(tokens.Draft{Name: "read", Role: tokens.RoleRead})
	if err != nil {
		t.Fatal(err)
	}
	writeToken, writePlaintext, err := store.Create(tokens.Draft{Name: "write", Role: tokens.RoleWrite})
	if err != nil {
		t.Fatal(err)
	}
	revokedToken, revokedPlaintext, err := store.Create(tokens.Draft{Name: "revoked", Role: tokens.RoleWrite})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(revokedToken.ID); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		requiredRole string
		header       string
		wantStatus   int
		wantTokenID  string
	}{
		{name: "missing", requiredRole: tokens.RoleRead, wantStatus: http.StatusUnauthorized},
		{name: "malformed", requiredRole: tokens.RoleRead, header: "Basic " + readPlaintext, wantStatus: http.StatusUnauthorized},
		{name: "invalid", requiredRole: tokens.RoleRead, header: "Bearer not-a-token", wantStatus: http.StatusUnauthorized},
		{name: "revoked", requiredRole: tokens.RoleRead, header: "Bearer " + revokedPlaintext, wantStatus: http.StatusUnauthorized, wantTokenID: revokedToken.ID},
		{name: "read on read", requiredRole: tokens.RoleRead, header: "Bearer " + readPlaintext, wantStatus: http.StatusNoContent, wantTokenID: readToken.ID},
		{name: "read on write", requiredRole: tokens.RoleWrite, header: "Bearer " + readPlaintext, wantStatus: http.StatusForbidden, wantTokenID: readToken.ID},
		{name: "write on read", requiredRole: tokens.RoleRead, header: "Bearer " + writePlaintext, wantStatus: http.StatusNoContent, wantTokenID: writeToken.ID},
		{name: "write on write", requiredRole: tokens.RoleWrite, header: "Bearer " + writePlaintext, wantStatus: http.StatusNoContent, wantTokenID: writeToken.ID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotAuth AuthInfo
			var gotAuthOK bool
			handler := BearerAuth(store, tt.requiredRole)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth, gotAuthOK = AuthInfoFromContext(r.Context())
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodGet, "/dicomweb/studies", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus == http.StatusNoContent {
				if !gotAuthOK || gotAuth.TokenID != tt.wantTokenID {
					t.Fatalf("AuthInfo = %#v ok=%v, want token ID %q", gotAuth, gotAuthOK, tt.wantTokenID)
				}
			}
		})
	}
}

func TestAuditMiddlewareWritesAllowedAndDeniedRecords(t *testing.T) {
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	store := tokens.NewStore(filepath.Join(t.TempDir(), "tokens.json"))
	readToken, readPlaintext, err := store.Create(tokens.Draft{Name: "read", Role: tokens.RoleRead})
	if err != nil {
		t.Fatal(err)
	}
	writeToken, writePlaintext, err := store.Create(tokens.Draft{Name: "write", Role: tokens.RoleWrite})
	if err != nil {
		t.Fatal(err)
	}

	handler := Chain(
		AuditMiddleware(catalog),
		BearerAuth(store, tokens.RoleWrite),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestIDFromContext(r.Context()) == "" {
			t.Error("request ID missing from context")
		}
		if _, err := w.Write([]byte("ok")); err != nil {
			t.Error(err)
		}
	}))

	allowed := httptest.NewRequest(http.MethodPost, "/dicomweb/studies/1.2.3/series/4.5/instances/6.7?PatientName=DOE", nil)
	allowed.RemoteAddr = "192.0.2.10:12345"
	allowed.Header.Set("Authorization", "Bearer "+writePlaintext)
	allowedRec := httptest.NewRecorder()
	handler.ServeHTTP(allowedRec, allowed)
	if allowedRec.Code != http.StatusOK {
		t.Fatalf("allowed status = %d, want 200", allowedRec.Code)
	}

	denied := httptest.NewRequest(http.MethodPost, "/dicomweb/studies/1.2.3", nil)
	denied.RemoteAddr = "192.0.2.11:12345"
	denied.Header.Set("Authorization", "Bearer "+readPlaintext)
	deniedRec := httptest.NewRecorder()
	handler.ServeHTTP(deniedRec, denied)
	if deniedRec.Code != http.StatusForbidden {
		t.Fatalf("denied status = %d, want 403", deniedRec.Code)
	}

	records, err := catalog.AuditRecords(allowed.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2: %#v", len(records), records)
	}
	if records[0].TokenID != writeToken.ID || records[0].Status != http.StatusOK || records[0].Bytes != 2 {
		t.Fatalf("allowed audit = %#v", records[0])
	}
	if records[0].RemoteAddr != "192.0.2.10:12345" || records[0].Operation != "POST /dicomweb/studies/1.2.3/series/4.5/instances/6.7" {
		t.Fatalf("allowed audit address/operation = %#v", records[0])
	}
	if records[0].DurationMS < 0 || records[0].OccurredAt.IsZero() {
		t.Fatalf("allowed audit timing = %#v", records[0])
	}
	if records[0].UIDScope != "study=1.2.3 series=4.5 instance=6.7" {
		t.Fatalf("UIDScope = %q", records[0].UIDScope)
	}
	if records[1].TokenID != readToken.ID || records[1].Status != http.StatusForbidden || records[1].ErrorSummary == "" {
		t.Fatalf("denied audit = %#v", records[1])
	}
	if records[0].RequestID == "" || records[0].RequestID == records[1].RequestID {
		t.Fatalf("request IDs not unique: %#v", records)
	}

	rendered := fmt.Sprintf("%#v", records)
	for _, forbidden := range []string{writePlaintext, readPlaintext, writeToken.TokenHash, readToken.TokenHash, "DOE"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("audit records leaked forbidden value %q: %s", forbidden, rendered)
		}
	}
}

func TestAuditMiddlewareFailureDoesNotFailRequest(t *testing.T) {
	catalog, err := archive.Open(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Close(); err != nil {
		t.Fatal(err)
	}
	handler := AuditMiddleware(catalog)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dicomweb/studies", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}
