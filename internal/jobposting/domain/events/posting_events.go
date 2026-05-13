// Package events defines domain events emitted by the jobposting context.
// Events are immutable transport records — fields are exported for clean JSON.
package events

import (
	"time"

	"github.com/hustle/hireflow/internal/jobposting/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// Event is implemented by every domain event emitted by jobposting.
type Event interface {
	EventName() string
	AggregateID() valueobjects.PostingID
	Tenant() shared.TenantID
	At() time.Time
}

// JobPostingCreated is raised when a posting is created from a confirmed intent.
type JobPostingCreated struct {
	PostingID  valueobjects.PostingID `json:"posting_id"`
	TenantID   shared.TenantID        `json:"tenant_id"`
	IntentID   string                 `json:"intent_id"`
	Title      string                 `json:"title"`
	OccurredAt time.Time              `json:"occurred_at"`
}

// NewJobPostingCreated constructs the event.
func NewJobPostingCreated(id valueobjects.PostingID, tenant shared.TenantID, intentID, title string, at time.Time) JobPostingCreated {
	return JobPostingCreated{PostingID: id, TenantID: tenant, IntentID: intentID, Title: title, OccurredAt: at}
}

func (e JobPostingCreated) EventName() string                  { return "jobposting.JobPostingCreated" }
func (e JobPostingCreated) AggregateID() valueobjects.PostingID { return e.PostingID }
func (e JobPostingCreated) Tenant() shared.TenantID            { return e.TenantID }
func (e JobPostingCreated) At() time.Time                      { return e.OccurredAt }

// JobPostingPublished is raised when a posting is distributed to one or more sources.
// The sourcing context can subscribe to start ingestion for the picked channels.
type JobPostingPublished struct {
	PostingID  valueobjects.PostingID    `json:"posting_id"`
	TenantID   shared.TenantID           `json:"tenant_id"`
	Channels   []valueobjects.SourceChannel `json:"channels"`
	Version    int                       `json:"version"`
	OccurredAt time.Time                 `json:"occurred_at"`
}

// NewJobPostingPublished constructs the event.
func NewJobPostingPublished(id valueobjects.PostingID, tenant shared.TenantID, channels []valueobjects.SourceChannel, version int, at time.Time) JobPostingPublished {
	return JobPostingPublished{PostingID: id, TenantID: tenant, Channels: append([]valueobjects.SourceChannel(nil), channels...), Version: version, OccurredAt: at}
}

func (e JobPostingPublished) EventName() string                  { return "jobposting.JobPostingPublished" }
func (e JobPostingPublished) AggregateID() valueobjects.PostingID { return e.PostingID }
func (e JobPostingPublished) Tenant() shared.TenantID            { return e.TenantID }
func (e JobPostingPublished) At() time.Time                      { return e.OccurredAt }

// JobPostingAmended is raised after a successful AmendJD on a Draft or
// Published posting. Carries the new JD version so subscribers
// (sourcing republish, audit log, recruiter notifications) can decide
// whether to act without rehydrating the aggregate.
type JobPostingAmended struct {
	PostingID  valueobjects.PostingID `json:"posting_id"`
	TenantID   shared.TenantID        `json:"tenant_id"`
	Version    int                    `json:"version"`
	OccurredAt time.Time              `json:"occurred_at"`
}

// NewJobPostingAmended constructs the event.
func NewJobPostingAmended(id valueobjects.PostingID, tenant shared.TenantID, version int, at time.Time) JobPostingAmended {
	return JobPostingAmended{PostingID: id, TenantID: tenant, Version: version, OccurredAt: at}
}

func (e JobPostingAmended) EventName() string                  { return "jobposting.JobPostingAmended" }
func (e JobPostingAmended) AggregateID() valueobjects.PostingID { return e.PostingID }
func (e JobPostingAmended) Tenant() shared.TenantID            { return e.TenantID }
func (e JobPostingAmended) At() time.Time                      { return e.OccurredAt }

// JobPostingClosed is raised when a posting is taken down (filled / cancelled).
type JobPostingClosed struct {
	PostingID  valueobjects.PostingID `json:"posting_id"`
	TenantID   shared.TenantID        `json:"tenant_id"`
	Reason     string                 `json:"reason"`
	OccurredAt time.Time              `json:"occurred_at"`
}

// NewJobPostingClosed constructs the event.
func NewJobPostingClosed(id valueobjects.PostingID, tenant shared.TenantID, reason string, at time.Time) JobPostingClosed {
	return JobPostingClosed{PostingID: id, TenantID: tenant, Reason: reason, OccurredAt: at}
}

func (e JobPostingClosed) EventName() string                  { return "jobposting.JobPostingClosed" }
func (e JobPostingClosed) AggregateID() valueobjects.PostingID { return e.PostingID }
func (e JobPostingClosed) Tenant() shared.TenantID            { return e.TenantID }
func (e JobPostingClosed) At() time.Time                      { return e.OccurredAt }
