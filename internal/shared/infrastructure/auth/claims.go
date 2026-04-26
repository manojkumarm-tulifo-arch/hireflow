// Package auth provides JWT verification and identity context propagation
// shared across all bounded contexts. Token issuing lives in the auth
// bounded context (not yet built); this package only handles verification
// for downstream services.
package auth

import (
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// Errors surfaced to callers.
var (
	// ErrMissingToken is returned when no Authorization header is present.
	ErrMissingToken = errors.New("missing bearer token")
	// ErrInvalidToken is returned for malformed/expired/wrong-signature tokens.
	ErrInvalidToken = errors.New("invalid token")
	// ErrMissingClaim is returned when a required claim is absent.
	ErrMissingClaim = errors.New("required claim missing")
)

// Claims is the payload of an hireflow access token.
// Mirrors the standard layout from authentication-standards.md and adds
// TenantID + RecruiterID — the two identifiers every business request needs.
type Claims struct {
	TenantID    string   `json:"tenant_id"`
	RecruiterID string   `json:"recruiter_id"`
	Roles       []string `json:"roles,omitempty"`
	jwt.RegisteredClaims
}

// Verifier validates HS256 access tokens against a shared secret.
type Verifier struct {
	secret []byte
	issuer string
}

// NewVerifier constructs a verifier. Secret must be non-empty; issuer is
// optional but recommended (defends against cross-service token reuse).
func NewVerifier(secret []byte, issuer string) (*Verifier, error) {
	if len(secret) == 0 {
		return nil, errors.New("auth: verifier secret must not be empty")
	}
	return &Verifier{secret: secret, issuer: issuer}, nil
}

// Verify parses and validates a bearer token string. On success it returns
// the typed Claims. All failures collapse into ErrInvalidToken so callers
// don't accidentally leak parser internals to clients.
func (v *Verifier) Verify(token string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return v.secret, nil
	})
	if err != nil || !parsed.Valid {
		return nil, ErrInvalidToken
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok {
		return nil, ErrInvalidToken
	}
	if v.issuer != "" && claims.Issuer != v.issuer {
		return nil, ErrInvalidToken
	}
	if claims.TenantID == "" || claims.RecruiterID == "" {
		return nil, ErrMissingClaim
	}
	return claims, nil
}
