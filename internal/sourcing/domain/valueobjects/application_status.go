package valueobjects

import "errors"

// ApplicationStatus is the lifecycle state of an Application (Candidate × Intent pair).
// Slice 3 statuses: New, Scored, Excluded, EmbedFailed, JudgeFailed, Stale.
// Slice 4 statuses: Shortlisted, Rejected, Interviewing, Hired — declared here for
// forward-compat DB schema alignment but not used by slice-3 logic.
type ApplicationStatus string

const (
	AppStatusNew       ApplicationStatus = "New"
	AppStatusScored    ApplicationStatus = "Scored"
	AppStatusExcluded  ApplicationStatus = "Excluded"
	AppStatusEmbedFailed ApplicationStatus = "EmbedFailed"
	AppStatusJudgeFailed ApplicationStatus = "JudgeFailed"
	AppStatusStale     ApplicationStatus = "Stale"

	// Slice-4 statuses — declared for forward-compat; not used in slice 3.
	AppStatusShortlisted  ApplicationStatus = "Shortlisted"
	AppStatusRejected     ApplicationStatus = "Rejected"
	AppStatusInterviewing ApplicationStatus = "Interviewing"
	AppStatusHired        ApplicationStatus = "Hired"
)

// ErrInvalidApplicationStatus is returned by ParseApplicationStatus when the value is unknown.
var ErrInvalidApplicationStatus = errors.New("invalid application status")

// ParseApplicationStatus validates and returns an ApplicationStatus for the given string.
func ParseApplicationStatus(s string) (ApplicationStatus, error) {
	switch ApplicationStatus(s) {
	case AppStatusNew, AppStatusScored, AppStatusExcluded,
		AppStatusEmbedFailed, AppStatusJudgeFailed, AppStatusStale,
		AppStatusShortlisted, AppStatusRejected, AppStatusInterviewing, AppStatusHired:
		return ApplicationStatus(s), nil
	default:
		return "", ErrInvalidApplicationStatus
	}
}

// String returns the string representation of the status.
func (s ApplicationStatus) String() string { return string(s) }

// IsTerminal reports whether the status is a terminal state from which the
// worker will not proceed without an explicit rescore trigger.
//
// Terminal statuses (slice 4): Excluded, EmbedFailed, JudgeFailed, Stale,
// Rejected, Hired.
// Non-terminal: New, Scored, Shortlisted, Interviewing.
func (s ApplicationStatus) IsTerminal() bool {
	switch s {
	case AppStatusExcluded, AppStatusEmbedFailed, AppStatusJudgeFailed, AppStatusStale,
		AppStatusRejected, AppStatusHired:
		return true
	}
	return false
}

// CanTransitionTo reports whether s → next is a permitted state transition.
//
// Permitted transitions:
//
//	New            → Scored | Excluded | EmbedFailed
//	Scored         → JudgeFailed | Stale | Shortlisted | Rejected | Hired
//	Shortlisted    → Interviewing | Rejected | Hired | New (rescore)
//	Interviewing   → Rejected | Hired | New (rescore)
//	Terminals      → New  (explicit rescore path)
func (s ApplicationStatus) CanTransitionTo(next ApplicationStatus) bool {
	// All terminals can be reset to New via the rescore path.
	if s.IsTerminal() {
		return next == AppStatusNew
	}

	switch s {
	case AppStatusNew:
		switch next {
		case AppStatusScored, AppStatusExcluded, AppStatusEmbedFailed:
			return true
		}
	case AppStatusScored:
		switch next {
		case AppStatusJudgeFailed, AppStatusStale,
			AppStatusShortlisted, AppStatusRejected, AppStatusHired:
			return true
		}
	case AppStatusShortlisted:
		switch next {
		case AppStatusInterviewing, AppStatusRejected, AppStatusHired, AppStatusNew:
			return true
		}
	case AppStatusInterviewing:
		switch next {
		case AppStatusRejected, AppStatusHired, AppStatusNew:
			return true
		}
	}
	return false
}
