package events

import (
	"time"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ApplicationScored is emitted after an Application is marked Scored.
// OverallScore is nil when the application has been embedding-scored but not
// yet judged by the LLM (i.e., it was not in the top-K for the intent).
type ApplicationScored struct {
	ApplicationID  uuid.UUID       `json:"application_id"`
	CandidateID    uuid.UUID       `json:"candidate_id"`
	IntentID       uuid.UUID       `json:"intent_id"`
	TenantID       shared.TenantID `json:"tenant_id"`
	OverallScore   *float64        `json:"overall_score,omitempty"` // nil if not yet judged
	ScoreBand      string          `json:"score_band,omitempty"`    // "strong"|"moderate"|"weak"|"" if unjudged
	EmbeddingScore float64         `json:"embedding_score"`
	OccurredAt     time.Time       `json:"occurred_at"`
}

func (e ApplicationScored) EventName() string       { return "sourcing.ApplicationScored" }
func (e ApplicationScored) AggregateID() uuid.UUID  { return e.ApplicationID }
func (e ApplicationScored) Tenant() shared.TenantID { return e.TenantID }
func (e ApplicationScored) At() time.Time           { return e.OccurredAt }

// ApplicationExcluded is emitted after an Application is excluded due to
// failing required rule criteria.
type ApplicationExcluded struct {
	ApplicationID uuid.UUID       `json:"application_id"`
	CandidateID   uuid.UUID       `json:"candidate_id"`
	IntentID      uuid.UUID       `json:"intent_id"`
	TenantID      shared.TenantID `json:"tenant_id"`
	Reason        string          `json:"reason"`
	OccurredAt    time.Time       `json:"occurred_at"`
}

func (e ApplicationExcluded) EventName() string       { return "sourcing.ApplicationExcluded" }
func (e ApplicationExcluded) AggregateID() uuid.UUID  { return e.ApplicationID }
func (e ApplicationExcluded) Tenant() shared.TenantID { return e.TenantID }
func (e ApplicationExcluded) At() time.Time           { return e.OccurredAt }

// ApplicationEmbedFailed is emitted when embedding the candidate profile fails
// and the application cannot be scored.
type ApplicationEmbedFailed struct {
	ApplicationID uuid.UUID       `json:"application_id"`
	TenantID      shared.TenantID `json:"tenant_id"`
	Reason        string          `json:"reason"`
	OccurredAt    time.Time       `json:"occurred_at"`
}

func (e ApplicationEmbedFailed) EventName() string       { return "sourcing.ApplicationEmbedFailed" }
func (e ApplicationEmbedFailed) AggregateID() uuid.UUID  { return e.ApplicationID }
func (e ApplicationEmbedFailed) Tenant() shared.TenantID { return e.TenantID }
func (e ApplicationEmbedFailed) At() time.Time           { return e.OccurredAt }

// ApplicationJudgeFailed is emitted when the LLM judge fails for an application
// that was in the top-K scoring set.
type ApplicationJudgeFailed struct {
	ApplicationID uuid.UUID       `json:"application_id"`
	TenantID      shared.TenantID `json:"tenant_id"`
	Reason        string          `json:"reason"`
	OccurredAt    time.Time       `json:"occurred_at"`
}

func (e ApplicationJudgeFailed) EventName() string       { return "sourcing.ApplicationJudgeFailed" }
func (e ApplicationJudgeFailed) AggregateID() uuid.UUID  { return e.ApplicationID }
func (e ApplicationJudgeFailed) Tenant() shared.TenantID { return e.TenantID }
func (e ApplicationJudgeFailed) At() time.Time           { return e.OccurredAt }

// ApplicationShortlisted is emitted after an Application transitions Scored → Shortlisted.
type ApplicationShortlisted struct {
	ApplicationID uuid.UUID       `json:"application_id"`
	CandidateID   uuid.UUID       `json:"candidate_id"`
	IntentID      uuid.UUID       `json:"intent_id"`
	TenantID      shared.TenantID `json:"tenant_id"`
	ActorUserID   uuid.UUID       `json:"actor_user_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
}

func (e ApplicationShortlisted) EventName() string       { return "sourcing.ApplicationShortlisted" }
func (e ApplicationShortlisted) AggregateID() uuid.UUID  { return e.ApplicationID }
func (e ApplicationShortlisted) Tenant() shared.TenantID { return e.TenantID }
func (e ApplicationShortlisted) At() time.Time           { return e.OccurredAt }

// ApplicationRejected is emitted after an Application is rejected with a reason.
type ApplicationRejected struct {
	ApplicationID uuid.UUID       `json:"application_id"`
	CandidateID   uuid.UUID       `json:"candidate_id"`
	IntentID      uuid.UUID       `json:"intent_id"`
	TenantID      shared.TenantID `json:"tenant_id"`
	ActorUserID   uuid.UUID       `json:"actor_user_id"`
	Reason        string          `json:"reason"`
	OccurredAt    time.Time       `json:"occurred_at"`
}

func (e ApplicationRejected) EventName() string       { return "sourcing.ApplicationRejected" }
func (e ApplicationRejected) AggregateID() uuid.UUID  { return e.ApplicationID }
func (e ApplicationRejected) Tenant() shared.TenantID { return e.TenantID }
func (e ApplicationRejected) At() time.Time           { return e.OccurredAt }

// ApplicationHired is emitted after an Application transitions to Hired.
type ApplicationHired struct {
	ApplicationID uuid.UUID       `json:"application_id"`
	CandidateID   uuid.UUID       `json:"candidate_id"`
	IntentID      uuid.UUID       `json:"intent_id"`
	TenantID      shared.TenantID `json:"tenant_id"`
	ActorUserID   uuid.UUID       `json:"actor_user_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
}

func (e ApplicationHired) EventName() string       { return "sourcing.ApplicationHired" }
func (e ApplicationHired) AggregateID() uuid.UUID  { return e.ApplicationID }
func (e ApplicationHired) Tenant() shared.TenantID { return e.TenantID }
func (e ApplicationHired) At() time.Time           { return e.OccurredAt }

// ApplicationMovedToInterviewing is emitted after an Application transitions
// Shortlisted → Interviewing.
type ApplicationMovedToInterviewing struct {
	ApplicationID uuid.UUID       `json:"application_id"`
	CandidateID   uuid.UUID       `json:"candidate_id"`
	IntentID      uuid.UUID       `json:"intent_id"`
	TenantID      shared.TenantID `json:"tenant_id"`
	ActorUserID   uuid.UUID       `json:"actor_user_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
}

func (e ApplicationMovedToInterviewing) EventName() string {
	return "sourcing.ApplicationMovedToInterviewing"
}
func (e ApplicationMovedToInterviewing) AggregateID() uuid.UUID  { return e.ApplicationID }
func (e ApplicationMovedToInterviewing) Tenant() shared.TenantID { return e.TenantID }
func (e ApplicationMovedToInterviewing) At() time.Time           { return e.OccurredAt }

// CandidateErased is emitted on the sourcing side when a candidate's PII is
// erased per a data erasure request.
type CandidateErased struct {
	CandidateID uuid.UUID       `json:"candidate_id"`
	TenantID    shared.TenantID `json:"tenant_id"`
	ActorUserID uuid.UUID       `json:"actor_user_id"`
	OccurredAt  time.Time       `json:"occurred_at"`
}

func (e CandidateErased) EventName() string       { return "sourcing.CandidateErased" }
func (e CandidateErased) AggregateID() uuid.UUID  { return e.CandidateID }
func (e CandidateErased) Tenant() shared.TenantID { return e.TenantID }
func (e CandidateErased) At() time.Time           { return e.OccurredAt }
