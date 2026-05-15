// Package repositories defines the persistence ports of the interview context.
package repositories

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/domain/entities"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ErrProcessNotFound is returned when a process lookup finds no row.
var ErrProcessNotFound = errors.New("interview: process not found")

// ErrProcessDuplicate is returned by Save when (tenant_id, application_id)
// already exists. ApplicationShortlistedConsumer treats this as a no-op
// (idempotent re-delivery of the bus event).
var ErrProcessDuplicate = errors.New("interview: process duplicate")

// ProcessListFilter controls which rows ListByTenant returns.
type ProcessListFilter struct {
	IntentID uuid.UUID
	Status   string // empty = all
	Limit    int
	Offset   int
}

// ProcessRepository persists InterviewProcess aggregates (including their
// rounds). All methods are tenant-scoped.
type ProcessRepository interface {
	Save(ctx context.Context, p *entities.InterviewProcess) error
	FindByID(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (*entities.InterviewProcess, error)
	FindByApplicationID(ctx context.Context, tenant shared.TenantID, applicationID uuid.UUID) (*entities.InterviewProcess, error)
	// FindByRoundID returns the InterviewProcess containing the given round.
	// Returns ErrProcessNotFound when no process contains that round id.
	FindByRoundID(ctx context.Context, tenant shared.TenantID, roundID uuid.UUID) (*entities.InterviewProcess, error)
	ListByTenant(ctx context.Context, tenant shared.TenantID, filter ProcessListFilter) ([]*entities.InterviewProcess, error)
	ClaimNextPendingRound(ctx context.Context) (*entities.InterviewProcess, uuid.UUID, error)
}
