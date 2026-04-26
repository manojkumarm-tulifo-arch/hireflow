package valueobjects

import "errors"

// ErrInvalidUserStatus is returned when a status string cannot be parsed.
var ErrInvalidUserStatus = errors.New("invalid user status")

// UserStatus represents the lifecycle state of a User.
type UserStatus string

const (
	// StatusPendingVerification means signup OTP has not yet been verified.
	StatusPendingVerification UserStatus = "PENDING_VERIFICATION"
	// StatusActive means the user can sign in.
	StatusActive UserStatus = "ACTIVE"
	// StatusLocked means too many failed attempts; admin must unlock or wait
	// for the cooldown to elapse (cooldown logic handled by the domain).
	StatusLocked UserStatus = "LOCKED"
	// StatusSuspended means an admin has disabled the account.
	StatusSuspended UserStatus = "SUSPENDED"
)

// ParseUserStatus validates a string and returns the matching status.
func ParseUserStatus(s string) (UserStatus, error) {
	switch UserStatus(s) {
	case StatusPendingVerification, StatusActive, StatusLocked, StatusSuspended:
		return UserStatus(s), nil
	default:
		return "", ErrInvalidUserStatus
	}
}

func (s UserStatus) String() string { return string(s) }

// CanSignIn reports whether a user in this status is allowed to sign in.
func (s UserStatus) CanSignIn() bool { return s == StatusActive }
