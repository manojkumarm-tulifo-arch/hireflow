package repositories

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
)

// ErrJudgeJobNotFound is returned when a JudgeJob lookup finds no row.
var ErrJudgeJobNotFound = errors.New("judge job not found")

// JudgeJobRepository persists JudgeJob queue rows. The judge worker pool uses
// ClaimNextPending to pick up work; ScoreIntent (or ScoreCandidate for direct
// flows) enqueues jobs via Save.
type JudgeJobRepository interface {
	// Save inserts or updates the JudgeJob. Idempotent on the primary key.
	// ScoreIntent calls this for each top-K Application; if a Pending job
	// already exists for application_id the adapter MUST treat it as a no-op.
	Save(ctx context.Context, j *entities.JudgeJob) error

	// ClaimNextPending is the judge-worker entry point. Picks one job with
	// status=Pending (or Running-but-stale) and next_attempt_at <= now(),
	// advances it to Running, and returns it. Returns ErrJudgeJobNotFound
	// when no work is ready.
	ClaimNextPending(ctx context.Context) (*entities.JudgeJob, error)

	// FindByID returns the JudgeJob with the given id.
	// Returns ErrJudgeJobNotFound when no matching row exists.
	FindByID(ctx context.Context, id uuid.UUID) (*entities.JudgeJob, error)
}
