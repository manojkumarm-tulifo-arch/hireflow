// Package events defines the domain events emitted by the interview context.
package events

import (
	"time"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// Event is the minimum interface every interview event satisfies, matching
// the shape consumed by the outbox dispatcher.
type Event interface {
	EventName() string
	AggregateID() uuid.UUID
	Tenant() shared.TenantID
	At() time.Time
}

// InterviewProcessCreated is emitted after a new InterviewProcess is created
// in response to ApplicationShortlisted.
type InterviewProcessCreated struct {
	ProcessID     uuid.UUID       `json:"process_id"`
	TenantID      shared.TenantID `json:"tenant_id"`
	ApplicationID uuid.UUID       `json:"application_id"`
	CandidateID   uuid.UUID       `json:"candidate_id"`
	IntentID      uuid.UUID       `json:"intent_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
}

func (e InterviewProcessCreated) EventName() string       { return "interview.InterviewProcessCreated" }
func (e InterviewProcessCreated) AggregateID() uuid.UUID  { return e.ProcessID }
func (e InterviewProcessCreated) Tenant() shared.TenantID { return e.TenantID }
func (e InterviewProcessCreated) At() time.Time           { return e.OccurredAt }

// InterviewQuestionsGenerated is emitted after a round's questions are
// successfully generated.
type InterviewQuestionsGenerated struct {
	RoundID       uuid.UUID       `json:"round_id"`
	ProcessID     uuid.UUID       `json:"process_id"`
	Kind          string          `json:"kind"`
	QuestionCount int             `json:"question_count"`
	TenantID      shared.TenantID `json:"tenant_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
}

func (e InterviewQuestionsGenerated) EventName() string       { return "interview.InterviewQuestionsGenerated" }
func (e InterviewQuestionsGenerated) AggregateID() uuid.UUID  { return e.RoundID }
func (e InterviewQuestionsGenerated) Tenant() shared.TenantID { return e.TenantID }
func (e InterviewQuestionsGenerated) At() time.Time           { return e.OccurredAt }

// InterviewFeedbackRecorded is emitted after a feedback row is persisted.
type InterviewFeedbackRecorded struct {
	FeedbackID uuid.UUID       `json:"feedback_id"`
	RoundID    uuid.UUID       `json:"round_id"`
	Decision   string          `json:"decision"`
	TenantID   shared.TenantID `json:"tenant_id"`
	OccurredAt time.Time       `json:"occurred_at"`
}

func (e InterviewFeedbackRecorded) EventName() string       { return "interview.InterviewFeedbackRecorded" }
func (e InterviewFeedbackRecorded) AggregateID() uuid.UUID  { return e.FeedbackID }
func (e InterviewFeedbackRecorded) Tenant() shared.TenantID { return e.TenantID }
func (e InterviewFeedbackRecorded) At() time.Time           { return e.OccurredAt }
