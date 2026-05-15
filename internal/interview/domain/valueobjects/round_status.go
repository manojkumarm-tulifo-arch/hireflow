package valueobjects

import "errors"

// RoundStatus is the lifecycle state of an interview round.
type RoundStatus string

const (
	RoundStatusPending          RoundStatus = "Pending"
	RoundStatusQuestionsReady   RoundStatus = "QuestionsReady"
	RoundStatusCompleted        RoundStatus = "Completed"
	RoundStatusSkipped          RoundStatus = "Skipped"
	RoundStatusGenerationFailed RoundStatus = "GenerationFailed"
)

// ErrInvalidRoundStatus is returned by ParseRoundStatus when the value is unknown.
var ErrInvalidRoundStatus = errors.New("invalid round status")

// ParseRoundStatus validates and returns a RoundStatus for the given string.
func ParseRoundStatus(s string) (RoundStatus, error) {
	switch RoundStatus(s) {
	case RoundStatusPending, RoundStatusQuestionsReady, RoundStatusCompleted,
		RoundStatusSkipped, RoundStatusGenerationFailed:
		return RoundStatus(s), nil
	default:
		return "", ErrInvalidRoundStatus
	}
}

// IsTerminal reports whether the round status admits no further transitions
// (Completed or Skipped). GenerationFailed is NOT terminal — recruiter can
// regenerate.
func (s RoundStatus) IsTerminal() bool {
	return s == RoundStatusCompleted || s == RoundStatusSkipped
}

// CanTransitionTo reports whether s -> next is a permitted state transition.
//
// Permitted transitions:
//
//	Pending           -> QuestionsReady | GenerationFailed | Skipped
//	QuestionsReady    -> Completed | Skipped | Pending  (recruiter regenerates)
//	GenerationFailed  -> Pending | Skipped                (recruiter regenerates or skips)
//	Completed, Skipped (terminals) — no outbound transitions
func (s RoundStatus) CanTransitionTo(next RoundStatus) bool {
	switch s {
	case RoundStatusPending:
		switch next {
		case RoundStatusQuestionsReady, RoundStatusGenerationFailed, RoundStatusSkipped:
			return true
		}
	case RoundStatusQuestionsReady:
		switch next {
		case RoundStatusCompleted, RoundStatusSkipped, RoundStatusPending:
			return true
		}
	case RoundStatusGenerationFailed:
		switch next {
		case RoundStatusPending, RoundStatusSkipped:
			return true
		}
	}
	return false
}

// String returns the canonical string form.
func (s RoundStatus) String() string { return string(s) }
