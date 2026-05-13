package valueobjects_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

func TestNewRoleEmbedding_Accept1024(t *testing.T) {
	v := make([]float32, 1024)
	re, err := vo.NewRoleEmbedding(v)
	require.NoError(t, err)
	assert.Equal(t, 1024, re.Dim())
	assert.Len(t, re.Floats(), 1024)
}

func TestNewRoleEmbedding_RejectWrongDim(t *testing.T) {
	cases := []int{0, 1, 512, 1023, 1025, 2048}
	for _, n := range cases {
		v := make([]float32, n)
		_, err := vo.NewRoleEmbedding(v)
		assert.ErrorIs(t, err, vo.ErrInvalidEmbeddingDim, "expected error for dim %d", n)
	}
}
