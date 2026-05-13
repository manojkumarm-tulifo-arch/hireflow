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
