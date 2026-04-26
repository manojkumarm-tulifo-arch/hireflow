// Package repositories defines the persistence ports for the auth context.
package repositories

import (
	"context"
	"errors"

	"github.com/hustle/hireflow/internal/auth/domain/entities"
	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ErrUserNotFound is returned when a query targets a non-existent user.
var ErrUserNotFound = errors.New("user not found")

// ErrEmailAlreadyRegistered is returned when signup hits a unique-violation.
var ErrEmailAlreadyRegistered = errors.New("email already registered")

// UserRepository persists User aggregates.
// Save persists the aggregate and any pending events atomically (outbox).
type UserRepository interface {
	Save(ctx context.Context, user *entities.User) error
	FindByID(ctx context.Context, id valueobjects.UserID) (*entities.User, error)
	FindByEmail(ctx context.Context, tenantID shared.TenantID, email valueobjects.Email) (*entities.User, error)
	// FindByEmailAcrossTenants is used at signin time when we don't yet know
	// the tenant — the email column is unique across tenants in our schema.
	FindByEmailAcrossTenants(ctx context.Context, email valueobjects.Email) (*entities.User, error)
}
