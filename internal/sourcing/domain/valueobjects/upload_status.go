// Package valueobjects holds the value objects of the sourcing context.
package valueobjects

import "errors"

// UploadStatus is the lifecycle state of a ResumeUpload.
// Slice 1 only uses a subset; later slices introduce Parsing/Embedding/Scoring.
type UploadStatus string

const (
	StatusPending     UploadStatus = "Pending"
	StatusScanning    UploadStatus = "Scanning"
	StatusExtracting  UploadStatus = "Extracting"
	StatusParsing     UploadStatus = "Parsing"   // slice 2
	StatusEmbedding   UploadStatus = "Embedding" // slice 3
	StatusScoring     UploadStatus = "Scoring"   // slice 3
	StatusExtracted   UploadStatus = "Extracted" // terminal in slice 1
	StatusScored      UploadStatus = "Scored"    // terminal in slice 3
	StatusFailed      UploadStatus = "Failed"
	StatusQuarantined UploadStatus = "Quarantined"
)

// ErrInvalidStatus is returned by ParseUploadStatus when the value is unknown.
var ErrInvalidStatus = errors.New("invalid upload status")

// ParseUploadStatus validates and returns an UploadStatus for the given string.
func ParseUploadStatus(s string) (UploadStatus, error) {
	switch UploadStatus(s) {
	case StatusPending, StatusScanning, StatusExtracting, StatusParsing,
		StatusEmbedding, StatusScoring, StatusExtracted, StatusScored,
		StatusFailed, StatusQuarantined:
		return UploadStatus(s), nil
	default:
		return "", ErrInvalidStatus
	}
}

// IsTerminal reports whether the status is a terminal state (no further worker action).
func (s UploadStatus) IsTerminal() bool {
	switch s {
	case StatusExtracted, StatusScored, StatusFailed, StatusQuarantined:
		return true
	}
	return false
}

// CanTransitionTo reports whether s -> next is a permitted state transition.
// Failed/Quarantined are reachable from any non-terminal state (operator may
// also force them, but the entity guards that).
func (s UploadStatus) CanTransitionTo(next UploadStatus) bool {
	if s.IsTerminal() {
		return false
	}
	if next == StatusFailed || next == StatusQuarantined {
		return true
	}
	switch s {
	case StatusPending:
		return next == StatusScanning
	case StatusScanning:
		return next == StatusExtracting
	case StatusExtracting:
		// Slice 1 terminates here; slice 2 will transition to Parsing instead.
		return next == StatusExtracted || next == StatusParsing
	}
	return false
}
