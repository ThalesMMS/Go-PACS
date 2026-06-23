package web

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/tokens"
)

type Middleware func(http.Handler) http.Handler

// Chain combines multiple middlewares into a single middleware.
func Chain(middlewares ...Middleware) Middleware {
	return func(next http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
}

type contextKey string

const (
	authInfoContextKey       contextKey = "authInfo"
	authInfoHolderContextKey contextKey = "authInfoHolder"
	requestIDContextKey      contextKey = "requestID"
)

type AuthInfo struct {
	TokenID string
	Role    string
}

type authInfoHolder struct {
	info AuthInfo
	set  bool
}

// AuthInfoFromContext retrieves authentication information stored in the request context. It returns the AuthInfo and a boolean indicating whether it was successfully found.
func AuthInfoFromContext(ctx context.Context) (AuthInfo, bool) {
	if info, ok := ctx.Value(authInfoContextKey).(AuthInfo); ok {
		return info, true
	}
	holder, ok := ctx.Value(authInfoHolderContextKey).(*authInfoHolder)
	if !ok || !holder.set {
		return AuthInfo{}, false
	}
	return holder.info, true
}

// RequestIDFromContext retrieves the request ID from the context. If the request ID is not present or not a string, it returns an empty string.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDContextKey).(string)
	return id
}

// BearerAuth returns a middleware that validates Bearer tokens and enforces role-based authorization.
func BearerAuth(store *tokens.Store, requiredRole string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			plaintext, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				writeJSON(w, http.StatusUnauthorized, apiResponse{Error: "missing or malformed bearer token"})
				return
			}
			if store == nil {
				writeJSON(w, http.StatusUnauthorized, apiResponse{Error: "token store unavailable"})
				return
			}
			token, err := store.GetByHash(tokens.HashToken(plaintext))
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, apiResponse{Error: "invalid bearer token"})
				return
			}
			ctx := withAuthInfo(r.Context(), AuthInfo{
				TokenID: token.ID,
				Role:    token.Role,
			})
			r = r.WithContext(ctx)
			if token.RevokedAt != "" {
				writeJSON(w, http.StatusUnauthorized, apiResponse{Error: "token revoked"})
				return
			}
			if !tokens.RoleAllows(token.Role, requiredRole) {
				writeJSON(w, http.StatusForbidden, apiResponse{Error: "insufficient token role"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// withAuthInfo stores authentication info in the context, updating the auth-info holder if one exists.
func withAuthInfo(ctx context.Context, info AuthInfo) context.Context {
	if holder, ok := ctx.Value(authInfoHolderContextKey).(*authInfoHolder); ok {
		holder.info = info
		holder.set = true
	}
	return context.WithValue(ctx, authInfoContextKey, info)
}

// bearerToken extracts and validates a Bearer token from an Authorization header.
// It returns the token and true if the header is valid, empty string and false otherwise.
func bearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", false
	}
	return parts[1], true
}

// AuditMiddleware returns middleware that records an audit entry after the handler completes.
// The middleware generates a unique request ID, initializes context storage for authentication information, captures response status and bytes written, and records an audit entry containing the operation (method and path), UID scope derived from the URL path, status code, response size, request duration, and an error summary for responses with HTTP status greater than or equal to 400.
// If the catalog is nil, the middleware returns without writing audit data.
// Audit records are written with a 2-second timeout; any write errors are logged and do not affect the response.
func AuditMiddleware(catalog *archive.Catalog) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := uuid.NewString()
			holder := &authInfoHolder{}
			ctx := context.WithValue(r.Context(), authInfoHolderContextKey, holder)
			ctx = context.WithValue(ctx, requestIDContextKey, requestID)
			r = r.WithContext(ctx)
			recorder := &auditResponseRecorder{ResponseWriter: w}
			start := time.Now()

			next.ServeHTTP(recorder, r)

			if catalog == nil {
				return
			}
			auth, _ := AuthInfoFromContext(r.Context())
			record := archive.AuditRecord{
				RequestID:  requestID,
				TokenID:    auth.TokenID,
				RemoteAddr: r.RemoteAddr,
				Operation:  r.Method + " " + r.URL.Path,
				UIDScope:   uidScopeFromPath(r.URL.Path),
				Status:     recorder.statusCode(),
				Bytes:      recorder.bytes,
				DurationMS: int64(time.Since(start) / time.Millisecond),
				OccurredAt: time.Now().UTC(),
			}
			if record.Status >= 400 {
				record.ErrorSummary = http.StatusText(record.Status)
			}
			writeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := catalog.WriteAuditRecord(writeCtx, record); err != nil {
				log.Printf("write DICOMweb audit record: %v", err)
			}
		})
	}
}

type auditResponseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *auditResponseRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *auditResponseRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(data)
	r.bytes += int64(n)
	return n, err
}

func (r *auditResponseRecorder) statusCode() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

// uidScopeFromPath extracts UID scope identifiers from a URL path, returning a space-separated string of scope pairs formatted as "study=", "series=", and "instance=" pairs derived from path segments.
func uidScopeFromPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	var scope []string
	for i := 0; i+1 < len(parts); i++ {
		switch strings.ToLower(parts[i]) {
		case "studies":
			scope = append(scope, "study="+parts[i+1])
		case "series":
			scope = append(scope, "series="+parts[i+1])
		case "instances":
			scope = append(scope, "instance="+parts[i+1])
		}
	}
	return strings.Join(scope, " ")
}
