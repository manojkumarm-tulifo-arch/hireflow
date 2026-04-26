package valueobjects

import "errors"

// ErrInvalidIntentStatus is returned when a status string cannot be parsed.
var ErrInvalidIntentStatus = errors.New("invalid intent status")

// IntentStatus represents the lifecycle state of a HiringIntent.
type IntentStatus string

const (
	// StatusDrafted is the initial state after creation. Mutations allowed.
	StatusDrafted IntentStatus = "DRAFTED"
	// StatusConfirmed is the locked state after recruiter sign-off. Triggers downstream.
	StatusConfirmed IntentStatus = "CONFIRMED"
	// StatusCancelled is a terminal state; the intent was withdrawn.
	StatusCancelled IntentStatus = "CANCELLED"
	// StatusClosed is a terminal state; the posting was filled or expired.
	StatusClosed IntentStatus = "CLOSED"
)

// ParseIntentStatus validates a string and returns the matching status.
func ParseIntentStatus(s string) (IntentStatus, error) {
	switch IntentStatus(s) {
	case StatusDrafted, StatusConfirmed, StatusCancelled, StatusClosed:
		return IntentStatus(s), nil
	default:
		return "", ErrInvalidIntentStatus
	}
}

// String returns the canonical string form.
func (s IntentStatus) String() string { return string(s) }

// IsTerminal reports whether the status disallows further transitions.
func (s IntentStatus) IsTerminal() bool {
	return s == StatusCancelled || s == StatusClosed
}
