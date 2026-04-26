package valueobjects

import "errors"

// ErrInvalidPostingStatus is returned when a status string cannot be parsed.
var ErrInvalidPostingStatus = errors.New("invalid posting status")

// PostingStatus represents the lifecycle state of a JobPosting.
type PostingStatus string

const (
	// StatusDraft is the initial state — JD generated from intent, not yet live.
	StatusDraft PostingStatus = "DRAFT"
	// StatusPublished — JD has been distributed to one or more sources.
	StatusPublished PostingStatus = "PUBLISHED"
	// StatusClosed — terminal, position(s) filled or no longer hiring.
	StatusClosed PostingStatus = "CLOSED"
	// StatusArchived — terminal, retained for analytics but no longer visible.
	StatusArchived PostingStatus = "ARCHIVED"
)

// ParsePostingStatus validates a string and returns the matching status.
func ParsePostingStatus(s string) (PostingStatus, error) {
	switch PostingStatus(s) {
	case StatusDraft, StatusPublished, StatusClosed, StatusArchived:
		return PostingStatus(s), nil
	default:
		return "", ErrInvalidPostingStatus
	}
}

// String returns the canonical string form.
func (s PostingStatus) String() string { return string(s) }

// IsTerminal reports whether the status disallows further transitions.
func (s PostingStatus) IsTerminal() bool {
	return s == StatusClosed || s == StatusArchived
}
