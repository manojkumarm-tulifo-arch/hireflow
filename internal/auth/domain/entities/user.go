// Package entities holds the aggregate roots and entities of the auth context.
package entities

import (
	"errors"
	"time"

	"github.com/hustle/hireflow/internal/auth/domain/events"
	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// Domain errors enforced at the User boundary.
var (
	ErrCannotVerifyNonPending      = errors.New("only pending-verification users can be verified")
	ErrCannotSignInWhenNotActive   = errors.New("user cannot sign in in current status")
	ErrAccountLocked               = errors.New("account locked due to repeated failures")
	ErrInvalidUserConstruction     = errors.New("invalid user construction")
)

// MaxFailedAttempts is the number of consecutive failed sign-in attempts before
// the account is auto-locked.
const MaxFailedAttempts = 5

// LockCooldown is how long an auto-lock lasts before the next attempt is allowed.
const LockCooldown = 15 * time.Minute

// User is the aggregate root of the auth context. Owns identity, lifecycle
// state, role assignments, and failed-attempt tracking.
type User struct {
	id              valueobjects.UserID
	tenantID        shared.TenantID
	email           valueobjects.Email
	name            string
	status          valueobjects.UserStatus
	roles           []string
	failedAttempts  int
	lockedUntil     *time.Time
	createdAt       time.Time
	updatedAt       time.Time
	verifiedAt      *time.Time
	lastSignedInAt  *time.Time

	pendingEvents []events.Event
}

// NewUser creates a fresh user in PendingVerification state and emits UserRegistered.
func NewUser(tenantID shared.TenantID, email valueobjects.Email, name string, roles []string) (*User, error) {
	if tenantID.IsZero() {
		return nil, ErrInvalidUserConstruction
	}
	if email.IsZero() {
		return nil, ErrInvalidUserConstruction
	}
	now := time.Now().UTC()
	id := valueobjects.NewUserID()
	if len(roles) == 0 {
		roles = []string{"recruiter"}
	}
	u := &User{
		id:        id,
		tenantID:  tenantID,
		email:     email,
		name:      name,
		status:    valueobjects.StatusPendingVerification,
		roles:     append([]string(nil), roles...),
		createdAt: now,
		updatedAt: now,
	}
	u.raise(events.NewUserRegistered(id, tenantID, email.String(), now))
	return u, nil
}

// HydrateUser reconstitutes a User from persistence (no events emitted).
func HydrateUser(
	id valueobjects.UserID,
	tenantID shared.TenantID,
	email valueobjects.Email,
	name string,
	status valueobjects.UserStatus,
	roles []string,
	failedAttempts int,
	lockedUntil *time.Time,
	createdAt, updatedAt time.Time,
	verifiedAt, lastSignedInAt *time.Time,
) *User {
	return &User{
		id:             id,
		tenantID:       tenantID,
		email:          email,
		name:           name,
		status:         status,
		roles:          append([]string(nil), roles...),
		failedAttempts: failedAttempts,
		lockedUntil:    lockedUntil,
		createdAt:      createdAt,
		updatedAt:      updatedAt,
		verifiedAt:     verifiedAt,
		lastSignedInAt: lastSignedInAt,
	}
}

// Getters.
func (u *User) ID() valueobjects.UserID         { return u.id }
func (u *User) TenantID() shared.TenantID       { return u.tenantID }

// AsRecruiterID projects the user's identity into the cross-context
// `shared.RecruiterID` type. The auth context owns user identity and
// represents it as `UserID`; downstream contexts (hiringintent,
// jobposting, candidate-bgv) refer to the same person as `RecruiterID`.
// The two carry the same UUID by convention — this method makes that
// invariant explicit and discoverable instead of relying on every
// caller knowing to call `shared.ParseRecruiterID(u.ID().String())`.
//
// Defensive — `ParseRecruiterID` only fails on a malformed UUID, which
// can't happen here because UserID is already a parsed UUID. The error
// is dropped on the floor; if it ever fires the caller has a bigger
// problem (corrupt aggregate state) than this projection can express.
func (u *User) AsRecruiterID() shared.RecruiterID {
	r, _ := shared.ParseRecruiterID(u.id.String())
	return r
}

func (u *User) Email() valueobjects.Email       { return u.email }
func (u *User) Name() string                    { return u.name }
func (u *User) Status() valueobjects.UserStatus { return u.status }
func (u *User) Roles() []string                 { return append([]string(nil), u.roles...) }
func (u *User) FailedAttempts() int             { return u.failedAttempts }
func (u *User) LockedUntil() *time.Time         { return u.lockedUntil }
func (u *User) CreatedAt() time.Time            { return u.createdAt }
func (u *User) UpdatedAt() time.Time            { return u.updatedAt }
func (u *User) VerifiedAt() *time.Time          { return u.verifiedAt }
func (u *User) LastSignedInAt() *time.Time      { return u.lastSignedInAt }

// MarkVerified transitions PendingVerification → Active. Emits UserVerified.
func (u *User) MarkVerified() error {
	if u.status != valueobjects.StatusPendingVerification {
		return ErrCannotVerifyNonPending
	}
	now := time.Now().UTC()
	u.status = valueobjects.StatusActive
	u.verifiedAt = &now
	u.updatedAt = now
	u.raise(events.NewUserVerified(u.id, u.tenantID, now))
	return nil
}

// RecordSignIn marks a successful sign-in: clears failure counter, updates
// lastSignedInAt, emits UserSignedIn. Caller should check CanSignInNow first.
func (u *User) RecordSignIn() error {
	if !u.CanSignInNow() {
		if u.lockedUntil != nil && time.Now().Before(*u.lockedUntil) {
			return ErrAccountLocked
		}
		return ErrCannotSignInWhenNotActive
	}
	now := time.Now().UTC()
	u.failedAttempts = 0
	u.lockedUntil = nil
	u.lastSignedInAt = &now
	u.updatedAt = now
	u.raise(events.NewUserSignedIn(u.id, u.tenantID, now))
	return nil
}

// RecordFailedAttempt increments the failure counter and locks the account
// once the threshold is reached. Returns the new locked state for the caller.
func (u *User) RecordFailedAttempt() {
	u.failedAttempts++
	if u.failedAttempts >= MaxFailedAttempts {
		until := time.Now().UTC().Add(LockCooldown)
		u.lockedUntil = &until
		u.status = valueobjects.StatusLocked
	}
	u.updatedAt = time.Now().UTC()
}

// CanSignInNow reports whether the user is in a sign-in-able state right now.
// Auto-unlocks if the cooldown has passed.
func (u *User) CanSignInNow() bool {
	if u.status == valueobjects.StatusLocked && u.lockedUntil != nil && time.Now().UTC().After(*u.lockedUntil) {
		// Cooldown expired — auto-unlock.
		u.status = valueobjects.StatusActive
		u.failedAttempts = 0
		u.lockedUntil = nil
	}
	return u.status.CanSignIn()
}

// PullEvents returns and clears the pending event buffer.
func (u *User) PullEvents() []events.Event {
	out := u.pendingEvents
	u.pendingEvents = nil
	return out
}

func (u *User) raise(e events.Event) {
	u.pendingEvents = append(u.pendingEvents, e)
}
