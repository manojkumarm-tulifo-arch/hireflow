package ocr_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sharedanthropic "github.com/hustle/hireflow/internal/shared/infrastructure/llm/anthropic"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/ocr"
)

// ocrRoundTripper is a minimal http.RoundTripper that returns a canned response,
// mirroring the pattern in internal/hiringintent/infrastructure/llm/anthropic_extractor_test.go.
type ocrRoundTripper struct {
	resp   string
	status int
}

func (r *ocrRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
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

// ocrSuccessResponse is a canned Messages API reply with a single text block.
const ocrSuccessResponse = `{
  "id": "msg_test",
  "type": "message",
  "role": "assistant",
  "model": "claude-opus-4-7",
  "stop_reason": "end_turn",
  "content": [
    {
      "type": "text",
      "text": "Hello, this is the resume text."
    }
  ],
  "usage": {"input_tokens": 100, "output_tokens": 20}
}`

func newOCRExtractor(rt http.RoundTripper) *ocr.ClaudeVision {
	c := sharedanthropic.NewClient(sharedanthropic.Config{
		APIKey:     "sk-test",
		Model:      "claude-opus-4-7",
		HTTPClient: &http.Client{Transport: rt},
	})
	return ocr.NewClaudeVision(c.SDK(), c.Model())
}

func TestClaudeVision_HappyPath(t *testing.T) {
	rt := &ocrRoundTripper{resp: ocrSuccessResponse}
	ex := newOCRExtractor(rt)

	body := []byte("%PDF-1.4\nfake pdf bytes")
	got, err := ex.ExtractFromBytes(context.Background(), body, "application/pdf")
	require.NoError(t, err)
	assert.Equal(t, "Hello, this is the resume text.", got.Text)
	assert.GreaterOrEqual(t, got.PageCount, 1)
}

func TestClaudeVision_RejectsUnsupportedMime(t *testing.T) {
	// No round-tripper needed: the adapter must reject non-PDF before any API call.
	rt := &ocrRoundTripper{resp: ""}
	ex := newOCRExtractor(rt)

	_, err := ex.ExtractFromBytes(context.Background(), []byte("data"), "image/png")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported mime")
}
