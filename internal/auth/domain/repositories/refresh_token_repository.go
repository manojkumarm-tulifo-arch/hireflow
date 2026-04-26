package repositories

import (
	"context"
	"errors"

	"github.com/hustle/hireflow/internal/auth/domain/entities"
	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
)

// ErrRefreshTokenNotFound is returned when a query targets a non-existent token.
var ErrRefreshTokenNotFound = errors.New("refresh token not found")

// RefreshTokenRepository persists RefreshToken records.
type RefreshTokenRepository interface {
	Save(ctx context.Context, token *entities.RefreshToken) error
	FindByID(ctx context.Context, id valueobjects.RefreshTokenID) (*entities.RefreshToken, error)
	RevokeAllForUser(ctx context.Context, userID valueobjects.UserID) error
}
