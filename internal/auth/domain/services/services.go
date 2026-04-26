// Package services defines the domain-side ports for cryptography, token
// issuance, and notification — kept out of infrastructure so the application
// layer can be tested with fakes.
package services

import (
	"context"
	"time"

	"github.com/hustle/hireflow/internal/auth/domain/entities"
	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
)

// OTPGenerator produces a fresh 6-digit OTP code using crypto/rand.
type OTPGenerator interface {
	Generate() (valueobjects.OTPCode, error)
}

// OTPHasher hashes OTP codes for storage and verifies candidates against
// the stored hash in constant time. Implementation typically uses Argon2id.
type OTPHasher interface {
	Hash(code valueobjects.OTPCode) (string, error)
	Matches(hash, candidate string) bool
}

// RefreshTokenSecretGenerator produces an opaque random secret to give to
// the client (raw form) and a hash to persist.
type RefreshTokenSecretGenerator interface {
	Generate() (raw string, hash string, err error)
	Matches(hash, candidate string) bool
}

// IssuedTokenPair is what the application returns to the client on
// successful sign-in / signup verify / refresh.
type IssuedTokenPair struct {
	AccessToken     string
	AccessExpiresAt time.Time
	RefreshTokenID  valueobjects.RefreshTokenID
	RefreshToken    string // raw; only known here at issue time
	RefreshExpires  time.Time
}

// TokenIssuer mints the access JWT (verified later by the shared auth
// middleware) and returns the access claim/expiry alongside.
type TokenIssuer interface {
	IssueAccess(user *entities.User, ttl time.Duration) (token string, expiresAt time.Time, err error)
}

// OTPSender delivers an OTP code to the user — email in production,
// stdout log in dev. Returns whatever transport id is useful for ops.
type OTPSender interface {
	Send(ctx context.Context, email valueobjects.Email, code valueobjects.OTPCode, purpose valueobjects.OTPPurpose) error
}
