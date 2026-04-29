package messaging

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/jobposting/domain/events"
	"github.com/hustle/hireflow/internal/jobposting/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// TestDecodeEvent_RoundTrip protects the JSON contract between the postgres
// repo (writing the outbox row) and the dispatcher (reading it back).
func TestDecodeEvent_RoundTrip(t *testing.T) {
	postingID := valueobjects.NewPostingID()
	tenantID := shared.NewTenantID()
	at := time.Now().UTC().Truncate(time.Microsecond)

	tests := []struct {
		name  string
		event events.Event
	}{
		{"JobPostingCreated", events.NewJobPostingCreated(postingID, tenantID, "intent-1", "Senior Backend", at)},
		{"JobPostingPublished", events.NewJobPostingPublished(postingID, tenantID, []valueobjects.SourceChannel{}, 1, at)},
		{"JobPostingClosed", events.NewJobPostingClosed(postingID, tenantID, "filled", at)},
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
	_, err := decodeEvent("jobposting.GhostEvent", []byte(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown event name")
}

func TestDecodeEvent_MalformedPayload(t *testing.T) {
	_, err := decodeEvent("jobposting.JobPostingCreated", []byte(`{not-json`))
	require.Error(t, err)
}
