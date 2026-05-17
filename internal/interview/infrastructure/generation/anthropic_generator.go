package generation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"github.com/hustle/hireflow/internal/interview/domain/services"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

// ErrInvalidLLMOutput is returned when the LLM response cannot be parsed
// into the Question schema. Caller maps this to FailureKindInvalidJSON.
var ErrInvalidLLMOutput = errors.New("interview: invalid LLM output")

// ErrLLMAuthFailed is returned on Anthropic auth/permission errors. Caller
// maps this to FailureKindLLMAuth and aborts the round.
var ErrLLMAuthFailed = errors.New("interview: LLM auth failed")

// AnthropicQuestionGenerator generates interview questions via Anthropic.
type AnthropicQuestionGenerator struct {
	client *anthropic.Client
	model  string
}

var _ services.QuestionGenerator = (*AnthropicQuestionGenerator)(nil)

// NewAnthropicQuestionGenerator wires the adapter.
func NewAnthropicQuestionGenerator(client *anthropic.Client, model string) *AnthropicQuestionGenerator {
	return &AnthropicQuestionGenerator{client: client, model: model}
}

// Generate calls Anthropic and returns the parsed Question slice.
//
// Error classification:
//   - Anthropic call fails with auth/permission → ErrLLMAuthFailed
//   - Empty / malformed / validation-failing output → ErrInvalidLLMOutput
//   - Other SDK failures → returned as-is (caller treats as transient)
func (g *AnthropicQuestionGenerator) Generate(ctx context.Context, in services.GenerationInput) ([]vo.Question, error) {
	system, user := BuildPrompt(in)
	resp, err := g.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(g.model),
		MaxTokens: 4000,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
	})
	if err != nil {
		// Classify auth errors so the caller can abort rather than retry.
		// Pattern from sourcing.AnthropicJudge: inspect the error string for
		// HTTP status codes and Anthropic error type names.
		es := err.Error()
		if strings.Contains(es, "401") || strings.Contains(es, "403") ||
			strings.Contains(es, "authentication_error") ||
			strings.Contains(es, "permission_error") {
			return nil, fmt.Errorf("%w: %v", ErrLLMAuthFailed, err)
		}
		return nil, fmt.Errorf("anthropic call: %w", err)
	}

	// Extract text from response content blocks.
	var text string
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			text += tb.Text
		}
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("%w: empty response", ErrInvalidLLMOutput)
	}

	text = trimJSONFences(text)

	var questions []vo.Question
	if err := json.Unmarshal([]byte(text), &questions); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidLLMOutput, err)
	}
	if len(questions) == 0 {
		return nil, fmt.Errorf("%w: empty array", ErrInvalidLLMOutput)
	}
	for i, q := range questions {
		if err := q.Validate(); err != nil {
			return nil, fmt.Errorf("%w: question %d: %v", ErrInvalidLLMOutput, i, err)
		}
	}
	return questions, nil
}

// trimJSONFences strips optional markdown code fences from the LLM response.
// Handles both raw JSON and ```json...``` or ```...``` fenced output.
func trimJSONFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if newline := strings.Index(s, "\n"); newline >= 0 {
			s = s[newline+1:]
		}
		if strings.HasSuffix(s, "```") {
			s = s[:len(s)-3]
		}
	}
	return strings.TrimSpace(s)
}
