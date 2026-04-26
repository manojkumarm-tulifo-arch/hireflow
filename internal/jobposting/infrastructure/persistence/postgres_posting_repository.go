// Package persistence holds infrastructure-side implementations of the
// jobposting context's repository interfaces.
package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hustle/hireflow/internal/jobposting/domain/entities"
	"github.com/hustle/hireflow/internal/jobposting/domain/repositories"
	"github.com/hustle/hireflow/internal/jobposting/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// PostgresPostingRepository persists JobPosting aggregates to Postgres
// with the same outbox pattern used by the hiringintent context.
type PostgresPostingRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresPostingRepository wires the repository.
func NewPostgresPostingRepository(pool *pgxpool.Pool) *PostgresPostingRepository {
	return &PostgresPostingRepository{pool: pool}
}

// Save upserts the aggregate and appends pending events to the outbox.
func (r *PostgresPostingRepository) Save(ctx context.Context, posting *entities.JobPosting) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, err := serialize(posting)
	if err != nil {
		return fmt.Errorf("serialize: %w", err)
	}

	_, err = tx.Exec(ctx, upsertSQL,
		row.id, row.tenantID, row.intentID,
		row.jd, row.sources,
		row.status, row.createdAt, row.updatedAt, row.publishedAt, row.closedAt, row.closeReason,
	)
	if err != nil {
		return fmt.Errorf("upsert posting: %w", err)
	}

	for _, ev := range posting.PullEvents() {
		payload, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("marshal event %s: %w", ev.EventName(), err)
		}
		_, err = tx.Exec(ctx, insertOutboxSQL,
			ev.EventName(), ev.AggregateID().String(), ev.Tenant().String(), payload, ev.At(),
		)
		if err != nil {
			return fmt.Errorf("insert outbox %s: %w", ev.EventName(), err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// FindByID returns the aggregate scoped to a tenant.
func (r *PostgresPostingRepository) FindByID(ctx context.Context, tenantID shared.TenantID, id valueobjects.PostingID) (*entities.JobPosting, error) {
	var row postingRow
	err := r.pool.QueryRow(ctx, selectByIDSQL, id.String(), tenantID.String()).Scan(
		&row.id, &row.tenantID, &row.intentID,
		&row.jd, &row.sources,
		&row.status, &row.createdAt, &row.updatedAt, &row.publishedAt, &row.closedAt, &row.closeReason,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repositories.ErrPostingNotFound
		}
		return nil, fmt.Errorf("select posting: %w", err)
	}
	return deserialize(row)
}

// FindByIntentID returns the posting (if any) created from a given intent.
func (r *PostgresPostingRepository) FindByIntentID(ctx context.Context, tenantID shared.TenantID, intentID string) (*entities.JobPosting, error) {
	var row postingRow
	err := r.pool.QueryRow(ctx, selectByIntentSQL, intentID, tenantID.String()).Scan(
		&row.id, &row.tenantID, &row.intentID,
		&row.jd, &row.sources,
		&row.status, &row.createdAt, &row.updatedAt, &row.publishedAt, &row.closedAt, &row.closeReason,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repositories.ErrPostingNotFound
		}
		return nil, fmt.Errorf("select posting by intent: %w", err)
	}
	return deserialize(row)
}

// List returns aggregates within a tenant matching the filter.
func (r *PostgresPostingRepository) List(ctx context.Context, tenantID shared.TenantID, filter repositories.PostingFilter) ([]*entities.JobPosting, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args := []any{tenantID.String(), limit, filter.Offset}
	sql := selectListSQL
	if filter.Status != nil {
		sql += fmt.Sprintf(" AND status = $%d", len(args)+1)
		args = append(args, string(*filter.Status))
	}
	if filter.IntentID != "" {
		sql += fmt.Sprintf(" AND intent_id = $%d", len(args)+1)
		args = append(args, filter.IntentID)
	}
	sql += " ORDER BY created_at DESC LIMIT $2 OFFSET $3"

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list query: %w", err)
	}
	defer rows.Close()

	var out []*entities.JobPosting
	for rows.Next() {
		var row postingRow
		if err := rows.Scan(
			&row.id, &row.tenantID, &row.intentID,
			&row.jd, &row.sources,
			&row.status, &row.createdAt, &row.updatedAt, &row.publishedAt, &row.closedAt, &row.closeReason,
		); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		posting, err := deserialize(row)
		if err != nil {
			return nil, fmt.Errorf("deserialize: %w", err)
		}
		out = append(out, posting)
	}
	return out, rows.Err()
}

const upsertSQL = `
INSERT INTO job_postings (
    id, tenant_id, intent_id,
    jd, sources,
    status, created_at, updated_at, published_at, closed_at, close_reason
) VALUES (
    $1, $2, $3,
    $4, $5,
    $6, $7, $8, $9, $10, $11
)
ON CONFLICT (id) DO UPDATE SET
    jd            = EXCLUDED.jd,
    sources       = EXCLUDED.sources,
    status        = EXCLUDED.status,
    updated_at    = EXCLUDED.updated_at,
    published_at  = EXCLUDED.published_at,
    closed_at     = EXCLUDED.closed_at,
    close_reason  = EXCLUDED.close_reason
`

const insertOutboxSQL = `
INSERT INTO job_posting_outbox (event_name, aggregate_id, tenant_id, payload, occurred_at)
VALUES ($1, $2, $3, $4, $5)
`

const selectByIDSQL = `
SELECT id, tenant_id, intent_id, jd, sources,
       status, created_at, updated_at, published_at, closed_at, close_reason
FROM job_postings
WHERE id = $1 AND tenant_id = $2
`

const selectByIntentSQL = `
SELECT id, tenant_id, intent_id, jd, sources,
       status, created_at, updated_at, published_at, closed_at, close_reason
FROM job_postings
WHERE intent_id = $1 AND tenant_id = $2
`

const selectListSQL = `
SELECT id, tenant_id, intent_id, jd, sources,
       status, created_at, updated_at, published_at, closed_at, close_reason
FROM job_postings
WHERE tenant_id = $1
`
