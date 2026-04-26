package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/shared/infrastructure/auth"
)

const (
	testSecret = "test-secret-please-use-32+-bytes-in-prod"
	testIssuer = "hireflow-test"
)

func newTestVerifier(t *testing.T) *auth.Verifier {
	t.Helper()
	v, err := auth.NewVerifier([]byte(testSecret), testIssuer)
	require.NoError(t, err)
	return v
}

// signToken issues a token with the given claim overrides.
// Pass empty strings/nil to omit; expiry is 5 min in the future by default.
func signToken(t *testing.T, mutate func(c *auth.Claims)) string {
	t.Helper()
	now := time.Now()
	claims := &auth.Claims{
		TenantID:    shared.NewTenantID().String(),
		RecruiterID: shared.NewRecruiterID().String(),
		Roles:       []string{"recruiter"},
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    testIssuer,
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	if mutate != nil {
		mutate(claims)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(testSecret))
	require.NoError(t, err)
	return signed
}

// captureHandler is a downstream handler that records whether it was called
// and what identity it observed in the request context.
type captureHandler struct {
	called   bool
	identity auth.Identity
	err      error
}

func (c *captureHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.called = true
	c.identity, c.err = auth.IdentityFromContext(r.Context())
	w.WriteHeader(http.StatusOK)
}

func runRequest(t *testing.T, mw func(http.Handler) http.Handler, req *http.Request) (*captureHandler, *httptest.ResponseRecorder) {
	t.Helper()
	cap := &captureHandler{}
	rec := httptest.NewRecorder()
	mw(cap).ServeHTTP(rec, req)
	return cap, rec
}

func TestMiddleware_ValidTokenAttachesIdentity(t *testing.T) {
	v := newTestVerifier(t)
	token := signToken(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	cap, rec := runRequest(t, auth.Middleware(v), req)

	assert.True(t, cap.called, "next handler should be invoked")
	assert.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, cap.err)
	assert.False(t, cap.identity.TenantID.IsZero())
	assert.False(t, cap.identity.RecruiterID.IsZero())
	assert.Equal(t, []string{"recruiter"}, cap.identity.Roles)
}

func TestMiddleware_RejectsUnsignedRequests(t *testing.T) {
	v := newTestVerifier(t)
	tests := []struct {
		name       string
		authHeader string
		wantCode   string
	}{
		{"missing header", "", "missing_token"},
		{"wrong scheme", "Basic abc", "missing_token"},
		{"empty bearer", "Bearer ", "missing_token"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			cap, rec := runRequest(t, auth.Middleware(v), req)
			assert.False(t, cap.called)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			var body map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			errBlock, _ := body["error"].(map[string]any)
			assert.Equal(t, tc.wantCode, errBlock["code"])
		})
	}
}

func TestMiddleware_RejectsExpiredToken(t *testing.T) {
	v := newTestVerifier(t)
	token := signToken(t, func(c *auth.Claims) {
		c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-1 * time.Minute))
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	cap, rec := runRequest(t, auth.Middleware(v), req)

	assert.False(t, cap.called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMiddleware_RejectsWrongIssuer(t *testing.T) {
	v := newTestVerifier(t)
	token := signToken(t, func(c *auth.Claims) { c.Issuer = "someone-else" })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	cap, rec := runRequest(t, auth.Middleware(v), req)

	assert.False(t, cap.called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMiddleware_RejectsWrongSecret(t *testing.T) {
	v := newTestVerifier(t)
	wrongClaims := &auth.Claims{
		TenantID:    shared.NewTenantID().String(),
		RecruiterID: shared.NewRecruiterID().String(),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    testIssuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, wrongClaims)
	signed, err := tok.SignedString([]byte("a-completely-different-secret-value"))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	cap, rec := runRequest(t, auth.Middleware(v), req)

	assert.False(t, cap.called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMiddleware_RejectsMissingClaims(t *testing.T) {
	v := newTestVerifier(t)
	token := signToken(t, func(c *auth.Claims) { c.TenantID = "" })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	cap, rec := runRequest(t, auth.Middleware(v), req)

	assert.False(t, cap.called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	errBlock, _ := body["error"].(map[string]any)
	assert.Equal(t, "missing_claim", errBlock["code"])
}

func TestMiddleware_RejectsMalformedTenantClaim(t *testing.T) {
	v := newTestVerifier(t)
	token := signToken(t, func(c *auth.Claims) { c.TenantID = "not-a-uuid" })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	cap, rec := runRequest(t, auth.Middleware(v), req)

	assert.False(t, cap.called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	errBlock, _ := body["error"].(map[string]any)
	assert.Equal(t, "invalid_tenant_claim", errBlock["code"])
}

func TestIdentityFromContext_NoIdentityReturnsError(t *testing.T) {
	_, err := auth.IdentityFromContext(httptest.NewRequest(http.MethodGet, "/", nil).Context())
	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrNoIdentity)
}

func TestNewVerifier_RejectsEmptySecret(t *testing.T) {
	_, err := auth.NewVerifier(nil, "issuer")
	require.Error(t, err)
}
