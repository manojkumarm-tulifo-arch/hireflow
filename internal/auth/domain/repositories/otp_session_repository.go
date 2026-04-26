package repositories

import (
	"context"
	"errors"

	"github.com/hustle/hireflow/internal/auth/domain/entities"
	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
)

// ErrOTPSessionNotFound is returned when a query targets a non-existent session.
var ErrOTPSessionNotFound = errors.New("otp session not found")

// OTPSessionRepository persists OTPSession aggregates.
// Save invalidates any prior unverified session for (email, purpose) so only
// the latest is queryable — prevents replay against stale sessions.
type OTPSessionRepository interface {
	Save(ctx context.Context, session *entities.OTPSession) error
	FindLatestForEmail(ctx context.Context, email valueobjects.Email, purpose valueobjects.OTPPurpose) (*entities.OTPSession, error)
}
