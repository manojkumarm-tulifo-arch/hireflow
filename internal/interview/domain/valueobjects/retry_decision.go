package valueobjects

import "time"

// RetryAction describes what the worker should do with a failed generation.
type RetryAction int

const (
	// RetryActionAbort marks the round as GenerationFailed (terminal until
	// the recruiter manually regenerates).
	RetryActionAbort RetryAction = iota
	// RetryActionRetry schedules a fresh attempt after the returned backoff.
	RetryActionRetry
)

// RetryDecision is the worker's response to a generation failure. It is built
// from the failure characteristics (transient, LLM auth, invalid output, etc.)
// and the current attempt count.
type RetryDecision struct {
	Action  RetryAction
	Backoff time.Duration // honored only when Action == RetryActionRetry
	Detail  string        // free-text for the round's last_error column
}

// FailureKind classifies an upstream Anthropic failure.
type FailureKind int

const (
	FailureKindUnknown FailureKind = iota
	FailureKindTransient
	FailureKindLLMAuth
	FailureKindInvalidJSON
)

// DecideRetry returns the next action for a given failure + attempt count.
// Attempt count is 1-indexed (i.e., the just-completed attempt was attempt N).
//
// Schedules (from the spec):
//
//	transient: [1m, 5m, 15m, 1h, 4h] then abort
//	llm_auth: abort immediately
//	invalid_json: one retry at 30s then abort
//	unknown: [1m, 5m, 15m] then abort
func DecideRetry(kind FailureKind, attempt int, detail string) RetryDecision {
	switch kind {
	case FailureKindLLMAuth:
		return RetryDecision{Action: RetryActionAbort, Detail: detail}
	case FailureKindInvalidJSON:
		if attempt == 1 {
			return RetryDecision{Action: RetryActionRetry, Backoff: 30 * time.Second, Detail: detail}
		}
		return RetryDecision{Action: RetryActionAbort, Detail: detail}
	case FailureKindTransient:
		schedule := []time.Duration{
			1 * time.Minute, 5 * time.Minute, 15 * time.Minute,
			1 * time.Hour, 4 * time.Hour,
		}
		if attempt <= len(schedule) {
			return RetryDecision{Action: RetryActionRetry, Backoff: schedule[attempt-1], Detail: detail}
		}
		return RetryDecision{Action: RetryActionAbort, Detail: detail}
	default:
		schedule := []time.Duration{1 * time.Minute, 5 * time.Minute, 15 * time.Minute}
		if attempt <= len(schedule) {
			return RetryDecision{Action: RetryActionRetry, Backoff: schedule[attempt-1], Detail: detail}
		}
		return RetryDecision{Action: RetryActionAbort, Detail: detail}
	}
}
