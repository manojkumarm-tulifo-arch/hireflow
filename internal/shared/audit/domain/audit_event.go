package domain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// AuditEvent is one row in the audit log. Immutable; no behaviour.
type AuditEvent struct {
	ActorUserID  uuid.UUID
	TenantID     shared.TenantID
	Action       string
	ResourceKind string
	ResourceID   uuid.UUID
	Payload      map[string]any
	OccurredAt   time.Time
}

// Validate enforces minimum invariants.
func (e AuditEvent) Validate() error {
	if e.Action == "" {
		return errors.New("audit: action required")
	}
	if e.ResourceKind == "" {
		return errors.New("audit: resource_kind required")
	}
	return nil
}

// MarshalPayload returns the JSON-encoded payload bytes, or `{}` if empty.
func (e AuditEvent) MarshalPayload() ([]byte, error) {
	if len(e.Payload) == 0 {
		return []byte(`{}`), nil
	}
	return json.Marshal(e.Payload)
}
