package parsing_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sharedanthropic "github.com/hustle/hireflow/internal/shared/infrastructure/llm/anthropic"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/parsing"
)

// roundTripper lets a test substitute a canned response for the SDK's HTTP
// call without actually hitting the network. Mirrors the pattern in
// internal/hiringintent/infrastructure/llm/anthropic_extractor_test.go.
type roundTripper struct {
	resp   string
	status int
}

func (r *roundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
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

// successResponse is a canned Messages API reply containing a parse_resume tool call.
const successResponse = `{
  "id": "msg_test",
  "type": "message",
  "role": "assistant",
  "model": "claude-opus-4-7",
  "stop_reason": "tool_use",
  "content": [
    {
      "type": "tool_use",
      "id": "toolu_test",
      "name": "parse_resume",
      "input": {
        "schema_version": 1,
        "personal": {
          "full_name": "Alice Smith",
          "email": "alice@example.com",
          "phone": "+91-9876543210",
          "location": "Bangalore"
        },
        "headline": "Senior Backend Engineer",
        "summary": "10 years building distributed systems.",
        "skills": [
          {"name": "Go", "years": 5.0},
          {"name": "Kubernetes", "years": 3.0}
        ],
        "experiences": [
          {
            "id": "exp_0",
            "company": "Razorpay",
            "title": "Senior Backend Engineer",
            "start": "2020-04",
            "end": "2025-01",
            "skills_used": ["Go", "Kubernetes"]
          }
        ],
        "education": [
          {
            "institution": "IIT Bombay",
            "degree": "B.Tech",
            "field": "Computer Science",
            "start": "2011-07",
            "end": "2015-05"
          }
        ],
        "warnings": []
      }
    }
  ],
  "usage": {"input_tokens": 200, "output_tokens": 80}
}`

// schemaVersionZeroResponse returns schema_version: 0 — adapter should normalise to 1.
const schemaVersionZeroResponse = `{
  "id": "msg_test2",
  "type": "message",
  "role": "assistant",
  "model": "claude-opus-4-7",
  "stop_reason": "tool_use",
  "content": [
    {
      "type": "tool_use",
      "id": "toolu_test2",
      "name": "parse_resume",
      "input": {
        "schema_version": 0,
        "personal": {"full_name": "Bob"},
        "headline": "Analyst"
      }
    }
  ],
  "usage": {"input_tokens": 50, "output_tokens": 20}
}`

// refusedResponse is a text-only response with no tool_use block.
const refusedResponse = `{
  "id": "msg_test3",
  "type": "message",
  "role": "assistant",
  "model": "claude-opus-4-7",
  "stop_reason": "end_turn",
  "content": [
    {
      "type": "text",
      "text": "I cannot parse this document."
    }
  ],
  "usage": {"input_tokens": 20, "output_tokens": 8}
}`

// malformedJSONResponse has a tool_use block whose input is not valid JSON.
const malformedJSONResponse = `{
  "id": "msg_test4",
  "type": "message",
  "role": "assistant",
  "model": "claude-opus-4-7",
  "stop_reason": "tool_use",
  "content": [
    {
      "type": "tool_use",
      "id": "toolu_test4",
      "name": "parse_resume",
      "input": "not-an-object"
    }
  ],
  "usage": {"input_tokens": 20, "output_tokens": 8}
}`

func newParser(rt http.RoundTripper) *parsing.AnthropicParser {
	c := sharedanthropic.NewClient(sharedanthropic.Config{
		APIKey:     "sk-test",
		Model:      "claude-opus-4-7",
		HTTPClient: &http.Client{Transport: rt},
	})
	return parsing.NewAnthropicParser(c.SDK(), c.Model())
}

// Test 1: Happy path — valid tool-use response returns a well-populated ParsedProfile.
func TestAnthropicParser_HappyPath(t *testing.T) {
	rt := &roundTripper{resp: successResponse}
	p := newParser(rt)

	profile, err := p.Parse(context.Background(), "Alice Smith's resume text")
	require.NoError(t, err)

	assert.Equal(t, 1, profile.SchemaVersion)
	assert.Equal(t, "Alice Smith", profile.Personal.FullName)
	assert.Equal(t, "alice@example.com", profile.Personal.Email)
	assert.Equal(t, "Bangalore", profile.Personal.Location)
	assert.Equal(t, "Senior Backend Engineer", profile.Headline)
	require.Len(t, profile.Skills, 2)
	assert.Equal(t, "Go", profile.Skills[0].Name)
	assert.Equal(t, 5.0, profile.Skills[0].Years)
	require.Len(t, profile.Experiences, 1)
	assert.Equal(t, "exp_0", profile.Experiences[0].ID)
	assert.Equal(t, "Razorpay", profile.Experiences[0].Company)
	require.Len(t, profile.Education, 1)
	assert.Equal(t, "IIT Bombay", profile.Education[0].Institution)
}

// Test 2: schema_version 0 in tool response — adapter normalises to 1.
func TestAnthropicParser_SchemaVersionZeroNormalisedToOne(t *testing.T) {
	rt := &roundTripper{resp: schemaVersionZeroResponse}
	p := newParser(rt)

	profile, err := p.Parse(context.Background(), "Bob's resume")
	require.NoError(t, err)

	assert.Equal(t, 1, profile.SchemaVersion, "adapter must normalise schema_version 0 → 1")
	assert.Equal(t, "Bob", profile.Personal.FullName)
}

// Test 3: model refuses to call tool — adapter returns non-retryable ResumeParseError.
func TestAnthropicParser_RefusedParse_ReturnsNoToolUseError(t *testing.T) {
	rt := &roundTripper{resp: refusedResponse}
	p := newParser(rt)

	_, err := p.Parse(context.Background(), "some resume text")
	require.Error(t, err)

	var pe services.ResumeParseError
	require.True(t, errors.As(err, &pe), "error must be ResumeParseError, got: %T %v", err, err)
	assert.False(t, pe.Retryable, "no_tool_use should be non-retryable")
	assert.Equal(t, "no_tool_use", pe.Reason)
}

// Test 4: tool_use block contains malformed JSON — adapter returns non-retryable error.
func TestAnthropicParser_MalformedToolJSON_ReturnsInvalidJSONError(t *testing.T) {
	rt := &roundTripper{resp: malformedJSONResponse}
	p := newParser(rt)

	_, err := p.Parse(context.Background(), "some resume text")
	require.Error(t, err)

	var pe services.ResumeParseError
	require.True(t, errors.As(err, &pe), "error must be ResumeParseError, got: %T %v", err, err)
	assert.False(t, pe.Retryable, "tool_invalid_json should be non-retryable")
	assert.Equal(t, "tool_invalid_json", pe.Reason)
}
