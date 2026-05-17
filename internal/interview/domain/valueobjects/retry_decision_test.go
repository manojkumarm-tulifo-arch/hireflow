package valueobjects

import (
	"testing"
	"time"
)

func TestDecideRetry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		kind        FailureKind
		attempt     int
		wantAction  RetryAction
		wantBackoff time.Duration
	}{
		// llm_auth: abort immediately regardless of attempt
		{
			name:        "llm_auth attempt 1 → abort",
			kind:        FailureKindLLMAuth,
			attempt:     1,
			wantAction:  RetryActionAbort,
			wantBackoff: 0,
		},
		{
			name:        "llm_auth attempt 3 → abort",
			kind:        FailureKindLLMAuth,
			attempt:     3,
			wantAction:  RetryActionAbort,
			wantBackoff: 0,
		},

		// invalid_json: one retry at 30s then abort
		{
			name:        "invalid_json attempt 1 → retry 30s",
			kind:        FailureKindInvalidJSON,
			attempt:     1,
			wantAction:  RetryActionRetry,
			wantBackoff: 30 * time.Second,
		},
		{
			name:        "invalid_json attempt 2 → abort",
			kind:        FailureKindInvalidJSON,
			attempt:     2,
			wantAction:  RetryActionAbort,
			wantBackoff: 0,
		},
		{
			name:        "invalid_json attempt 3 → abort",
			kind:        FailureKindInvalidJSON,
			attempt:     3,
			wantAction:  RetryActionAbort,
			wantBackoff: 0,
		},

		// transient: [1m, 5m, 15m, 1h, 4h] then abort
		{
			name:        "transient attempt 1 → retry 1m",
			kind:        FailureKindTransient,
			attempt:     1,
			wantAction:  RetryActionRetry,
			wantBackoff: 1 * time.Minute,
		},
		{
			name:        "transient attempt 2 → retry 5m",
			kind:        FailureKindTransient,
			attempt:     2,
			wantAction:  RetryActionRetry,
			wantBackoff: 5 * time.Minute,
		},
		{
			name:        "transient attempt 3 → retry 15m",
			kind:        FailureKindTransient,
			attempt:     3,
			wantAction:  RetryActionRetry,
			wantBackoff: 15 * time.Minute,
		},
		{
			name:        "transient attempt 4 → retry 1h",
			kind:        FailureKindTransient,
			attempt:     4,
			wantAction:  RetryActionRetry,
			wantBackoff: 1 * time.Hour,
		},
		{
			name:        "transient attempt 5 → retry 4h",
			kind:        FailureKindTransient,
			attempt:     5,
			wantAction:  RetryActionRetry,
			wantBackoff: 4 * time.Hour,
		},
		{
			name:        "transient attempt 6 → abort",
			kind:        FailureKindTransient,
			attempt:     6,
			wantAction:  RetryActionAbort,
			wantBackoff: 0,
		},

		// unknown: [1m, 5m, 15m] then abort
		{
			name:        "unknown attempt 1 → retry 1m",
			kind:        FailureKindUnknown,
			attempt:     1,
			wantAction:  RetryActionRetry,
			wantBackoff: 1 * time.Minute,
		},
		{
			name:        "unknown attempt 2 → retry 5m",
			kind:        FailureKindUnknown,
			attempt:     2,
			wantAction:  RetryActionRetry,
			wantBackoff: 5 * time.Minute,
		},
		{
			name:        "unknown attempt 3 → retry 15m",
			kind:        FailureKindUnknown,
			attempt:     3,
			wantAction:  RetryActionRetry,
			wantBackoff: 15 * time.Minute,
		},
		{
			name:        "unknown attempt 4 → abort",
			kind:        FailureKindUnknown,
			attempt:     4,
			wantAction:  RetryActionAbort,
			wantBackoff: 0,
		},
	}

	const detail = "some error detail"

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DecideRetry(tc.kind, tc.attempt, detail)
			if got.Action != tc.wantAction {
				t.Errorf("Action: got %v, want %v", got.Action, tc.wantAction)
			}
			if got.Backoff != tc.wantBackoff {
				t.Errorf("Backoff: got %v, want %v", got.Backoff, tc.wantBackoff)
			}
			if got.Detail != detail {
				t.Errorf("Detail: got %q, want %q", got.Detail, detail)
			}
		})
	}
}
