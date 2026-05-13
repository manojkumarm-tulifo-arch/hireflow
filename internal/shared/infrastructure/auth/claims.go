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
	// ErrWrongSubjectKind is returned when the token's subject_kind is not
	// accepted by this service (hireflow serves recruiters only).
	ErrWrongSubjectKind = errors.New("wrong subject kind")
)

// SubjectKind discriminates principal types. Sibling services (e.g.
// candidate-bgv) may issue or verify tokens for non-recruiter subjects
// (candidates). hireflow only mints recruiter tokens today; the field is
// emitted on every token so downstream services can route on it without a
// brittle "recruiter_id present" inference.
type SubjectKind string

const (
	// SubjectRecruiter — JWT was issued for a recruiter user.
	SubjectRecruiter SubjectKind = "recruiter"
	// SubjectCandidate — JWT was issued for a candidate (issued by a
	// downstream identity service, not by hireflow).
	SubjectCandidate SubjectKind = "candidate"
)

// Claims is the payload of an hireflow access token. The shape is shared
// with sibling services (candidate-bgv) — any field added here must also be
// reflected in candidate-bgv/internal/shared/infrastructure/auth/claims.go.
type Claims struct {
	TenantID    string   `json:"tenant_id"`
	SubjectKind string   `json:"subject_kind,omitempty"`
	RecruiterID string   `json:"recruiter_id,omitempty"`
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
	// hireflow's API surface is recruiter-only. Candidate tokens (minted by
	// the upstream identity service for the BGV flow) must not pass here —
	// they would lack a recruiter_id but might still resolve a tenant.
	if claims.SubjectKind != "" && SubjectKind(claims.SubjectKind) != SubjectRecruiter {
		return nil, ErrWrongSubjectKind
	}
	if claims.TenantID == "" || claims.RecruiterID == "" {
		return nil, ErrMissingClaim
	}
	return claims, nil
}
