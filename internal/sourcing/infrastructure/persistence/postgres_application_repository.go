package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
)

// PostgresApplicationRepository persists Application aggregates.
type PostgresApplicationRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresApplicationRepository wires the repository.
func NewPostgresApplicationRepository(pool *pgxpool.Pool) *PostgresApplicationRepository {
	return &PostgresApplicationRepository{pool: pool}
}

const applicationUpsertSQL = `
INSERT INTO applications (
    id, tenant_id, candidate_id, intent_id,
    intent_spec_version, profile_schema_version,
    status, overall_score, score_band, rule_match,
    embedding_score, llm_judgment,
    last_error, attempt_count, next_attempt_at,
    scored_at, created_at, updated_at
) VALUES (
    $1, $2, $3, $4,
    $5, $6,
    $7, $8, $9, $10,
    $11, $12,
    $13, $14, $15,
    $16, $17, $18
)
ON CONFLICT (tenant_id, candidate_id, intent_id) DO UPDATE SET
    intent_spec_version    = EXCLUDED.intent_spec_version,
    profile_schema_version = EXCLUDED.profile_schema_version,
    status                 = EXCLUDED.status,
    overall_score          = EXCLUDED.overall_score,
    score_band             = EXCLUDED.score_band,
    rule_match             = EXCLUDED.rule_match,
    embedding_score        = EXCLUDED.embedding_score,
    llm_judgment           = EXCLUDED.llm_judgment,
    last_error             = EXCLUDED.last_error,
    attempt_count          = EXCLUDED.attempt_count,
    next_attempt_at        = EXCLUDED.next_attempt_at,
    scored_at              = EXCLUDED.scored_at,
    updated_at             = EXCLUDED.updated_at`

