package repositories

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/domain/entities"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ErrLoopTemplateNotFound is returned when a loop template lookup finds no row.
var ErrLoopTemplateNotFound = errors.New("interview: loop template not found")

// LoopTemplateRepository persists LoopTemplate aggregates.
type LoopTemplateRepository interface {
	Save(ctx context.Context, t *entities.LoopTemplate) error
	FindByIntent(ctx context.Context, tenant shared.TenantID, intentID uuid.UUID) (*entities.LoopTemplate, error)
}
