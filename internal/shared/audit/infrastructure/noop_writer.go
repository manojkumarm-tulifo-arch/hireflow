package infrastructure

import (
	"context"

	"github.com/hustle/hireflow/internal/shared/audit/domain"
)

// NoopAuditWriter discards every audit event. Intended for unit tests that
// exercise application logic but do not care about audit persistence.
type NoopAuditWriter struct{}

// NewNoopAuditWriter returns a NoopAuditWriter.
func NewNoopAuditWriter() *NoopAuditWriter {
	return &NoopAuditWriter{}
}

// Write always returns nil without doing anything.
func (w *NoopAuditWriter) Write(_ context.Context, _ domain.AuditEvent) error {
	return nil
}
