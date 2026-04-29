package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hustle/hireflow/internal/hiringintent/application/dto"
	"github.com/hustle/hireflow/internal/hiringintent/domain/entities"
	"github.com/hustle/hireflow/internal/hiringintent/domain/services"
)

// ErrUserMessageRequired is returned when the input has no current turn.
var ErrUserMessageRequired = errors.New("extract: user_message is required")

// ErrUserMessageTooLong is returned when the current turn exceeds the limit.
var ErrUserMessageTooLong = errors.New("extract: user_message exceeds 4000 chars")

// MaxUserMessageChars caps a single recruiter turn. Cheap abuse prevention
// — well above any real recruiter input but below model context cost.
const MaxUserMessageChars = 4000

// MaxHistoryTurns truncates older messages server-side so a runaway chat
// can't blow up token cost. Keeps the most recent N entries.
const MaxHistoryTurns = 20

// ExtractIntentHandler orchestrates one extraction turn: validates input,
// calls the IntentExtractor port, then runs domain validation on the
// returned patch (dropping invalid fields into warnings rather than
// failing the whole turn — the LLM is treated as untrusted).
type ExtractIntentHandler struct {
	extractor services.IntentExtractor
}

// NewExtractIntentHandler wires the handler.
func NewExtractIntentHandler(extractor services.IntentExtractor) *ExtractIntentHandler {
	return &ExtractIntentHandler{extractor: extractor}
}

// Handle executes the use case.
func (h *ExtractIntentHandler) Handle(ctx context.Context, in dto.ExtractInput) (dto.ExtractOutput, error) {
	if in.UserMessage == "" {
		return dto.ExtractOutput{}, ErrUserMessageRequired
	}
	if len(in.UserMessage) > MaxUserMessageChars {
		return dto.ExtractOutput{}, ErrUserMessageTooLong
	}
	if len(in.Messages) > MaxHistoryTurns {
		in.Messages = in.Messages[len(in.Messages)-MaxHistoryTurns:]
	}

	out, err := h.extractor.Extract(ctx, in)
	if err != nil {
		return dto.ExtractOutput{}, fmt.Errorf("extract: %w", err)
	}

	out.Patch, out.Warnings = sanitizePatch(out.Patch, out.Warnings)
	if out.Reply == "" {
		out.Reply = "Got it."
	}
	return out, nil
}

// sanitizePatch enforces domain invariants on the extractor's proposed
// patch. Invalid values are dropped (set to nil / removed from slice) and
// recorded as warnings. The schema in the tool definition should prevent
// most of these — this is defense-in-depth against schema drift or model
// mistakes.
func sanitizePatch(p dto.DraftPatch, warnings []string) (dto.DraftPatch, []string) {
	if p.WorkMode != nil && !validWorkMode(*p.WorkMode) {
		warnings = append(warnings, fmt.Sprintf("dropped invalid work_mode %q", *p.WorkMode))
		p.WorkMode = nil
	}
	if p.Priority != nil && !validPriority(*p.Priority) {
		warnings = append(warnings, fmt.Sprintf("dropped invalid priority %q", *p.Priority))
		p.Priority = nil
	}
	if p.Headcount != nil && *p.Headcount <= 0 {
		warnings = append(warnings, fmt.Sprintf("dropped invalid headcount %d", *p.Headcount))
		p.Headcount = nil
	}
	if p.MinYears != nil && p.MaxYears != nil && *p.MinYears > *p.MaxYears {
		warnings = append(warnings, fmt.Sprintf("dropped invalid years range %d-%d", *p.MinYears, *p.MaxYears))
		p.MinYears = nil
		p.MaxYears = nil
	}
	if len(p.Skills) > 0 {
		filtered := p.Skills[:0]
		for _, s := range p.Skills {
			if s.Name == "" {
				continue
			}
			filtered = append(filtered, s)
		}
		p.Skills = filtered
	}
	p.Reason, warnings = sanitizeContextField("reason", p.Reason, warnings)
	p.Team, warnings = sanitizeContextField("team", p.Team, warnings)
	p.ReportsTo, warnings = sanitizeContextField("reports_to", p.ReportsTo, warnings)

	if p.Budget != nil {
		// Normalize currency to upper-case ISO; reject anything that isn't
		// 3 letters. Mirrors valueobjects.NewBudgetRange so the BE accepts
		// what we hand the FE.
		p.Budget.Currency = strings.ToUpper(strings.TrimSpace(p.Budget.Currency))
		switch {
		case len(p.Budget.Currency) != 3:
			warnings = append(warnings, fmt.Sprintf("dropped budget: invalid currency %q", p.Budget.Currency))
			p.Budget = nil
		case p.Budget.MinMinor < 0 || p.Budget.MaxMinor < 0:
			warnings = append(warnings, "dropped budget: amounts must be non-negative")
			p.Budget = nil
		case p.Budget.MinMinor > p.Budget.MaxMinor:
			warnings = append(warnings, fmt.Sprintf("dropped budget: min %d exceeds max %d", p.Budget.MinMinor, p.Budget.MaxMinor))
			p.Budget = nil
		}
	}
	return p, warnings
}

func validWorkMode(s string) bool {
	switch s {
	case "ONSITE", "REMOTE", "HYBRID":
		return true
	}
	return false
}

func validPriority(s string) bool {
	switch s {
	case "LOW", "MEDIUM", "HIGH", "CRITICAL":
		return true
	}
	return false
}

// sanitizeContextField trims whitespace, drops the field when it exceeds
// the domain's MaxContextFieldLen, and records a warning. Mirrors the
// per-field setters in the aggregate so the FE merge has clean values.
func sanitizeContextField(name string, raw *string, warnings []string) (*string, []string) {
	if raw == nil {
		return nil, warnings
	}
	trimmed := strings.TrimSpace(*raw)
	if len(trimmed) > entities.MaxContextFieldLen {
		warnings = append(warnings, fmt.Sprintf("dropped %s: exceeds %d chars", name, entities.MaxContextFieldLen))
		return nil, warnings
	}
	return &trimmed, warnings
}
