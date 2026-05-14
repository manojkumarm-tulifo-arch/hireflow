package events_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/events"
)

func TestApplicationScored_Shape(t *testing.T) {
	appID := uuid.New()
	candidateID := uuid.New()
	intentID := uuid.New()
	tenant := shared.NewTenantID()
	at := time.Now().UTC().Truncate(time.Millisecond)
	score := 87.5
	band := "strong"

	ev := events.ApplicationScored{
		ApplicationID:  appID,
		CandidateID:    candidateID,
		IntentID:       intentID,
		TenantID:       tenant,
		OverallScore:   &score,
		ScoreBand:      band,
		EmbeddingScore: 0.81,
		OccurredAt:     at,
	}

	assert.Equal(t, "sourcing.ApplicationScored", ev.EventName())
	assert.Equal(t, appID, ev.AggregateID())
	assert.Equal(t, tenant, ev.Tenant())
	assert.Equal(t, at, ev.At())
	require.NotNil(t, ev.OverallScore)
	assert.Equal(t, 87.5, *ev.OverallScore)
	assert.Equal(t, "strong", ev.ScoreBand)
}

func TestApplicationScored_NilOverallScore(t *testing.T) {
	// OverallScore is nil when the application has not yet been LLM-judged.
	ev := events.ApplicationScored{
		ApplicationID:  uuid.New(),
		CandidateID:    uuid.New(),
		IntentID:       uuid.New(),
		TenantID:       shared.NewTenantID(),
		OverallScore:   nil,
		ScoreBand:      "",
		EmbeddingScore: 0.72,
		OccurredAt:     time.Now().UTC(),
	}
	assert.Nil(t, ev.OverallScore)
	assert.Empty(t, ev.ScoreBand)
}

func TestApplicationScored_JSONRoundTrip(t *testing.T) {
	score := 73.0
	original := events.ApplicationScored{
		ApplicationID:  uuid.New(),
		CandidateID:    uuid.New(),
		IntentID:       uuid.New(),
		TenantID:       shared.NewTenantID(),
		OverallScore:   &score,
		ScoreBand:      "moderate",
		EmbeddingScore: 0.65,
		OccurredAt:     time.Now().UTC().Truncate(time.Millisecond),
	}

	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded events.ApplicationScored
	require.NoError(t, json.Unmarshal(b, &decoded))

	assert.Equal(t, original.ApplicationID, decoded.ApplicationID)
	assert.Equal(t, original.CandidateID, decoded.CandidateID)
	assert.Equal(t, original.IntentID, decoded.IntentID)
	assert.Equal(t, original.TenantID, decoded.TenantID)
	require.NotNil(t, decoded.OverallScore)
	assert.Equal(t, *original.OverallScore, *decoded.OverallScore)
	assert.Equal(t, original.ScoreBand, decoded.ScoreBand)
	assert.Equal(t, original.EmbeddingScore, decoded.EmbeddingScore)
	assert.Equal(t, original.OccurredAt, decoded.OccurredAt)
}

func TestApplicationExcluded_Shape(t *testing.T) {
	appID := uuid.New()
	candidateID := uuid.New()
	intentID := uuid.New()
	tenant := shared.NewTenantID()
	at := time.Now().UTC().Truncate(time.Millisecond)

	ev := events.ApplicationExcluded{
		ApplicationID: appID,
		CandidateID:   candidateID,
		IntentID:      intentID,
		TenantID:      tenant,
		Reason:        "required_skills_not_met",
		OccurredAt:    at,
	}

	assert.Equal(t, "sourcing.ApplicationExcluded", ev.EventName())
	assert.Equal(t, appID, ev.AggregateID())
	assert.Equal(t, tenant, ev.Tenant())
	assert.Equal(t, at, ev.At())
	assert.Equal(t, "required_skills_not_met", ev.Reason)
}

