package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hustle/hireflow/internal/auth/domain/entities"
	"github.com/hustle/hireflow/internal/auth/domain/repositories"
	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// PostgresUserRepository persists User aggregates with the same outbox
// pattern used by the other contexts.
type PostgresUserRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresUserRepository wires the repository.
func NewPostgresUserRepository(pool *pgxpool.Pool) *PostgresUserRepository {
	return &PostgresUserRepository{pool: pool}
}

// Save upserts the user row and appends pending events to the outbox.
// Maps the unique-violation on the email column to ErrEmailAlreadyRegistered.
func (r *PostgresUserRepository) Save(ctx context.Context, user *entities.User) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, upsertUserSQL,
		user.ID().String(),
		user.TenantID().String(),
		user.Email().String(),
		user.Name(),
		string(user.Status()),
		user.Roles(),
		user.FailedAttempts(),
		user.LockedUntil(),
		user.CreatedAt(),
		user.UpdatedAt(),
		user.VerifiedAt(),
		user.LastSignedInAt(),
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "email") {
			return repositories.ErrEmailAlreadyRegistered
		}
		return fmt.Errorf("upsert user: %w", err)
	}

	for _, ev := range user.PullEvents() {
		payload, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("marshal event %s: %w", ev.EventName(), err)
		}
		_, err = tx.Exec(ctx, insertOutboxSQL,
			ev.EventName(), ev.AggregateID().String(), ev.Tenant().String(), payload, ev.At(),
		)
		if err != nil {
			return fmt.Errorf("insert outbox: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// FindByID returns a user by id, or ErrUserNotFound.
func (r *PostgresUserRepository) FindByID(ctx context.Context, id valueobjects.UserID) (*entities.User, error) {
	row := r.pool.QueryRow(ctx, selectUserByIDSQL, id.String())
	return scanUser(row)
}

// FindByEmail returns a user by (tenant, email).
func (r *PostgresUserRepository) FindByEmail(ctx context.Context, tenantID shared.TenantID, email valueobjects.Email) (*entities.User, error) {
	row := r.pool.QueryRow(ctx, selectUserByTenantEmailSQL, tenantID.String(), email.String())
	return scanUser(row)
}

// FindByEmailAcrossTenants returns a user by email regardless of tenant.
// Used at signin time when we don't yet know the tenant. Email is unique
// across tenants — see the migration.
func (r *PostgresUserRepository) FindByEmailAcrossTenants(ctx context.Context, email valueobjects.Email) (*entities.User, error) {
	row := r.pool.QueryRow(ctx, selectUserByEmailSQL, email.String())
	return scanUser(row)
}

func scanUser(row pgx.Row) (*entities.User, error) {
	var (
		id, tenantID, email, name, status string
		roles                             []string
		failedAttempts                    int
		lockedUntil                       *time.Time
		createdAt, updatedAt              time.Time
		verifiedAt, lastSignedInAt        *time.Time
	)
	err := row.Scan(&id, &tenantID, &email, &name, &status, &roles,
		&failedAttempts, &lockedUntil, &createdAt, &updatedAt, &verifiedAt, &lastSignedInAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repositories.ErrUserNotFound
		}
		return nil, fmt.Errorf("scan user: %w", err)
	}
	uid, err := valueobjects.ParseUserID(id)
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
	st, err := valueobjects.ParseUserStatus(status)
	if err != nil {
		return nil, err
	}
	return entities.HydrateUser(
		uid, tid, em, name, st, roles,
		failedAttempts, lockedUntil,
		createdAt, updatedAt, verifiedAt, lastSignedInAt,
	), nil
}

const upsertUserSQL = `
INSERT INTO users (
    id, tenant_id, email, name, status, roles,
    failed_attempts, locked_until,
    created_at, updated_at, verified_at, last_signed_in_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (id) DO UPDATE SET
    name              = EXCLUDED.name,
    status            = EXCLUDED.status,
    roles             = EXCLUDED.roles,
    failed_attempts   = EXCLUDED.failed_attempts,
    locked_until      = EXCLUDED.locked_until,
    updated_at        = EXCLUDED.updated_at,
    verified_at       = EXCLUDED.verified_at,
    last_signed_in_at = EXCLUDED.last_signed_in_at
`

const insertOutboxSQL = `
INSERT INTO auth_outbox (event_name, aggregate_id, tenant_id, payload, occurred_at)
VALUES ($1, $2, $3, $4, $5)
`

const selectUserCols = `
SELECT id, tenant_id, email, name, status, roles,
       failed_attempts, locked_until, created_at, updated_at, verified_at, last_signed_in_at
FROM users`

var (
	selectUserByIDSQL          = selectUserCols + ` WHERE id = $1`
	selectUserByTenantEmailSQL = selectUserCols + ` WHERE tenant_id = $1 AND email = $2`
	selectUserByEmailSQL       = selectUserCols + ` WHERE email = $1`
)
