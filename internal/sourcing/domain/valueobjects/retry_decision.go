package valueobjects

import "time"

// RetryDecision is returned by every pipeline stage. The adapter (not the
// worker) decides whether an error is retryable, because only the adapter
// knows the semantics of its upstream.
type RetryDecision struct {
	Retryable   bool
	Reason      string        // e.g. "anthropic_429", "virus_detected", "ocr_empty"
	Detail      string        // human-readable, lands in last_error
	BackoffHint time.Duration // 0 means "use worker default schedule"
}

// Retryable builds a retryable decision with the given reason/detail.
func Retryable(reason, detail string) RetryDecision {
	return RetryDecision{Retryable: true, Reason: reason, Detail: detail}
}

// Fatal builds a non-retryable decision with the given reason/detail.
func Fatal(reason, detail string) RetryDecision {
	return RetryDecision{Retryable: false, Reason: reason, Detail: detail}
}

// WithBackoff overrides the worker's default backoff schedule for this retry.
func (d RetryDecision) WithBackoff(b time.Duration) RetryDecision {
	d.BackoffHint = b
	return d
}
