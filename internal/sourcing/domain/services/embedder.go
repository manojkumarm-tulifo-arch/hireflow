package services

import (
	"context"
	"fmt"
)

// EmbeddingError is returned by Embedder adapters when embedding fails.
// Retryable indicates whether the caller should re-attempt the operation.
// Adapters MUST use this type (not a raw error) whenever the failure is
// classifiable — the worker layer uses errors.As to decide between
// ScheduleRetry and MarkEmbedFailed.
type EmbeddingError struct {
	Retryable bool
	Reason    string // short code, e.g. "voyage_5xx", "voyage_429", "bad_dimension"
	Detail    string // human-readable
}

func (e EmbeddingError) Error() string {
	return fmt.Sprintf("embed: %s: %s", e.Reason, e.Detail)
}

// Embedder is the port for computing dense vector representations of text.
// The canonical implementation calls Voyage AI (voyage-3 model, 1024 dims).
// Tests use a deterministic stub that is stable across runs.
//
// Errors should be EmbeddingError when classified; raw errors are treated as
// retryable by the worker layer.
type Embedder interface {
	// EmbedDocument embeds a single text document into a 1024-dim float32 vector.
	// The returned slice MUST have exactly 1024 elements and be L2-normalised
	// (Voyage guarantees this; the stub mirrors it). Callers MAY assert len == 1024.
	EmbedDocument(ctx context.Context, text string) ([]float32, error)
}
