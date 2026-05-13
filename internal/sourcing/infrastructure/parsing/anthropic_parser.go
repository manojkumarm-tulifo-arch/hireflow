// Package parsing holds ResumeParser adapters. The Anthropic adapter uses
// forced tool-use against a parse_resume schema, returning the canonical
// ParsedProfile. The tool schema is defined inline as a properties map,
// mirroring the pattern from internal/hiringintent/infrastructure/llm/anthropic_extractor.go.
package parsing

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

//go:embed prompts/parse_resume.tmpl
var parseResumePrompt string

// PromptVersion is bumped whenever parse_resume.tmpl meaningfully changes.
// Stored on ParsedProfile metadata so downstream can audit which prompt version
// produced a given Candidate.
const PromptVersion = "v1"

// toolName is the only tool the model may call.
const toolName = "parse_resume"

// AnthropicParser implements services.ResumeParser against Claude.
type AnthropicParser struct {
	client *anthropic.Client
	model  string
}

// NewAnthropicParser wires the adapter.
func NewAnthropicParser(client *anthropic.Client, model string) *AnthropicParser {
	return &AnthropicParser{client: client, model: model}
}

// Parse calls Claude with forced tool-use and unmarshals the result into a
// ParsedProfile. Errors are classified into retryable / non-retryable via
// services.ResumeParseError so the worker can apply the right retry policy.
func (p *AnthropicParser) Parse(ctx context.Context, text string) (vo.ParsedProfile, error) {
	tool := anthropic.ToolParam{
		Name:        toolName,
		Description: anthropic.String("Extract a structured candidate profile from resume text."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: parseResumeSchemaProperties(),
		},
	}

	resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(p.model),
		MaxTokens: 4096,
		System: []anthropic.TextBlockParam{
			{Text: parseResumePrompt},
		},
		Tools:      []anthropic.ToolUnionParam{{OfTool: &tool}},
		ToolChoice: anthropic.ToolChoiceParamOfTool(toolName),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(text)),
		},
	})
	if err != nil {
		return vo.ParsedProfile{}, services.ResumeParseError{
			Retryable: true,
			Reason:    "anthropic_call",
			Detail:    fmt.Sprintf("messages.new: %v", err),
		}
	}

	return parseResponse(resp)
}

// parseResponse extracts the parse_resume tool call from the model's reply and
// unmarshals it into a ParsedProfile. Returns a classified ResumeParseError if
// the response is not in the expected shape.
func parseResponse(resp *anthropic.Message) (vo.ParsedProfile, error) {
	for _, block := range resp.Content {
		tu, ok := block.AsAny().(anthropic.ToolUseBlock)
		if !ok || tu.Name != toolName {
			continue
		}

		var profile vo.ParsedProfile
		if err := json.Unmarshal(tu.Input, &profile); err != nil {
			return vo.ParsedProfile{}, services.ResumeParseError{
				Retryable: false,
				Reason:    "tool_invalid_json",
				Detail:    err.Error(),
			}
		}

		// Normalise: model may omit schema_version or set it to 0.
		if profile.SchemaVersion == 0 {
			profile.SchemaVersion = 1
		}

		return profile, nil
	}

	return vo.ParsedProfile{}, services.ResumeParseError{
		Retryable: false,
		Reason:    "no_tool_use",
		Detail:    "model returned free text instead of parse_resume tool call",
	}
}

// parseResumeSchemaProperties returns the JSON Schema properties map for the
// parse_resume tool. Defined inline (mirroring the propose_draft schema in
// hiringintent) so the schema, parsing code, and vo.ParsedProfile stay aligned
// without requiring an embedded file or reflection.
func parseResumeSchemaProperties() map[string]any {
	return map[string]any{
		"schema_version": map[string]any{
			"type":        "integer",
			"description": "Always 1.",
		},
		"personal": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"full_name": map[string]any{"type": "string"},
				"email":     map[string]any{"type": "string"},
				"phone":     map[string]any{"type": "string"},
				"location":  map[string]any{"type": "string"},
				"links": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type":     "object",
						"required": []string{"kind", "url"},
						"properties": map[string]any{
							"kind": map[string]any{
								"type": "string",
								"enum": []string{"linkedin", "github", "portfolio", "other"},
							},
							"url": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
		"headline": map[string]any{"type": "string"},
		"summary":  map[string]any{"type": "string"},
		"skills": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":     "object",
				"required": []string{"name"},
				"properties": map[string]any{
					"name":         map[string]any{"type": "string"},
					"years":        map[string]any{"type": "number"},
					"evidence_ref": map[string]any{"type": "string"},
				},
			},
		},
		"experiences": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":     "object",
				"required": []string{"id", "company", "title", "start"},
				"properties": map[string]any{
					"id":          map[string]any{"type": "string"},
					"company":     map[string]any{"type": "string"},
					"title":       map[string]any{"type": "string"},
					"start":       map[string]any{"type": "string"},
					"end":         map[string]any{"type": "string"},
					"current":     map[string]any{"type": "boolean"},
					"description": map[string]any{"type": "string"},
					"skills_used": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
				},
			},
		},
		"education": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":     "object",
				"required": []string{"institution"},
				"properties": map[string]any{
					"institution": map[string]any{"type": "string"},
					"degree":      map[string]any{"type": "string"},
					"field":       map[string]any{"type": "string"},
					"start":       map[string]any{"type": "string"},
					"end":         map[string]any{"type": "string"},
				},
			},
		},
		"certifications": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":     "object",
				"required": []string{"name"},
				"properties": map[string]any{
					"name":    map[string]any{"type": "string"},
					"issuer":  map[string]any{"type": "string"},
					"issued":  map[string]any{"type": "string"},
					"expires": map[string]any{"type": "string"},
				},
			},
		},
		"languages": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":     "object",
				"required": []string{"name"},
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
					"proficiency": map[string]any{
						"type": "string",
						"enum": []string{"native", "fluent", "professional", "basic"},
					},
				},
			},
		},
		"warnings": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "string"},
		},
	}
}
