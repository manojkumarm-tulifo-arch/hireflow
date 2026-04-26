// Package persistence holds infrastructure-side implementations of the
// hiringintent context's repository interfaces.
package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hustle/hireflow/internal/hiringintent/domain/entities"
	"github.com/hustle/hireflow/internal/hiringintent/domain/repositories"
	"github.com/hustle/hireflow/internal/hiringintent/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// PostgresIntentRepository persists HiringIntent aggregates to Postgres.
// Save uses a transaction to atomically upsert the aggregate row and append
// pending events to the outbox table. A separate dispatcher process reads
// the outbox and publishes via the messaging.EventPublisher.
type PostgresIntentRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresIntentRepository wires the repository.
func NewPostgresIntentRepository(pool *pgxpool.Pool) *PostgresIntentRepository {
	return &PostgresIntentRepository{pool: pool}
}

// Save upserts the aggregate and appends its pending events to the outbox.
func (r *PostgresIntentRepository) Save(ctx context.Context, intent *entities.HiringIntent) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, err := serialize(intent)
	if err != nil {
		return fmt.Errorf("serialize: %w", err)
	}

	_, err = tx.Exec(ctx, upsertSQL,
		row.id, row.tenantID, row.recruiterID,
		row.role, row.priority, row.intentSignals, row.trustSignals, row.budget,
		row.status, row.createdAt, row.updatedAt, row.confirmedAt, row.cancelledAt, row.cancelReason,
	)
	if err != nil {
		return fmt.Errorf("upsert intent: %w", err)
	}

	for _, ev := range intent.PullEvents() {
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
func (r *PostgresIntentRepository) FindByID(ctx context.Context, tenantID shared.TenantID, id valueobjects.IntentID) (*entities.HiringIntent, error) {
	var row intentRow
	err := r.pool.QueryRow(ctx, selectByIDSQL, id.String(), tenantID.String()).Scan(
		&row.id, &row.tenantID, &row.recruiterID,
		&row.role, &row.priority, &row.intentSignals, &row.trustSignals, &row.budget,
		&row.status, &row.createdAt, &row.updatedAt, &row.confirmedAt, &row.cancelledAt, &row.cancelReason,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repositories.ErrIntentNotFound
		}
		return nil, fmt.Errorf("select intent: %w", err)
	}
	return deserialize(row)
}

// List returns aggregates within a tenant matching the filter.
func (r *PostgresIntentRepository) List(ctx context.Context, tenantID shared.TenantID, filter repositories.IntentFilter) ([]*entities.HiringIntent, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	sql := selectListSQL
	args := []any{tenantID.String()}

	if filter.Status != nil {
		args = append(args, string(*filter.Status))
		sql += fmt.Sprintf(" AND status = $%d", len(args))
	}
	if filter.RecruiterID != nil {
		args = append(args, filter.RecruiterID.String())
		sql += fmt.Sprintf(" AND recruiter_id = $%d", len(args))
	}
	if s := strings.TrimSpace(filter.Search); s != "" {
		args = append(args, "%"+s+"%")
		sql += fmt.Sprintf(" AND role->>'title' ILIKE $%d", len(args))
	}

	switch filter.SortBy {
	case repositories.SortUrgentFirst:
		sql += " ORDER BY " + priorityRankSQL + " ASC, created_at DESC"
	default:
		sql += " ORDER BY created_at DESC"
	}

	args = append(args, limit, filter.Offset)
	sql += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list query: %w", err)
	}
	defer rows.Close()

	var out []*entities.HiringIntent
	for rows.Next() {
		var row intentRow
		if err := rows.Scan(
			&row.id, &row.tenantID, &row.recruiterID,
			&row.role, &row.priority, &row.intentSignals, &row.trustSignals, &row.budget,
			&row.status, &row.createdAt, &row.updatedAt, &row.confirmedAt, &row.cancelledAt, &row.cancelReason,
		); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		intent, err := deserialize(row)
		if err != nil {
			return nil, fmt.Errorf("deserialize: %w", err)
		}
		out = append(out, intent)
	}
	return out, rows.Err()
}

// Counts returns a per-status histogram for a tenant.
func (r *PostgresIntentRepository) Counts(ctx context.Context, tenantID shared.TenantID) (repositories.StatusCounts, error) {
	rows, err := r.pool.Query(ctx, countsSQL, tenantID.String())
	if err != nil {
		return nil, fmt.Errorf("counts query: %w", err)
	}
	defer rows.Close()

	out := repositories.StatusCounts{
		valueobjects.StatusDrafted:   0,
		valueobjects.StatusConfirmed: 0,
		valueobjects.StatusCancelled: 0,
		valueobjects.StatusClosed:    0,
	}
	for rows.Next() {
		var statusStr string
		var n int
		if err := rows.Scan(&statusStr, &n); err != nil {
			return nil, fmt.Errorf("counts scan: %w", err)
		}
		st, err := valueobjects.ParseIntentStatus(statusStr)
		if err != nil {
			continue
		}
		out[st] = n
	}
	return out, rows.Err()
}

const upsertSQL = `
INSERT INTO hiring_intents (
    id, tenant_id, recruiter_id,
    role, priority, intent_signals, trust_signals, budget,
    status, created_at, updated_at, confirmed_at, cancelled_at, cancel_reason
) VALUES (
    $1, $2, $3,
    $4, $5, $6, $7, $8,
    $9, $10, $11, $12, $13, $14
)
ON CONFLICT (id) DO UPDATE SET
    role           = EXCLUDED.role,
    priority       = EXCLUDED.priority,
    intent_signals = EXCLUDED.intent_signals,
    trust_signals  = EXCLUDED.trust_signals,
    budget         = EXCLUDED.budget,
    status         = EXCLUDED.status,
    updated_at     = EXCLUDED.updated_at,
    confirmed_at   = EXCLUDED.confirmed_at,
    cancelled_at   = EXCLUDED.cancelled_at,
    cancel_reason  = EXCLUDED.cancel_reason
`

const insertOutboxSQL = `
INSERT INTO hiring_intent_outbox (event_name, aggregate_id, tenant_id, payload, occurred_at)
VALUES ($1, $2, $3, $4, $5)
`

const selectByIDSQL = `
SELECT id, tenant_id, recruiter_id,
       role, priority, intent_signals, trust_signals, budget,
       status, created_at, updated_at, confirmed_at, cancelled_at, cancel_reason
FROM hiring_intents
WHERE id = $1 AND tenant_id = $2
`

const selectListSQL = `
SELECT id, tenant_id, recruiter_id,
       role, priority, intent_signals, trust_signals, budget,
       status, created_at, updated_at, confirmed_at, cancelled_at, cancel_reason
FROM hiring_intents
WHERE tenant_id = $1
`

// priorityRankSQL maps the textual priority to an ordinal so SortUrgentFirst
// puts CRITICAL/HIGH at the top. Lower rank = more urgent.
const priorityRankSQL = `CASE priority
        WHEN 'CRITICAL' THEN 1
        WHEN 'HIGH' THEN 2
        WHEN 'MEDIUM' THEN 3
        WHEN 'LOW' THEN 4
        ELSE 5 END`

const countsSQL = `
SELECT status, COUNT(*)::int
FROM hiring_intents
WHERE tenant_id = $1
GROUP BY status
`
