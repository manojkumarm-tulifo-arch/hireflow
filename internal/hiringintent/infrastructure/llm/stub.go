// Package llm holds infrastructure adapters for the IntentExtractor port.
// Stub is a deterministic implementation for local development and demo use
// (STUB_LLMS=true). No real Anthropic call is made; a canned ExtractOutput
// is returned so recruiters can exercise the full chat-to-confirm flow without
// API keys.
package llm

import (
	"context"

	"github.com/hustle/hireflow/internal/hiringintent/application/dto"
	"github.com/hustle/hireflow/internal/hiringintent/domain/services"
)

// Stub is a deterministic IntentExtractor for use when STUB_LLMS=true.
// It echoes a minimal DraftPatch derived from the user message and marks
// the draft complete once the user message is non-empty, so the FE can
// walk through the full confirm flow without LLM credits.
type Stub struct{}

// compile-time interface check.
var _ services.IntentExtractor = (*Stub)(nil)

// NewStub returns a ready-to-use Stub extractor.
func NewStub() *Stub {
	return &Stub{}
}

// Extract returns a canned ExtractOutput. The patch sets all required fields
// to sensible defaults so the draft reaches "complete" on the very first turn,
// allowing the recruiter to immediately confirm the intent in local dev.
func (Stub) Extract(_ context.Context, in dto.ExtractInput) (dto.ExtractOutput, error) {
	roleTitle := "Senior Backend Engineer"
	minYears := 3
	maxYears := 7
	headcount := 2
	workMode := "HYBRID"
	priority := "HIGH"

	return dto.ExtractOutput{
		Reply: "Got it! I've set up a stub hiring intent for a Senior Backend Engineer role. " +
			"All required fields are pre-filled for local development — feel free to confirm.",
		Patch: dto.DraftPatch{
			RoleTitle: &roleTitle,
			Skills: []dto.SkillPatch{
				{Name: "Go", Required: true},
				{Name: "Kafka", Required: false},
				{Name: "Postgres", Required: true},
			},
			MinYears:  &minYears,
			MaxYears:  &maxYears,
			Headcount: &headcount,
			Locations: []string{"Bangalore", "Remote"},
			WorkMode:  &workMode,
			Priority:  &priority,
		},
		Complete: true,
		Missing:  nil,
		// Warnings surface the stub mode so the UI can display a banner if desired.
		Warnings: []string{"stub_mode: LLM is stubbed; this response is canned. Input was: " + truncate(in.UserMessage, 80)},
	}, nil
}

// truncate caps s to maxLen runes, appending "..." if trimmed.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