func TestApplicationExcluded_JSONRoundTrip(t *testing.T) {
	original := events.ApplicationExcluded{
		ApplicationID: uuid.New(),
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
		TenantID:      shared.NewTenantID(),
		Reason:        "required_skills_not_met",
		OccurredAt:    time.Now().UTC().Truncate(time.Millisecond),
	}

	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded events.ApplicationExcluded
	require.NoError(t, json.Unmarshal(b, &decoded))

	assert.Equal(t, original.ApplicationID, decoded.ApplicationID)
	assert.Equal(t, original.CandidateID, decoded.CandidateID)
	assert.Equal(t, original.IntentID, decoded.IntentID)
	assert.Equal(t, original.TenantID, decoded.TenantID)
	assert.Equal(t, original.Reason, decoded.Reason)
	assert.Equal(t, original.OccurredAt, decoded.OccurredAt)
}

func TestApplicationEmbedFailed_Shape(t *testing.T) {
	appID := uuid.New()
	tenant := shared.NewTenantID()
	at := time.Now().UTC().Truncate(time.Millisecond)

	ev := events.ApplicationEmbedFailed{
		ApplicationID: appID,
		TenantID:      tenant,
		Reason:        "voyage_api_unavailable",
		OccurredAt:    at,
	}

	assert.Equal(t, "sourcing.ApplicationEmbedFailed", ev.EventName())
	assert.Equal(t, appID, ev.AggregateID())
	assert.Equal(t, tenant, ev.Tenant())
	assert.Equal(t, at, ev.At())
	assert.Equal(t, "voyage_api_unavailable", ev.Reason)
}

func TestApplicationEmbedFailed_JSONRoundTrip(t *testing.T) {
	original := events.ApplicationEmbedFailed{
		ApplicationID: uuid.New(),
		TenantID:      shared.NewTenantID(),
		Reason:        "voyage_api_timeout",
		OccurredAt:    time.Now().UTC().Truncate(time.Millisecond),
	}

	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded events.ApplicationEmbedFailed
	require.NoError(t, json.Unmarshal(b, &decoded))

	assert.Equal(t, original.ApplicationID, decoded.ApplicationID)
	assert.Equal(t, original.TenantID, decoded.TenantID)
	assert.Equal(t, original.Reason, decoded.Reason)
	assert.Equal(t, original.OccurredAt, decoded.OccurredAt)
}

func TestApplicationJudgeFailed_Shape(t *testing.T) {
	appID := uuid.New()
	tenant := shared.NewTenantID()
	at := time.Now().UTC().Truncate(time.Millisecond)

	ev := events.ApplicationJudgeFailed{
		ApplicationID: appID,
		TenantID:      tenant,
		Reason:        "max_retries_exceeded",
		OccurredAt:    at,
	}

	assert.Equal(t, "sourcing.ApplicationJudgeFailed", ev.EventName())
	assert.Equal(t, appID, ev.AggregateID())
	assert.Equal(t, tenant, ev.Tenant())
	assert.Equal(t, at, ev.At())
	assert.Equal(t, "max_retries_exceeded", ev.Reason)
}

func TestApplicationJudgeFailed_JSONRoundTrip(t *testing.T) {
	original := events.ApplicationJudgeFailed{
		ApplicationID: uuid.New(),
		TenantID:      shared.NewTenantID(),
		Reason:        "claude_overloaded",
		OccurredAt:    time.Now().UTC().Truncate(time.Millisecond),
	}

	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded events.ApplicationJudgeFailed
	require.NoError(t, json.Unmarshal(b, &decoded))

	assert.Equal(t, original.ApplicationID, decoded.ApplicationID)
	assert.Equal(t, original.TenantID, decoded.TenantID)
	assert.Equal(t, original.Reason, decoded.Reason)
	assert.Equal(t, original.OccurredAt, decoded.OccurredAt)
}

// ── ApplicationShortlisted ────────────────────────────────────────────────────

