// Package llm holds infrastructure adapters for the IntentExtractor port.
// This file is the Claude (Anthropic) implementation. Each turn is a single
// Messages API call with tool-use forced via tool_choice — the model must
// call propose_draft, which gives us a strict-shape JSON patch to merge.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/shared"

	"github.com/hustle/hireflow/internal/hiringintent/application/dto"
	"github.com/hustle/hireflow/internal/hiringintent/domain/services"
	sharedanthropic "github.com/hustle/hireflow/internal/shared/infrastructure/llm/anthropic"
)

// systemPrompt is sent on every turn. Pinned in source so prompt changes go
// through code review, and so the prompt-cache prefix stays stable across
// requests (any byte change here invalidates every recruiter's cache).
const systemPrompt = `You are an assistant helping a recruiter capture a structured hiring intent through conversation. The recruiter will describe a role in their own words.

Your job each turn:
1. Read the current draft and the conversation so far.
2. Extract any new information from the recruiter's latest message.
3. Call the propose_draft tool with ONLY the fields you are updating this turn. Do not echo unchanged fields. If the recruiter is asking a question or you have nothing to update, call the tool with an empty object {}.
4. After the tool call, write a brief, friendly reply (1-2 sentences). If any required field is still missing, ask about ONE missing field — do not list everything at once. If all required fields are present, set complete=true and confirm readiness.

Required fields for a complete draft: role_title, at least one required skill, min_years, max_years, headcount, work_mode, priority.

Constraints:
- Headcount must be a positive integer.
- min_years <= max_years; both >= 0.
- work_mode is one of: ONSITE, REMOTE, HYBRID.
- priority is one of: LOW, MEDIUM, HIGH, CRITICAL.
- Never invent details the recruiter has not said. If unsure, ask.`

// toolName is the only tool the model may call.
const toolName = "propose_draft"

// AnthropicExtractor implements services.IntentExtractor against Claude.
type AnthropicExtractor struct {
	client *sharedanthropic.Client
}

// NewAnthropicExtractor wires the adapter.
func NewAnthropicExtractor(client *sharedanthropic.Client) *AnthropicExtractor {
	return &AnthropicExtractor{client: client}
}

// Extract executes one extraction turn against Claude.
func (e *AnthropicExtractor) Extract(ctx context.Context, in dto.ExtractInput) (dto.ExtractOutput, error) {
	params := e.buildParams(in)
	resp, err := e.client.SDK().Messages.New(ctx, params)
	if err != nil {
		return dto.ExtractOutput{}, classifyError(err)
	}
	return parseResponse(resp)
}

// classifyError maps SDK / network errors onto the ErrLLM* sentinels so the
// HTTP handler can pick a specific status + message. The original error is
// preserved in the chain via fmt.Errorf("%w") for logs.
func classifyError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %v", services.ErrLLMTimeout, err)
	}

	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		// Network / DNS / protocol error — no HTTP response.
		return fmt.Errorf("%w: %v", services.ErrLLMUpstream, err)
	}

	// Anthropic returns invalid_request_error (400) for billing rather than
	// the dedicated billing_error type, so message-sniff as a fallback.
	body := apiErr.RawJSON()
	if apiErr.Type() == shared.ErrorTypeBillingError ||
		(apiErr.Type() == shared.ErrorTypeInvalidRequestError &&
			(strings.Contains(body, "credit balance") || strings.Contains(body, "Plans & Billing"))) {
		return fmt.Errorf("%w: %v", services.ErrLLMBilling, err)
	}

	switch apiErr.Type() {
	case shared.ErrorTypeAuthenticationError:
		return fmt.Errorf("%w: %v", services.ErrLLMAuth, err)
	case shared.ErrorTypePermissionError:
		return fmt.Errorf("%w: %v", services.ErrLLMPermission, err)
	case shared.ErrorTypeRateLimitError:
		return fmt.Errorf("%w: %v", services.ErrLLMRateLimit, err)
	case shared.ErrorTypeOverloadedError:
		return fmt.Errorf("%w: %v", services.ErrLLMOverloaded, err)
	case shared.ErrorTypeTimeoutError:
		return fmt.Errorf("%w: %v", services.ErrLLMTimeout, err)
	}

	// Status-code fallback for response shapes the SDK didn't classify.
	switch apiErr.StatusCode {
	case 401:
		return fmt.Errorf("%w: %v", services.ErrLLMAuth, err)
	case 403:
		return fmt.Errorf("%w: %v", services.ErrLLMPermission, err)
	case 429:
		return fmt.Errorf("%w: %v", services.ErrLLMRateLimit, err)
	case 529:
		return fmt.Errorf("%w: %v", services.ErrLLMOverloaded, err)
	}
	return fmt.Errorf("%w: %v", services.ErrLLMUpstream, err)
}

