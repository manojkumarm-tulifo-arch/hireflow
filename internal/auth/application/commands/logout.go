package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/hustle/hireflow/internal/auth/domain/repositories"
)

// LogoutInput accepts the refresh token to revoke. (We could also/instead
// blacklist the access JWT, but rotation already limits its lifetime;
// revoking the refresh prevents long-tail re-issue.)
type LogoutInput struct {
	RefreshToken string
}

// LogoutHandler revokes the supplied refresh token. Idempotent — a missing
// or already-revoked token returns nil.
type LogoutHandler struct {
	refreshTokens repositories.RefreshTokenRepository
	refreshGen    interface {
		Matches(hash, candidate string) bool
	}
}

// NewLogoutHandler wires the handler.
func NewLogoutHandler(rt repositories.RefreshTokenRepository, gen interface {
	Matches(hash, candidate string) bool
}) *LogoutHandler {
	return &LogoutHandler{refreshTokens: rt, refreshGen: gen}
}

// Handle executes the use case.
func (h *LogoutHandler) Handle(ctx context.Context, in LogoutInput) error {
	id, secret, err := splitRefreshToken(in.RefreshToken)
	if err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	row, err := h.refreshTokens.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, repositories.ErrRefreshTokenNotFound) {
			return nil // idempotent
		}
		return fmt.Errorf("logout: %w", err)
	}
	if !h.refreshGen.Matches(row.Hash(), secret) {
		return nil // mismatched secret — silently no-op (don't leak token id existence)
	}
	row.Revoke()
	if err := h.refreshTokens.Save(ctx, row); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}
