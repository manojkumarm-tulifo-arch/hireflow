package embedding_test

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/infrastructure/embedding"
)

func TestStub_Deterministic(t *testing.T) {
	s := embedding.NewStub()
	ctx := context.Background()
	text := "software engineer with 5 years Go experience"

	v1, err := s.EmbedDocument(ctx, text)
	require.NoError(t, err)

	v2, err := s.EmbedDocument(ctx, text)
	require.NoError(t, err)

	assert.Equal(t, v1, v2, "same input must produce identical vectors")
}

func TestStub_DifferentInputs(t *testing.T) {
	s := embedding.NewStub()
	ctx := context.Background()

	v1, err := s.EmbedDocument(ctx, "software engineer")
	require.NoError(t, err)

	v2, err := s.EmbedDocument(ctx, "marketing manager")
	require.NoError(t, err)

	// With overwhelming probability sha256 seeds differ → vectors differ.
	assert.NotEqual(t, v1, v2, "different inputs should produce different vectors")
}

func TestStub_Dimension(t *testing.T) {
	s := embedding.NewStub()
	vec, err := s.EmbedDocument(context.Background(), "any text here")
	require.NoError(t, err)
	assert.Len(t, vec, 1024)
}

func TestStub_L2Normalised(t *testing.T) {
	s := embedding.NewStub()
	vec, err := s.EmbedDocument(context.Background(), "test normalisation")
	require.NoError(t, err)

	var sumSq float64
	for _, v := range vec {
		sumSq += float64(v) * float64(v)
	}
	norm := math.Sqrt(sumSq)

	assert.InDelta(t, 1.0, norm, 1e-3, "vector must be L2-normalised (norm ≈ 1.0)")
}

func TestStub_DeterministicAcrossCallsWithDifferentInstances(t *testing.T) {
	// Two separate Stub instances must agree — seeding is purely text-derived.
	s1 := embedding.NewStub()
	s2 := embedding.NewStub()
	ctx := context.Background()
	text := "determinism check"

	v1, err := s1.EmbedDocument(ctx, text)
	require.NoError(t, err)

	v2, err := s2.EmbedDocument(ctx, text)
	require.NoError(t, err)

	assert.Equal(t, v1, v2, "different Stub instances must agree for the same text")
}
