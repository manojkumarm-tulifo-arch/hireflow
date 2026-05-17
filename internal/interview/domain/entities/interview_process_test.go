package entities_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/events"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// helpers

func mustParseTenantIP(t *testing.T) shared.TenantID {
	t.Helper()
	tid, err := shared.ParseTenantID(uuid.New().String())
	if err != nil {
		t.Fatalf("parse tenant: %v", err)
	}
	return tid
}

func threeRounds() []entities.TemplateRound {
	return []entities.TemplateRound{
		{Kind: vo.RoundKindScreen, Sequence: 1},
		{Kind: vo.RoundKindTechnical, Sequence: 2},
		{Kind: vo.RoundKindBehavioral, Sequence: 3},
	}
}

func validQuestion() vo.Question {
	return vo.Question{
		Prompt:          "Describe a challenging project.",
		SkillProbed:     "communication",
		Why:             "Tests clarity of thought.",
		ExpectedSignals: []string{"signal1", "signal2", "signal3"},
		ModelAnswer:     "A great model answer.",
		RedFlags:        []string{"flag1", "flag2"},
		FollowUps:       []string{"follow up question"},
	}
}

func newProcessInput(t *testing.T) entities.NewInterviewProcessInput {
	t.Helper()
	return entities.NewInterviewProcessInput{
		TenantID:      mustParseTenantIP(t),
		ApplicationID: uuid.New(),
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
		Rounds:        threeRounds(),
		Now:           fixedNow(time.Now().UTC()),
	}
}

