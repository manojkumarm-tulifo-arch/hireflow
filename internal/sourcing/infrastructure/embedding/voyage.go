// Package embedding holds Embedder adapters.
// VoyageClient is a thin HTTP client for the Voyage AI embeddings API.
// Voyage wraps VoyageClient and implements services.Embedder.
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/hustle/hireflow/internal/sourcing/domain/services"
)

const (
	defaultBaseURL  = "https://api.voyageai.com/v1"
	defaultModel    = "voyage-3"
	expectedDim     = 1024
)

// VoyageClient is a low-level HTTP client for the Voyage AI embeddings API.
// Construct via NewVoyageClient (production) or NewVoyageClientWithBaseURL (tests).
type VoyageClient struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

// NewVoyageClient returns a VoyageClient configured for the production Voyage AI endpoint.
func NewVoyageClient(apiKey, model string) *VoyageClient {
	if model == "" {
		model = defaultModel
	}
	return &VoyageClient{
		apiKey:  apiKey,
		model:   model,
		baseURL: defaultBaseURL,
		http:    &http.Client{},
	}
}

// NewVoyageClientWithBaseURL returns a VoyageClient with a custom base URL and
// http.Client, useful for pointing at a fake server in tests.
func NewVoyageClientWithBaseURL(apiKey, model, baseURL string, hc *http.Client) *VoyageClient {
	if model == "" {
		model = defaultModel
	}
	if hc == nil {
		hc = &http.Client{}
	}
	return &VoyageClient{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		http:    hc,
	}
}

// voyageRequest is the JSON body sent to POST /embeddings.
type voyageRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

// voyageDataItem represents a single embedding entry in the response data array.
type voyageDataItem struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

// voyageResponse is the JSON body returned by POST /embeddings on success.
type voyageResponse struct {
	Data []voyageDataItem `json:"data"`
}

// Voyage implements services.Embedder via the Voyage AI HTTP API.
type Voyage struct {
	client *VoyageClient
}

// NewVoyage constructs a Voyage adapter wrapping the given client.
func NewVoyage(client *VoyageClient) *Voyage {
	return &Voyage{client: client}
}

// EmbedDocument sends text to the Voyage AI embeddings endpoint and returns a
// 1024-dim L2-normalised float32 vector.  All failures are classified as
// services.EmbeddingError so the worker layer can decide between retry and
// permanent failure.
func (v *Voyage) EmbedDocument(ctx context.Context, text string) ([]float32, error) {
	reqBody := voyageRequest{
		Input: []string{text},
		Model: v.client.model,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, services.EmbeddingError{
			Retryable: false,
			Reason:    "voyage_bad_response",
			Detail:    fmt.Sprintf("failed to marshal request: %v", err),
		}
	}

	url := v.client.baseURL + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, services.EmbeddingError{
			Retryable: false,
			Reason:    "voyage_bad_response",
			Detail:    fmt.Sprintf("failed to build request: %v", err),
		}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+v.client.apiKey)

	resp, err := v.client.http.Do(req)
	if err != nil {
		return nil, services.EmbeddingError{
			Retryable: true,
			Reason:    "voyage_network",
			Detail:    fmt.Sprintf("http do: %v", err),
		}
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, services.EmbeddingError{
			Retryable: true,
			Reason:    "voyage_network",
			Detail:    fmt.Sprintf("reading body: %v", err),
		}
	}

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, services.EmbeddingError{
			Retryable: true,
			Reason:    "voyage_429",
			Detail:    fmt.Sprintf("rate limited: %s", respBytes),
		}
	case resp.StatusCode >= 500:
		return nil, services.EmbeddingError{
			Retryable: true,
			Reason:    "voyage_5xx",
			Detail:    fmt.Sprintf("server error %d: %s", resp.StatusCode, respBytes),
		}
	case resp.StatusCode == http.StatusBadRequest,
		resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusForbidden,
		resp.StatusCode == 422:
		return nil, services.EmbeddingError{
			Retryable: false,
			Reason:    "voyage_4xx",
			Detail:    fmt.Sprintf("client error %d: %s", resp.StatusCode, respBytes),
		}
	case resp.StatusCode != http.StatusOK:
		return nil, services.EmbeddingError{
			Retryable: false,
			Reason:    "voyage_4xx",
			Detail:    fmt.Sprintf("unexpected status %d: %s", resp.StatusCode, respBytes),
		}
	}

	var voyageResp voyageResponse
	if err := json.Unmarshal(respBytes, &voyageResp); err != nil {
		return nil, services.EmbeddingError{
			Retryable: false,
			Reason:    "voyage_bad_response",
			Detail:    fmt.Sprintf("failed to decode response: %v", err),
		}
	}

	if len(voyageResp.Data) == 0 {
		return nil, services.EmbeddingError{
			Retryable: false,
			Reason:    "voyage_bad_response",
			Detail:    "response data array is empty",
		}
	}

	embedding := voyageResp.Data[0].Embedding
	if len(embedding) != expectedDim {
		return nil, services.EmbeddingError{
			Retryable: false,
			Reason:    "voyage_bad_response",
			Detail:    fmt.Sprintf("expected %d dimensions, got %d", expectedDim, len(embedding)),
		}
	}

	return embedding, nil
}
