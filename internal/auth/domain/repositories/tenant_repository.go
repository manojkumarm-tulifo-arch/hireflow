package repositories

import (
	"context"
	"errors"

	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ErrTenantNotFound is returned when a slug doesn't match any tenant.
var ErrTenantNotFound = errors.New("tenant not found")

// TenantRepository is a read-only port for resolving tenant slugs to IDs.
// The full Tenant aggregate (creation, suspension, settings) belongs in a
// future platform-admin context — this is just a lookup.
type TenantRepository interface {
	FindIDBySlug(ctx context.Context, slug valueobjects.TenantSlug) (shared.TenantID, error)
}
