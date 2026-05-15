package commands_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/application/commands"
	"github.com/hustle/hireflow/internal/interview/domain/events"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ---------------------------------------------------------------------------
// In-memory fakes for record_feedback tests
// ---------------------------------------------------------------------------

// fakeFeedbackRepo is an in-memory FeedbackRepository.
type fakeFeedbackRepo struct {
	rows      []repositories.FeedbackRow
	appendErr error // if set, Append returns this error
}

func (r *fakeFeedbackRepo) Append(_ context.Context, row repositories.FeedbackRow) error {
	if r.appendErr != nil {
		return r.appendErr
	}
	if err := row.Feedback.Validate(); err != nil {
		return err
	}
	r.rows = append(r.rows, row)
	return nil
}

func (r *fakeFeedbackRepo) ListByRound(_ context.Context, _ shared.TenantID, _ uuid.UUID) ([]repositories.FeedbackRow, error) {
	return r.rows, nil
}

// fakeOutboxAppender captures emitted events.
type fakeOutboxAppender struct {
	events []events.Event
	err    error // if set, Append returns this error
}

func (a *fakeOutboxAppender) Append(_ context.Context, ev events.Event) error {
	if a.err != nil {
		return a.err
	}
	a.events = append(a.events, ev)
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// validFeedback returns a Feedback value that passes Validate().
func validFeedback(actorID uuid.UUID) vo.Feedback {
	return vo.Feedback{
		InterviewerName: "Alice Interviewer",
		Decision:        vo.FeedbackDecisionStrongYes,
		Notes:           "Great candidate.",
		SubmittedBy:     actorID,
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRecordFeedback_HappyPath_AppendsRowAuditAndEvent(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	actorID := uuid.New()

	p, roundID := seedProcess(t, processes, tenantID)
	advanceRoundToStatus(t, processes, p, roundID, vo.RoundStatusQuestionsReady)

	feedback := &fakeFeedbackRepo{}
	outbox := &fakeOutboxAppender{}
	audit := &captureAuditWriter{}

	h := commands.NewRecordFeedbackHandler(feedback, processes, audit, outbox)
	in := commands.RecordFeedbackInput{
		TenantID:    tenantID,
		ActorUserID: actorID,
		RoundID:     roundID,
		Feedback:    validFeedback(actorID),
	}

	if err := h.Handle(context.Background(), in); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	// Feedback row appended exactly once.
	if len(feedback.rows) != 1 {
		t.Errorf("feedback rows: want 1, got %d", len(feedback.rows))
	}

	// Outbox event emitted exactly once.
	if len(outbox.events) != 1 {
		t.Fatalf("outbox events: want 1, got %d", len(outbox.events))
	}
	ev, ok := outbox.events[0].(events.InterviewFeedbackRecorded)
	if !ok {
		t.Fatalf("outbox event type: want InterviewFeedbackRecorded, got %T", outbox.events[0])
	}
	if ev.RoundID != roundID {
		t.Errorf("event RoundID: want %v, got %v", roundID, ev.RoundID)
	}
	if ev.Decision != string(vo.FeedbackDecisionStrongYes) {
		t.Errorf("event Decision: want %q, got %q", vo.FeedbackDecisionStrongYes, ev.Decision)
	}

	// Audit written exactly once.
	if len(audit.events) != 1 {
		t.Fatalf("audit events: want 1, got %d", len(audit.events))
	}
	ae := audit.events[0]
	if ae.Action != "interview_round_feedback_recorded" {
		t.Errorf("audit action: want %q, got %q", "interview_round_feedback_recorded", ae.Action)
	}
	if ae.ResourceID != roundID {
		t.Errorf("audit resource_id: want %v, got %v", roundID, ae.ResourceID)
	}
}

func TestRecordFeedback_RoundNotFound_ReturnsErr(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	// No process seeded — FindByRoundID returns ErrProcessNotFound.

	feedback := &fakeFeedbackRepo{}
	outbox := &fakeOutboxAppender{}
	audit := &captureAuditWriter{}

	h := commands.NewRecordFeedbackHandler(feedback, processes, audit, outbox)
	in := commands.RecordFeedbackInput{
		TenantID:    tenantID,
		ActorUserID: uuid.New(),
		RoundID:     uuid.New(),
		Feedback:    validFeedback(uuid.New()),
	}

	err := h.Handle(context.Background(), in)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, repositories.ErrProcessNotFound) {
		t.Errorf("expected ErrProcessNotFound, got: %v", err)
	}
}

func TestRecordFeedback_RoundNotInQuestionsReady_ReturnsErr(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	actorID := uuid.New()

	p, roundID := seedProcess(t, processes, tenantID)
	// Advance to Completed (via QuestionsReady).
	advanceRoundToStatus(t, processes, p, roundID, vo.RoundStatusCompleted)

	feedback := &fakeFeedbackRepo{}
	outbox := &fakeOutboxAppender{}
	audit := &captureAuditWriter{}

	h := commands.NewRecordFeedbackHandler(feedback, processes, audit, outbox)
	in := commands.RecordFeedbackInput{
		TenantID:    tenantID,
		ActorUserID: actorID,
		RoundID:     roundID,
		Feedback:    validFeedback(actorID),
	}

	err := h.Handle(context.Background(), in)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "QuestionsReady") {
		t.Errorf("expected 'must be QuestionsReady' error, got: %v", err)
	}
}

func TestRecordFeedback_InvalidFeedback_RejectedByRepoValidate(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	actorID := uuid.New()

	p, roundID := seedProcess(t, processes, tenantID)
	advanceRoundToStatus(t, processes, p, roundID, vo.RoundStatusQuestionsReady)

	feedback := &fakeFeedbackRepo{}
	outbox := &fakeOutboxAppender{}
	audit := &captureAuditWriter{}

	h := commands.NewRecordFeedbackHandler(feedback, processes, audit, outbox)

	// Empty InterviewerName — fails Validate().
	badFeedback := vo.Feedback{
		InterviewerName: "",
		Decision:        vo.FeedbackDecisionYes,
		SubmittedBy:     actorID,
	}
	in := commands.RecordFeedbackInput{
		TenantID:    tenantID,
		ActorUserID: actorID,
		RoundID:     roundID,
		Feedback:    badFeedback,
	}

	err := h.Handle(context.Background(), in)
	if err == nil {
		t.Fatal("expected error for invalid feedback, got nil")
	}
}

func TestRecordFeedback_AuditFailurePropagates(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	actorID := uuid.New()

	p, roundID := seedProcess(t, processes, tenantID)
	advanceRoundToStatus(t, processes, p, roundID, vo.RoundStatusQuestionsReady)

	feedback := &fakeFeedbackRepo{}
	outbox := &fakeOutboxAppender{}
	auditErr := errors.New("audit store down")
	audit := &captureAuditWriter{err: auditErr}

	h := commands.NewRecordFeedbackHandler(feedback, processes, audit, outbox)
	in := commands.RecordFeedbackInput{
		TenantID:    tenantID,
		ActorUserID: actorID,
		RoundID:     roundID,
		Feedback:    validFeedback(actorID),
	}

	err := h.Handle(context.Background(), in)
	if err == nil {
		t.Fatal("expected error from audit failure, got nil")
	}
	if !errors.Is(err, auditErr) {
		t.Errorf("expected error to wrap auditErr, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || findSubstring(s, sub))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