// Save upserts the Application row and writes pending domain events to
// sourcing_outbox in the same transaction.
func (r *PostgresApplicationRepository) Save(ctx context.Context, a *entities.Application) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, err := serializeApplication(a)
	if err != nil {
		return fmt.Errorf("serialize: %w", err)
	}

	_, err = tx.Exec(ctx, applicationUpsertSQL,
		row.id, row.tenantID, row.candidateID, row.intentID,
		row.intentSpecVersion, row.profileSchemaVersion,
		row.status, row.overallScore, row.scoreBand, row.ruleMatch,
		row.embeddingScore, row.llmJudgment,
		row.lastError, row.attemptCount, row.nextAttemptAt,
		row.scoredAt, row.createdAt, row.updatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert application: %w", err)
	}

	for _, ev := range a.PullEvents() {
		payload, mErr := json.Marshal(ev)
		if mErr != nil {
			return fmt.Errorf("marshal event %s: %w", ev.EventName(), mErr)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO sourcing_outbox (event_name, aggregate_id, tenant_id, payload, occurred_at)
			VALUES ($1, $2, $3, $4, $5)
		`, ev.EventName(), ev.AggregateID(), ev.Tenant().String(), payload, ev.At())
		if err != nil {
			return fmt.Errorf("insert outbox: %w", err)
		}
	}

	return tx.Commit(ctx)
}

const applicationSelectSQL = `
SELECT id, tenant_id, candidate_id, intent_id,
       intent_spec_version, profile_schema_version,
       status, overall_score, score_band, rule_match,
       embedding_score, llm_judgment,
       last_error, attempt_count, next_attempt_at,
       scored_at, created_at, updated_at
FROM applications`

// FindByID returns the Application with the given id, scoped to tenant.
func (r *PostgresApplicationRepository) FindByID(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (*entities.Application, error) {
	row := r.pool.QueryRow(ctx, applicationSelectSQL+` WHERE tenant_id=$1 AND id=$2`, tenant.String(), id)
	return scanApplication(row)
}

// FindByCandidateAndIntent returns the unique Application for the (tenant, candidate, intent) triple.
func (r *PostgresApplicationRepository) FindByCandidateAndIntent(ctx context.Context, tenant shared.TenantID, candidateID, intentID uuid.UUID) (*entities.Application, error) {
	row := r.pool.QueryRow(ctx, applicationSelectSQL+` WHERE tenant_id=$1 AND candidate_id=$2 AND intent_id=$3`,
		tenant.String(), candidateID, intentID)
	return scanApplication(row)
}

// ListByIntent returns Applications for the given intent, scoped to tenant,
// filtered and sorted per filter.
func (r *PostgresApplicationRepository) ListByIntent(ctx context.Context, tenant shared.TenantID, intentID uuid.UUID, filter repositories.ApplicationListFilter) ([]*entities.Application, error) {
	args := []any{tenant.String(), intentID}
	idx := 3

	var whereClauses []string
	if filter.Status != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("status = $%d", idx))
		args = append(args, string(*filter.Status))
		idx++
	}
	if filter.MinScore != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("overall_score >= $%d", idx))
		args = append(args, *filter.MinScore)
		idx++
	}

	query := applicationSelectSQL + ` WHERE tenant_id=$1 AND intent_id=$2`
	if len(whereClauses) > 0 {
		query += " AND " + strings.Join(whereClauses, " AND ")
	}

	switch filter.Sort {
	case "recent":
		query += " ORDER BY created_at DESC"
	default: // "score_desc" and anything else
		query += " ORDER BY overall_score DESC NULLS LAST, embedding_score DESC NULLS LAST"
	}

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", idx)
		args = append(args, filter.Limit)
		idx++
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", idx)
		args = append(args, filter.Offset)
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var out []*entities.Application
	for rows.Next() {
		a, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ClaimNextNew returns the oldest Application with status=New and
// next_attempt_at <= now(). Returns ErrApplicationNotFound when nothing is ready.
//
// Slice 4 hardens this with FOR UPDATE SKIP LOCKED; slice 3 uses the simpler
// optimistic pattern consistent with ClaimNextPending in the resume-upload repo.
func (r *PostgresApplicationRepository) ClaimNextNew(ctx context.Context) (*entities.Application, error) {
	row := r.pool.QueryRow(ctx, applicationSelectSQL+`
		WHERE status = 'New'
		  AND next_attempt_at <= now()
		ORDER BY next_attempt_at ASC
		LIMIT 1`)
	a, err := scanApplication(row)
	if err != nil {
		if errors.Is(err, repositories.ErrApplicationNotFound) {
			return nil, repositories.ErrApplicationNotFound
		}
		return nil, err
	}
	return a, nil
}

// TopByCoarseScoreForIntent returns up to limit Applications for the given
// intent that have a non-nil embedding_score, ordered by coarse score
// (= required_pass_rate*100 + embedding_score*20) descending.
//
// required_pass_rate is stored as a top-level field in the rule_match JSONB
// by RuleMatchReport.Marshal(), enabling this SQL ordering without decoding
// the full results array server-side.
func (r *PostgresApplicationRepository) TopByCoarseScoreForIntent(ctx context.Context, tenant shared.TenantID, intentID uuid.UUID, limit int) ([]*entities.Application, error) {
	q := applicationSelectSQL + `
		WHERE tenant_id=$1 AND intent_id=$2
		  AND embedding_score IS NOT NULL
		ORDER BY (
		    (rule_match->>'required_pass_rate')::numeric * 100
		    + COALESCE(embedding_score, 0) * 20
		) DESC
		LIMIT $3`

	rows, err := r.pool.Query(ctx, q, tenant.String(), intentID, limit)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var out []*entities.Application
	for rows.Next() {
		a, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func scanApplication(rs rowScanner) (*entities.Application, error) {
	var row applicationRow
	err := rs.Scan(
		&row.id, &row.tenantID, &row.candidateID, &row.intentID,
		&row.intentSpecVersion, &row.profileSchemaVersion,
		&row.status, &row.overallScore, &row.scoreBand, &row.ruleMatch,
		&row.embeddingScore, &row.llmJudgment,
		&row.lastError, &row.attemptCount, &row.nextAttemptAt,
		&row.scoredAt, &row.createdAt, &row.updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repositories.ErrApplicationNotFound
		}
		return nil, fmt.Errorf("scan application: %w", err)
	}
	return hydrateApplication(row)
}
