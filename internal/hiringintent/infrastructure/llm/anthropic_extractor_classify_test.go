package llm

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/hiringintent/application/dto"
	"github.com/hustle/hireflow/internal/hiringintent/domain/services"
)

// extractWithStatus runs one extraction against a fake transport that returns
// the given status code + body. Returns the classified error.
func extractWithStatus(t *testing.T, status int, body string) error {
	t.Helper()
	rt := &roundTripper{status: status, resp: body}
	e := newExtractor(rt)
	_, err := e.Extract(context.Background(), dto.ExtractInput{UserMessage: "hi"})
	require.Error(t, err)
	return err
}

func TestClassify_BillingViaInvalidRequestMessage(t *testing.T) {
	// Real-world shape: Anthropic returns 400 invalid_request_error for credit issues,
	// not the dedicated billing_error type.
	body := `{"type":"error","error":{"type":"invalid_request_error","message":"Your credit balance is too low to access the Anthropic API. Please go to Plans & Billing to upgrade."}}`
	err := extractWithStatus(t, http.StatusBadRequest, body)
	assert.ErrorIs(t, err, services.ErrLLMBilling)
}

func TestClassify_BillingViaTypedError(t *testing.T) {
	body := `{"type":"error","error":{"type":"billing_error","message":"billing problem"}}`
	err := extractWithStatus(t, http.StatusBadRequest, body)
	assert.ErrorIs(t, err, services.ErrLLMBilling)
}

func TestClassify_AuthError(t *testing.T) {
	body := `{"type":"error","error":{"type":"authentication_error","message":"invalid api key"}}`
	err := extractWithStatus(t, http.StatusUnauthorized, body)
	assert.ErrorIs(t, err, services.ErrLLMAuth)
}

func TestClassify_PermissionError(t *testing.T) {
	body := `{"type":"error","error":{"type":"permission_error","message":"no access to model"}}`
	err := extractWithStatus(t, http.StatusForbidden, body)
	assert.ErrorIs(t, err, services.ErrLLMPermission)
}

func TestClassify_RateLimit(t *testing.T) {
	body := `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`
	err := extractWithStatus(t, http.StatusTooManyRequests, body)
	assert.ErrorIs(t, err, services.ErrLLMRateLimit)
}

func TestClassify_Overloaded(t *testing.T) {
	body := `{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`
	err := extractWithStatus(t, 529, body)
	assert.ErrorIs(t, err, services.ErrLLMOverloaded)
}

func TestClassify_GenericUpstream(t *testing.T) {
	body := `{"type":"error","error":{"type":"api_error","message":"server error"}}`
	err := extractWithStatus(t, http.StatusInternalServerError, body)
	assert.ErrorIs(t, err, services.ErrLLMUpstream)
}

func TestClassify_NoToolCallIsResponseShape(t *testing.T) {
	body := `{
		"id": "msg_test", "type": "message", "role": "assistant",
		"model": "claude-opus-4-7", "stop_reason": "end_turn",
		"content": [{"type": "text", "text": "I have nothing to extract."}],
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`
	err := extractWithStatus(t, http.StatusOK, body)
	assert.ErrorIs(t, err, services.ErrLLMResponseShape)
}

// transportErr returns a network-level error with no HTTP response.
type transportErr struct{ msg string }

func (t *transportErr) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New(t.msg)
}

func TestClassify_NetworkErrorIsUpstream(t *testing.T) {
	e := newExtractor(&transportErr{msg: "dial tcp: connection refused"})
	_, err := e.Extract(context.Background(), dto.ExtractInput{UserMessage: "hi"})
	require.Error(t, err)
	assert.ErrorIs(t, err, services.ErrLLMUpstream)
	assert.True(t, strings.Contains(err.Error(), "connection refused"))
}
