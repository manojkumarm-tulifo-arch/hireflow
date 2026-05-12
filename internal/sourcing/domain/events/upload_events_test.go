package events_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/hustle/hireflow/internal/sourcing/domain/events"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

func TestResumeUploadAccepted_Shape(t *testing.T) {
	id := uuid.New()
	tenant := shared.NewTenantID()
	at := time.Now().UTC()

	ev := events.ResumeUploadAccepted{
		UploadID:    id,
		TenantID:    tenant,
		IntentID:    uuid.New(),
		BatchID:     uuid.New(),
		ContentHash: "abc",
		OccurredAt:  at,
	}

	assert.Equal(t, "sourcing.ResumeUploadAccepted", ev.EventName())
	assert.Equal(t, id, ev.AggregateID())
	assert.Equal(t, tenant, ev.Tenant())
	assert.Equal(t, at, ev.At())
}

func TestResumeUploadFailed_CarriesReason(t *testing.T) {
	ev := events.ResumeUploadFailed{
		UploadID:   uuid.New(),
		TenantID:   shared.NewTenantID(),
		Reason:     "virus_detected",
		Detail:     "EICAR-TEST",
		OccurredAt: time.Now().UTC(),
	}
	assert.Equal(t, "sourcing.ResumeUploadFailed", ev.EventName())
	assert.Equal(t, "virus_detected", ev.Reason)
}

func TestResumeExtracted_Shape(t *testing.T) {
	ev := events.ResumeExtracted{
		UploadID:   uuid.New(),
		TenantID:   shared.NewTenantID(),
		PageCount:  3,
		OccurredAt: time.Now().UTC(),
	}
	assert.Equal(t, "sourcing.ResumeExtracted", ev.EventName())
}
