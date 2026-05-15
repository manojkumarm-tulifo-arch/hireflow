package valueobjects_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

func TestParseRoundStatus_KnownValues(t *testing.T) {
	cases := []vo.RoundStatus{
		vo.RoundStatusPending,
		vo.RoundStatusQuestionsReady,
		vo.RoundStatusCompleted,
		vo.RoundStatusSkipped,
		vo.RoundStatusGenerationFailed,
	}
	for _, c := range cases {
		got, err := vo.ParseRoundStatus(string(c))
		require.NoError(t, err)
		assert.Equal(t, c, got)
	}
}

func TestParseRoundStatus_RejectsInvalid(t *testing.T) {
	for _, s := range []string{"invalid", "", "pending", "COMPLETED"} {
		_, err := vo.ParseRoundStatus(s)
		assert.ErrorIs(t, err, vo.ErrInvalidRoundStatus, "expected error for %q", s)
	}
}

func TestRoundStatus_IsTerminal(t *testing.T) {
	cases := []struct {
		status   vo.RoundStatus
		terminal bool
	}{
		{vo.RoundStatusPending, false},
		{vo.RoundStatusQuestionsReady, false},
		{vo.RoundStatusCompleted, true},
		{vo.RoundStatusSkipped, true},
		{vo.RoundStatusGenerationFailed, false}, // NOT terminal — recruiter can regenerate
	}
	for _, tc := range cases {
		assert.Equal(t, tc.terminal, tc.status.IsTerminal(), "IsTerminal(%s)", tc.status)
	}
}

func TestRoundStatus_CanTransitionTo(t *testing.T) {
	// Full 5x5 transition matrix: all 25 (from, to) combinations.
	//
	// Permitted transitions (true):
	//   Pending          -> QuestionsReady, GenerationFailed, Skipped
	//   QuestionsReady   -> Completed, Skipped, Pending
	//   GenerationFailed -> Pending, Skipped
	// Everything else is false (including terminals and self-transitions).

	P  := vo.RoundStatusPending
	QR := vo.RoundStatusQuestionsReady
	C  := vo.RoundStatusCompleted
	SK := vo.RoundStatusSkipped
	GF := vo.RoundStatusGenerationFailed

	cases := []struct {
		from vo.RoundStatus
		to   vo.RoundStatus
		ok   bool
	}{
		// from Pending (row 1)
		{P, P, false},
		{P, QR, true},
		{P, C, false},
		{P, SK, true},
		{P, GF, true},

		// from QuestionsReady (row 2)
		{QR, P, true},
		{QR, QR, false},
		{QR, C, true},
		{QR, SK, true},
		{QR, GF, false},

		// from Completed — terminal, no outbound (row 3)
		{C, P, false},
		{C, QR, false},
		{C, C, false},
		{C, SK, false},
		{C, GF, false},

		// from Skipped — terminal, no outbound (row 4)
		{SK, P, false},
		{SK, QR, false},
		{SK, C, false},
		{SK, SK, false},
		{SK, GF, false},

		// from GenerationFailed (row 5)
		{GF, P, true},
		{GF, QR, false},
		{GF, C, false},
		{GF, SK, true},
		{GF, GF, false},
	}

	for _, tc := range cases {
		got := tc.from.CanTransitionTo(tc.to)
		assert.Equal(t, tc.ok, got, "%s -> %s", tc.from, tc.to)
	}
}
