package repositories

import (
	"context"
	"errors"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// ErrApplicationNotFound is returned when an Application lookup finds no row.
var ErrApplicationNotFound = errors.New("application not found")

// ApplicationListFilter controls which rows ListByIntent returns and in what order.
// Sort values: "score_desc" (overall_score DESC NULLS LAST, default) | "recent" (updated_at DESC).
type ApplicationListFilter struct {
	Status   *vo.ApplicationStatus
	MinScore *float64
	Sort     string // "score_desc" | "recent"
	Limit    int
	Offset   int
}

// ApplicationRepository persists Application aggregates and supports the
// scoring pipeline query patterns. All methods are tenant-scoped.
type ApplicationRepository interface {
	// Save upserts on (tenant_id, candidate_id, intent_id) and atomically
	// drains the aggregate's pending events into sourcing_outbox in the same
	// transaction. Idempotent — re-saving with no changes is a no-op.
	Save(ctx context.Context, a *entities.Application) error

	// FindByID returns the Application with the given id, scoped to tenant.
	// Returns ErrApplicationNotFound when no matching row exists.
	FindByID(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (*entities.Application, error)

	// FindByCandidateAndIntent returns the unique Application for the
	// (tenant, candidate, intent) triple, or ErrApplicationNotFound.
	FindByCandidateAndIntent(ctx context.Context, tenant shared.TenantID,
		candidateID, intentID uuid.UUID) (*entities.Application, error)

	// ListByIntent returns Applications for the given intent, scoped to tenant,
	// filtered and sorted per filter. Used by the GET endpoint.
	ListByIntent(ctx context.Context, tenant shared.TenantID, intentID uuid.UUID,
		filter ApplicationListFilter) ([]*entities.Application, error)

	// ClaimNextNew is the match-worker entry point. Picks one Application with
	// status=New and next_attempt_at <= now(), advances it to an in-flight
	// state, and returns it. Returns ErrApplicationNotFound when nothing is ready.
	//
	// Slice 4 hardens this with FOR UPDATE SKIP LOCKED; slice 3 uses the
	// simpler optimistic pattern (load → update status → save in a new tx).
	ClaimNextNew(ctx context.Context) (*entities.Application, error)

	// TopByCoarseScoreForIntent returns up to limit Applications for the given
	// intent that have a non-nil embedding_score, ordered by coarse score
	// (= required_pass_rate*100 + embedding_score*20) descending. Used by
	// ScoreIntent to select the top-K candidates for LLM judging.
	TopByCoarseScoreForIntent(ctx context.Context, tenant shared.TenantID,
		intentID uuid.UUID, limit int) ([]*entities.Application, error)

	// InvalidateJudgmentsForIntent nulls out llm_judgment, overall_score, and
	// score_band for all applications belonging to the intent. Used by rescore
	// to clear cached LLM judgments so the judge worker re-runs them.
	// Only those three fields are touched; status, embedding_score, rule_match,
	// and all other columns are left unchanged.
	InvalidateJudgmentsForIntent(ctx context.Context, tenant shared.TenantID, intentID uuid.UUID) error
}
