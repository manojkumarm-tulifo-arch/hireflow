package scanning_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/infrastructure/scanning"
)

func TestNoop_AlwaysClean(t *testing.T) {
	s := scanning.NewNoop()
	v, err := s.Scan(context.Background(), bytes.NewReader([]byte("anything")))
	require.NoError(t, err)
	assert.True(t, v.Clean)
}
