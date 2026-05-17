package events_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/domain/events"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

func mustParseTenant(t *testing.T) shared.TenantID {
	t.Helper()
	tid, err := shared.ParseTenantID(uuid.New().String())
	if err != nil {
		t.Fatalf("parse tenant: %v", err)
	}
	return tid
}

func TestInterviewProcessCreated_Interface(t *testing.T) {
	processID := uuid.New()
	appID := uuid.New()
	candID := uuid.New()
	intentID := uuid.New()
	tenantID := mustParseTenant(t)
	now := time.Now().UTC()

	e := events.InterviewProcessCreated{
		ProcessID:     processID,
		TenantID:      tenantID,
		ApplicationID: appID,
		CandidateID:   candID,
		IntentID:      intentID,
		OccurredAt:    now,
	}

	if got := e.EventName(); got != "interview.InterviewProcessCreated" {
		t.Errorf("EventName() = %q, want %q", got, "interview.InterviewProcessCreated")
	}
	if got := e.AggregateID(); got != processID {
		t.Errorf("AggregateID() = %v, want %v", got, processID)
	}
	if !e.Tenant().Equals(tenantID) {
		t.Errorf("Tenant() = %v, want %v", e.Tenant(), tenantID)
	}
	if !e.At().Equal(now) {
		t.Errorf("At() = %v, want %v", e.At(), now)
	}
}

func TestInterviewQuestionsGenerated_Interface(t *testing.T) {
	roundID := uuid.New()
	processID := uuid.New()
	tenantID := mustParseTenant(t)
	now := time.Now().UTC()

	e := events.InterviewQuestionsGenerated{
		RoundID:       roundID,
		ProcessID:     processID,
		Kind:          "technical",
		QuestionCount: 5,
		TenantID:      tenantID,
		OccurredAt:    now,
	}

	if got := e.EventName(); got != "interview.InterviewQuestionsGenerated" {
		t.Errorf("EventName() = %q, want %q", got, "interview.InterviewQuestionsGenerated")
	}
	if got := e.AggregateID(); got != roundID {
		t.Errorf("AggregateID() = %v, want %v", got, roundID)
	}
	if !e.Tenant().Equals(tenantID) {
		t.Errorf("Tenant() = %v, want %v", e.Tenant(), tenantID)
	}
	if !e.At().Equal(now) {
		t.Errorf("At() = %v, want %v", e.At(), now)
	}
}

func TestInterviewFeedbackRecorded_Interface(t *testing.T) {
	feedbackID := uuid.New()
	roundID := uuid.New()
	tenantID := mustParseTenant(t)
	now := time.Now().UTC()

	e := events.InterviewFeedbackRecorded{
		FeedbackID: feedbackID,
		RoundID:    roundID,
		Decision:   "advance",
		TenantID:   tenantID,
		OccurredAt: now,
	}

	if got := e.EventName(); got != "interview.InterviewFeedbackRecorded" {
		t.Errorf("EventName() = %q, want %q", got, "interview.InterviewFeedbackRecorded")
	}
	if got := e.AggregateID(); got != feedbackID {
		t.Errorf("AggregateID() = %v, want %v", got, feedbackID)
	}
	if !e.Tenant().Equals(tenantID) {
		t.Errorf("Tenant() = %v, want %v", e.Tenant(), tenantID)
	}
	if !e.At().Equal(now) {
		t.Errorf("At() = %v, want %v", e.At(), now)
	}
}

