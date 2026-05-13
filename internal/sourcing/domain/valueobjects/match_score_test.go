package valueobjects_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

func TestDeriveBand_Thresholds(t *testing.T) {
	cases := []struct {
		overall  float64
		expected vo.ScoreBand
	}{
		{80.0, vo.BandStrong},
		{100.0, vo.BandStrong},
		{79.99, vo.BandModerate},
		{60.0, vo.BandModerate},
		{59.99, vo.BandWeak},
		{0.0, vo.BandWeak},
	}
	for _, tc := range cases {
		got := vo.DeriveBand(tc.overall)
		assert.Equal(t, tc.expected, got, "DeriveBand(%v)", tc.overall)
	}
}

func TestDeriveBand_None(t *testing.T) {
	// BandNone is returned when caller explicitly signals "no score yet"
	// by passing -1 as a sentinel. The caller is responsible for this
	// contract — DeriveBand(-1) must return BandNone.
	got := vo.DeriveBand(-1)
	assert.Equal(t, vo.BandNone, got)
}

func TestMatchScore_Fields(t *testing.T) {
	ms := vo.MatchScore{
		Overall:   85.5,
		Embedding: 0.82,
		Band:      vo.BandStrong,
	}
	assert.Equal(t, 85.5, ms.Overall)
	assert.Equal(t, 0.82, ms.Embedding)
	assert.Equal(t, vo.BandStrong, ms.Band)
}

func TestScoreBand_String(t *testing.T) {
	assert.Equal(t, "strong", string(vo.BandStrong))
	assert.Equal(t, "moderate", string(vo.BandModerate))
	assert.Equal(t, "weak", string(vo.BandWeak))
	assert.Equal(t, "", string(vo.BandNone))
}
