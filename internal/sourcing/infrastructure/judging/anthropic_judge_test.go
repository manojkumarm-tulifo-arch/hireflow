package judging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sharedanthropic "github.com/hustle/hireflow/internal/shared/infrastructure/llm/anthropic"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/judging"
)

// recordingRoundTripper captures the outgoing request body while returning a
// canned response. Used in tests that need to inspect what was sent to Anthropic.
type recordingRoundTripper struct {
	resp    string
	status  int
	lastReq []byte
}

func (r *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Capture the request body for assertion.
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		r.lastReq = body
		req.Body = io.NopCloser(bytes.NewReader(body))
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

// judgeSuccessResponse is a canned Messages API reply containing a valid
// judge_match tool call.
const judgeSuccessResponse = `{
  "id": "msg_judge_01",
  "type": "message",
  "role": "assistant",
  "model": "claude-opus-4-7",
  "stop_reason": "tool_use",
  "content": [
    {
      "type": "tool_use",
      "id": "toolu_judge_01",
      "name": "judge_match",
      "input": {
        "score": 82,
        "evidence": [
          {
            "kind": "skill",
            "skill": "Go",
            "claim": "5 years of Go development",
            "support": "Senior Backend Engineer at Razorpay using Go and Kubernetes 2020-2025"
          },
          {
            "kind": "experience",
            "claim": "Led distributed systems work",
            "support": "Senior Backend Engineer, Razorpay, 2020-04 to 2025-01"
          }
        ],
        "summary": "Strong match with 5 years of Go and distributed systems experience aligned to the role. Primary concern is limited cloud infrastructure depth beyond Kubernetes.",
        "concerns": []
      }
    }
  ],
  "usage": {"input_tokens": 400, "output_tokens": 120}
}`

// judgeTextOnlyResponse has no tool_use block — model responded in free text.
const judgeTextOnlyResponse = `{
  "id": "msg_judge_02",
  "type": "message",
  "role": "assistant",
  "model": "claude-opus-4-7",
  "stop_reason": "end_turn",
  "content": [
    {
      "type": "text",
      "text": "I cannot evaluate this candidate."
    }
  ],
  "usage": {"input_tokens": 100, "output_tokens": 10}
}`

// judgeMalformedJSONResponse has a tool_use block whose input is not a JSON object.
const judgeMalformedJSONResponse = `{
  "id": "msg_judge_03",
  "type": "message",
  "role": "assistant",
  "model": "claude-opus-4-7",
  "stop_reason": "tool_use",
  "content": [
    {
      "type": "tool_use",
      "id": "toolu_judge_03",
      "name": "judge_match",
      "input": "not-valid-json-for-judgment"
    }
  ],
  "usage": {"input_tokens": 100, "output_tokens": 10}
}`

// newJudge constructs an AnthropicJudge using the provided fake transport.
func newJudge(rt http.RoundTripper) *judging.AnthropicJudge {
	c := sharedanthropic.NewClient(sharedanthropic.Config{
		APIKey:     "sk-test",
		Model:      "claude-opus-4-7",
		HTTPClient: &http.Client{Transport: rt},
	})
	return judging.NewAnthropicJudge(c.SDK(), c.Model())
}

// sampleProfile builds a test candidate profile with PII set.
func sampleProfile() vo.ParsedProfile {
	return vo.ParsedProfile{
		SchemaVersion: 1,
		Personal: vo.ParsedPersonal{
			FullName: "Alice Smith",
			Email:    "alice@example.com",
			Phone:    "+91-9876543210",
			Location: "Bangalore",
		},
		Headline: "Senior Backend Engineer",
		Skills: []vo.ParsedSkill{
			{Name: "Go", Years: 5.0},
			{Name: "Kubernetes", Years: 3.0},
		},
		Experiences: []vo.ParsedExperience{
			{
				ID:          "exp_0",
				Company:     "Razorpay",
				Title:       "Senior Backend Engineer",
				Start:       "2020-04",
				End:         "2025-01",
				SkillsUsed:  []string{"Go", "Kubernetes"},
				Description: "Built distributed payment systems using Go and Kubernetes.",
			},
		},
	}
}

// sampleRole builds a minimal RoleSpec for tests.
func sampleRole() services.RoleSpec {
	return services.RoleSpec{
		Title: "Senior Backend Engineer",
		RequiredSkills: []services.SkillSpec{
			{Name: "Go", MinYears: 3},
		},
		OptionalSkills: []services.SkillSpec{
			{Name: "Kubernetes", MinYears: 1},
		},
		MinYears: 4,
		MaxYears: 10,
		WorkMode: "hybrid",
	}
}

// sampleRules builds a passing RuleMatchReport.
func sampleRules() vo.RuleMatchReport {
	return vo.RuleMatchReport{
		Results: []vo.RuleResult{
			{
				Criterion: vo.RuleCriterion{Type: "skill", Name: "Go", Required: true},
				Passed:    true,
				Actual:    "5.0 years",
			},
		},
	}
}

// Test 1: Happy path — valid tool-use response returns a well-populated LLMJudgment
// with PromptVersion stamped.
func TestAnthropicJudge_HappyPath(t *testing.T) {
	rt := &recordingRoundTripper{resp: judgeSuccessResponse}
	j := newJudge(rt)

	judgment, err := j.Judge(context.Background(), sampleProfile(), sampleRole(), sampleRules())
	require.NoError(t, err)

	assert.Equal(t, 82, judgment.Score)
	assert.Equal(t, judging.PromptVersion, judgment.PromptVersion, "PromptVersion must be stamped onto returned judgment")
	require.Len(t, judgment.Evidence, 2)
	assert.Equal(t, "skill", judgment.Evidence[0].Kind)
	assert.Equal(t, "Go", judgment.Evidence[0].Skill)
	assert.Equal(t, "5 years of Go development", judgment.Evidence[0].Claim)
	assert.Equal(t, "experience", judgment.Evidence[1].Kind)
	assert.NotEmpty(t, judgment.Summary)
	assert.Empty(t, judgment.Concerns)
}

// Test 2: Model returns text only — adapter returns non-retryable JudgeError
// with Reason "no_tool_use".
func TestAnthropicJudge_NoToolUse_ReturnsClassifiedError(t *testing.T) {
	rt := &recordingRoundTripper{resp: judgeTextOnlyResponse}
	j := newJudge(rt)

	_, err := j.Judge(context.Background(), sampleProfile(), sampleRole(), sampleRules())
	require.Error(t, err)

	var je services.JudgeError
	require.True(t, errors.As(err, &je), "error must be JudgeError, got: %T %v", err, err)
	assert.False(t, je.Retryable, "no_tool_use should be non-retryable")
	assert.Equal(t, "no_tool_use", je.Reason)
}

// Test 3: Tool-use block contains malformed JSON — adapter returns non-retryable
// JudgeError with Reason "tool_invalid_json".
func TestAnthropicJudge_MalformedToolJSON_ReturnsClassifiedError(t *testing.T) {
	rt := &recordingRoundTripper{resp: judgeMalformedJSONResponse}
	j := newJudge(rt)

	_, err := j.Judge(context.Background(), sampleProfile(), sampleRole(), sampleRules())
	require.Error(t, err)

	var je services.JudgeError
	require.True(t, errors.As(err, &je), "error must be JudgeError, got: %T %v", err, err)
	assert.False(t, je.Retryable, "tool_invalid_json should be non-retryable")
	assert.Equal(t, "tool_invalid_json", je.Reason)
}

// Test 4: PII strip — the request body sent to Anthropic must NOT contain the
// candidate's full name, email, or phone number.
func TestAnthropicJudge_PIIStripped_FromRequestBody(t *testing.T) {
	rt := &recordingRoundTripper{resp: judgeSuccessResponse}
	j := newJudge(rt)

	profile := sampleProfile()
	// Ensure the profile has PII values we can assert on.
	require.Equal(t, "Alice Smith", profile.Personal.FullName)
	require.Equal(t, "alice@example.com", profile.Personal.Email)
	require.Equal(t, "+91-9876543210", profile.Personal.Phone)

	_, err := j.Judge(context.Background(), profile, sampleRole(), sampleRules())
	require.NoError(t, err)

	// rt.lastReq is the raw JSON body sent to the Anthropic API.
	require.NotEmpty(t, rt.lastReq, "transport must have captured a request body")

	bodyStr := string(rt.lastReq)
	assert.NotContains(t, bodyStr, "Alice Smith", "full_name must be stripped before sending to Anthropic")
	assert.NotContains(t, bodyStr, "alice@example.com", "email must be stripped before sending to Anthropic")
	assert.NotContains(t, bodyStr, "+91-9876543210", "phone must be stripped before sending to Anthropic")

	// Sanity: non-PII fields should still be present.
	assert.Contains(t, bodyStr, "Senior Backend Engineer", "headline should be present in request")

	// Also verify the body is valid JSON.
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(rt.lastReq, &parsed), "request body must be valid JSON")
}
