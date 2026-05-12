// Package repositories defines the persistence ports of the sourcing context.
//
// All methods MUST scope by tenant_id (either via explicit parameter or via
// the aggregate's own TenantID). Implementations include tenant_id in every
// WHERE clause so the partitioned applications table can prune correctly
// even though this repo doesn't touch it directly — same convention applies.
package repositories

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ErrNotFound is returned when an upload is not found.
var ErrNotFound = errors.New("resume upload not found")

// ErrDuplicate is returned when content_hash already exists for a tenant.
// UploadResumeBatchHandler uses this to attach to the existing row.
var ErrDuplicate = errors.New("resume upload duplicate")

// ResumeUploadRepository persists ResumeUpload aggregates.
type ResumeUploadRepository interface {
	// Save upserts the aggregate, drains its pending events into the outbox
	// table in the same transaction. Honors the (tenant_id, content_hash)
	// uniqueness — returns ErrDuplicate when violated.
	Save(ctx context.Context, u *entities.ResumeUpload) error

	// FindByID loads an upload by id. Tenant must match.
	FindByID(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (*entities.ResumeUpload, error)

	// FindByContentHash returns the existing upload (any intent) matching
	// (tenant, content_hash), or ErrNotFound.
	FindByContentHash(ctx context.Context, tenant shared.TenantID, hash string) (*entities.ResumeUpload, error)

	// ClaimNextPending claims one row in (Pending or any non-terminal status
	// where next_attempt_at <= now) using FOR UPDATE SKIP LOCKED. Returns
	// ErrNotFound if no claimable row exists. The caller MUST hold the
	// returned transaction-bound aggregate until they call Save (which
	// commits the tx) — see worker pool for the exact pattern.
	//
	// For slice 1, ClaimNextPending uses the simpler "load row, advance
	// status, save in a new tx" pattern — single-binary deployment + idempotent
	// stages mean we tolerate a brief overlap window if two workers claim the
	// same row. SLICE 4 hardens this with proper SKIP LOCKED + tx-scoped Save.
	ClaimNextPending(ctx context.Context) (*entities.ResumeUpload, error)

	// ListByBatch returns all uploads in a batch (tenant-scoped) for the
	// status endpoint.
	ListByBatch(ctx context.Context, tenant shared.TenantID, batchID uuid.UUID) ([]*entities.ResumeUpload, error)
}
