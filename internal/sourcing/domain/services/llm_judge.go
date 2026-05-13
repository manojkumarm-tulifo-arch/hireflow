package services

import (
	"context"
	"fmt"

	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// JudgeError is returned by LLMJudge adapters when judging fails.
// Retryable indicates whether the caller should re-attempt the operation.
// The worker layer uses errors.As to decide between ScheduleRetry
// (Retryable=true) and Application.MarkJudgeFailed (Retryable=false).
type JudgeError struct {
	Retryable bool
	Reason    string // short code, e.g. "anthropic_5xx", "tool_invalid_json"
	Detail    string // human-readable
}

func (e JudgeError) Error() string {
	return fmt.Sprintf("llm judge: %s: %s", e.Reason, e.Detail)
}

// LLMJudge is the port for LLM-driven qualitative assessment of a single
// (Candidate, Intent) pair. The canonical implementation (AnthropicJudge)
// uses Claude forced tool-use against the judge_match schema.
//
// Errors should be JudgeError when classified; raw errors are treated as
// retryable by the worker layer.
type LLMJudge interface {
	// Judge evaluates the candidate profile against the role spec, grounding
	// each claim in evidence from the parsed profile. It uses the rule match
	// report to focus the judgment on criteria that were flagged as failing or
	// borderline.
	//
	// Returns a structured LLMJudgment with a 0–100 score, evidence items,
	// a two-sentence summary, and optional concerns.
	Judge(ctx context.Context, profile vo.ParsedProfile, role RoleSpec,
		rules vo.RuleMatchReport) (vo.LLMJudgment, error)
}
