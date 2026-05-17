// Package persistence holds Postgres-backed implementations of the sourcing
// repositories.
package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
)

// PostgresResumeUploadRepository persists ResumeUpload aggregates.
type PostgresResumeUploadRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresResumeUploadRepository wires the repository.
func NewPostgresResumeUploadRepository(pool *pgxpool.Pool) *PostgresResumeUploadRepository {
	return &PostgresResumeUploadRepository{pool: pool}
}

const upsertSQL = `
INSERT INTO resume_uploads (
    id, tenant_id, intent_id, batch_id, candidate_id, storage_key, original_name,
    mime_type, size_bytes, content_hash, status, stage_artifacts,
    attempt_count, last_error, next_attempt_at, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9, $10, $11, $12,
    $13, $14, $15, $16, $17
)
ON CONFLICT (created_at, id) DO UPDATE SET
    candidate_id    = EXCLUDED.candidate_id,
    status          = EXCLUDED.status,
    stage_artifacts = EXCLUDED.stage_artifacts,
    attempt_count   = EXCLUDED.attempt_count,
    last_error      = EXCLUDED.last_error,
    next_attempt_at = EXCLUDED.next_attempt_at,
    updated_at      = EXCLUDED.updated_at`

// Save atomically upserts the row and appends pending events to sourcing_outbox.
func (r *PostgresResumeUploadRepository) Save(ctx context.Context, u *entities.ResumeUpload) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, err := serialize(u)
	if err != nil {
		return fmt.Errorf("serialize: %w", err)
	}

	_, err = tx.Exec(ctx, upsertSQL,
		row.id, row.tenantID, row.intentID, row.batchID, row.candidateID,
		row.storageKey, row.originalName, row.mimeType, row.sizeBytes,
		row.contentHash, row.status, row.stageArtifacts,
		row.attemptCount, row.lastError, row.nextAttemptAt,
		row.createdAt, row.updatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert upload: %w", err)
	}

	// Enforce per-intent content_hash uniqueness via the dedup table.
	// First-write of a (tenant_id, intent_id, content_hash) inserts; subsequent
	// attempts from a different upload_id for the SAME intent fire the unique
	// constraint and return ErrDuplicate. The same upload_id (a status-update
	// Save on an existing aggregate) is a no-op — DO NOTHING handles that.
	// The same hash uploaded to a DIFFERENT intent is allowed (per spec S-5).
	_, err = tx.Exec(ctx, `
		INSERT INTO resume_uploads_dedup (tenant_id, intent_id, content_hash, upload_id, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, intent_id, content_hash) DO NOTHING
	`, row.tenantID, row.intentID, row.contentHash, row.id, row.createdAt)
	if err != nil {
		return fmt.Errorf("insert dedup: %w", err)
	}
	// Verify our upload_id won — if another upload_id owns the dedup row for
	// this (tenant, intent, hash) triple, this is a duplicate.
	var owner uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT upload_id FROM resume_uploads_dedup WHERE tenant_id=$1 AND intent_id=$2 AND content_hash=$3`,
		row.tenantID, row.intentID, row.contentHash,
	).Scan(&owner)
	if err != nil {
		return fmt.Errorf("read dedup: %w", err)
	}
	if owner != row.id {
		return repositories.ErrDuplicate
	}

	for _, ev := range u.PullEvents() {
		payload, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("marshal event %s: %w", ev.EventName(), err)
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

const selectSQL = `
SELECT id, tenant_id, intent_id, batch_id, candidate_id, storage_key, original_name,
       mime_type, size_bytes, content_hash, status, stage_artifacts,
       attempt_count, last_error, next_attempt_at, created_at, updated_at
FROM resume_uploads
`

// FindByID — tenant-scoped lookup.
func (r *PostgresResumeUploadRepository) FindByID(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (*entities.ResumeUpload, error) {
	row := r.pool.QueryRow(ctx, selectSQL+" WHERE tenant_id=$1 AND id=$2", tenant.String(), id)
	return scanRow(row)
}

// FindByContentHash — tenant-scoped lookup by content hash.
func (r *PostgresResumeUploadRepository) FindByContentHash(ctx context.Context, tenant shared.TenantID, hash string) (*entities.ResumeUpload, error) {
	row := r.pool.QueryRow(ctx, selectSQL+" WHERE tenant_id=$1 AND content_hash=$2", tenant.String(), hash)
	return scanRow(row)
}

// FindByContentHashAndIntent — tenant + intent + content_hash lookup.
// Used by the upload command for per-intent dedup detection.
func (r *PostgresResumeUploadRepository) FindByContentHashAndIntent(
	ctx context.Context, tenant shared.TenantID, intentID uuid.UUID, hash string,
) (*entities.ResumeUpload, error) {
	row := r.pool.QueryRow(ctx,
		selectSQL+" WHERE tenant_id=$1 AND intent_id=$2 AND content_hash=$3",
		tenant.String(), intentID, hash,
	)
	return scanRow(row)
}

// ListByBatch — tenant-scoped list.
func (r *PostgresResumeUploadRepository) ListByBatch(ctx context.Context, tenant shared.TenantID, batchID uuid.UUID) ([]*entities.ResumeUpload, error) {
	rows, err := r.pool.Query(ctx, selectSQL+" WHERE tenant_id=$1 AND batch_id=$2 ORDER BY created_at ASC",
		tenant.String(), batchID)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()
	var out []*entities.ResumeUpload
	for rows.Next() {
		u, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// BatchExistsForTenant reports whether at least one resume_uploads row exists
// for the given (tenant, batch_id). Used by the SSE endpoint to verify the
// caller's tenant owns the batch before opening the stream.
func (r *PostgresResumeUploadRepository) BatchExistsForTenant(ctx context.Context, tenant shared.TenantID, batchID uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM resume_uploads WHERE tenant_id=$1 AND batch_id=$2)`,
		tenant.String(), batchID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("batch exists check: %w", err)
	}
	return exists, nil
}

// ClaimNextPending — slice 1 simple polling. Returns the next claimable row
// without locking; idempotent stages tolerate the rare two-workers-pick-same-row
// race that results. Slice 4 swaps this for an UPDATE ... RETURNING that flips
// status to a "claimed" marker atomically, eliminating overlap.
// Note: Extracted is no longer excluded (slice 2 makes it an intermediate state
// that the worker re-claims to run the parsing stage).
func (r *PostgresResumeUploadRepository) ClaimNextPending(ctx context.Context) (*entities.ResumeUpload, error) {
	row := r.pool.QueryRow(ctx, selectSQL+`
		WHERE status NOT IN ('Parsed','Scored','Failed','Quarantined')
		  AND next_attempt_at <= now()
		ORDER BY next_attempt_at ASC
		LIMIT 1`)
	u, err := scanRow(row)
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil, repositories.ErrNotFound
		}
		return nil, err
	}
	return u, nil
}

// scanRow adapts a pgx.Row/Rows into a hydrated aggregate.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanRow(rs rowScanner) (*entities.ResumeUpload, error) {
	var row uploadRow
	err := rs.Scan(
		&row.id, &row.tenantID, &row.intentID, &row.batchID, &row.candidateID,
		&row.storageKey, &row.originalName, &row.mimeType, &row.sizeBytes,
		&row.contentHash, &row.status, &row.stageArtifacts,
		&row.attemptCount, &row.lastError, &row.nextAttemptAt,
		&row.createdAt, &row.updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repositories.ErrNotFound
		}
		return nil, fmt.Errorf("scan: %w", err)
	}
	return hydrate(row)
}
