// Package events defines domain events emitted by the auth context.
package events

import (
	"time"

	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// Event is implemented by every auth domain event.
type Event interface {
	EventName() string
	AggregateID() valueobjects.UserID
	Tenant() shared.TenantID
	At() time.Time
}

// UserRegistered is raised when a user signs up (still PendingVerification).
type UserRegistered struct {
	UserID     valueobjects.UserID `json:"user_id"`
	TenantID   shared.TenantID     `json:"tenant_id"`
	Email      string              `json:"email"`
	OccurredAt time.Time           `json:"occurred_at"`
}

func NewUserRegistered(id valueobjects.UserID, tenant shared.TenantID, email string, at time.Time) UserRegistered {
	return UserRegistered{UserID: id, TenantID: tenant, Email: email, OccurredAt: at}
}

func (e UserRegistered) EventName() string                { return "auth.UserRegistered" }
func (e UserRegistered) AggregateID() valueobjects.UserID { return e.UserID }
func (e UserRegistered) Tenant() shared.TenantID          { return e.TenantID }
func (e UserRegistered) At() time.Time                    { return e.OccurredAt }

// UserVerified is raised when a user completes their signup OTP.
type UserVerified struct {
	UserID     valueobjects.UserID `json:"user_id"`
	TenantID   shared.TenantID     `json:"tenant_id"`
	OccurredAt time.Time           `json:"occurred_at"`
}

func NewUserVerified(id valueobjects.UserID, tenant shared.TenantID, at time.Time) UserVerified {
	return UserVerified{UserID: id, TenantID: tenant, OccurredAt: at}
}

func (e UserVerified) EventName() string                { return "auth.UserVerified" }
func (e UserVerified) AggregateID() valueobjects.UserID { return e.UserID }
func (e UserVerified) Tenant() shared.TenantID          { return e.TenantID }
func (e UserVerified) At() time.Time                    { return e.OccurredAt }

// UserSignedIn is raised on every successful sign-in (signin or refresh).
type UserSignedIn struct {
	UserID     valueobjects.UserID `json:"user_id"`
	TenantID   shared.TenantID     `json:"tenant_id"`
	OccurredAt time.Time           `json:"occurred_at"`
}

func NewUserSignedIn(id valueobjects.UserID, tenant shared.TenantID, at time.Time) UserSignedIn {
	return UserSignedIn{UserID: id, TenantID: tenant, OccurredAt: at}
}

func (e UserSignedIn) EventName() string                { return "auth.UserSignedIn" }
func (e UserSignedIn) AggregateID() valueobjects.UserID { return e.UserID }
func (e UserSignedIn) Tenant() shared.TenantID          { return e.TenantID }
func (e UserSignedIn) At() time.Time                    { return e.OccurredAt }
