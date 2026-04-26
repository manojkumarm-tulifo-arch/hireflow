package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hustle/hireflow/internal/auth/application/dto"
	"github.com/hustle/hireflow/internal/auth/domain/entities"
	"github.com/hustle/hireflow/internal/auth/domain/repositories"
	"github.com/hustle/hireflow/internal/auth/domain/services"
	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
)

// ErrMalformedRefreshToken is returned when the opaque token doesn't parse.
var ErrMalformedRefreshToken = errors.New("malformed refresh token")

// RefreshSessionInput is just the opaque refresh string from the client.
type RefreshSessionInput struct {
	RefreshToken string
}

// RefreshSessionHandler exchanges an unrevoked refresh token for a fresh
// access + refresh pair. Implements **rotation**: the old refresh row is
// revoked atomically with creation of the new one — using an old token
// after rotation surfaces as Revoked.
type RefreshSessionHandler struct {
	users         repositories.UserRepository
	refreshTokens repositories.RefreshTokenRepository
	issuer        services.TokenIssuer
	refreshGen    services.RefreshTokenSecretGenerator
	tokens        *issueTokensService
}

// NewRefreshSessionHandler wires the handler.
func NewRefreshSessionHandler(
	users repositories.UserRepository,
	refreshTokens repositories.RefreshTokenRepository,
	issuer services.TokenIssuer,
	refreshGen services.RefreshTokenSecretGenerator,
) *RefreshSessionHandler {
	return &RefreshSessionHandler{
		users:         users,
		refreshTokens: refreshTokens,
		issuer:        issuer,
		refreshGen:    refreshGen,
		tokens:        newIssueTokensService(refreshTokens, issuer, refreshGen),
	}
}

// Handle executes the use case.
func (h *RefreshSessionHandler) Handle(ctx context.Context, in RefreshSessionInput) (dto.TokenPairDTO, error) {
	id, secret, err := splitRefreshToken(in.RefreshToken)
	if err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("refresh: %w", err)
	}
	row, err := h.refreshTokens.FindByID(ctx, id)
	if err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("refresh: %w", err)
	}
	if !h.refreshGen.Matches(row.Hash(), secret) {
		return dto.TokenPairDTO{}, fmt.Errorf("refresh: %w", entities.ErrRefreshTokenInvalid)
	}
	if err := row.CheckUsable(); err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("refresh: %w", err)
	}

	user, err := h.users.FindByID(ctx, row.UserID())
	if err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("refresh: load user: %w", err)
	}
	if !user.CanSignInNow() {
		// Lock the refresh row too — user is no longer entitled.
		row.Revoke()
		_ = h.refreshTokens.Save(ctx, row)
		return dto.TokenPairDTO{}, fmt.Errorf("refresh: %w", entities.ErrCannotSignInWhenNotActive)
	}

	// Rotate: revoke the consumed token, then issue a new pair.
	row.Revoke()
	if err := h.refreshTokens.Save(ctx, row); err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("refresh: revoke old: %w", err)
	}
	return h.tokens.issue(ctx, user)
}

// splitRefreshToken parses the wire format `<uuid>.<secret>` produced by
// issueTokensService.issue — the id lets us locate the row in O(1) without
// scanning every hash.
func splitRefreshToken(raw string) (valueobjects.RefreshTokenID, string, error) {
	parts := strings.SplitN(raw, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return valueobjects.RefreshTokenID{}, "", ErrMalformedRefreshToken
	}
	id, err := valueobjects.ParseRefreshTokenID(parts[0])
	if err != nil {
		return valueobjects.RefreshTokenID{}, "", ErrMalformedRefreshToken
	}
	return id, parts[1], nil
}
