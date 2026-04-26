package messaging

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/hiringintent/domain/events"
	"github.com/hustle/hireflow/internal/hiringintent/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// TestDecodeEvent_RoundTrip exercises the marshal/decode contract used by
// the postgres repo (writes JSON to outbox) and the dispatcher (reads JSON
// out of outbox). If these drift, the dispatcher silently drops events.
func TestDecodeEvent_RoundTrip(t *testing.T) {
	intentID := valueobjects.NewIntentID()
	tenantID := shared.NewTenantID()
	recruiterID := shared.NewRecruiterID()
	at := time.Now().UTC().Truncate(time.Microsecond)

	tests := []struct {
		name  string
		event events.Event
	}{
		{"IntentDrafted", events.NewIntentDrafted(intentID, tenantID, recruiterID, "Senior Backend", at)},
		{"IntentRoleUpdated", events.NewIntentRoleUpdated(intentID, tenantID, "Staff Backend", at)},
		{"IntentConfirmed", events.NewIntentConfirmed(intentID, tenantID, recruiterID, valueobjects.PriorityHigh, at)},
		{"IntentCancelled", events.NewIntentCancelled(intentID, tenantID, "no longer needed", at)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(tc.event)
			require.NoError(t, err)

			decoded, err := decodeEvent(tc.event.EventName(), payload)
			require.NoError(t, err)

			assert.Equal(t, tc.event.EventName(), decoded.EventName())
			assert.True(t, decoded.AggregateID().Equals(tc.event.AggregateID()))
			assert.True(t, decoded.Tenant().Equals(tc.event.Tenant()))
			assert.True(t, decoded.At().Equal(tc.event.At()))
		})
	}
}

func TestDecodeEvent_UnknownName(t *testing.T) {
	_, err := decodeEvent("hiringintent.GhostEvent", []byte(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown event name")
}

func TestDecodeEvent_MalformedPayload(t *testing.T) {
	_, err := decodeEvent("hiringintent.IntentDrafted", []byte(`{not-json`))
	require.Error(t, err)
}
