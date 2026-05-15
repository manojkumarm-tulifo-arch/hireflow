package valueobjects_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

func TestParseRoundKind_KnownValues(t *testing.T) {
	cases := []vo.RoundKind{
		vo.RoundKindScreen,
		vo.RoundKindTechnical,
		vo.RoundKindSystemDesign,
		vo.RoundKindBehavioral,
		vo.RoundKindBarRaiser,
	}
	for _, c := range cases {
		got, err := vo.ParseRoundKind(string(c))
		require.NoError(t, err)
		assert.Equal(t, c, got)
	}
}

func TestParseRoundKind_RejectsUnknown(t *testing.T) {
	_, err := vo.ParseRoundKind("unknown")
	assert.ErrorIs(t, err, vo.ErrInvalidRoundKind)
}

func TestParseRoundKind_RejectsEmpty(t *testing.T) {
	_, err := vo.ParseRoundKind("")
	assert.ErrorIs(t, err, vo.ErrInvalidRoundKind)
}
