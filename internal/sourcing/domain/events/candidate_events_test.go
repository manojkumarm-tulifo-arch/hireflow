package events_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/hustle/hireflow/internal/sourcing/domain/events"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

func TestCandidateParsed_Shape(t *testing.T) {
	id := uuid.New()
	tenant := shared.NewTenantID()
	at := time.Now().UTC()
	ev := events.CandidateParsed{
		CandidateID:   id,
		TenantID:      tenant,
		ContentHash:   "abc",
		SchemaVersion: 1,
		OccurredAt:    at,
	}
	assert.Equal(t, "sourcing.CandidateParsed", ev.EventName())
	assert.Equal(t, id, ev.AggregateID())
	assert.Equal(t, tenant, ev.Tenant())
	assert.Equal(t, at, ev.At())
}
