package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hustle/hireflow/internal/auth/domain/entities"
	"github.com/hustle/hireflow/internal/auth/domain/repositories"
	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// PostgresOTPSessionRepository persists OTPSession aggregates.
// On Save of a brand-new (un-verified, no prior id) session for an email +
// purpose, any earlier un-verified rows for that pair are deleted — only
// the latest challenge is queryable.
type PostgresOTPSessionRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresOTPSessionRepository wires the repository.
func NewPostgresOTPSessionRepository(pool *pgxpool.Pool) *PostgresOTPSessionRepository {
	return &PostgresOTPSessionRepository{pool: pool}
}

// Save upserts the session row. New (i.e., never-persisted) rows displace
// prior un-verified sessions for the same (email, purpose).
func (r *PostgresOTPSessionRepository) Save(ctx context.Context, s *entities.OTPSession) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Insert-only path: if id is novel, drop prior unverified rows for this
	// email + purpose so the "latest" lookup is unambiguous.
	tag, err := tx.Exec(ctx, upsertOTPSessionSQL,
		s.ID().String(), s.TenantID().String(), s.Email().String(), string(s.Purpose()),
		s.CodeHash(), s.AttemptsLeft(), s.ExpiresAt(), s.VerifiedAt(),
		s.CreatedAt(), s.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("upsert otp session: %w", err)
	}
	// rowsAffected == 1 with an insert (vs an update) means brand-new row.
	if tag.RowsAffected() == 1 && s.UpdatedAt().Equal(s.CreatedAt()) {
		_, err = tx.Exec(ctx, deletePriorOTPSessionsSQL,
			s.Email().String(), string(s.Purpose()), s.ID().String(),
		)
		if err != nil {
			return fmt.Errorf("clear prior sessions: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// FindLatestForEmail returns the most recent un-verified session for the pair,
// or ErrOTPSessionNotFound.
func (r *PostgresOTPSessionRepository) FindLatestForEmail(ctx context.Context, email valueobjects.Email, purpose valueobjects.OTPPurpose) (*entities.OTPSession, error) {
	row := r.pool.QueryRow(ctx, selectLatestOTPSessionSQL, email.String(), string(purpose))
	return scanOTPSession(row)
}

func scanOTPSession(row pgx.Row) (*entities.OTPSession, error) {
	var (
		id, tenantID, email, purpose, codeHash string
		attemptsLeft                           int
		expiresAt                              time.Time
		verifiedAt                             *time.Time
		createdAt, updatedAt                   time.Time
	)
	err := row.Scan(&id, &tenantID, &email, &purpose, &codeHash,
		&attemptsLeft, &expiresAt, &verifiedAt, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repositories.ErrOTPSessionNotFound
		}
		return nil, fmt.Errorf("scan otp session: %w", err)
	}
	sid, err := valueobjects.ParseOTPSessionID(id)
	if err != nil {
		return nil, err
	}
	tid, err := shared.ParseTenantID(tenantID)
	if err != nil {
		return nil, err
	}
	em, err := valueobjects.NewEmail(email)
	if err != nil {
		return nil, err
	}
	pp, err := valueobjects.ParseOTPPurpose(purpose)
	if err != nil {
		return nil, err
	}
	return entities.HydrateOTPSession(sid, tid, em, pp, codeHash, attemptsLeft, expiresAt, verifiedAt, createdAt, updatedAt), nil
}

const upsertOTPSessionSQL = `
INSERT INTO otp_sessions (
    id, tenant_id, email, purpose, code_hash,
    attempts_left, expires_at, verified_at, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (id) DO UPDATE SET
    attempts_left = EXCLUDED.attempts_left,
    verified_at   = EXCLUDED.verified_at,
    updated_at    = EXCLUDED.updated_at
`

const deletePriorOTPSessionsSQL = `
DELETE FROM otp_sessions
WHERE email = $1 AND purpose = $2 AND id <> $3 AND verified_at IS NULL
`

const selectLatestOTPSessionSQL = `
SELECT id, tenant_id, email, purpose, code_hash,
       attempts_left, expires_at, verified_at, created_at, updated_at
FROM otp_sessions
WHERE email = $1 AND purpose = $2
ORDER BY created_at DESC
LIMIT 1
`
