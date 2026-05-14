package infrastructure

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hustle/hireflow/internal/shared/audit/domain"
)

// PostgresAuditWriter persists AuditEvents to the audit_log table.
type PostgresAuditWriter struct {
	pool *pgxpool.Pool
}

// NewPostgresAuditWriter constructs a PostgresAuditWriter backed by pool.
func NewPostgresAuditWriter(pool *pgxpool.Pool) *PostgresAuditWriter {
	return &PostgresAuditWriter{pool: pool}
}

// Write validates the event, marshals the payload, and inserts a row.
// Validation or marshalling failures are returned without wrapping ErrAuditFailed.
// Database failures are wrapped with ErrAuditFailed.
func (w *PostgresAuditWriter) Write(ctx context.Context, e domain.AuditEvent) error {
	if err := e.Validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	payload, err := e.MarshalPayload()
	if err != nil {
		return fmt.Errorf("payload: %w", err)
	}
	_, err = w.pool.Exec(ctx, `
		INSERT INTO audit_log (
			actor_user_id, tenant_id, action, resource_kind, resource_id, payload, occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, e.ActorUserID, e.TenantID.String(), e.Action, e.ResourceKind, e.ResourceID, payload, e.OccurredAt)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrAuditFailed, err)
	}
	return nil
}
