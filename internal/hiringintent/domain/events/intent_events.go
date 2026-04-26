// Package events defines domain events emitted by the hiringintent context.
// Events are immutable value records intended to cross context boundaries —
// fields are exported so they marshal cleanly to JSON for the outbox.
// Consumers may live in this context or downstream (jobposting, sourcing).
package events

import (
	"time"

	"github.com/hustle/hireflow/internal/hiringintent/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// Event is implemented by every domain event emitted by hiringintent.
type Event interface {
	// EventName returns the stable type name used for messaging routing.
	EventName() string
	// AggregateID returns the aggregate id the event was raised on.
	AggregateID() valueobjects.IntentID
	// Tenant returns the owning tenant of the aggregate.
	Tenant() shared.TenantID
	// At returns the time the event was raised.
	At() time.Time
}

// IntentDrafted is raised when a HiringIntent is first constructed.
type IntentDrafted struct {
	IntentID    valueobjects.IntentID `json:"intent_id"`
	TenantID    shared.TenantID       `json:"tenant_id"`
	RecruiterID shared.RecruiterID    `json:"recruiter_id"`
	RoleTitle   string                `json:"role_title"`
	OccurredAt  time.Time             `json:"occurred_at"`
}

// NewIntentDrafted constructs the event.
func NewIntentDrafted(id valueobjects.IntentID, tenant shared.TenantID, recruiter shared.RecruiterID, title string, at time.Time) IntentDrafted {
	return IntentDrafted{IntentID: id, TenantID: tenant, RecruiterID: recruiter, RoleTitle: title, OccurredAt: at}
}

func (e IntentDrafted) EventName() string                  { return "hiringintent.IntentDrafted" }
func (e IntentDrafted) AggregateID() valueobjects.IntentID { return e.IntentID }
func (e IntentDrafted) Tenant() shared.TenantID            { return e.TenantID }
func (e IntentDrafted) At() time.Time                      { return e.OccurredAt }

// IntentRoleUpdated is raised after a successful UpdateRole on a Drafted intent.
type IntentRoleUpdated struct {
	IntentID   valueobjects.IntentID `json:"intent_id"`
	TenantID   shared.TenantID       `json:"tenant_id"`
	NewTitle   string                `json:"new_title"`
	OccurredAt time.Time             `json:"occurred_at"`
}

// NewIntentRoleUpdated constructs the event.
func NewIntentRoleUpdated(id valueobjects.IntentID, tenant shared.TenantID, newTitle string, at time.Time) IntentRoleUpdated {
	return IntentRoleUpdated{IntentID: id, TenantID: tenant, NewTitle: newTitle, OccurredAt: at}
}

func (e IntentRoleUpdated) EventName() string                  { return "hiringintent.IntentRoleUpdated" }
func (e IntentRoleUpdated) AggregateID() valueobjects.IntentID { return e.IntentID }
func (e IntentRoleUpdated) Tenant() shared.TenantID            { return e.TenantID }
func (e IntentRoleUpdated) At() time.Time                      { return e.OccurredAt }

// IntentConfirmed is raised after the recruiter signs off on a draft intent.
// Downstream: jobposting creates a draft posting; sourcing warms its adapters.
type IntentConfirmed struct {
	IntentID    valueobjects.IntentID `json:"intent_id"`
	TenantID    shared.TenantID       `json:"tenant_id"`
	RecruiterID shared.RecruiterID    `json:"recruiter_id"`
	Priority    valueobjects.Priority `json:"priority"`
	OccurredAt  time.Time             `json:"occurred_at"`
}

// NewIntentConfirmed constructs the event.
func NewIntentConfirmed(id valueobjects.IntentID, tenant shared.TenantID, recruiter shared.RecruiterID, priority valueobjects.Priority, at time.Time) IntentConfirmed {
	return IntentConfirmed{IntentID: id, TenantID: tenant, RecruiterID: recruiter, Priority: priority, OccurredAt: at}
}

func (e IntentConfirmed) EventName() string                  { return "hiringintent.IntentConfirmed" }
func (e IntentConfirmed) AggregateID() valueobjects.IntentID { return e.IntentID }
func (e IntentConfirmed) Tenant() shared.TenantID            { return e.TenantID }
func (e IntentConfirmed) At() time.Time                      { return e.OccurredAt }

// IntentCancelled is raised when an intent is withdrawn before being filled.
type IntentCancelled struct {
	IntentID   valueobjects.IntentID `json:"intent_id"`
	TenantID   shared.TenantID       `json:"tenant_id"`
	Reason     string                `json:"reason"`
	OccurredAt time.Time             `json:"occurred_at"`
}

// NewIntentCancelled constructs the event.
func NewIntentCancelled(id valueobjects.IntentID, tenant shared.TenantID, reason string, at time.Time) IntentCancelled {
	return IntentCancelled{IntentID: id, TenantID: tenant, Reason: reason, OccurredAt: at}
}

func (e IntentCancelled) EventName() string                  { return "hiringintent.IntentCancelled" }
func (e IntentCancelled) AggregateID() valueobjects.IntentID { return e.IntentID }
func (e IntentCancelled) Tenant() shared.TenantID            { return e.TenantID }
func (e IntentCancelled) At() time.Time                      { return e.OccurredAt }
