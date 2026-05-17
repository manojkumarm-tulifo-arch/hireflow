//go:build integration

package messaging_test

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/interview/domain/events"
	"github.com/hustle/hireflow/internal/interview/infrastructure/messaging"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// newPool creates a test pool, skips if DATABASE_URL is unset, and truncates
// interview_outbox for test isolation.
func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(context.Background(), `TRUNCATE interview_outbox`)
	require.NoError(t, err)
	return pool
}

// recordingPublisher captures events forwarded by the dispatcher.
type recordingPublisher struct {
	mu    sync.Mutex
	calls []events.Event
}

func (p *recordingPublisher) Publish(_ context.Context, ev events.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, ev)
	return nil
}

func (p *recordingPublisher) Events() []events.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]events.Event, len(p.calls))
	copy(out, p.calls)
	return out
}

// insertOutboxRow inserts a row into interview_outbox and returns its id.
func insertOutboxRow(t *testing.T, pool *pgxpool.Pool, eventName string, aggregateID uuid.UUID, tenant shared.TenantID, payload []byte, dispatchedAt *time.Time) int64 {
	t.Helper()
	var id int64
	var err error
	if dispatchedAt == nil {
		err = pool.QueryRow(context.Background(), `
			INSERT INTO interview_outbox (event_name, aggregate_id, tenant_id, payload, occurred_at)
			VALUES ($1, $2, $3, $4, now())
			RETURNING id
		`, eventName, aggregateID, tenant.UUID(), payload).Scan(&id)
	} else {
		err = pool.QueryRow(context.Background(), `
			INSERT INTO interview_outbox (event_name, aggregate_id, tenant_id, payload, occurred_at, dispatched_at)
			VALUES ($1, $2, $3, $4, now(), $5)
			RETURNING id
		`, eventName, aggregateID, tenant.UUID(), payload, *dispatchedAt).Scan(&id)
	}
	require.NoError(t, err)
	return id
}

func newDispatcher(t *testing.T, pool *pgxpool.Pool, pub *recordingPublisher) *messaging.OutboxDispatcher {
	t.Helper()
	return messaging.NewOutboxDispatcher(pool, pub, zerolog.Nop(), messaging.DispatcherConfig{
		BatchSize:    50,
		PollInterval: time.Second,
	})
}

func TestDispatcher_DispatchesPendingEvents(t *testing.T) {
	pool := newPool(t)
	pub := &recordingPublisher{}
	d := newDispatcher(t, pool, pub)
	ctx := context.Background()
	tenant := shared.NewTenantID()

	// Build 3 different event payloads.
	processID := uuid.New()
	created := events.InterviewProcessCreated{
		ProcessID:     processID,
		TenantID:      tenant,
		ApplicationID: uuid.New(),
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
		OccurredAt:    time.Now().UTC().Truncate(time.Millisecond),
	}
	createdPayload, err := json.Marshal(created)
	require.NoError(t, err)

	roundID := uuid.New()
	generated := events.InterviewQuestionsGenerated{
		RoundID:       roundID,
		ProcessID:     processID,
		Kind:          "technical",
		QuestionCount: 5,
		TenantID:      tenant,
		OccurredAt:    time.Now().UTC().Truncate(time.Millisecond),
	}
	generatedPayload, err := json.Marshal(generated)
	require.NoError(t, err)

	feedbackID := uuid.New()
	feedback := events.InterviewFeedbackRecorded{
		FeedbackID: feedbackID,
		RoundID:    roundID,
		Decision:   "pass",
		TenantID:   tenant,
		OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	feedbackPayload, err := json.Marshal(feedback)
	require.NoError(t, err)

	id1 := insertOutboxRow(t, pool, "interview.InterviewProcessCreated", processID, tenant, createdPayload, nil)
	id2 := insertOutboxRow(t, pool, "interview.InterviewQuestionsGenerated", roundID, tenant, generatedPayload, nil)
	id3 := insertOutboxRow(t, pool, "interview.InterviewFeedbackRecorded", feedbackID, tenant, feedbackPayload, nil)

	require.NoError(t, d.DispatchBatch(ctx))

	got := pub.Events()
	assert.Len(t, got, 3)
	assert.Equal(t, "interview.InterviewProcessCreated", got[0].EventName())
	assert.Equal(t, "interview.InterviewQuestionsGenerated", got[1].EventName())
	assert.Equal(t, "interview.InterviewFeedbackRecorded", got[2].EventName())

	// All three rows must now have dispatched_at set.
	for _, id := range []int64{id1, id2, id3} {
		var dispatchedAt *time.Time
		require.NoError(t, pool.QueryRow(ctx, `SELECT dispatched_at FROM interview_outbox WHERE id=$1`, id).Scan(&dispatchedAt))
		assert.NotNil(t, dispatchedAt, "id=%d should be marked dispatched", id)
	}
}

func TestDispatcher_SkipsAlreadyDispatched(t *testing.T) {
	pool := newPool(t)
	pub := &recordingPublisher{}
	d := newDispatcher(t, pool, pub)
	ctx := context.Background()
	tenant := shared.NewTenantID()

	processID := uuid.New()
	created := events.InterviewProcessCreated{
		ProcessID:     processID,
		TenantID:      tenant,
		ApplicationID: uuid.New(),
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
		OccurredAt:    time.Now().UTC().Truncate(time.Millisecond),
	}
	payload, err := json.Marshal(created)
	require.NoError(t, err)

	alreadyDispatched := time.Now().UTC()
	insertOutboxRow(t, pool, "interview.InterviewProcessCreated", processID, tenant, payload, &alreadyDispatched)

	require.NoError(t, d.DispatchBatch(ctx))

	assert.Len(t, pub.Events(), 0, "already-dispatched row should be skipped")
}

func TestDispatcher_DecodeFailure_LeavesRowUndispatched(t *testing.T) {
	pool := newPool(t)
	pub := &recordingPublisher{}
	d := newDispatcher(t, pool, pub)
	ctx := context.Background()
	tenant := shared.NewTenantID()

	aggID := uuid.New()
	id := insertOutboxRow(t, pool, "interview.UnknownEvent", aggID, tenant, []byte(`{"garbage":true}`), nil)

	require.NoError(t, d.DispatchBatch(ctx))

	// Publisher should receive nothing.
	assert.Len(t, pub.Events(), 0)

	// Row must still be undispatched.
	var dispatchedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT dispatched_at FROM interview_outbox WHERE id=$1`, id).Scan(&dispatchedAt))
	assert.Nil(t, dispatchedAt, "decode-failed row should remain undispatched")
}
