package valueobjects_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

func TestParseFeedbackDecision_KnownValues(t *testing.T) {
	cases := []vo.FeedbackDecision{
		vo.FeedbackDecisionStrongYes,
		vo.FeedbackDecisionYes,
		vo.FeedbackDecisionMixed,
		vo.FeedbackDecisionNo,
		vo.FeedbackDecisionStrongNo,
	}
	for _, c := range cases {
		got, err := vo.ParseFeedbackDecision(string(c))
		require.NoError(t, err)
		assert.Equal(t, c, got)
	}
}

func TestParseFeedbackDecision_RejectsInvalid(t *testing.T) {
	for _, s := range []string{"invalid", "", "Yes", "NO", "strong"} {
		_, err := vo.ParseFeedbackDecision(s)
		assert.ErrorIs(t, err, vo.ErrInvalidFeedbackDecision, "expected error for %q", s)
	}
}