// buildParams assembles the Messages API request. Kept pure (no network) so
// it can be unit-tested in isolation.
func (e *AnthropicExtractor) buildParams(in dto.ExtractInput) anthropic.MessageNewParams {
	msgs := make([]anthropic.MessageParam, 0, len(in.Messages)+2)
	if !in.Draft.IsEmpty() {
		// Inject the current draft as a synthetic user-turn note so the model
		// always sees what's already filled. Keeping it inside the messages
		// array (rather than the system prompt) preserves cache stability.
		raw, _ := json.Marshal(in.Draft)
		msgs = append(msgs, anthropic.NewUserMessage(
			anthropic.NewTextBlock("Current draft state: "+string(raw)),
		))
	}
	for _, m := range in.Messages {
		switch m.Role {
		case "user":
			msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Text)))
		case "assistant":
			msgs = append(msgs, anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Text)))
		}
	}
	msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(in.UserMessage)))

	tool := anthropic.ToolParam{
		Name:        toolName,
		Description: anthropic.String("Propose changes to the hiring intent draft this turn."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: toolInputSchemaProperties(),
		},
	}

	disabledThinking := anthropic.NewThinkingConfigDisabledParam()
	return anthropic.MessageNewParams{
		Model:     anthropic.Model(e.client.Model()),
		MaxTokens: 1024,
		System: []anthropic.TextBlockParam{{
			Text:         systemPrompt,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}},
		Thinking:   anthropic.ThinkingConfigParamUnion{OfDisabled: &disabledThinking},
		Tools:      []anthropic.ToolUnionParam{{OfTool: &tool}},
		ToolChoice: anthropic.ToolChoiceParamOfTool(toolName),
		Messages:   msgs,
	}
}

// toolInputSchemaProperties is the JSON Schema for propose_draft. Centralized
// so the schema, the parsing code, and the validation code all stay aligned.
func toolInputSchemaProperties() map[string]any {
	return map[string]any{
		"role_title": map[string]any{"type": "string"},
		"skills": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":     map[string]any{"type": "string"},
					"required": map[string]any{"type": "boolean"},
				},
				"required": []string{"name", "required"},
			},
		},
		"min_years": map[string]any{"type": "integer", "minimum": 0},
		"max_years": map[string]any{"type": "integer", "minimum": 0},
		"headcount": map[string]any{"type": "integer", "minimum": 1},
		"locations": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "string"},
		},
		"work_mode": map[string]any{
			"type": "string",
			"enum": []string{"ONSITE", "REMOTE", "HYBRID"},
		},
		"priority": map[string]any{
			"type": "string",
			"enum": []string{"LOW", "MEDIUM", "HIGH", "CRITICAL"},
		},
		"budget": map[string]any{
			"type": "object",
			"description": "Salary band. min_minor and max_minor are integer amounts in the smallest currency unit (paise for INR, cents for USD). Example: 60 LPA INR is 600000000 paise. Currency is a 3-letter ISO 4217 code (INR, USD, EUR, GBP, ...).",
			"properties": map[string]any{
				"min_minor": map[string]any{"type": "integer", "minimum": 0},
				"max_minor": map[string]any{"type": "integer", "minimum": 0},
				"currency":  map[string]any{"type": "string", "minLength": 3, "maxLength": 3},
			},
			"required": []string{"min_minor", "max_minor", "currency"},
		},
		"reason": map[string]any{
			"type":        "string",
			"description": "Why the role exists in 1-2 sentences (backfill, growth, new product, etc). Free text. Set only if the recruiter mentions a reason.",
			"maxLength":   500,
		},
		"team": map[string]any{
			"type":        "string",
			"description": "The team / squad / pod the hire joins (e.g. 'Payments Platform', 'Growth — Onboarding'). Set only if mentioned.",
			"maxLength":   500,
		},
		"reports_to": map[string]any{
			"type":        "string",
			"description": "Hiring manager or reporting line. Either a name or a title (e.g. 'Aisha Khan, VP Engineering' or 'Director of Product'). Set only if mentioned.",
			"maxLength":   500,
		},
		"reply": map[string]any{
			"type":        "string",
			"description": "1-2 sentence natural-language reply to the recruiter.",
		},
		"complete": map[string]any{"type": "boolean"},
		"missing": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "string"},
		},
	}
}