func TestInterviewProcessCreated_JSONRoundTrip(t *testing.T) {
	processID := uuid.New()
	appID := uuid.New()
	candID := uuid.New()
	intentID := uuid.New()
	tenantID := mustParseTenant(t)
	now := time.Now().UTC().Truncate(time.Millisecond) // JSON truncates sub-ms

	orig := events.InterviewProcessCreated{
		ProcessID:     processID,
		TenantID:      tenantID,
		ApplicationID: appID,
		CandidateID:   candID,
		IntentID:      intentID,
		OccurredAt:    now,
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got events.InterviewProcessCreated
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.ProcessID != orig.ProcessID {
		t.Errorf("ProcessID: got %v, want %v", got.ProcessID, orig.ProcessID)
	}
	if !got.TenantID.Equals(orig.TenantID) {
		t.Errorf("TenantID: got %v, want %v", got.TenantID, orig.TenantID)
	}
	if got.ApplicationID != orig.ApplicationID {
		t.Errorf("ApplicationID: got %v, want %v", got.ApplicationID, orig.ApplicationID)
	}
	if got.CandidateID != orig.CandidateID {
		t.Errorf("CandidateID: got %v, want %v", got.CandidateID, orig.CandidateID)
	}
	if got.IntentID != orig.IntentID {
		t.Errorf("IntentID: got %v, want %v", got.IntentID, orig.IntentID)
	}
	if !got.OccurredAt.Equal(orig.OccurredAt) {
		t.Errorf("OccurredAt: got %v, want %v", got.OccurredAt, orig.OccurredAt)
	}
}

func TestInterviewQuestionsGenerated_JSONRoundTrip(t *testing.T) {
	roundID := uuid.New()
	processID := uuid.New()
	tenantID := mustParseTenant(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	orig := events.InterviewQuestionsGenerated{
		RoundID:       roundID,
		ProcessID:     processID,
		Kind:          "behavioral",
		QuestionCount: 7,
		TenantID:      tenantID,
		OccurredAt:    now,
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got events.InterviewQuestionsGenerated
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.RoundID != orig.RoundID {
		t.Errorf("RoundID: got %v, want %v", got.RoundID, orig.RoundID)
	}
	if got.ProcessID != orig.ProcessID {
		t.Errorf("ProcessID: got %v, want %v", got.ProcessID, orig.ProcessID)
	}
	if got.Kind != orig.Kind {
		t.Errorf("Kind: got %v, want %v", got.Kind, orig.Kind)
	}
	if got.QuestionCount != orig.QuestionCount {
		t.Errorf("QuestionCount: got %v, want %v", got.QuestionCount, orig.QuestionCount)
	}
	if !got.TenantID.Equals(orig.TenantID) {
		t.Errorf("TenantID: got %v, want %v", got.TenantID, orig.TenantID)
	}
	if !got.OccurredAt.Equal(orig.OccurredAt) {
		t.Errorf("OccurredAt: got %v, want %v", got.OccurredAt, orig.OccurredAt)
	}
}

func TestInterviewFeedbackRecorded_JSONRoundTrip(t *testing.T) {
	feedbackID := uuid.New()
	roundID := uuid.New()
	tenantID := mustParseTenant(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	orig := events.InterviewFeedbackRecorded{
		FeedbackID: feedbackID,
		RoundID:    roundID,
		Decision:   "reject",
		TenantID:   tenantID,
		OccurredAt: now,
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got events.InterviewFeedbackRecorded
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.FeedbackID != orig.FeedbackID {
		t.Errorf("FeedbackID: got %v, want %v", got.FeedbackID, orig.FeedbackID)
	}
	if got.RoundID != orig.RoundID {
		t.Errorf("RoundID: got %v, want %v", got.RoundID, orig.RoundID)
	}
	if got.Decision != orig.Decision {
		t.Errorf("Decision: got %v, want %v", got.Decision, orig.Decision)
	}
	if !got.TenantID.Equals(orig.TenantID) {
		t.Errorf("TenantID: got %v, want %v", got.TenantID, orig.TenantID)
	}
	if !got.OccurredAt.Equal(orig.OccurredAt) {
		t.Errorf("OccurredAt: got %v, want %v", got.OccurredAt, orig.OccurredAt)
	}
}

// compile-time check: all concrete types satisfy events.Event
var (
	_ events.Event = events.InterviewProcessCreated{}
	_ events.Event = events.InterviewQuestionsGenerated{}
	_ events.Event = events.InterviewFeedbackRecorded{}
)
