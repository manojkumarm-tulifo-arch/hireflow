//go:build integration

package persistence_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/persistence"
)

func makeVector(dim int, fill float32) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = fill
	}
	return v
}

// TestIntentEmbeddingSave_RoundTrip saves a vector and reads it back.
func TestIntentEmbeddingSave_RoundTrip(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresIntentEmbeddingRepository(pool)
	tenant := shared.NewTenantID()
	intentID := uuid.New()
	specVersion := 1

	vec := makeVector(1024, 0.1)
	require.NoError(t, repo.Save(context.Background(), intentID, tenant, specVersion, vec))

	got, err := repo.Find(context.Background(), intentID, specVersion)
	require.NoError(t, err)
	require.Len(t, got, 1024)
	for i, v := range got {
		assert.InDelta(t, float32(0.1), v, 1e-6, "element %d mismatch", i)
	}
}

// TestIntentEmbeddingSave_UpsertOnSameVersion re-saves the same (intent_id, spec_version)
// and verifies the new vector replaces the old one.
func TestIntentEmbeddingSave_UpsertOnSameVersion(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresIntentEmbeddingRepository(pool)
	tenant := shared.NewTenantID()
	intentID := uuid.New()
	specVersion := 2

	first := makeVector(1024, 0.2)
	require.NoError(t, repo.Save(context.Background(), intentID, tenant, specVersion, first))

	second := makeVector(1024, 0.8)
	require.NoError(t, repo.Save(context.Background(), intentID, tenant, specVersion, second))

	got, err := repo.Find(context.Background(), intentID, specVersion)
	require.NoError(t, err)
	assert.InDelta(t, float32(0.8), got[0], 1e-6, "second save must overwrite the first")
}

// TestIntentEmbeddingFind_NotFound expects ErrIntentEmbeddingNotFound for an
// unknown (intent_id, spec_version) pair.
func TestIntentEmbeddingFind_NotFound(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresIntentEmbeddingRepository(pool)

	// spec_version 99 was never saved.
	_, err := repo.Find(context.Background(), uuid.New(), 99)
	assert.ErrorIs(t, err, repositories.ErrIntentEmbeddingNotFound)
}
