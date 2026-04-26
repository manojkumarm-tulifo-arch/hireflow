package auth

import (
	"context"
	"errors"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ErrNoIdentity is returned when the request context lacks identity claims —
// the middleware was either not mounted or rejected the token.
var ErrNoIdentity = errors.New("no identity in context")

type ctxKey struct{}

// Identity is the resolved tenant + recruiter pair extracted from a verified token.
type Identity struct {
	TenantID    shared.TenantID
	RecruiterID shared.RecruiterID
	Roles       []string
}

// WithIdentity attaches an identity to a context (used by the middleware).
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// IdentityFromContext returns the identity attached to the context.
// Handlers call this instead of reading raw headers.
func IdentityFromContext(ctx context.Context) (Identity, error) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	if !ok {
		return Identity{}, ErrNoIdentity
	}
	return id, nil
}
