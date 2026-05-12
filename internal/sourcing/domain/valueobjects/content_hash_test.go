package valueobjects_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

func TestNewContentHash_AcceptsValidHex(t *testing.T) {
	h, err := vo.NewContentHash(strings.Repeat("a", 64))
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("a", 64), h.String())
}

func TestNewContentHash_RejectsWrongLength(t *testing.T) {
	_, err := vo.NewContentHash("abc")
	assert.ErrorIs(t, err, vo.ErrInvalidContentHash)
}

func TestNewContentHash_RejectsNonHex(t *testing.T) {
	_, err := vo.NewContentHash(strings.Repeat("z", 64))
	assert.ErrorIs(t, err, vo.ErrInvalidContentHash)
}

func TestComputeContentHash_Deterministic(t *testing.T) {
	a := vo.ComputeContentHash([]byte("hello world"))
	b := vo.ComputeContentHash([]byte("hello world"))
	assert.Equal(t, a, b)
	assert.NotEqual(t, vo.ContentHash{}, a)
}

func TestComputeContentHash_DifferentInputsDiffer(t *testing.T) {
	a := vo.ComputeContentHash([]byte("a"))
	b := vo.ComputeContentHash([]byte("b"))
	assert.NotEqual(t, a, b)
}
