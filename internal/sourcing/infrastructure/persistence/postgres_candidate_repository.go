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

// PostgresCandidateRepository persists Candidate aggregates.
type PostgresCandidateRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresCandidateRepository wires the repository.
func NewPostgresCandidateRepository(pool *pgxpool.Pool) *PostgresCandidateRepository {
	return &PostgresCandidateRepository{pool: pool}
}

const candidateInsertSQL = `
INSERT INTO candidates (
    id, tenant_id, content_hash,
    full_name_enc, email_enc, phone_enc,
    location, headline,
    parsed_profile, profile_schema,
    source, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
ON CONFLICT (tenant_id, content_hash) DO NOTHING
RETURNING id`

// Save creates the candidate row + outbox entries atomically. On
// (tenant_id, content_hash) collision, returns the existing candidate
// instead of erroring (caller intent: "create or attach").
func (r *PostgresCandidateRepository) Save(ctx context.Context, c *entities.Candidate) (*entities.Candidate, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, err := serializeCandidate(c)
	if err != nil {
		return nil, fmt.Errorf("serialize: %w", err)
	}

	var returnedID uuid.UUID
	err = tx.QueryRow(ctx, candidateInsertSQL,
		row.id, row.tenantID, row.contentHash,
		row.fullNameEnc, row.emailEnc, row.phoneEnc,
		row.location, row.headline,
		row.parsedProfile, row.profileSchema,
		row.source, row.createdAt, row.updatedAt,
	).Scan(&returnedID)

	if errors.Is(err, pgx.ErrNoRows) {
		// Collision — fetch the existing row and return it instead.
		existing, ferr := r.findByContentHashTx(ctx, tx, c.TenantID(), c.ContentHash().String())
		if ferr != nil {
			return nil, fmt.Errorf("fetch existing: %w", ferr)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit: %w", err)
		}
		_ = c.PullEvents() // drop the "new" event — we attached to an existing row
		return existing, nil
	}
	if err != nil {
		return nil, fmt.Errorf("insert candidate: %w", err)
	}

	// Outbox.
	for _, ev := range c.PullEvents() {
		payload, mErr := json.Marshal(ev)
		if mErr != nil {
			return nil, fmt.Errorf("marshal event: %w", mErr)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO sourcing_outbox (event_name, aggregate_id, tenant_id, payload, occurred_at)
			VALUES ($1, $2, $3, $4, $5)
		`, ev.EventName(), ev.AggregateID(), ev.Tenant().String(), payload, ev.At())
		if err != nil {
			return nil, fmt.Errorf("insert outbox: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return c, nil
}

const candidateSelectSQL = `
SELECT id, tenant_id, content_hash,
       full_name_enc, email_enc, phone_enc,
       location, headline,
       parsed_profile, profile_schema,
       source, created_at, updated_at
FROM candidates`

// FindByID — tenant-scoped lookup.
func (r *PostgresCandidateRepository) FindByID(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (*entities.Candidate, error) {
	row := r.pool.QueryRow(ctx, candidateSelectSQL+" WHERE tenant_id=$1 AND id=$2", tenant.String(), id)
	return scanCandidate(row)
}

// FindByContentHash — tenant-scoped lookup by hash.
func (r *PostgresCandidateRepository) FindByContentHash(ctx context.Context, tenant shared.TenantID, hash string) (*entities.Candidate, error) {
	row := r.pool.QueryRow(ctx, candidateSelectSQL+" WHERE tenant_id=$1 AND content_hash=$2", tenant.String(), hash)
	return scanCandidate(row)
}

// findByContentHashTx is the in-transaction variant used by Save's collision path.
func (r *PostgresCandidateRepository) findByContentHashTx(ctx context.Context, tx pgx.Tx, tenant shared.TenantID, hash string) (*entities.Candidate, error) {
	row := tx.QueryRow(ctx, candidateSelectSQL+" WHERE tenant_id=$1 AND content_hash=$2", tenant.String(), hash)
	return scanCandidate(row)
}

// ListByTenant returns all candidates belonging to the given tenant.
func (r *PostgresCandidateRepository) ListByTenant(ctx context.Context, tenant shared.TenantID) ([]*entities.Candidate, error) {
	rows, err := r.pool.Query(ctx, candidateSelectSQL+" WHERE tenant_id=$1 ORDER BY created_at ASC", tenant.String())
	if err != nil {
		return nil, fmt.Errorf("list candidates: %w", err)
	}
	defer rows.Close()

	var out []*entities.Candidate
	for rows.Next() {
		c, err := scanCandidate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateProfileEmbedding persists the profile embedding vector for the candidate.
// This is a targeted UPDATE rather than a full Save to avoid overwriting other
// fields and to avoid re-emitting events.
func (r *PostgresCandidateRepository) UpdateProfileEmbedding(ctx context.Context, candidateID uuid.UUID, tenant shared.TenantID, vector []float32) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE candidates SET profile_embedding = $1, updated_at = now() WHERE id = $2 AND tenant_id = $3`,
		vector, candidateID, tenant.String(),
	)
	if err != nil {
		return fmt.Errorf("update profile_embedding: %w", err)
	}
	return nil
}

// EraseCascade transactionally deletes the candidate and all associated rows.
// Deletion order: judge_jobs → applications → resume_uploads_dedup → resume_uploads → candidates.
// Returns ErrCandidateNotFound when the candidate row does not exist.
// Storage keys of deleted resume_uploads are returned so the caller can
// best-effort delete blobs outside the transaction.
func (r *PostgresCandidateRepository) EraseCascade(ctx context.Context, tenant shared.TenantID, candidateID uuid.UUID) ([]string, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Collect storage keys before deleting rows so we can clean up blobs.
	rows, err := tx.Query(ctx,
		`SELECT storage_key FROM resume_uploads WHERE candidate_id=$1 AND tenant_id=$2`,
		candidateID, tenant.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("query storage keys: %w", err)
	}
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan storage key: %w", err)
		}
		keys = append(keys, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate storage keys: %w", err)
	}

	// Delete in dependency order.
	if _, err := tx.Exec(ctx,
		`DELETE FROM judge_jobs WHERE application_id IN (SELECT id FROM applications WHERE candidate_id=$1 AND tenant_id=$2)`,
		candidateID, tenant.String(),
	); err != nil {
		return nil, fmt.Errorf("delete judge_jobs: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM applications WHERE candidate_id=$1 AND tenant_id=$2`,
		candidateID, tenant.String(),
	); err != nil {
		return nil, fmt.Errorf("delete applications: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM resume_uploads_dedup WHERE tenant_id=$1 AND content_hash IN (SELECT content_hash FROM resume_uploads WHERE candidate_id=$2 AND tenant_id=$1)`,
		tenant.String(), candidateID,
	); err != nil {
		return nil, fmt.Errorf("delete resume_uploads_dedup: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM resume_uploads WHERE candidate_id=$1 AND tenant_id=$2`,
		candidateID, tenant.String(),
	); err != nil {
		return nil, fmt.Errorf("delete resume_uploads: %w", err)
	}

	tag, err := tx.Exec(ctx,
		`DELETE FROM candidates WHERE id=$1 AND tenant_id=$2`,
		candidateID, tenant.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("delete candidate: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, repositories.ErrCandidateNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return keys, nil
}

func scanCandidate(rs rowScanner) (*entities.Candidate, error) {
	var row candidateRow
	err := rs.Scan(
		&row.id, &row.tenantID, &row.contentHash,
		&row.fullNameEnc, &row.emailEnc, &row.phoneEnc,
		&row.location, &row.headline,
		&row.parsedProfile, &row.profileSchema,
		&row.source, &row.createdAt, &row.updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repositories.ErrCandidateNotFound
		}
		return nil, fmt.Errorf("scan candidate: %w", err)
	}
	return hydrateCandidate(row)
}
