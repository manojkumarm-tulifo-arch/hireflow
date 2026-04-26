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

// PostgresRefreshTokenRepository persists RefreshToken records.
type PostgresRefreshTokenRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRefreshTokenRepository wires the repository.
func NewPostgresRefreshTokenRepository(pool *pgxpool.Pool) *PostgresRefreshTokenRepository {
	return &PostgresRefreshTokenRepository{pool: pool}
}

// Save upserts the row.
func (r *PostgresRefreshTokenRepository) Save(ctx context.Context, t *entities.RefreshToken) error {
	_, err := r.pool.Exec(ctx, upsertRefreshTokenSQL,
		t.ID().String(), t.UserID().String(), t.TenantID().String(),
		t.Hash(), t.ExpiresAt(), t.CreatedAt(), t.RevokedAt(),
	)
	if err != nil {
		return fmt.Errorf("upsert refresh token: %w", err)
	}
	return nil
}

// FindByID returns the row, or ErrRefreshTokenNotFound.
func (r *PostgresRefreshTokenRepository) FindByID(ctx context.Context, id valueobjects.RefreshTokenID) (*entities.RefreshToken, error) {
	row := r.pool.QueryRow(ctx, selectRefreshTokenByIDSQL, id.String())
	return scanRefreshToken(row)
}

// RevokeAllForUser bulk-revokes every active refresh token for a user.
// Used by future "logout everywhere" + admin actions.
func (r *PostgresRefreshTokenRepository) RevokeAllForUser(ctx context.Context, userID valueobjects.UserID) error {
	_, err := r.pool.Exec(ctx, revokeAllForUserSQL, userID.String(), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("revoke all: %w", err)
	}
	return nil
}

func scanRefreshToken(row pgx.Row) (*entities.RefreshToken, error) {
	var (
		id, userID, tenantID, hash string
		expiresAt, createdAt       time.Time
		revokedAt                  *time.Time
	)
	err := row.Scan(&id, &userID, &tenantID, &hash, &expiresAt, &createdAt, &revokedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repositories.ErrRefreshTokenNotFound
		}
		return nil, fmt.Errorf("scan refresh token: %w", err)
	}
	rid, err := valueobjects.ParseRefreshTokenID(id)
	if err != nil {
		return nil, err
	}
	uid, err := valueobjects.ParseUserID(userID)
	if err != nil {
		return nil, err
	}
	tid, err := shared.ParseTenantID(tenantID)
	if err != nil {
		return nil, err
	}
	return entities.HydrateRefreshToken(rid, uid, tid, hash, expiresAt, createdAt, revokedAt), nil
}

const upsertRefreshTokenSQL = `
INSERT INTO refresh_tokens (id, user_id, tenant_id, hash, expires_at, created_at, revoked_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (id) DO UPDATE SET
    revoked_at = EXCLUDED.revoked_at
`

const selectRefreshTokenByIDSQL = `
SELECT id, user_id, tenant_id, hash, expires_at, created_at, revoked_at
FROM refresh_tokens
WHERE id = $1
`

const revokeAllForUserSQL = `
UPDATE refresh_tokens SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL
`
