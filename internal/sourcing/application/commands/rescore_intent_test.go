package commands_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
)

// ---------------------------------------------------------------------------
// stubScoreIntentDispatcher
// ---------------------------------------------------------------------------

// stubScoreIntentDispatcher satisfies the scoreIntentDispatcher interface used
// internally by RescoreIntentHandler. It records received inputs and can be
// configured to return an error.
type stubScoreIntentDispatcher struct {
	calls  []commands.ScoreIntentInput
	retErr error
}

func (s *stubScoreIntentDispatcher) Handle(_ context.Context, in commands.ScoreIntentInput) error {
	s.calls = append(s.calls, in)
	return s.retErr
}

// ---------------------------------------------------------------------------
// invalidateTrackingRepo
// ---------------------------------------------------------------------------

// invalidateTrackingRepo wraps fakeApplicationRepo and adds tracking for the
// InvalidateJudgmentsForIntent method plus an error injection point.
type invalidateTrackingRepo struct {
	*fakeApplicationRepo
	invalidateCalls  int
	invalidateIntent uuid.UUID
	invalidateErr    error
}

func newInvalidateTrackingRepo() *invalidateTrackingRepo {
	return &invalidateTrackingRepo{fakeApplicationRepo: newFakeApplicationRepo()}
}

func (r *invalidateTrackingRepo) InvalidateJudgmentsForIntent(_ context.Context, _ shared.TenantID, intentID uuid.UUID) error {
	r.invalidateCalls++
	r.invalidateIntent = intentID
	return r.invalidateErr
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRescoreIntent_HappyPath_CallsAllThreeSteps(t *testing.T) {
	tenant := shared.NewTenantID()
	intentID := uuid.New()
	actorID := uuid.New()

	appRepo := newInvalidateTrackingRepo()
	dispatcher := &stubScoreIntentDispatcher{}
	audit := &fakeAuditWriter{}

	h := commands.NewRescoreIntentHandler(appRepo, dispatcher, audit)

	err := h.Handle(context.Background(), commands.RescoreIntentInput{
		TenantID:    tenant,
		ActorUserID: actorID,
		IntentID:    intentID,
	})
	require.NoError(t, err)

	// InvalidateJudgmentsForIntent was called once for the correct intent.
	assert.Equal(t, 1, appRepo.invalidateCalls)
	assert.Equal(t, intentID, appRepo.invalidateIntent)

	// ScoreIntentHandler was called once with matching TenantID + IntentID.
	require.Len(t, dispatcher.calls, 1)
	assert.Equal(t, tenant, dispatcher.calls[0].TenantID)
	assert.Equal(t, intentID, dispatcher.calls[0].IntentID)

	// Audit event was written.
	require.Len(t, audit.events, 1)
	ev := audit.events[0]
	assert.Equal(t, "intent_rescored", ev.Action)
	assert.Equal(t, "intent", ev.ResourceKind)
	assert.Equal(t, intentID, ev.ResourceID)
	assert.Equal(t, actorID, ev.ActorUserID)
}

func TestRescoreIntent_InvalidateFails_ReturnsErrorAndSkipsRest(t *testing.T) {
	tenant := shared.NewTenantID()
	intentID := uuid.New()

	appRepo := newInvalidateTrackingRepo()
	appRepo.invalidateErr = errors.New("db connection error")
	dispatcher := &stubScoreIntentDispatcher{}
	audit := &fakeAuditWriter{}

	h := commands.NewRescoreIntentHandler(appRepo, dispatcher, audit)

	err := h.Handle(context.Background(), commands.RescoreIntentInput{
		TenantID: tenant,
		IntentID: intentID,
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "db connection error")

	// ScoreIntent and audit must NOT have been called.
	assert.Empty(t, dispatcher.calls)
	assert.Empty(t, audit.events)
}

func TestRescoreIntent_ScoreFails_ReturnsErrorAndSkipsAudit(t *testing.T) {
	tenant := shared.NewTenantID()
	intentID := uuid.New()

	appRepo := newInvalidateTrackingRepo()
	dispatcher := &stubScoreIntentDispatcher{retErr: errors.New("score_intent: intent not confirmed")}
	audit := &fakeAuditWriter{}

	h := commands.NewRescoreIntentHandler(appRepo, dispatcher, audit)

	err := h.Handle(context.Background(), commands.RescoreIntentInput{
		TenantID: tenant,
		IntentID: intentID,
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "score_intent")

	// Invalidate was called; audit was not.
	assert.Equal(t, 1, appRepo.invalidateCalls)
	assert.Empty(t, audit.events)
}

func TestRescoreIntent_AuditFails_PropagatesError(t *testing.T) {
	tenant := shared.NewTenantID()
	intentID := uuid.New()

	appRepo := newInvalidateTrackingRepo()
	dispatcher := &stubScoreIntentDispatcher{}
	auditErr := errors.New("audit db down")
	audit := &fakeAuditWriter{writeErr: auditErr}

	h := commands.NewRescoreIntentHandler(appRepo, dispatcher, audit)

	err := h.Handle(context.Background(), commands.RescoreIntentInput{
		TenantID: tenant,
		IntentID: intentID,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, auditErr))

	// Both invalidate and scoreIntent were still called.
	assert.Equal(t, 1, appRepo.invalidateCalls)
	assert.Len(t, dispatcher.calls, 1)
}