// toolPayload mirrors the schema above. Used to JSON-decode the tool input.
type toolPayload struct {
	RoleTitle *string          `json:"role_title,omitempty"`
	Skills    []dto.SkillPatch `json:"skills,omitempty"`
	MinYears  *int             `json:"min_years,omitempty"`
	MaxYears  *int             `json:"max_years,omitempty"`
	Headcount *int             `json:"headcount,omitempty"`
	Locations []string         `json:"locations,omitempty"`
	WorkMode  *string          `json:"work_mode,omitempty"`
	Priority  *string          `json:"priority,omitempty"`
	Budget    *dto.BudgetPatch `json:"budget,omitempty"`
	Reason    *string          `json:"reason,omitempty"`
	Team      *string          `json:"team,omitempty"`
	ReportsTo *string          `json:"reports_to,omitempty"`
	Reply     string           `json:"reply,omitempty"`
	Complete  bool             `json:"complete,omitempty"`
	Missing   []string         `json:"missing,omitempty"`
}

// parseResponse pulls the propose_draft tool call out of the model's reply.
// We require the tool call (forced by tool_choice) and prefer reply text from
// inside the tool payload; we fall back to any sibling text block if the
// model omitted reply.
func parseResponse(resp *anthropic.Message) (dto.ExtractOutput, error) {
	var (
		toolFound bool
		payload   toolPayload
		fallback  string
	)
	for _, block := range resp.Content {
		switch v := block.AsAny().(type) {
		case anthropic.ToolUseBlock:
			if v.Name != toolName {
				continue
			}
			raw := v.JSON.Input.Raw()
			if raw == "" {
				return dto.ExtractOutput{}, fmt.Errorf("%w: empty tool input", services.ErrLLMResponseShape)
			}
			if err := json.Unmarshal([]byte(raw), &payload); err != nil {
				return dto.ExtractOutput{}, fmt.Errorf("%w: parse tool input: %v", services.ErrLLMResponseShape, err)
			}
			toolFound = true
		case anthropic.TextBlock:
			fallback = v.Text
		}
	}
	if !toolFound {
		return dto.ExtractOutput{}, fmt.Errorf("%w: model did not call propose_draft", services.ErrLLMResponseShape)
	}

	reply := payload.Reply
	if reply == "" {
		reply = fallback
	}

	return dto.ExtractOutput{
		Reply: reply,
		Patch: dto.DraftPatch{
			RoleTitle: payload.RoleTitle,
			Skills:    payload.Skills,
			MinYears:  payload.MinYears,
			MaxYears:  payload.MaxYears,
			Headcount: payload.Headcount,
			Locations: payload.Locations,
			WorkMode:  payload.WorkMode,
			Priority:  payload.Priority,
			Budget:    payload.Budget,
			Reason:    payload.Reason,
			Team:      payload.Team,
			ReportsTo: payload.ReportsTo,
		},
		Complete: payload.Complete,
		Missing:  payload.Missing,
	}, nil
}
