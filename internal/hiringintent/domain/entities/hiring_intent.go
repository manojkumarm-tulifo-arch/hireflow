// Package entities holds the aggregate roots and entities of the
// hiringintent bounded context.
package entities

import (
	"errors"
	"time"

	"github.com/hustle/hireflow/internal/hiringintent/domain/events"
	"github.com/hustle/hireflow/internal/hiringintent/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// Domain errors enforced at the aggregate boundary.
var (
	// ErrCannotModifyConfirmed is returned when a write is attempted on a non-Drafted intent.
	ErrCannotModifyConfirmed = errors.New("cannot modify a non-drafted intent")
	// ErrCannotConfirmNonDrafted is returned when Confirm is called on a non-Drafted intent.
	ErrCannotConfirmNonDrafted = errors.New("can only confirm a drafted intent")
	// ErrCannotConfirmWithoutSkills is returned when no required skill is present at Confirm time.
	ErrCannotConfirmWithoutSkills = errors.New("intent must have at least one required skill before confirm")
	// ErrCannotCancelTerminal is returned when Cancel is called on an already-terminal intent.
	ErrCannotCancelTerminal = errors.New("cannot cancel an intent in a terminal state")
)

// HiringIntent is the aggregate root of the hiringintent bounded context.
// It owns role spec, intent signals, trust signals, and lifecycle state.
// External code accesses everything through this root; mutations go through
// methods that enforce invariants.
type HiringIntent struct {
	id            valueobjects.IntentID
	tenantID      shared.TenantID
	recruiterID   shared.RecruiterID
	role          valueobjects.RoleSpec
	priority      valueobjects.Priority
	intentSignals []valueobjects.IntentSignal
	trustSignals  []valueobjects.TrustSignal
	budget        *valueobjects.BudgetRange
	status        valueobjects.IntentStatus
	createdAt     time.Time
	updatedAt     time.Time
	confirmedAt   *time.Time
	cancelledAt   *time.Time
	cancelReason  string

	pendingEvents []events.Event
}

// NewHiringIntent constructs a fresh intent in Drafted state and emits IntentDrafted.
// The role spec must already be valid (constructed via valueobjects.NewRoleSpec).
func NewHiringIntent(
	tenantID shared.TenantID,
	recruiterID shared.RecruiterID,
	role valueobjects.RoleSpec,
	priority valueobjects.Priority,
) (*HiringIntent, error) {
	if tenantID.IsZero() {
		return nil, errors.New("tenantID is required")
	}
	if recruiterID.IsZero() {
		return nil, errors.New("recruiterID is required")
	}
	now := time.Now().UTC()
	id := valueobjects.NewIntentID()
	intent := &HiringIntent{
		id:          id,
		tenantID:    tenantID,
		recruiterID: recruiterID,
		role:        role,
		priority:    priority,
		status:      valueobjects.StatusDrafted,
		createdAt:   now,
		updatedAt:   now,
	}
	intent.raise(events.NewIntentDrafted(id, tenantID, recruiterID, role.Title(), now))
	return intent, nil
}

// HydrateHiringIntent reconstitutes an aggregate from persistence. Used only by
// repository implementations — does not raise events.
func HydrateHiringIntent(
	id valueobjects.IntentID,
	tenantID shared.TenantID,
	recruiterID shared.RecruiterID,
	role valueobjects.RoleSpec,
	priority valueobjects.Priority,
	intentSignals []valueobjects.IntentSignal,
	trustSignals []valueobjects.TrustSignal,
	budget *valueobjects.BudgetRange,
	status valueobjects.IntentStatus,
	createdAt, updatedAt time.Time,
	confirmedAt, cancelledAt *time.Time,
	cancelReason string,
) *HiringIntent {
	return &HiringIntent{
		id:            id,
		tenantID:      tenantID,
		recruiterID:   recruiterID,
		role:          role,
		priority:      priority,
		intentSignals: append([]valueobjects.IntentSignal(nil), intentSignals...),
		trustSignals:  append([]valueobjects.TrustSignal(nil), trustSignals...),
		budget:        budget,
		status:        status,
		createdAt:     createdAt,
		updatedAt:     updatedAt,
		confirmedAt:   confirmedAt,
		cancelledAt:   cancelledAt,
		cancelReason:  cancelReason,
	}
}

// Getters.
func (h *HiringIntent) ID() valueobjects.IntentID                  { return h.id }
func (h *HiringIntent) TenantID() shared.TenantID                  { return h.tenantID }
func (h *HiringIntent) RecruiterID() shared.RecruiterID            { return h.recruiterID }
func (h *HiringIntent) Role() valueobjects.RoleSpec                { return h.role }
func (h *HiringIntent) Priority() valueobjects.Priority            { return h.priority }
func (h *HiringIntent) IntentSignals() []valueobjects.IntentSignal { return append([]valueobjects.IntentSignal(nil), h.intentSignals...) }
func (h *HiringIntent) TrustSignals() []valueobjects.TrustSignal   { return append([]valueobjects.TrustSignal(nil), h.trustSignals...) }
func (h *HiringIntent) Budget() *valueobjects.BudgetRange          { return h.budget }
func (h *HiringIntent) Status() valueobjects.IntentStatus          { return h.status }
func (h *HiringIntent) CreatedAt() time.Time                       { return h.createdAt }
func (h *HiringIntent) UpdatedAt() time.Time                       { return h.updatedAt }
func (h *HiringIntent) ConfirmedAt() *time.Time                    { return h.confirmedAt }
func (h *HiringIntent) CancelledAt() *time.Time                    { return h.cancelledAt }
func (h *HiringIntent) CancelReason() string                       { return h.cancelReason }

// IsModifiable reports whether the intent accepts mutations.
func (h *HiringIntent) IsModifiable() bool {
	return h.status == valueobjects.StatusDrafted
}

// UpdateRole replaces the role spec. Allowed only in Drafted state.
func (h *HiringIntent) UpdateRole(role valueobjects.RoleSpec) error {
	if !h.IsModifiable() {
		return ErrCannotModifyConfirmed
	}
	h.role = role
	h.touch()
	h.raise(events.NewIntentRoleUpdated(h.id, h.tenantID, role.Title(), h.updatedAt))
	return nil
}

// AddIntentSignal appends an intent signal. Allowed only in Drafted state.
func (h *HiringIntent) AddIntentSignal(s valueobjects.IntentSignal) error {
	if !h.IsModifiable() {
		return ErrCannotModifyConfirmed
	}
	h.intentSignals = append(h.intentSignals, s)
	h.touch()
	return nil
}

// AddTrustSignal appends a trust signal. Allowed only in Drafted state.
func (h *HiringIntent) AddTrustSignal(s valueobjects.TrustSignal) error {
	if !h.IsModifiable() {
		return ErrCannotModifyConfirmed
	}
	h.trustSignals = append(h.trustSignals, s)
	h.touch()
	return nil
}

// SetBudget assigns or replaces the budget range. Allowed only in Drafted state.
func (h *HiringIntent) SetBudget(b valueobjects.BudgetRange) error {
	if !h.IsModifiable() {
		return ErrCannotModifyConfirmed
	}
	h.budget = &b
	h.touch()
	return nil
}

// Confirm transitions the intent from Drafted to Confirmed.
// Enforces confirm-time invariants: at least one required skill.
func (h *HiringIntent) Confirm() error {
	if h.status != valueobjects.StatusDrafted {
		return ErrCannotConfirmNonDrafted
	}
	if !h.role.HasRequiredSkill() {
		return ErrCannotConfirmWithoutSkills
	}
	now := time.Now().UTC()
	h.status = valueobjects.StatusConfirmed
	h.confirmedAt = &now
	h.updatedAt = now
	h.raise(events.NewIntentConfirmed(h.id, h.tenantID, h.recruiterID, h.priority, now))
	return nil
}

// Cancel transitions the intent to Cancelled. Allowed unless already terminal.
func (h *HiringIntent) Cancel(reason string) error {
	if h.status.IsTerminal() {
		return ErrCannotCancelTerminal
	}
	now := time.Now().UTC()
	h.status = valueobjects.StatusCancelled
	h.cancelledAt = &now
	h.cancelReason = reason
	h.updatedAt = now
	h.raise(events.NewIntentCancelled(h.id, h.tenantID, reason, now))
	return nil
}

// PullEvents returns and clears the pending event buffer.
// Repositories call this after a successful Save to publish via the outbox.
func (h *HiringIntent) PullEvents() []events.Event {
	out := h.pendingEvents
	h.pendingEvents = nil
	return out
}

func (h *HiringIntent) raise(e events.Event) {
	h.pendingEvents = append(h.pendingEvents, e)
}

func (h *HiringIntent) touch() {
	h.updatedAt = time.Now().UTC()
}
