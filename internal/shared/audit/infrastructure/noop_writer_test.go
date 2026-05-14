package infrastructure_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/shared/audit/domain"
	"github.com/hustle/hireflow/internal/shared/audit/infrastructure"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

func validAuditEvent(t *testing.T) domain.AuditEvent {
	t.Helper()
	tid, err := shared.ParseTenantID(uuid.New().String())
	require.NoError(t, err)
	return domain.AuditEvent{
		ActorUserID:  uuid.New(),
		TenantID:     tid,
		Action:       "candidate.viewed",
		ResourceKind: "candidate",
		ResourceID:   uuid.New(),
		Payload:      map[string]any{"key": "value"},
		OccurredAt:   time.Now().UTC(),
	}
}

func TestNoopAuditWriter_WriteReturnsNil(t *testing.T) {
	w := infrastructure.NewNoopAuditWriter()
	err := w.Write(context.Background(), validAuditEvent(t))
	require.NoError(t, err)
}

func TestNoopAuditWriter_WriteReturnsNil_EmptyPayload(t *testing.T) {
	w := infrastructure.NewNoopAuditWriter()
	e := validAuditEvent(t)
	e.Payload = nil
	err := w.Write(context.Background(), e)
	require.NoError(t, err)
}

