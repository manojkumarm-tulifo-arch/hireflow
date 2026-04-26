// Package tokens provides the JWT issuer implementation for the auth context.
// The issued tokens are verified by the shared middleware in
// internal/shared/infrastructure/auth — claims are intentionally aligned.
package tokens

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/hustle/hireflow/internal/auth/domain/entities"
	sharedauth "github.com/hustle/hireflow/internal/shared/infrastructure/auth"
)

// JWTIssuer mints HS256 access tokens that the shared auth middleware accepts.
type JWTIssuer struct {
	secret []byte
	issuer string
}

// NewJWTIssuer wires the issuer. Secret must match the verifier's secret.
func NewJWTIssuer(secret []byte, issuer string) (*JWTIssuer, error) {
	if len(secret) == 0 {
		return nil, errors.New("auth tokens: secret must not be empty")
	}
	return &JWTIssuer{secret: secret, issuer: issuer}, nil
}

// IssueAccess mints a token for the given user. Implements services.TokenIssuer.
func (i *JWTIssuer) IssueAccess(user *entities.User, ttl time.Duration) (string, time.Time, error) {
	now := time.Now().UTC()
	exp := now.Add(ttl)
	claims := &sharedauth.Claims{
		TenantID:    user.TenantID().String(),
		RecruiterID: user.ID().String(),
		Roles:       user.Roles(),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    i.issuer,
			Subject:   user.ID().String(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(i.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, exp, nil
}