// Test 1: Constructor happy path with 3 rounds; emitted event has expected fields.
func TestNewInterviewProcess_HappyPath(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	appID := uuid.New()
	candID := uuid.New()
	intentID := uuid.New()
	tenantID := mustParseTenantIP(t)
	fixedID := uuid.New()

	p, err := entities.NewInterviewProcess(entities.NewInterviewProcessInput{
		TenantID:      tenantID,
		ApplicationID: appID,
		CandidateID:   candID,
		IntentID:      intentID,
		Rounds:        threeRounds(),
		Now:           fixedNow(now),
		ID:            fixedID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.ID() != fixedID {
		t.Errorf("ID: got %v, want %v", p.ID(), fixedID)
	}
	if !p.TenantID().Equals(tenantID) {
		t.Errorf("TenantID mismatch")
	}
	if p.ApplicationID() != appID {
		t.Errorf("ApplicationID: got %v, want %v", p.ApplicationID(), appID)
	}
	if p.CandidateID() != candID {
		t.Errorf("CandidateID: got %v, want %v", p.CandidateID(), candID)
	}
	if p.IntentID() != intentID {
		t.Errorf("IntentID: got %v, want %v", p.IntentID(), intentID)
	}
	if p.Status() != vo.ProcessStatusNew {
		t.Errorf("Status: got %v, want New", p.Status())
	}
	if len(p.Rounds()) != 3 {
		t.Errorf("Rounds len: got %d, want 3", len(p.Rounds()))
	}
	for _, r := range p.Rounds() {
		if r.Status() != vo.RoundStatusPending {
			t.Errorf("round %d status: got %v, want Pending", r.Sequence(), r.Status())
		}
	}
	if !p.CreatedAt().Equal(now) {
		t.Errorf("CreatedAt: got %v, want %v", p.CreatedAt(), now)
	}

	evts := p.PullEvents()
	if len(evts) != 1 {
		t.Fatalf("PullEvents len: got %d, want 1", len(evts))
	}
	created, ok := evts[0].(events.InterviewProcessCreated)
	if !ok {
		t.Fatalf("event type: got %T, want InterviewProcessCreated", evts[0])
	}
	if created.ProcessID != fixedID {
		t.Errorf("event ProcessID: got %v, want %v", created.ProcessID, fixedID)
	}
	if !created.TenantID.Equals(tenantID) {
		t.Errorf("event TenantID mismatch")
	}
	if created.ApplicationID != appID {
		t.Errorf("event ApplicationID: got %v, want %v", created.ApplicationID, appID)
	}
	if created.CandidateID != candID {
		t.Errorf("event CandidateID: got %v, want %v", created.CandidateID, candID)
	}
	if created.IntentID != intentID {
		t.Errorf("event IntentID: got %v, want %v", created.IntentID, intentID)
	}
	if !created.OccurredAt.Equal(now) {
		t.Errorf("event OccurredAt: got %v, want %v", created.OccurredAt, now)
	}
}

// Test 2: Missing tenant → error.
func TestNewInterviewProcess_MissingTenant(t *testing.T) {
	in := newProcessInput(t)
	in.TenantID = shared.TenantID{} // zero value
	_, err := entities.NewInterviewProcess(in)
	if err == nil {
		t.Fatal("expected error for missing tenant, got nil")
	}
}

// Test 3: Missing application_id → error.
func TestNewInterviewProcess_MissingApplicationID(t *testing.T) {
	in := newProcessInput(t)
	in.ApplicationID = uuid.Nil
	_, err := entities.NewInterviewProcess(in)
	if err == nil {
		t.Fatal("expected error for missing application_id, got nil")
	}
}

// Test 4: Missing candidate_id → error.
func TestNewInterviewProcess_MissingCandidateID(t *testing.T) {
	in := newProcessInput(t)
	in.CandidateID = uuid.Nil
	_, err := entities.NewInterviewProcess(in)
	if err == nil {
		t.Fatal("expected error for missing candidate_id, got nil")
	}
}

// Test 5: Missing intent_id → error.
func TestNewInterviewProcess_MissingIntentID(t *testing.T) {
	in := newProcessInput(t)
	in.IntentID = uuid.Nil
	_, err := entities.NewInterviewProcess(in)
	if err == nil {
		t.Fatal("expected error for missing intent_id, got nil")
	}
}

// Test 6: Empty rounds → error.
func TestNewInterviewProcess_EmptyRounds(t *testing.T) {
	in := newProcessInput(t)
	in.Rounds = nil
	_, err := entities.NewInterviewProcess(in)
	if err == nil {
		t.Fatal("expected error for empty rounds, got nil")
	}
}

// Test 7: Non-contiguous sequences → error.
func TestNewInterviewProcess_NonContiguousSequences(t *testing.T) {
	in := newProcessInput(t)
	in.Rounds = []entities.TemplateRound{
		{Kind: vo.RoundKindScreen, Sequence: 1},
		{Kind: vo.RoundKindTechnical, Sequence: 3}, // gap: missing 2
	}
	_, err := entities.NewInterviewProcess(in)
	if err == nil {
		t.Fatal("expected error for non-contiguous sequences, got nil")
	}
}

// Test 8: Duplicate sequence → error.
func TestNewInterviewProcess_DuplicateSequence(t *testing.T) {
	in := newProcessInput(t)
	in.Rounds = []entities.TemplateRound{
		{Kind: vo.RoundKindScreen, Sequence: 1},
		{Kind: vo.RoundKindTechnical, Sequence: 1}, // duplicate
	}
	_, err := entities.NewInterviewProcess(in)
	if err == nil {
		t.Fatal("expected error for duplicate sequence, got nil")
	}
}

// Test 9: MarkRoundQuestionsReady from Pending with valid questions → state change + event.
func TestMarkRoundQuestionsReady_FromPending(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents() // drain constructor event

	round := p.Rounds()[0]
	roundID := round.ID()
	qs := []vo.Question{validQuestion()}

	if err := p.MarkRoundQuestionsReady(roundID, qs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the round again after state change
	var updated *entities.InterviewRound
	for _, r := range p.Rounds() {
		if r.ID() == roundID {
			updated = r
		}
	}
	if updated == nil {
		t.Fatal("round not found after update")
	}
	if updated.Status() != vo.RoundStatusQuestionsReady {
		t.Errorf("status: got %v, want QuestionsReady", updated.Status())
	}
	if len(updated.Questions()) != 1 {
		t.Errorf("questions len: got %d, want 1", len(updated.Questions()))
	}

	evts := p.PullEvents()
	if len(evts) != 1 {
		t.Fatalf("events len: got %d, want 1", len(evts))
	}
	gen, ok := evts[0].(events.InterviewQuestionsGenerated)
	if !ok {
		t.Fatalf("event type: got %T, want InterviewQuestionsGenerated", evts[0])
	}
	if gen.RoundID != roundID {
		t.Errorf("event RoundID: got %v, want %v", gen.RoundID, roundID)
	}
	if gen.QuestionCount != 1 {
		t.Errorf("event QuestionCount: got %d, want 1", gen.QuestionCount)
	}
	if gen.Kind != string(vo.RoundKindScreen) {
		t.Errorf("event Kind: got %v, want screen", gen.Kind)
	}
}

// Test 10: MarkRoundQuestionsReady from Completed → ErrInvalidTransition.
func TestMarkRoundQuestionsReady_FromCompleted(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	round := p.Rounds()[0]
	roundID := round.ID()

	// Transition to QuestionsReady first
	_ = p.MarkRoundQuestionsReady(roundID, []vo.Question{validQuestion()})
	p.PullEvents()
	// Then to Completed
	_ = p.MarkRoundCompleted(roundID)

	err := p.MarkRoundQuestionsReady(roundID, []vo.Question{validQuestion()})
	if !errors.Is(err, entities.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

// Test 11: MarkRoundQuestionsReady with empty questions → error.
func TestMarkRoundQuestionsReady_EmptyQuestions(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	round := p.Rounds()[0]
	err := p.MarkRoundQuestionsReady(round.ID(), nil)
	if err == nil {
		t.Fatal("expected error for empty questions, got nil")
	}
}

// Test 12: MarkRoundQuestionsReady with one invalid question → error from Validate.
func TestMarkRoundQuestionsReady_InvalidQuestion(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	round := p.Rounds()[0]
	invalid := vo.Question{Prompt: "something"} // missing all other required fields
	err := p.MarkRoundQuestionsReady(round.ID(), []vo.Question{invalid})
	if err == nil {
		t.Fatal("expected error for invalid question, got nil")
	}
}

// Test 13: MarkRoundGenerationFailed from Pending → state change.
func TestMarkRoundGenerationFailed_FromPending(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	round := p.Rounds()[0]
	roundID := round.ID()

	if err := p.MarkRoundGenerationFailed(roundID, "LLM timeout"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range p.Rounds() {
		if r.ID() == roundID {
			if r.Status() != vo.RoundStatusGenerationFailed {
				t.Errorf("status: got %v, want GenerationFailed", r.Status())
			}
			if r.LastError() != "LLM timeout" {
				t.Errorf("lastError: got %v, want 'LLM timeout'", r.LastError())
			}
		}
	}
}

// Test 14: MarkRoundGenerationFailed from QuestionsReady → ErrInvalidTransition.
func TestMarkRoundGenerationFailed_FromQuestionsReady(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	round := p.Rounds()[0]
	roundID := round.ID()

	_ = p.MarkRoundQuestionsReady(roundID, []vo.Question{validQuestion()})
	p.PullEvents()

	err := p.MarkRoundGenerationFailed(roundID, "should fail")
	if !errors.Is(err, entities.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

// Test 15: RecordGenerationAttempt increments count + sets last_error + next_attempt_at.
func TestRecordGenerationAttempt(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	round := p.Rounds()[0]
	roundID := round.ID()
	nextAttempt := time.Now().Add(30 * time.Second).UTC()

	if err := p.RecordGenerationAttempt(roundID, "rate limit", nextAttempt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range p.Rounds() {
		if r.ID() == roundID {
			if r.AttemptCount() != 1 {
				t.Errorf("attemptCount: got %d, want 1", r.AttemptCount())
			}
			if r.LastError() != "rate limit" {
				t.Errorf("lastError: got %v, want 'rate limit'", r.LastError())
			}
			if !r.NextAttemptAt().Equal(nextAttempt) {
				t.Errorf("nextAttemptAt: got %v, want %v", r.NextAttemptAt(), nextAttempt)
			}
		}
	}

	// Second attempt increments again
	if err := p.RecordGenerationAttempt(roundID, "timeout", nextAttempt.Add(time.Minute)); err != nil {
		t.Fatalf("unexpected error on second attempt: %v", err)
	}
	for _, r := range p.Rounds() {
		if r.ID() == roundID {
			if r.AttemptCount() != 2 {
				t.Errorf("attemptCount after 2nd: got %d, want 2", r.AttemptCount())
			}
		}
	}
}

// Test 16: ResetRoundForRegeneration from QuestionsReady → Pending with cleared fields + cleared questions.
func TestResetRoundForRegeneration_FromQuestionsReady(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	round := p.Rounds()[0]
	roundID := round.ID()

	_ = p.MarkRoundQuestionsReady(roundID, []vo.Question{validQuestion()})
	p.PullEvents()

	// Verify questions are set before reset
	for _, r := range p.Rounds() {
		if r.ID() == roundID && len(r.Questions()) == 0 {
			t.Error("expected questions to be set before reset")
		}
	}

	if err := p.ResetRoundForRegeneration(roundID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range p.Rounds() {
		if r.ID() == roundID {
			if r.Status() != vo.RoundStatusPending {
				t.Errorf("status: got %v, want Pending", r.Status())
			}
			if r.AttemptCount() != 0 {
				t.Errorf("attemptCount: got %d, want 0", r.AttemptCount())
			}
			if r.LastError() != "" {
				t.Errorf("lastError: got %q, want empty", r.LastError())
			}
			if len(r.Questions()) != 0 {
				t.Errorf("questions: got %d, want 0", len(r.Questions()))
			}
		}
	}
}

// Test 17: ResetRoundForRegeneration from GenerationFailed → Pending.
func TestResetRoundForRegeneration_FromGenerationFailed(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	round := p.Rounds()[0]
	roundID := round.ID()

	_ = p.MarkRoundGenerationFailed(roundID, "out of tokens")

	if err := p.ResetRoundForRegeneration(roundID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range p.Rounds() {
		if r.ID() == roundID {
			if r.Status() != vo.RoundStatusPending {
				t.Errorf("status: got %v, want Pending", r.Status())
			}
		}
	}
}

// Test 18: ResetRoundForRegeneration from Completed → ErrInvalidTransition.
func TestResetRoundForRegeneration_FromCompleted(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	round := p.Rounds()[0]
	roundID := round.ID()

	_ = p.MarkRoundQuestionsReady(roundID, []vo.Question{validQuestion()})
	p.PullEvents()
	_ = p.MarkRoundCompleted(roundID)

	err := p.ResetRoundForRegeneration(roundID)
	if !errors.Is(err, entities.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

// Test 19: MarkRoundCompleted from QuestionsReady → Completed.
func TestMarkRoundCompleted_FromQuestionsReady(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	round := p.Rounds()[0]
	roundID := round.ID()

	_ = p.MarkRoundQuestionsReady(roundID, []vo.Question{validQuestion()})
	p.PullEvents()

	if err := p.MarkRoundCompleted(roundID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range p.Rounds() {
		if r.ID() == roundID {
			if r.Status() != vo.RoundStatusCompleted {
				t.Errorf("status: got %v, want Completed", r.Status())
			}
		}
	}
}

// Test 20: MarkRoundCompleted from Pending → ErrInvalidTransition.
func TestMarkRoundCompleted_FromPending(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	round := p.Rounds()[0]

	err := p.MarkRoundCompleted(round.ID())
	if !errors.Is(err, entities.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

// Test 21: MarkRoundSkipped from Pending → Skipped.
func TestMarkRoundSkipped_FromPending(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	round := p.Rounds()[0]
	roundID := round.ID()

	if err := p.MarkRoundSkipped(roundID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range p.Rounds() {
		if r.ID() == roundID {
			if r.Status() != vo.RoundStatusSkipped {
				t.Errorf("status: got %v, want Skipped", r.Status())
			}
		}
	}
}

// Test 21b: MarkRoundSkipped from QuestionsReady → Skipped.
func TestMarkRoundSkipped_FromQuestionsReady(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	round := p.Rounds()[0]
	roundID := round.ID()

	_ = p.MarkRoundQuestionsReady(roundID, []vo.Question{validQuestion()})
	p.PullEvents()

	if err := p.MarkRoundSkipped(roundID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range p.Rounds() {
		if r.ID() == roundID {
			if r.Status() != vo.RoundStatusSkipped {
				t.Errorf("status: got %v, want Skipped", r.Status())
			}
		}
	}
}

// Test 21c: MarkRoundSkipped from GenerationFailed → Skipped.
func TestMarkRoundSkipped_FromGenerationFailed(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	round := p.Rounds()[0]
	roundID := round.ID()

	_ = p.MarkRoundGenerationFailed(roundID, "exhausted retries")

	if err := p.MarkRoundSkipped(roundID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range p.Rounds() {
		if r.ID() == roundID {
			if r.Status() != vo.RoundStatusSkipped {
				t.Errorf("status: got %v, want Skipped", r.Status())
			}
		}
	}
}

// Test 22: MarkRoundSkipped from Completed → ErrInvalidTransition.
func TestMarkRoundSkipped_FromCompleted(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	round := p.Rounds()[0]
	roundID := round.ID()

	_ = p.MarkRoundQuestionsReady(roundID, []vo.Question{validQuestion()})
	p.PullEvents()
	_ = p.MarkRoundCompleted(roundID)

	err := p.MarkRoundSkipped(roundID)
	if !errors.Is(err, entities.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

// Test 23: Complete with all rounds terminal → Completed.
func TestComplete_AllTerminal(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	rounds := p.Rounds()
	// Make rounds[0] and rounds[1] Completed, rounds[2] Skipped
	_ = p.MarkRoundQuestionsReady(rounds[0].ID(), []vo.Question{validQuestion()})
	p.PullEvents()
	_ = p.MarkRoundCompleted(rounds[0].ID())
	_ = p.MarkRoundQuestionsReady(rounds[1].ID(), []vo.Question{validQuestion()})
	p.PullEvents()
	_ = p.MarkRoundCompleted(rounds[1].ID())
	_ = p.MarkRoundSkipped(rounds[2].ID())

	if err := p.Complete(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Status() != vo.ProcessStatusCompleted {
		t.Errorf("status: got %v, want Completed", p.Status())
	}
}

// Test 24: Complete with one Pending round → error.
func TestComplete_WithPendingRound(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	rounds := p.Rounds()
	// Only complete one round, leave others pending
	_ = p.MarkRoundQuestionsReady(rounds[0].ID(), []vo.Question{validQuestion()})
	p.PullEvents()
	_ = p.MarkRoundCompleted(rounds[0].ID())

	err := p.Complete()
	if err == nil {
		t.Fatal("expected error with pending round, got nil")
	}
	if errors.Is(err, entities.ErrInvalidTransition) {
		t.Error("should not be ErrInvalidTransition; should be 'cannot complete' error")
	}
}

// Test 25: Complete with one GenerationFailed round → error.
func TestComplete_WithGenerationFailedRound(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	rounds := p.Rounds()
	_ = p.MarkRoundQuestionsReady(rounds[0].ID(), []vo.Question{validQuestion()})
	p.PullEvents()
	_ = p.MarkRoundCompleted(rounds[0].ID())
	_ = p.MarkRoundGenerationFailed(rounds[1].ID(), "exhausted")
	_ = p.MarkRoundSkipped(rounds[2].ID())

	err := p.Complete()
	if err == nil {
		t.Fatal("expected error with GenerationFailed round, got nil")
	}
}

// Test 26: Complete on already-Completed process → ErrInvalidTransition.
func TestComplete_AlreadyCompleted(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	rounds := p.Rounds()
	_ = p.MarkRoundQuestionsReady(rounds[0].ID(), []vo.Question{validQuestion()})
	p.PullEvents()
	_ = p.MarkRoundCompleted(rounds[0].ID())
	_ = p.MarkRoundQuestionsReady(rounds[1].ID(), []vo.Question{validQuestion()})
	p.PullEvents()
	_ = p.MarkRoundCompleted(rounds[1].ID())
	_ = p.MarkRoundSkipped(rounds[2].ID())
	_ = p.Complete()

	err := p.Complete()
	if !errors.Is(err, entities.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

// Test 27: Cancel from New → Cancelled.
func TestCancel_FromNew(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	if err := p.Cancel(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Status() != vo.ProcessStatusCancelled {
		t.Errorf("status: got %v, want Cancelled", p.Status())
	}
}

// Test 28: Cancel on already-Cancelled → ErrInvalidTransition.
func TestCancel_AlreadyCancelled(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	_ = p.Cancel()

	err := p.Cancel()
	if !errors.Is(err, entities.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

// Test 29: findRound with unknown id → ErrRoundNotFound (via any method that calls it).
func TestFindRound_UnknownID(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))
	p.PullEvents()

	err := p.MarkRoundCompleted(uuid.New()) // unknown round id
	if !errors.Is(err, entities.ErrRoundNotFound) {
		t.Errorf("expected ErrRoundNotFound, got %v", err)
	}
}

// Test 30: PullEvents drains the slice (subsequent calls return nil).
func TestPullEvents_Drains(t *testing.T) {
	p, _ := entities.NewInterviewProcess(newProcessInput(t))

	first := p.PullEvents()
	if len(first) == 0 {
		t.Fatal("expected events on first PullEvents, got none")
	}

	second := p.PullEvents()
	if len(second) != 0 {
		t.Errorf("expected nil/empty on second PullEvents, got %d events", len(second))
	}
}
