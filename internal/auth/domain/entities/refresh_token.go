package entities

import (
	"errors"
	"time"

	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// Domain errors enforced at the RefreshToken boundary.
var (
	ErrRefreshTokenRevoked = errors.New("refresh token revoked")
	ErrRefreshTokenExpired = errors.New("refresh token expired")
	ErrRefreshTokenInvalid = errors.New("refresh token invalid")
)

// RefreshTokenTTL is how long a refresh token is valid after issue.
const RefreshTokenTTL = 30 * 24 * time.Hour

// RefreshToken records an opaque refresh token issued at sign-in.
// The plaintext token is given to the client; only its hash is persisted —
// even an attacker with DB read access cannot impersonate users.
type RefreshToken struct {
	id        valueobjects.RefreshTokenID
	userID    valueobjects.UserID
	tenantID  shared.TenantID
	hash      string
	expiresAt time.Time
	createdAt time.Time
	revokedAt *time.Time
}

// NewRefreshToken creates a fresh token row.
// `hash` is produced by a RefreshTokenHasher impl.
func NewRefreshToken(userID valueobjects.UserID, tenantID shared.TenantID, hash string) (*RefreshToken, error) {
	if userID.IsZero() || tenantID.IsZero() || hash == "" {
		return nil, errors.New("invalid refresh token construction")
	}
	now := time.Now().UTC()
	return &RefreshToken{
		id:        valueobjects.NewRefreshTokenID(),
		userID:    userID,
		tenantID:  tenantID,
		hash:      hash,
		expiresAt: now.Add(RefreshTokenTTL),
		createdAt: now,
	}, nil
}

// HydrateRefreshToken reconstitutes a token from persistence.
func HydrateRefreshToken(
	id valueobjects.RefreshTokenID,
	userID valueobjects.UserID,
	tenantID shared.TenantID,
	hash string,
	expiresAt, createdAt time.Time,
	revokedAt *time.Time,
) *RefreshToken {
	return &RefreshToken{
		id: id, userID: userID, tenantID: tenantID, hash: hash,
		expiresAt: expiresAt, createdAt: createdAt, revokedAt: revokedAt,
	}
}

// Getters.
func (r *RefreshToken) ID() valueobjects.RefreshTokenID { return r.id }
func (r *RefreshToken) UserID() valueobjects.UserID     { return r.userID }
func (r *RefreshToken) TenantID() shared.TenantID       { return r.tenantID }
func (r *RefreshToken) Hash() string                    { return r.hash }
func (r *RefreshToken) ExpiresAt() time.Time            { return r.expiresAt }
func (r *RefreshToken) CreatedAt() time.Time            { return r.createdAt }
func (r *RefreshToken) RevokedAt() *time.Time           { return r.revokedAt }

// CheckUsable validates the token row is still issuable. Doesn't compare the
// hash — that's the caller's job (constant-time compare with the supplied secret).
func (r *RefreshToken) CheckUsable() error {
	if r.revokedAt != nil {
		return ErrRefreshTokenRevoked
	}
	if time.Now().UTC().After(r.expiresAt) {
		return ErrRefreshTokenExpired
	}
	return nil
}

// Revoke marks the token revoked. Idempotent.
func (r *RefreshToken) Revoke() {
	if r.revokedAt != nil {
		return
	}
	now := time.Now().UTC()
	r.revokedAt = &now
}
