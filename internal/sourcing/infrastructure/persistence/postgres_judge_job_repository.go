package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
)

// judgeJobRow mirrors the judge_jobs table columns.
type judgeJobRow struct {
	id            uuid.UUID
	tenantID      string
	applicationID uuid.UUID
	intentID      uuid.UUID
	coarseScore   float64
	status        string
	attemptCount  int
	lastError     *string
	nextAttemptAt time.Time
	enqueuedAt    time.Time
	completedAt   *time.Time
}

func serializeJudgeJob(j *entities.JudgeJob) judgeJobRow {
	row := judgeJobRow{
		id:            j.ID(),
		tenantID:      j.TenantID().String(),
		applicationID: j.ApplicationID(),
		intentID:      j.IntentID(),
		coarseScore:   j.CoarseScore(),
		status:        string(j.Status()),
		attemptCount:  j.AttemptCount(),
		nextAttemptAt: j.NextAttemptAt(),
		enqueuedAt:    j.EnqueuedAt(),
		completedAt:   j.CompletedAt(),
	}
	if j.LastError() != "" {
		e := j.LastError()
		row.lastError = &e
	}
	return row
}

func hydrateJudgeJob(r judgeJobRow) (*entities.JudgeJob, error) {
	tenant, err := shared.ParseTenantID(r.tenantID)
	if err != nil {
		return nil, fmt.Errorf("tenant: %w", err)
	}
	var lastErr string
	if r.lastError != nil {
		lastErr = *r.lastError
	}
	return entities.RehydrateJudgeJob(entities.RehydrateJudgeJobInput{
		ID:            r.id,
		TenantID:      tenant,
		ApplicationID: r.applicationID,
		IntentID:      r.intentID,
		CoarseScore:   r.coarseScore,
		Status:        entities.JudgeJobStatus(r.status),
		AttemptCount:  r.attemptCount,
		LastError:     lastErr,
		NextAttemptAt: r.nextAttemptAt,
		EnqueuedAt:    r.enqueuedAt,
		CompletedAt:   r.completedAt,
	}), nil
}

// PostgresJudgeJobRepository persists JudgeJob queue rows.
type PostgresJudgeJobRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresJudgeJobRepository wires the repository.
func NewPostgresJudgeJobRepository(pool *pgxpool.Pool) *PostgresJudgeJobRepository {
	return &PostgresJudgeJobRepository{pool: pool}
}

const judgeJobUpsertSQL = `
INSERT INTO judge_jobs (
    id, tenant_id, application_id, intent_id,
    coarse_score, status, attempt_count, last_error,
    next_attempt_at, enqueued_at, completed_at
) VALUES (
    $1, $2, $3, $4,
    $5, $6, $7, $8,
    $9, $10, $11
)
ON CONFLICT (id) DO UPDATE SET
    status          = EXCLUDED.status,
    attempt_count   = EXCLUDED.attempt_count,
    last_error      = EXCLUDED.last_error,
    next_attempt_at = EXCLUDED.next_attempt_at,
    completed_at    = EXCLUDED.completed_at`

// Save inserts or updates the JudgeJob. Idempotent on the primary key.
func (r *PostgresJudgeJobRepository) Save(ctx context.Context, j *entities.JudgeJob) error {
	row := serializeJudgeJob(j)
	_, err := r.pool.Exec(ctx, judgeJobUpsertSQL,
		row.id, row.tenantID, row.applicationID, row.intentID,
		row.coarseScore, row.status, row.attemptCount, row.lastError,
		row.nextAttemptAt, row.enqueuedAt, row.completedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert judge_job: %w", err)
	}
	return nil
}

const judgeJobSelectSQL = `
SELECT id, tenant_id, application_id, intent_id,
       coarse_score, status, attempt_count, last_error,
       next_attempt_at, enqueued_at, completed_at
FROM judge_jobs`

// ClaimNextPending picks one job with status=Pending (or Running-but-stale)
// and next_attempt_at <= now(), advances it to Running, and returns it.
// Higher coarse_score is prioritised — the most promising candidates are judged first.
// Returns ErrJudgeJobNotFound when no work is ready.
func (r *PostgresJudgeJobRepository) ClaimNextPending(ctx context.Context) (*entities.JudgeJob, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx, judgeJobSelectSQL+`
		WHERE status IN ('Pending', 'Running')
		  AND next_attempt_at <= now()
		ORDER BY coarse_score DESC, enqueued_at ASC
		LIMIT 1`)

	j, err := scanJudgeJob(row)
	if err != nil {
		return nil, err // includes ErrJudgeJobNotFound
	}

	// Advance to Running regardless of current status (handles stale Running rows).
	_, err = tx.Exec(ctx, `UPDATE judge_jobs SET status='Running' WHERE id=$1`, j.ID())
	if err != nil {
		return nil, fmt.Errorf("update status: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return j, nil
}

// FindByID returns the JudgeJob with the given id.
func (r *PostgresJudgeJobRepository) FindByID(ctx context.Context, id uuid.UUID) (*entities.JudgeJob, error) {
	row := r.pool.QueryRow(ctx, judgeJobSelectSQL+` WHERE id=$1`, id)
	return scanJudgeJob(row)
}

func scanJudgeJob(rs rowScanner) (*entities.JudgeJob, error) {
	var row judgeJobRow
	err := rs.Scan(
		&row.id, &row.tenantID, &row.applicationID, &row.intentID,
		&row.coarseScore, &row.status, &row.attemptCount, &row.lastError,
		&row.nextAttemptAt, &row.enqueuedAt, &row.completedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repositories.ErrJudgeJobNotFound
		}
		return nil, fmt.Errorf("scan judge_job: %w", err)
	}
	return hydrateJudgeJob(row)
}
