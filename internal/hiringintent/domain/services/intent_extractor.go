// Package services holds domain services for the hiringintent context.
// A domain service captures behavior that doesn't fit on a single aggregate
// — here, the LLM-driven extraction of a structured intent from natural
// language. The interface speaks DTO types only; SDK details live behind
// the infrastructure adapter that implements it.
package services

import (
	"context"
	"errors"

	"github.com/hustle/hireflow/internal/hiringintent/application/dto"
)

// LLM failure sentinels. Adapters wrap concrete SDK errors with these so the
// HTTP handler can map them to specific status codes and user-facing copy
// without importing infrastructure details. Use `errors.Is` to check.
var (
	// ErrLLMBilling — workspace is out of credits or billing-blocked.
	// Operator action required (top up); recruiter can't fix.
	ErrLLMBilling = errors.New("llm: billing")
	// ErrLLMAuth — API key is missing, malformed, or revoked.
	// Operator misconfiguration; recruiter sees a generic message.
	ErrLLMAuth = errors.New("llm: authentication")
	// ErrLLMPermission — workspace can't access the configured model.
	ErrLLMPermission = errors.New("llm: permission")
	// ErrLLMRateLimit — caller is rate-limited (transient; safe to retry).
	ErrLLMRateLimit = errors.New("llm: rate limited")
	// ErrLLMTimeout — request exceeded its deadline (transient).
	ErrLLMTimeout = errors.New("llm: timeout")
	// ErrLLMOverloaded — upstream reports it is overloaded (transient).
	ErrLLMOverloaded = errors.New("llm: overloaded")
	// ErrLLMUpstream — anything else from the upstream call.
	ErrLLMUpstream = errors.New("llm: upstream")
	// ErrLLMResponseShape — upstream succeeded but the response didn't fit
	// the contract (e.g. model didn't call the tool, malformed JSON).
	ErrLLMResponseShape = errors.New("llm: bad response shape")
)

// IntentExtractor turns a recruiter's chat turn into a structured patch on
// the in-progress draft, plus a natural-language reply. Implementations
// must be safe for concurrent use by multiple HTTP handlers.
//
// Adapters classify failures into the ErrLLM* sentinels above. Anything
// not matching a sentinel is wrapped in ErrLLMUpstream so callers can
// always rely on errors.Is to identify the broad failure mode.
type IntentExtractor interface {
	Extract(ctx context.Context, in dto.ExtractInput) (dto.ExtractOutput, error)
}
