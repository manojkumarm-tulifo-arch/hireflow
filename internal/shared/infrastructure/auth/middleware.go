package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// Middleware returns an HTTP middleware that verifies a bearer token via the
// supplied Verifier and attaches the resolved Identity to the request context.
// Failures short-circuit with 401 and a JSON error envelope.
func Middleware(v *Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := extractBearer(r)
			if err != nil {
				writeAuthError(w, "missing_token", "Authorization bearer token required")
				return
			}
			claims, err := v.Verify(token)
			if err != nil {
				code := "invalid_token"
				if errors.Is(err, ErrMissingClaim) {
					code = "missing_claim"
				}
				writeAuthError(w, code, "token rejected")
				return
			}

			tenantID, err := shared.ParseTenantID(claims.TenantID)
			if err != nil {
				writeAuthError(w, "invalid_tenant_claim", "tenant_id claim is not a valid UUID")
				return
			}
			recruiterID, err := shared.ParseRecruiterID(claims.RecruiterID)
			if err != nil {
				writeAuthError(w, "invalid_recruiter_claim", "recruiter_id claim is not a valid UUID")
				return
			}

			identity := Identity{TenantID: tenantID, RecruiterID: recruiterID, Roles: claims.Roles}
			ctx := WithIdentity(r.Context(), identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractBearer(r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	if header != "" {
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) {
			return "", ErrMissingToken
		}
		token := strings.TrimSpace(header[len(prefix):])
		if token == "" {
			return "", ErrMissingToken
		}
		return token, nil
	}

	// Fallback to query param for GET requests only (SSE EventSource compatibility).
	// This allows browsers to pass the token as ?token=... since the EventSource API
	// cannot set Authorization headers. Restrict to GET to prevent accidental credential
	// leaks via URL on state-changing endpoints.
	if r.Method == http.MethodGet {
		token := strings.TrimSpace(r.URL.Query().Get("token"))
		if token != "" {
			return token, nil
		}
	}

	return "", ErrMissingToken
}

func writeAuthError(w http.ResponseWriter, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"error":   map[string]string{"code": code, "message": message},
	})
}
