package valueobjects

import "errors"

// ErrInvalidPriority is returned when a priority string cannot be parsed.
var ErrInvalidPriority = errors.New("invalid priority")

// Priority captures hiring urgency for the intent.
type Priority string

const (
	PriorityLow      Priority = "LOW"
	PriorityMedium   Priority = "MEDIUM"
	PriorityHigh     Priority = "HIGH"
	PriorityCritical Priority = "CRITICAL"
)

// ParsePriority validates a string and returns the matching priority.
func ParsePriority(s string) (Priority, error) {
	switch Priority(s) {
	case PriorityLow, PriorityMedium, PriorityHigh, PriorityCritical:
		return Priority(s), nil
	default:
		return "", ErrInvalidPriority
	}
}

// String returns the canonical string form.
func (p Priority) String() string { return string(p) }
