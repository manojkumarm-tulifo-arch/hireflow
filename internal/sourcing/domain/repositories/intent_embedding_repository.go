package repositories

import (
	"context"
	"errors"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ErrIntentEmbeddingNotFound is returned when an intent embedding lookup
// finds no cached row for the given (intent_id, spec_version) pair.
var ErrIntentEmbeddingNotFound = errors.New("intent embedding not found")

// IntentEmbeddingRepository caches the dense vector representation of a
// hiring intent's RoleSpec. Keyed by (intent_id, spec_version) so that
// re-confirming an intent with a changed RoleSpec computes a fresh embedding
// without invalidating earlier versions.
type IntentEmbeddingRepository interface {
	// Save upserts the embedding for (intentID, specVersion). Re-saving the
	// same version is a no-op (the embedding is deterministic for a given spec).
	Save(ctx context.Context, intentID uuid.UUID, tenant shared.TenantID,
		specVersion int, vector []float32) error

	// Find returns the cached embedding for (intentID, specVersion).
	// Returns ErrIntentEmbeddingNotFound when no row exists yet — the caller
	// should then embed and call Save.
	Find(ctx context.Context, intentID uuid.UUID, specVersion int) ([]float32, error)
}
