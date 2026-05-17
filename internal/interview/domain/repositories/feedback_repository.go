package repositories

import (
	"context"

	"github.com/google/uuid"

	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// FeedbackRow is the persisted shape of an interview feedback entry. It's
// not an aggregate — feedback is append-only — so it's modeled as a plain
// row plus the FeedbackRepository contract below.
type FeedbackRow struct {
	ID       uuid.UUID
	TenantID shared.TenantID
	RoundID  uuid.UUID
	vo.Feedback
}

// FeedbackRepository persists interview feedback rows (append-only).
type FeedbackRepository interface {
	Append(ctx context.Context, row FeedbackRow) error
	ListByRound(ctx context.Context, tenant shared.TenantID, roundID uuid.UUID) ([]FeedbackRow, error)
}
