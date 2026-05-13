package repositories

import (
	"context"
	"errors"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
)

// ErrCandidateNotFound is returned when a candidate isn't found.
var ErrCandidateNotFound = errors.New("candidate not found")

// CandidateRepository persists Candidate aggregates and the upload-side
// candidate_id link, transactionally with the ResumeUpload.
type CandidateRepository interface {
	// Save inserts the candidate and drains its pending events into the
	// shared sourcing_outbox. Honors (tenant_id, content_hash) uniqueness —
	// returns nil + the existing candidate when the row already exists, so
	// the parsing handler can attach to it.
	Save(ctx context.Context, c *entities.Candidate) (*entities.Candidate, error)

	// FindByID — tenant-scoped lookup. Returns ErrCandidateNotFound when missing.
	FindByID(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (*entities.Candidate, error)

	// FindByContentHash — tenant-scoped lookup by content_hash. Used by the
	// parsing handler to dedup before creating a new aggregate.
	FindByContentHash(ctx context.Context, tenant shared.TenantID, hash string) (*entities.Candidate, error)

	// ListByTenant returns all candidates belonging to the given tenant.
	// Used by ScoreIntent to fan out over all parsed candidates when a new
	// intent is confirmed.
	ListByTenant(ctx context.Context, tenant shared.TenantID) ([]*entities.Candidate, error)

	// UpdateProfileEmbedding persists the 1024-dim embedding vector for the
	// given candidate. Called by ScoreApplicationHandler after the first embed
	// so subsequent scoring passes skip the Voyage API call.
	UpdateProfileEmbedding(ctx context.Context, candidateID uuid.UUID, tenant shared.TenantID, vector []float32) error
}
