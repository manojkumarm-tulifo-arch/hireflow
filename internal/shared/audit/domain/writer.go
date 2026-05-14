package domain

import (
	"context"
	"errors"
)

// ErrAuditFailed is returned when the audit write itself failed. Callers MUST
// treat this as load-bearing: if audit fails, the caller should NOT proceed
// (e.g., don't return PII to a caller you couldn't audit).
var ErrAuditFailed = errors.New("audit: write failed")

// AuditWriter persists audit events.
type AuditWriter interface {
	Write(ctx context.Context, event AuditEvent) error
}
