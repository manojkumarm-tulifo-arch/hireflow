package embedding_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/embedding"
)

// roundTripFunc implements http.RoundTripper via a function.
type roundTripFunc func(r *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// newFakeClient returns an *http.Client whose transport returns canned body + status.
func newFakeClient(statusCode int, body string) *http.Client {
	return &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: statusCode,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}
}

// make1024FloatJSON builds a Voyage-style 200 response with n floats.
func makeVoyageResponse(n int) string {
	emb := make([]float32, n)
	// Use a simple normalised vector: first element = 1, rest = 0.
	if n > 0 {
		emb[0] = 1.0
	}
	data, _ := json.Marshal(map[string]any{
		"data": []map[string]any{
			{"embedding": emb, "index": 0},
		},
	})
	return string(data)
}

func newVoyageWithFakeHTTP(hc *http.Client) *embedding.Voyage {
	client := embedding.NewVoyageClientWithBaseURL("key", "voyage-3", "http://fake", hc)
	return embedding.NewVoyage(client)
}

// ---------------------------------------------------------------------------
// Happy path
// ---------------------------------------------------------------------------

func TestVoyage_HappyPath(t *testing.T) {
	hc := newFakeClient(http.StatusOK, makeVoyageResponse(1024))
	v := newVoyageWithFakeHTTP(hc)

	vec, err := v.EmbedDocument(context.Background(), "hello world")
	require.NoError(t, err)
	assert.Len(t, vec, 1024)
	assert.InDelta(t, float32(1.0), vec[0], 1e-6)
}

// ---------------------------------------------------------------------------
// 429 → retryable
// ---------------------------------------------------------------------------

func TestVoyage_RateLimit(t *testing.T) {
	hc := newFakeClient(http.StatusTooManyRequests, `{"error":"rate limited"}`)
	v := newVoyageWithFakeHTTP(hc)

	_, err := v.EmbedDocument(context.Background(), "hello")
	require.Error(t, err)

	var embErr services.EmbeddingError
	require.True(t, errors.As(err, &embErr), "expected EmbeddingError, got %T: %v", err, err)
	assert.True(t, embErr.Retryable)
	assert.Equal(t, "voyage_429", embErr.Reason)
}

// ---------------------------------------------------------------------------
// 401 → non-retryable 4xx
// ---------------------------------------------------------------------------

func TestVoyage_Unauthorized(t *testing.T) {
	hc := newFakeClient(http.StatusUnauthorized, `{"error":"unauthorized"}`)
	v := newVoyageWithFakeHTTP(hc)

	_, err := v.EmbedDocument(context.Background(), "hello")
	require.Error(t, err)

	var embErr services.EmbeddingError
	require.True(t, errors.As(err, &embErr))
	assert.False(t, embErr.Retryable)
	assert.Equal(t, "voyage_4xx", embErr.Reason)
}

// ---------------------------------------------------------------------------
// 400 → non-retryable 4xx
// ---------------------------------------------------------------------------

func TestVoyage_BadRequest(t *testing.T) {
	hc := newFakeClient(http.StatusBadRequest, `{"error":"bad request"}`)
	v := newVoyageWithFakeHTTP(hc)

	_, err := v.EmbedDocument(context.Background(), "hello")
	require.Error(t, err)

	var embErr services.EmbeddingError
	require.True(t, errors.As(err, &embErr))
	assert.False(t, embErr.Retryable)
	assert.Equal(t, "voyage_4xx", embErr.Reason)
}

// ---------------------------------------------------------------------------
// 5xx → retryable
// ---------------------------------------------------------------------------

func TestVoyage_ServerError(t *testing.T) {
	hc := newFakeClient(http.StatusInternalServerError, `{"error":"internal server error"}`)
	v := newVoyageWithFakeHTTP(hc)

	_, err := v.EmbedDocument(context.Background(), "hello")
	require.Error(t, err)

	var embErr services.EmbeddingError
	require.True(t, errors.As(err, &embErr))
	assert.True(t, embErr.Retryable)
	assert.Equal(t, "voyage_5xx", embErr.Reason)
}

// ---------------------------------------------------------------------------
// Wrong dimension (512 floats instead of 1024)
// ---------------------------------------------------------------------------

func TestVoyage_WrongDimension(t *testing.T) {
	hc := newFakeClient(http.StatusOK, makeVoyageResponse(512))
	v := newVoyageWithFakeHTTP(hc)

	_, err := v.EmbedDocument(context.Background(), "hello")
	require.Error(t, err)

	var embErr services.EmbeddingError
	require.True(t, errors.As(err, &embErr))
	assert.False(t, embErr.Retryable)
	assert.Equal(t, "voyage_bad_response", embErr.Reason)
}

// ---------------------------------------------------------------------------
// Malformed JSON response
// ---------------------------------------------------------------------------

func TestVoyage_MalformedJSON(t *testing.T) {
	hc := newFakeClient(http.StatusOK, `{not valid json`)
	v := newVoyageWithFakeHTTP(hc)

	_, err := v.EmbedDocument(context.Background(), "hello")
	require.Error(t, err)

	var embErr services.EmbeddingError
	require.True(t, errors.As(err, &embErr))
	assert.False(t, embErr.Retryable)
	assert.Equal(t, "voyage_bad_response", embErr.Reason)
}

// ---------------------------------------------------------------------------
// Empty data array
// ---------------------------------------------------------------------------

func TestVoyage_EmptyDataArray(t *testing.T) {
	hc := newFakeClient(http.StatusOK, `{"data":[]}`)
	v := newVoyageWithFakeHTTP(hc)

	_, err := v.EmbedDocument(context.Background(), "hello")
	require.Error(t, err)

	var embErr services.EmbeddingError
	require.True(t, errors.As(err, &embErr))
	assert.False(t, embErr.Retryable)
	assert.Equal(t, "voyage_bad_response", embErr.Reason)
}
