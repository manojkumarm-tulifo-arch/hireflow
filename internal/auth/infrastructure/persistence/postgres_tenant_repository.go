// Package persistence holds the postgres implementations of the auth context
// repository ports.
package persistence

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hustle/hireflow/internal/auth/domain/repositories"
	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// PostgresTenantRepository is a read-only lookup by slug — full Tenant
// management belongs in a future platform-admin context.
type PostgresTenantRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresTenantRepository wires the repository.
func NewPostgresTenantRepository(pool *pgxpool.Pool) *PostgresTenantRepository {
	return &PostgresTenantRepository{pool: pool}
}

// FindIDBySlug resolves a tenant slug to its TenantID.
func (r *PostgresTenantRepository) FindIDBySlug(ctx context.Context, slug valueobjects.TenantSlug) (shared.TenantID, error) {
	var raw string
	err := r.pool.QueryRow(ctx, `SELECT id FROM tenants WHERE slug = $1`, slug.String()).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.TenantID{}, repositories.ErrTenantNotFound
		}
		return shared.TenantID{}, fmt.Errorf("select tenant: %w", err)
	}
	return shared.ParseTenantID(raw)
}
