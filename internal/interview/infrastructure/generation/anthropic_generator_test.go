package generation_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/interview/domain/services"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	"github.com/hustle/hireflow/internal/interview/infrastructure/generation"
	sharedanthropic "github.com/hustle/hireflow/internal/shared/infrastructure/llm/anthropic"
)

// generatorRoundTripper substitutes a canned HTTP response for Anthropic SDK
// calls without hitting the network. Mirrors the pattern from
// internal/sourcing/infrastructure/parsing/anthropic_parser_test.go.
type generatorRoundTripper struct {
	resp   string
	status int
}

func (r *generatorRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	status := r.status
	if status == 0 {
		status = 200
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(r.resp)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

// newGenerator constructs an AnthropicQuestionGenerator backed by the fake transport.
func newGenerator(rt http.RoundTripper) *generation.AnthropicQuestionGenerator {
	c := sharedanthropic.NewClient(sharedanthropic.Config{
		APIKey:     "sk-test",
		Model:      "claude-opus-4-7",
		HTTPClient: &http.Client{Transport: rt},
	})
	return generation.NewAnthropicQuestionGenerator(c.SDK(), c.Model())
}

// sampleGenerationInput builds a minimal GenerationInput for adapter tests.
func sampleGenerationInput() services.GenerationInput {
	return services.GenerationInput{
		RoundKind: vo.RoundKindTechnical,
		RoleSpec: services.RoleSpec{
			Title:  "Senior Backend Engineer",
			Skills: []services.SkillRequirement{{Name: "Go", Required: true}},
		},
		CandidateProfile: services.CandidateProfile{
			Headline: "Backend Engineer",
			Skills:   []string{"Go"},
		},
	}
}

// validQuestionsResponse wraps a single valid question in an Anthropic Messages
// API text-block response. The text field value is the JSON array as a plain
// (non-fenced) string.
const validQuestionsResponse = `{
  "id": "msg_gen_01",
  "type": "message",
  "role": "assistant",
  "model": "claude-opus-4-7",
  "stop_reason": "end_turn",
  "content": [
    {
      "type": "text",
      "text": "[{\"prompt\":\"Walk me through how you would design a rate limiter in Go.\",\"skill_probed\":\"Go\",\"why\":\"Candidate claims 5 years of Go at Razorpay building high-throughput systems.\",\"expected_signals\":[\"token bucket vs leaky bucket trade-offs\",\"concurrency safety with sync primitives\",\"Redis-backed distributed variant\"],\"model_answer\":\"A strong answer names at least two algorithms (token bucket for bursty traffic, sliding window for smoothing), implements the local variant with a sync.Mutex or atomic counter, and then extends to Redis with INCR+EXPIRE for distributed enforcement.\",\"red_flags\":[\"copy-pastes a library without explaining internals\",\"no mention of concurrency safety\"],\"follow_ups\":[\"How would you test this under concurrent load?\"]}]"
    }
  ],
  "usage": {"input_tokens": 300, "output_tokens": 150}
}`

// fencedQuestionsResponse wraps the same valid JSON in markdown code fences.
// The newline characters are escaped as \n within the JSON string value.
const fencedQuestionsResponse = `{
  "id": "msg_gen_02",
  "type": "message",
  "role": "assistant",
  "model": "claude-opus-4-7",
  "stop_reason": "end_turn",
  "content": [
    {
      "type": "text",
      "text": "` + "```json\\n[{\\\"prompt\\\":\\\"Walk me through how you would design a rate limiter in Go.\\\",\\\"skill_probed\\\":\\\"Go\\\",\\\"why\\\":\\\"Candidate claims 5 years of Go at Razorpay building high-throughput systems.\\\",\\\"expected_signals\\\":[\\\"token bucket vs leaky bucket trade-offs\\\",\\\"concurrency safety with sync primitives\\\",\\\"Redis-backed distributed variant\\\"],\\\"model_answer\\\":\\\"A strong answer names at least two algorithms.\\\",\\\"red_flags\\\":[\\\"copy-pastes a library without explaining internals\\\",\\\"no mention of concurrency safety\\\"],\\\"follow_ups\\\":[\\\"How would you test this under concurrent load?\\\"]}]\\n```" + `"
    }
  ],
  "usage": {"input_tokens": 300, "output_tokens": 160}
}`

// emptyTextResponse has a text block with only whitespace — triggers empty-response error.
const emptyTextResponse = `{
  "id": "msg_gen_03",
  "type": "message",
  "role": "assistant",
  "model": "claude-opus-4-7",
  "stop_reason": "end_turn",
  "content": [
    {
      "type": "text",
      "text": "   "
    }
  ],
  "usage": {"input_tokens": 100, "output_tokens": 5}
}`

// malformedJSONTextResponse contains a text block with invalid JSON.
const malformedJSONTextResponse = `{
  "id": "msg_gen_04",
  "type": "message",
  "role": "assistant",
  "model": "claude-opus-4-7",
  "stop_reason": "end_turn",
  "content": [
    {
      "type": "text",
      "text": "this is not json"
    }
  ],
  "usage": {"input_tokens": 100, "output_tokens": 10}
}`

// missingModelAnswerResponse has a question array where model_answer is absent.
const missingModelAnswerResponse = `{
  "id": "msg_gen_05",
  "type": "message",
  "role": "assistant",
  "model": "claude-opus-4-7",
  "stop_reason": "end_turn",
  "content": [
    {
      "type": "text",
      "text": "[{\"prompt\":\"What is Go?\",\"skill_probed\":\"Go\",\"why\":\"candidate knows Go\",\"expected_signals\":[\"sig1\",\"sig2\",\"sig3\"],\"red_flags\":[\"flag1\",\"flag2\"],\"follow_ups\":[\"tell me more\"]}]"
    }
  ],
  "usage": {"input_tokens": 100, "output_tokens": 20}
}`

// Test 1: Happy path — valid JSON text block returns parsed Question slice.
func TestAnthropicQuestionGenerator_HappyPath(t *testing.T) {
	rt := &generatorRoundTripper{resp: validQuestionsResponse}
	g := newGenerator(rt)

	questions, err := g.Generate(context.Background(), sampleGenerationInput())
	require.NoError(t, err)
	require.Len(t, questions, 1)

	q := questions[0]
	assert.Equal(t, "Walk me through how you would design a rate limiter in Go.", q.Prompt)
	assert.Equal(t, "Go", q.SkillProbed)
	assert.NotEmpty(t, q.Why)
	require.Len(t, q.ExpectedSignals, 3)
	assert.NotEmpty(t, q.ModelAnswer)
	require.Len(t, q.RedFlags, 2)
	require.Len(t, q.FollowUps, 1)
}

// Test 2: Fenced JSON — adapter strips ``` fences and parses successfully.
func TestAnthropicQuestionGenerator_FencedJSON_StripsAndParses(t *testing.T) {
	rt := &generatorRoundTripper{resp: fencedQuestionsResponse}
	g := newGenerator(rt)

	questions, err := g.Generate(context.Background(), sampleGenerationInput())
	require.NoError(t, err, "fenced JSON must be stripped and parsed without error")
	assert.Len(t, questions, 1)
}

// Test 3: Empty/whitespace-only text response → ErrInvalidLLMOutput.
func TestAnthropicQuestionGenerator_EmptyResponse_ReturnsErrInvalidLLMOutput(t *testing.T) {
	rt := &generatorRoundTripper{resp: emptyTextResponse}
	g := newGenerator(rt)

	_, err := g.Generate(context.Background(), sampleGenerationInput())
	require.Error(t, err)
	assert.True(t, errors.Is(err, generation.ErrInvalidLLMOutput),
		"empty response must wrap ErrInvalidLLMOutput, got: %v", err)
}

// Test 4: Malformed JSON text → ErrInvalidLLMOutput.
func TestAnthropicQuestionGenerator_MalformedJSON_ReturnsErrInvalidLLMOutput(t *testing.T) {
	rt := &generatorRoundTripper{resp: malformedJSONTextResponse}
	g := newGenerator(rt)

	_, err := g.Generate(context.Background(), sampleGenerationInput())
	require.Error(t, err)
	assert.True(t, errors.Is(err, generation.ErrInvalidLLMOutput),
		"malformed JSON must wrap ErrInvalidLLMOutput, got: %v", err)
}

// Test 5: Question failing Validate (missing model_answer) → ErrInvalidLLMOutput.
func TestAnthropicQuestionGenerator_ValidationFails_ReturnsErrInvalidLLMOutput(t *testing.T) {
	rt := &generatorRoundTripper{resp: missingModelAnswerResponse}
	g := newGenerator(rt)

	_, err := g.Generate(context.Background(), sampleGenerationInput())
	require.Error(t, err)
	assert.True(t, errors.Is(err, generation.ErrInvalidLLMOutput),
		"validation failure must wrap ErrInvalidLLMOutput, got: %v", err)
}

// Test 6: HTTP 401 → ErrLLMAuthFailed.
func TestAnthropicQuestionGenerator_HTTP401_ReturnsErrLLMAuthFailed(t *testing.T) {
	const errorResp = `{
  "type": "error",
  "error": {"type": "authentication_error", "message": "invalid x-api-key"}
}`
	rt := &generatorRoundTripper{resp: errorResp, status: 401}
	g := newGenerator(rt)

	_, err := g.Generate(context.Background(), sampleGenerationInput())
	require.Error(t, err)
	assert.True(t, errors.Is(err, generation.ErrLLMAuthFailed),
		"HTTP 401 must wrap ErrLLMAuthFailed, got: %v", err)
}

// Test 7: trimJSONFences unit — raw JSON is returned unchanged.
func TestTrimJSONFences_RawJSON_Unchanged(t *testing.T) {
	// trimJSONFences is exercised indirectly by the happy-path and fenced tests.
	// This extra unit-level check exercises the "no fences" branch via the
	// generator's full parse path.
	rt := &generatorRoundTripper{resp: validQuestionsResponse}
	g := newGenerator(rt)

	questions, err := g.Generate(context.Background(), sampleGenerationInput())
	require.NoError(t, err)
	assert.NotEmpty(t, questions)
}
