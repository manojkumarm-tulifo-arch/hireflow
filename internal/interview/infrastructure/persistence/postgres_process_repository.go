package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// Compile-time interface assertion.
var _ repositories.ProcessRepository = (*PostgresProcessRepository)(nil)

// PostgresProcessRepository persists InterviewProcess aggregates.
type PostgresProcessRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresProcessRepository wires the repository.
func NewPostgresProcessRepository(pool *pgxpool.Pool) *PostgresProcessRepository {
	return &PostgresProcessRepository{pool: pool}
}

const processUpsertSQL = `
INSERT INTO interview_processes (
    id, tenant_id, application_id, candidate_id, intent_id, status, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (id) DO UPDATE SET
    status     = EXCLUDED.status,
    updated_at = EXCLUDED.updated_at`

const roundUpsertSQL = `
INSERT INTO interview_rounds (
    id, tenant_id, process_id, kind, sequence, status,
    questions, attempt_count, last_error, next_attempt_at, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (id) DO UPDATE SET
    status         = EXCLUDED.status,
    questions      = EXCLUDED.questions,
    attempt_count  = EXCLUDED.attempt_count,
    last_error     = EXCLUDED.last_error,
    next_attempt_at = EXCLUDED.next_attempt_at,
    updated_at     = EXCLUDED.updated_at`

