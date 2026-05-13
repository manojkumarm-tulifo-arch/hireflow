package services

import (
	"context"
	"fmt"

	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// ResumeParser is the port for LLM-driven extraction of a structured profile
// from plain resume text. Returns the canonical ParsedProfile. Adapters
// classify errors via ResumeParseError so the worker can apply the right
// retry policy.
type ResumeParser interface {
	Parse(ctx context.Context, text string) (vo.ParsedProfile, error)
}

// ResumeParseError carries the parser adapter's retryability classification.
// The worker layer uses errors.As to unwrap and dispatch to ScheduleRetry
// (when Retryable=true) or MarkFailed (when Retryable=false).
type ResumeParseError struct {
	Retryable bool
	Reason    string // short code, e.g. "anthropic_5xx", "tool_invalid_json", "no_tool_use"
	Detail    string // human-readable
}

func (e ResumeParseError) Error() string {
	return fmt.Sprintf("resume parse: %s: %s", e.Reason, e.Detail)
}
