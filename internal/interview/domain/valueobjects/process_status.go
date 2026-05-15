package valueobjects

import "errors"

// ProcessStatus is the lifecycle state of an interview process.
type ProcessStatus string

const (
	ProcessStatusNew        ProcessStatus = "New"
	ProcessStatusInProgress ProcessStatus = "InProgress"
	ProcessStatusCompleted  ProcessStatus = "Completed"
	ProcessStatusCancelled  ProcessStatus = "Cancelled"
)

// ErrInvalidProcessStatus is returned by ParseProcessStatus when the value is unknown.
var ErrInvalidProcessStatus = errors.New("invalid process status")

// ParseProcessStatus validates and returns a ProcessStatus for the given string.
func ParseProcessStatus(s string) (ProcessStatus, error) {
	switch ProcessStatus(s) {
	case ProcessStatusNew, ProcessStatusInProgress,
		ProcessStatusCompleted, ProcessStatusCancelled:
		return ProcessStatus(s), nil
	default:
		return "", ErrInvalidProcessStatus
	}
}

// IsTerminal reports whether the status admits no further transitions.
func (s ProcessStatus) IsTerminal() bool {
	return s == ProcessStatusCompleted || s == ProcessStatusCancelled
}

// String returns the canonical string form.
func (s ProcessStatus) String() string { return string(s) }
