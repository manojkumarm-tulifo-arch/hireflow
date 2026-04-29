package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/hiringintent/application/dto"
	sharedanthropic "github.com/hustle/hireflow/internal/shared/infrastructure/llm/anthropic"
)

// roundTripper lets a test substitute a canned response for the SDK's HTTP
// call without actually hitting the network.
type roundTripper struct {
	gotURL  string
	gotBody []byte
	resp    string
	status  int
}

func (r *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.gotURL = req.URL.String()
	if req.Body != nil {
		r.gotBody, _ = io.ReadAll(req.Body)
	}
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

// successResponse is a canned Messages API reply with a propose_draft tool call.
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
      "name": "propose_draft",
      "input": {
        "role_title": "Senior Backend Engineer",
        "skills": [{"name": "Go", "required": true}],
        "headcount": 2,
        "min_years": 3,
        "max_years": 7,
        "work_mode": "HYBRID",
        "priority": "HIGH",
        "budget": {"min_minor": 4000000000, "max_minor": 6000000000, "currency": "INR"},
        "reason": "Growth — adding capacity for the new payments service.",
        "team": "Payments Platform",
        "reports_to": "Aisha Khan, VP Engineering",
        "reply": "Got it — drafting a Senior Backend Engineer role at ₹40-60 LPA.",
        "complete": true,
        "missing": []
      }
    }
  ],
  "usage": {"input_tokens": 100, "output_tokens": 50}
}`

func newExtractor(rt http.RoundTripper) *AnthropicExtractor {
	c := sharedanthropic.NewClient(sharedanthropic.Config{
		APIKey:     "sk-test",
		Model:      "claude-opus-4-7",
		HTTPClient: &http.Client{Transport: rt},
	})
	return NewAnthropicExtractor(c)
}

func TestExtract_ParsesSuccessfulResponse(t *testing.T) {
	rt := &roundTripper{resp: successResponse}
	e := newExtractor(rt)

	out, err := e.Extract(context.Background(), dto.ExtractInput{
		UserMessage: "Hiring 2 Go engineers, 3-7 years, hybrid in Bangalore, high priority",
	})
	require.NoError(t, err)

	assert.Equal(t, "Got it — drafting a Senior Backend Engineer role at ₹40-60 LPA.", out.Reply)
	require.NotNil(t, out.Patch.RoleTitle)
	assert.Equal(t, "Senior Backend Engineer", *out.Patch.RoleTitle)
	require.NotNil(t, out.Patch.Headcount)
	assert.Equal(t, 2, *out.Patch.Headcount)
	require.NotNil(t, out.Patch.WorkMode)
	assert.Equal(t, "HYBRID", *out.Patch.WorkMode)
	require.NotNil(t, out.Patch.Budget)
	assert.Equal(t, int64(4_000_000_000), out.Patch.Budget.MinMinor)
	assert.Equal(t, int64(6_000_000_000), out.Patch.Budget.MaxMinor)
	assert.Equal(t, "INR", out.Patch.Budget.Currency)
	require.NotNil(t, out.Patch.Reason)
	assert.Equal(t, "Growth — adding capacity for the new payments service.", *out.Patch.Reason)
	require.NotNil(t, out.Patch.Team)
	assert.Equal(t, "Payments Platform", *out.Patch.Team)
	require.NotNil(t, out.Patch.ReportsTo)
	assert.Equal(t, "Aisha Khan, VP Engineering", *out.Patch.ReportsTo)
	assert.True(t, out.Complete)
}

func TestExtract_RequestShape(t *testing.T) {
	rt := &roundTripper{resp: successResponse}
	e := newExtractor(rt)

	_, err := e.Extract(context.Background(), dto.ExtractInput{
		Messages: []dto.ChatMessage{
			{Role: "user", Text: "earlier turn"},
			{Role: "assistant", Text: "earlier reply"},
		},
		UserMessage: "Hire 2 backend engineers",
	})
	require.NoError(t, err)

	assert.Contains(t, rt.gotURL, "/v1/messages")

	var sent map[string]any
	require.NoError(t, json.Unmarshal(rt.gotBody, &sent))

	assert.Equal(t, "claude-opus-4-7", sent["model"])

	system, ok := sent["system"].([]any)
	require.True(t, ok, "system must be an array of text blocks for cache_control")
	require.Len(t, system, 1)
	first := system[0].(map[string]any)
	assert.Contains(t, first["text"], "structured hiring intent")
	assert.NotNil(t, first["cache_control"], "system block must carry cache_control")

	tools, ok := sent["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)
	assert.Equal(t, "propose_draft", tools[0].(map[string]any)["name"])

	choice := sent["tool_choice"].(map[string]any)
	assert.Equal(t, "tool", choice["type"])
	assert.Equal(t, "propose_draft", choice["name"])

	msgs := sent["messages"].([]any)
	// 2 history turns + 1 current user turn = 3 (no draft → no synthetic prefix)
	assert.Len(t, msgs, 3)
	last := msgs[len(msgs)-1].(map[string]any)
	assert.Equal(t, "user", last["role"])
}

func TestExtract_IncludesDraftStateWhenNonEmpty(t *testing.T) {
	rt := &roundTripper{resp: successResponse}
	e := newExtractor(rt)

	title := "Senior Backend Engineer"
	_, err := e.Extract(context.Background(), dto.ExtractInput{
		Draft:       dto.DraftPatch{RoleTitle: &title},
		UserMessage: "What else do you need?",
	})
	require.NoError(t, err)

	var sent map[string]any
	require.NoError(t, json.Unmarshal(rt.gotBody, &sent))
	msgs := sent["messages"].([]any)
	require.GreaterOrEqual(t, len(msgs), 2, "draft prefix + current user turn")

	first := msgs[0].(map[string]any)
	content := first["content"].([]any)[0].(map[string]any)
	assert.Contains(t, content["text"], "Current draft state")
	assert.Contains(t, content["text"], "Senior Backend Engineer")
}

func TestExtract_RejectsResponseWithoutToolCall(t *testing.T) {
	rt := &roundTripper{resp: `{
		"id": "msg_test", "type": "message", "role": "assistant",
		"model": "claude-opus-4-7", "stop_reason": "end_turn",
		"content": [{"type": "text", "text": "I have nothing to extract."}],
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`}
	e := newExtractor(rt)

	_, err := e.Extract(context.Background(), dto.ExtractInput{UserMessage: "hi"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not call propose_draft")
}