func TestApplicationShortlisted_Shape(t *testing.T) {
	appID := uuid.New()
	actor := uuid.New()
	tenant := shared.NewTenantID()
	at := time.Now().UTC().Truncate(time.Millisecond)

	ev := events.ApplicationShortlisted{
		ApplicationID: appID,
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
		TenantID:      tenant,
		ActorUserID:   actor,
		OccurredAt:    at,
	}

	assert.Equal(t, "sourcing.ApplicationShortlisted", ev.EventName())
	assert.Equal(t, appID, ev.AggregateID())
	assert.Equal(t, tenant, ev.Tenant())
	assert.Equal(t, at, ev.At())
	assert.Equal(t, actor, ev.ActorUserID)
}

func TestApplicationShortlisted_JSONRoundTrip(t *testing.T) {
	original := events.ApplicationShortlisted{
		ApplicationID: uuid.New(),
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
		TenantID:      shared.NewTenantID(),
		ActorUserID:   uuid.New(),
		OccurredAt:    time.Now().UTC().Truncate(time.Millisecond),
	}

	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded events.ApplicationShortlisted
	require.NoError(t, json.Unmarshal(b, &decoded))

	assert.Equal(t, original.ApplicationID, decoded.ApplicationID)
	assert.Equal(t, original.CandidateID, decoded.CandidateID)
	assert.Equal(t, original.IntentID, decoded.IntentID)
	assert.Equal(t, original.TenantID, decoded.TenantID)
	assert.Equal(t, original.ActorUserID, decoded.ActorUserID)
	assert.Equal(t, original.OccurredAt, decoded.OccurredAt)
}

// ── ApplicationRejected ───────────────────────────────────────────────────────

func TestApplicationRejected_Shape(t *testing.T) {
	appID := uuid.New()
	actor := uuid.New()
	tenant := shared.NewTenantID()
	at := time.Now().UTC().Truncate(time.Millisecond)

	ev := events.ApplicationRejected{
		ApplicationID: appID,
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
		TenantID:      tenant,
		ActorUserID:   actor,
		Reason:        "not qualified",
		OccurredAt:    at,
	}

	assert.Equal(t, "sourcing.ApplicationRejected", ev.EventName())
	assert.Equal(t, appID, ev.AggregateID())
	assert.Equal(t, tenant, ev.Tenant())
	assert.Equal(t, at, ev.At())
	assert.Equal(t, actor, ev.ActorUserID)
	assert.Equal(t, "not qualified", ev.Reason)
}

func TestApplicationRejected_JSONRoundTrip(t *testing.T) {
	original := events.ApplicationRejected{
		ApplicationID: uuid.New(),
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
		TenantID:      shared.NewTenantID(),
		ActorUserID:   uuid.New(),
		Reason:        "does not meet requirements",
		OccurredAt:    time.Now().UTC().Truncate(time.Millisecond),
	}

	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded events.ApplicationRejected
	require.NoError(t, json.Unmarshal(b, &decoded))

	assert.Equal(t, original.ApplicationID, decoded.ApplicationID)
	assert.Equal(t, original.CandidateID, decoded.CandidateID)
	assert.Equal(t, original.IntentID, decoded.IntentID)
	assert.Equal(t, original.TenantID, decoded.TenantID)
	assert.Equal(t, original.ActorUserID, decoded.ActorUserID)
	assert.Equal(t, original.Reason, decoded.Reason)
	assert.Equal(t, original.OccurredAt, decoded.OccurredAt)
}

// ── ApplicationHired ──────────────────────────────────────────────────────────

func TestApplicationHired_Shape(t *testing.T) {
	appID := uuid.New()
	actor := uuid.New()
	tenant := shared.NewTenantID()
	at := time.Now().UTC().Truncate(time.Millisecond)

	ev := events.ApplicationHired{
		ApplicationID: appID,
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
		TenantID:      tenant,
		ActorUserID:   actor,
		OccurredAt:    at,
	}

	assert.Equal(t, "sourcing.ApplicationHired", ev.EventName())
	assert.Equal(t, appID, ev.AggregateID())
	assert.Equal(t, tenant, ev.Tenant())
	assert.Equal(t, at, ev.At())
	assert.Equal(t, actor, ev.ActorUserID)
}

