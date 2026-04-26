// Package repositories defines repository interfaces for the hiringintent
// bounded context. Implementations live in infrastructure/persistence.
package repositories

import (
	"context"
	"errors"

	"github.com/hustle/hireflow/internal/hiringintent/domain/entities"
	"github.com/hustle/hireflow/internal/hiringintent/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ErrIntentNotFound is returned when a query targets a non-existent intent.
var ErrIntentNotFound = errors.New("hiring intent not found")

// ListSortOrder controls how List results are ordered.
type ListSortOrder string

const (
	// SortNewestFirst orders by created_at DESC (default).
	SortNewestFirst ListSortOrder = "NEWEST"
	// SortUrgentFirst orders by priority severity then created_at DESC.
	SortUrgentFirst ListSortOrder = "URGENT"
)

// IntentFilter narrows a List query. All fields are optional; zero values mean
// "no filter" and SortBy="" defaults to SortNewestFirst.
type IntentFilter struct {
	Status      *valueobjects.IntentStatus
	RecruiterID *shared.RecruiterID
	Search      string
	SortBy      ListSortOrder
	Limit       int
	Offset      int
}

// StatusCounts is the per-status histogram returned by Counts.
type StatusCounts map[valueobjects.IntentStatus]int

// IntentRepository is the persistence port for HiringIntent aggregates.
// Save must persist the aggregate state and any pending events atomically
// (e.g., via the outbox pattern). Implementations live in infrastructure/.
type IntentRepository interface {
	// Save inserts or updates the aggregate and persists pending events.
	Save(ctx context.Context, intent *entities.HiringIntent) error

	// FindByID returns the aggregate scoped to a tenant.
	// Returns ErrIntentNotFound if missing or owned by a different tenant.
	FindByID(ctx context.Context, tenantID shared.TenantID, id valueobjects.IntentID) (*entities.HiringIntent, error)

	// List returns aggregates within a tenant matching the filter.
	List(ctx context.Context, tenantID shared.TenantID, filter IntentFilter) ([]*entities.HiringIntent, error)

	// Counts returns a per-status histogram for a tenant. Returns a fully
	// populated map (zero entries for missing statuses) so callers can render
	// summary chips without further coalescing.
	Counts(ctx context.Context, tenantID shared.TenantID) (StatusCounts, error)
}
