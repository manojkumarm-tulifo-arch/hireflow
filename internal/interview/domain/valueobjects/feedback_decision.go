package valueobjects

import "errors"

// FeedbackDecision enumerates the hire/no-hire signals on round feedback.
type FeedbackDecision string

const (
	FeedbackDecisionStrongYes FeedbackDecision = "strong_yes"
	FeedbackDecisionYes       FeedbackDecision = "yes"
	FeedbackDecisionMixed     FeedbackDecision = "mixed"
	FeedbackDecisionNo        FeedbackDecision = "no"
	FeedbackDecisionStrongNo  FeedbackDecision = "strong_no"
)

// ErrInvalidFeedbackDecision is returned by ParseFeedbackDecision when the value is unknown.
var ErrInvalidFeedbackDecision = errors.New("invalid feedback decision")

// ParseFeedbackDecision validates and returns a FeedbackDecision for the given string.
func ParseFeedbackDecision(s string) (FeedbackDecision, error) {
	switch FeedbackDecision(s) {
	case FeedbackDecisionStrongYes, FeedbackDecisionYes, FeedbackDecisionMixed,
		FeedbackDecisionNo, FeedbackDecisionStrongNo:
		return FeedbackDecision(s), nil
	default:
		return "", ErrInvalidFeedbackDecision
	}
}

// String returns the canonical string form.
func (d FeedbackDecision) String() string { return string(d) }