func TestApplicationHired_JSONRoundTrip(t *testing.T) {
	original := events.ApplicationHired{
		ApplicationID: uuid.New(),
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
		TenantID:      shared.NewTenantID(),
		ActorUserID:   uuid.New(),
		OccurredAt:    time.Now().UTC().Truncate(time.Millisecond),
	}

	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded events.ApplicationHired
	require.NoError(t, json.Unmarshal(b, &decoded))

	assert.Equal(t, original.ApplicationID, decoded.ApplicationID)
	assert.Equal(t, original.CandidateID, decoded.CandidateID)
	assert.Equal(t, original.IntentID, decoded.IntentID)
	assert.Equal(t, original.TenantID, decoded.TenantID)
	assert.Equal(t, original.ActorUserID, decoded.ActorUserID)
	assert.Equal(t, original.OccurredAt, decoded.OccurredAt)
}

// ── ApplicationMovedToInterviewing ────────────────────────────────────────────

func TestApplicationMovedToInterviewing_Shape(t *testing.T) {
	appID := uuid.New()
	actor := uuid.New()
	tenant := shared.NewTenantID()
	at := time.Now().UTC().Truncate(time.Millisecond)

	ev := events.ApplicationMovedToInterviewing{
		ApplicationID: appID,
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
		TenantID:      tenant,
		ActorUserID:   actor,
		OccurredAt:    at,
	}

	assert.Equal(t, "sourcing.ApplicationMovedToInterviewing", ev.EventName())
	assert.Equal(t, appID, ev.AggregateID())
	assert.Equal(t, tenant, ev.Tenant())
	assert.Equal(t, at, ev.At())
	assert.Equal(t, actor, ev.ActorUserID)
}

func TestApplicationMovedToInterviewing_JSONRoundTrip(t *testing.T) {
	original := events.ApplicationMovedToInterviewing{
		ApplicationID: uuid.New(),
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
		TenantID:      shared.NewTenantID(),
		ActorUserID:   uuid.New(),
		OccurredAt:    time.Now().UTC().Truncate(time.Millisecond),
	}

	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded events.ApplicationMovedToInterviewing
	require.NoError(t, json.Unmarshal(b, &decoded))

	assert.Equal(t, original.ApplicationID, decoded.ApplicationID)
	assert.Equal(t, original.CandidateID, decoded.CandidateID)
	assert.Equal(t, original.IntentID, decoded.IntentID)
	assert.Equal(t, original.TenantID, decoded.TenantID)
	assert.Equal(t, original.ActorUserID, decoded.ActorUserID)
	assert.Equal(t, original.OccurredAt, decoded.OccurredAt)
}

// ── CandidateErased ───────────────────────────────────────────────────────────

func TestCandidateErased_Shape(t *testing.T) {
	candidateID := uuid.New()
	actor := uuid.New()
	tenant := shared.NewTenantID()
	at := time.Now().UTC().Truncate(time.Millisecond)

	ev := events.CandidateErased{
		CandidateID: candidateID,
		TenantID:    tenant,
		ActorUserID: actor,
		OccurredAt:  at,
	}

	assert.Equal(t, "sourcing.CandidateErased", ev.EventName())
	assert.Equal(t, candidateID, ev.AggregateID())
	assert.Equal(t, tenant, ev.Tenant())
	assert.Equal(t, at, ev.At())
	assert.Equal(t, actor, ev.ActorUserID)
}

func TestCandidateErased_JSONRoundTrip(t *testing.T) {
	original := events.CandidateErased{
		CandidateID: uuid.New(),
		TenantID:    shared.NewTenantID(),
		ActorUserID: uuid.New(),
		OccurredAt:  time.Now().UTC().Truncate(time.Millisecond),
	}

	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded events.CandidateErased
	require.NoError(t, json.Unmarshal(b, &decoded))

	assert.Equal(t, original.CandidateID, decoded.CandidateID)
	assert.Equal(t, original.TenantID, decoded.TenantID)
	assert.Equal(t, original.ActorUserID, decoded.ActorUserID)
	assert.Equal(t, original.OccurredAt, decoded.OccurredAt)
}