// Save upserts the process row + all round rows and drains pending events into
// interview_outbox in the same transaction. Returns ErrProcessDuplicate when
// the (tenant_id, application_id) unique constraint fires (PG error code 23505).
func (r *PostgresProcessRepository) Save(ctx context.Context, p *entities.InterviewProcess) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	pr, rrs, err := serializeProcess(p)
	if err != nil {
		return fmt.Errorf("serialize: %w", err)
	}

	_, err = tx.Exec(ctx, processUpsertSQL,
		pr.id, pr.tenantID, pr.applicationID, pr.candidateID,
		pr.intentID, pr.status, pr.createdAt, pr.updatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return repositories.ErrProcessDuplicate
		}
		return fmt.Errorf("upsert process: %w", err)
	}

	for _, rr := range rrs {
		_, err = tx.Exec(ctx, roundUpsertSQL,
			rr.id, rr.tenantID, rr.processID, rr.kind, rr.sequence, rr.status,
			rr.questions, rr.attemptCount, rr.lastError, rr.nextAttemptAt,
			rr.createdAt, rr.updatedAt,
		)
		if err != nil {
			return fmt.Errorf("upsert round %s: %w", rr.id, err)
		}
	}

	for _, ev := range p.PullEvents() {
		payload, mErr := json.Marshal(ev)
		if mErr != nil {
			return fmt.Errorf("marshal event %s: %w", ev.EventName(), mErr)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO interview_outbox (event_name, aggregate_id, tenant_id, payload, occurred_at)
			VALUES ($1, $2, $3, $4, $5)
		`, ev.EventName(), ev.AggregateID(), ev.Tenant().String(), payload, ev.At())
		if err != nil {
			return fmt.Errorf("insert outbox: %w", err)
		}
	}

	return tx.Commit(ctx)
}

const processSelectSQL = `
SELECT id, tenant_id, application_id, candidate_id, intent_id, status, created_at, updated_at
FROM interview_processes`

const roundSelectSQL = `
SELECT id, tenant_id, process_id, kind, sequence, status,
       questions, attempt_count, last_error, next_attempt_at, created_at, updated_at
FROM interview_rounds`

func (r *PostgresProcessRepository) scanProcessRow(rs rowScanner) (processRow, error) {
	var pr processRow
	err := rs.Scan(
		&pr.id, &pr.tenantID, &pr.applicationID, &pr.candidateID,
		&pr.intentID, &pr.status, &pr.createdAt, &pr.updatedAt,
	)
	return pr, err
}

func (r *PostgresProcessRepository) fetchRoundsForProcess(ctx context.Context, processID uuid.UUID) ([]roundRow, error) {
	rows, err := r.pool.Query(ctx,
		roundSelectSQL+` WHERE process_id=$1 ORDER BY sequence ASC`, processID)
	if err != nil {
		return nil, fmt.Errorf("query rounds: %w", err)
	}
	defer rows.Close()

	var rrs []roundRow
	for rows.Next() {
		var rr roundRow
		if err := rows.Scan(
			&rr.id, &rr.tenantID, &rr.processID, &rr.kind, &rr.sequence, &rr.status,
			&rr.questions, &rr.attemptCount, &rr.lastError, &rr.nextAttemptAt,
			&rr.createdAt, &rr.updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan round: %w", err)
		}
		rrs = append(rrs, rr)
	}
	return rrs, rows.Err()
}

// FindByID returns the process with the given id, scoped to tenant.
func (r *PostgresProcessRepository) FindByID(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (*entities.InterviewProcess, error) {
	row := r.pool.QueryRow(ctx,
		processSelectSQL+` WHERE tenant_id=$1 AND id=$2`,
		tenant.String(), id,
	)
	pr, err := r.scanProcessRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repositories.ErrProcessNotFound
		}
		return nil, fmt.Errorf("scan process: %w", err)
	}

	rrs, err := r.fetchRoundsForProcess(ctx, pr.id)
	if err != nil {
		return nil, err
	}
	return hydrateProcess(pr, rrs)
}

// FindByApplicationID returns the process for the given application, scoped to tenant.
func (r *PostgresProcessRepository) FindByApplicationID(ctx context.Context, tenant shared.TenantID, applicationID uuid.UUID) (*entities.InterviewProcess, error) {
	row := r.pool.QueryRow(ctx,
		processSelectSQL+` WHERE tenant_id=$1 AND application_id=$2`,
		tenant.String(), applicationID,
	)
	pr, err := r.scanProcessRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repositories.ErrProcessNotFound
		}
		return nil, fmt.Errorf("scan process: %w", err)
	}

	rrs, err := r.fetchRoundsForProcess(ctx, pr.id)
	if err != nil {
		return nil, err
	}
	return hydrateProcess(pr, rrs)
}

// FindByRoundID returns the InterviewProcess containing the given round.
// Returns ErrProcessNotFound when no process contains that round id.
func (r *PostgresProcessRepository) FindByRoundID(ctx context.Context, tenant shared.TenantID, roundID uuid.UUID) (*entities.InterviewProcess, error) {
	var processID uuid.UUID
	err := r.pool.QueryRow(ctx,
		`SELECT process_id FROM interview_rounds WHERE tenant_id=$1 AND id=$2`,
		tenant.String(), roundID,
	).Scan(&processID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repositories.ErrProcessNotFound
		}
		return nil, fmt.Errorf("scan round: %w", err)
	}
	return r.FindByID(ctx, tenant, processID)
}

// ListByTenant returns processes for the tenant, filtered and paginated.
func (r *PostgresProcessRepository) ListByTenant(ctx context.Context, tenant shared.TenantID, filter repositories.ProcessListFilter) ([]*entities.InterviewProcess, error) {
	args := []any{tenant.String()}
	idx := 2

	var whereClauses []string
	if filter.IntentID != uuid.Nil {
		whereClauses = append(whereClauses, fmt.Sprintf("intent_id = $%d", idx))
		args = append(args, filter.IntentID)
		idx++
	}
	if filter.Status != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("status = $%d", idx))
		args = append(args, filter.Status)
		idx++
	}

	query := processSelectSQL + ` WHERE tenant_id=$1`
	if len(whereClauses) > 0 {
		query += " AND " + strings.Join(whereClauses, " AND ")
	}
	query += " ORDER BY created_at DESC"

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
		return nil, fmt.Errorf("query processes: %w", err)
	}
	defer rows.Close()

	var prs []processRow
	for rows.Next() {
		pr, err := r.scanProcessRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan process: %w", err)
		}
		prs = append(prs, pr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]*entities.InterviewProcess, 0, len(prs))
	for _, pr := range prs {
		rrs, err := r.fetchRoundsForProcess(ctx, pr.id)
		if err != nil {
			return nil, err
		}
		p, err := hydrateProcess(pr, rrs)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// ClaimNextPendingRound finds the oldest round with status='Pending' and
// next_attempt_at <= now(), returns the owning process and the round id.
// Returns (nil, uuid.Nil, ErrProcessNotFound) when nothing is claimable.
func (r *PostgresProcessRepository) ClaimNextPendingRound(ctx context.Context) (*entities.InterviewProcess, uuid.UUID, error) {
	var roundID, processID uuid.UUID
	var tenantIDStr string
	err := r.pool.QueryRow(ctx, `
		SELECT id, process_id, tenant_id
		FROM interview_rounds
		WHERE status='Pending' AND next_attempt_at <= now()
		ORDER BY next_attempt_at ASC
		LIMIT 1`,
	).Scan(&roundID, &processID, &tenantIDStr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, uuid.Nil, repositories.ErrProcessNotFound
		}
		return nil, uuid.Nil, fmt.Errorf("claim next pending round: %w", err)
	}

	tenant, err := shared.ParseTenantID(tenantIDStr)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("parse tenant: %w", err)
	}

	p, err := r.FindByID(ctx, tenant, processID)
	if err != nil {
		return nil, uuid.Nil, err
	}
	return p, roundID, nil
}
