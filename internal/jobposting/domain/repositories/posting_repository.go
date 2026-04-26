// Package repositories defines repository interfaces for the jobposting context.
package repositories

import (
	"context"
	"errors"

	"github.com/hustle/hireflow/internal/jobposting/domain/entities"
	"github.com/hustle/hireflow/internal/jobposting/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ErrPostingNotFound is returned when a query targets a non-existent posting.
var ErrPostingNotFound = errors.New("job posting not found")

// PostingFilter narrows a List query.
type PostingFilter struct {
	Status   *valueobjects.PostingStatus
	IntentID string
	Limit    int
	Offset   int
}

// PostingRepository is the persistence port for JobPosting aggregates.
// Save must persist aggregate state and pending events atomically (outbox).
type PostingRepository interface {
	Save(ctx context.Context, posting *entities.JobPosting) error
	FindByID(ctx context.Context, tenantID shared.TenantID, id valueobjects.PostingID) (*entities.JobPosting, error)
	FindByIntentID(ctx context.Context, tenantID shared.TenantID, intentID string) (*entities.JobPosting, error)
	List(ctx context.Context, tenantID shared.TenantID, filter PostingFilter) ([]*entities.JobPosting, error)
}
