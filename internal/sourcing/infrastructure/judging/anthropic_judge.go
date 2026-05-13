// Package judging holds LLMJudge adapters. The Anthropic adapter uses
// forced tool-use against a judge_match schema, returning the canonical
// LLMJudgment. The tool schema is defined inline as a properties map,
// mirroring the pattern from internal/sourcing/infrastructure/parsing/anthropic_parser.go.
package judging

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

//go:embed prompts/judge_match.tmpl
var judgeMatchPrompt string

// PromptVersion is stamped onto every LLMJudgment produced by this adapter.
// Bump it whenever judge_match.tmpl meaningfully changes so historical scores
// can be traced back to the prompt that produced them.
const PromptVersion = "v1"

// judgeToolName is the only tool the model may call.
const judgeToolName = "judge_match"

// AnthropicJudge implements services.LLMJudge against Claude using forced
// tool-use. PII is stripped from the profile before the request leaves the
// process — the model only sees professional information.
type AnthropicJudge struct {
	client *anthropic.Client
	model  string
}

// NewAnthropicJudge wires the adapter.
func NewAnthropicJudge(client *anthropic.Client, model string) *AnthropicJudge {
	return &AnthropicJudge{client: client, model: model}
}

// Judge evaluates profile against role, grounding claims in the profile's
// experience prose. rules provides per-criterion pass/fail context so the
// model can focus on borderline or failed criteria.
//
// Error classification:
//   - SDK error              → services.JudgeError{Retryable: true,  Reason: "anthropic_call"}
//   - No tool-use block      → services.JudgeError{Retryable: false, Reason: "no_tool_use"}
//   - Invalid JSON in input  → services.JudgeError{Retryable: false, Reason: "tool_invalid_json"}
func (j *AnthropicJudge) Judge(
	ctx context.Context,
	profile vo.ParsedProfile,
	role services.RoleSpec,
	rules vo.RuleMatchReport,
) (vo.LLMJudgment, error) {
	// 1. Strip PII — the model never needs personal contact details.
	sanitised := profile
	sanitised.Personal.FullName = ""
	sanitised.Personal.Email = ""
	sanitised.Personal.Phone = ""

	// 2. Build the user message: JSON-serialised sanitised profile + role + rules.
	userMsg, err := buildUserMessage(sanitised, role, rules)
	if err != nil {
		return vo.LLMJudgment{}, services.JudgeError{
			Retryable: false,
			Reason:    "serialise_input",
			Detail:    fmt.Sprintf("marshal user message: %v", err),
		}
	}

	// 3. Forced tool-use call.
	tool := anthropic.ToolParam{
		Name:        judgeToolName,
		Description: anthropic.String("Produce a structured judgment of candidate fit against the role specification."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: judgeMatchSchemaProperties(),
		},
	}

	resp, err := j.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(j.model),
		MaxTokens: 2048,
		System: []anthropic.TextBlockParam{
			{Text: judgeMatchPrompt},
		},
		Tools:      []anthropic.ToolUnionParam{{OfTool: &tool}},
		ToolChoice: anthropic.ToolChoiceParamOfTool(judgeToolName),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userMsg)),
		},
	})
	if err != nil {
		return vo.LLMJudgment{}, services.JudgeError{
			Retryable: true,
			Reason:    "anthropic_call",
			Detail:    fmt.Sprintf("messages.new: %v", err),
		}
	}

	// 4. Extract the tool-use block from the response.
	return parseJudgeResponse(resp)
}

// parseJudgeResponse extracts the judge_match tool call and unmarshals it
// into an LLMJudgment. Returns a classified JudgeError if the response shape
// is unexpected.
func parseJudgeResponse(resp *anthropic.Message) (vo.LLMJudgment, error) {
	for _, block := range resp.Content {
		tu, ok := block.AsAny().(anthropic.ToolUseBlock)
		if !ok || tu.Name != judgeToolName {
			continue
		}

		var judgment vo.LLMJudgment
		if err := json.Unmarshal(tu.Input, &judgment); err != nil {
			return vo.LLMJudgment{}, services.JudgeError{
				Retryable: false,
				Reason:    "tool_invalid_json",
				Detail:    err.Error(),
			}
		}

		judgment.PromptVersion = PromptVersion
		return judgment, nil
	}

	return vo.LLMJudgment{}, services.JudgeError{
		Retryable: false,
		Reason:    "no_tool_use",
		Detail:    "model returned free text instead of judge_match tool call",
	}
}

// judgeInputMessage is the serialised payload sent as the user turn.
type judgeInputMessage struct {
	Profile vo.ParsedProfile  `json:"profile"`
	Role    services.RoleSpec `json:"role"`
	Rules   vo.RuleMatchReport `json:"rule_match"`
}

// buildUserMessage serialises the three inputs into a single JSON string that
// the model receives as its user-turn message.
func buildUserMessage(profile vo.ParsedProfile, role services.RoleSpec, rules vo.RuleMatchReport) (string, error) {
	payload := judgeInputMessage{
		Profile: profile,
		Role:    role,
		Rules:   rules,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// judgeMatchSchemaProperties returns the JSON Schema properties map for the
// judge_match tool. The schema mirrors vo.LLMJudgment's JSON shape so the
// model's output can be unmarshalled directly.
func judgeMatchSchemaProperties() map[string]any {
	return map[string]any{
		"score": map[string]any{
			"type":        "integer",
			"minimum":     0,
			"maximum":     100,
			"description": "Overall fit score from 0 (no fit) to 100 (exceptional fit).",
		},
		"evidence": map[string]any{
			"type":        "array",
			"description": "Grounded evidence items supporting the score. Each item must cite specific text from the candidate profile.",
			"items": map[string]any{
				"type":     "object",
				"required": []string{"kind", "support"},
				"properties": map[string]any{
					"kind": map[string]any{
						"type":        "string",
						"enum":        []string{"skill", "experience"},
						"description": "Whether this evidence relates to a skill or an experience entry.",
					},
					"skill": map[string]any{
						"type":        "string",
						"description": "For kind=skill: the skill name being evidenced.",
					},
					"claim": map[string]any{
						"type":        "string",
						"description": "The claim being supported (e.g. '5 years Go experience').",
					},
					"support": map[string]any{
						"type":        "string",
						"description": "A verbatim or near-verbatim excerpt from the profile that supports the claim.",
					},
				},
			},
		},
		"summary": map[string]any{
			"type":        "string",
			"description": "Exactly two sentences: first on overall fit, second on the strongest factor (positive or negative).",
		},
		"concerns": map[string]any{
			"type":        "array",
			"description": "Short-code concerns for the recruiter (e.g. 'gap_2022_to_2023', 'unsupported_skill:Rust'). Empty if none.",
			"items":       map[string]any{"type": "string"},
		},
	}
}
