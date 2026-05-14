package valueobjects_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

func TestParseApplicationStatus_KnownValues(t *testing.T) {
	cases := []vo.ApplicationStatus{
		vo.AppStatusNew, vo.AppStatusScored, vo.AppStatusExcluded,
		vo.AppStatusEmbedFailed, vo.AppStatusJudgeFailed, vo.AppStatusStale,
		vo.AppStatusShortlisted, vo.AppStatusRejected,
		vo.AppStatusInterviewing, vo.AppStatusHired,
	}
	for _, c := range cases {
		got, err := vo.ParseApplicationStatus(string(c))
		require.NoError(t, err, "expected %q to parse successfully", c)
		assert.Equal(t, c, got)
	}
}

func TestParseApplicationStatus_RejectsUnknown(t *testing.T) {
	_, err := vo.ParseApplicationStatus("Bogus")
	assert.ErrorIs(t, err, vo.ErrInvalidApplicationStatus)
}

func TestApplicationStatus_IsTerminal(t *testing.T) {
	terminals := []vo.ApplicationStatus{
		vo.AppStatusExcluded, vo.AppStatusEmbedFailed,
		vo.AppStatusJudgeFailed, vo.AppStatusStale,
		vo.AppStatusRejected, vo.AppStatusHired,
	}
	nonTerminals := []vo.ApplicationStatus{
		vo.AppStatusNew, vo.AppStatusScored,
		vo.AppStatusShortlisted, vo.AppStatusInterviewing,
	}
	for _, s := range terminals {
		assert.True(t, s.IsTerminal(), "%q should be terminal", s)
	}
	for _, s := range nonTerminals {
		assert.False(t, s.IsTerminal(), "%q should NOT be terminal", s)
	}
}

func TestApplicationStatus_CanTransitionTo(t *testing.T) {
	cases := []struct {
		from, to vo.ApplicationStatus
		ok       bool
	}{
		// New → allowed
		{vo.AppStatusNew, vo.AppStatusScored, true},
		{vo.AppStatusNew, vo.AppStatusExcluded, true},
		{vo.AppStatusNew, vo.AppStatusEmbedFailed, true},
		// New → disallowed
		{vo.AppStatusNew, vo.AppStatusJudgeFailed, false},
		{vo.AppStatusNew, vo.AppStatusStale, false},
		{vo.AppStatusNew, vo.AppStatusShortlisted, false},

		// Scored → allowed (slice 3)
		{vo.AppStatusScored, vo.AppStatusJudgeFailed, true},
		{vo.AppStatusScored, vo.AppStatusStale, true},
		// Scored → allowed (slice 4 forward-compat)
		{vo.AppStatusScored, vo.AppStatusShortlisted, true},
		{vo.AppStatusScored, vo.AppStatusRejected, true},
		{vo.AppStatusScored, vo.AppStatusHired, true},
		// Scored → disallowed
		{vo.AppStatusScored, vo.AppStatusNew, false},
		{vo.AppStatusScored, vo.AppStatusExcluded, false},
		{vo.AppStatusScored, vo.AppStatusEmbedFailed, false},

		// Terminals → New (rescore path)
		{vo.AppStatusExcluded, vo.AppStatusNew, true},
		{vo.AppStatusEmbedFailed, vo.AppStatusNew, true},
		{vo.AppStatusJudgeFailed, vo.AppStatusNew, true},
		{vo.AppStatusStale, vo.AppStatusNew, true},
		// Terminals → non-New disallowed
		{vo.AppStatusExcluded, vo.AppStatusScored, false},
		{vo.AppStatusHired, vo.AppStatusNew, true},
		{vo.AppStatusRejected, vo.AppStatusNew, true},
		// Terminals → non-New disallowed
		{vo.AppStatusHired, vo.AppStatusScored, false},
		{vo.AppStatusRejected, vo.AppStatusShortlisted, false},

		// Shortlisted → allowed
		{vo.AppStatusShortlisted, vo.AppStatusInterviewing, true},
		{vo.AppStatusShortlisted, vo.AppStatusRejected, true},
		{vo.AppStatusShortlisted, vo.AppStatusHired, true},
		{vo.AppStatusShortlisted, vo.AppStatusNew, true},
		// Shortlisted → disallowed
		{vo.AppStatusShortlisted, vo.AppStatusScored, false},
		{vo.AppStatusShortlisted, vo.AppStatusExcluded, false},

		// Interviewing → allowed
		{vo.AppStatusInterviewing, vo.AppStatusRejected, true},
		{vo.AppStatusInterviewing, vo.AppStatusHired, true},
		{vo.AppStatusInterviewing, vo.AppStatusNew, true},
		// Interviewing → disallowed
		{vo.AppStatusInterviewing, vo.AppStatusScored, false},
		{vo.AppStatusInterviewing, vo.AppStatusShortlisted, false},
	}
	for _, tc := range cases {
		got := tc.from.CanTransitionTo(tc.to)
		assert.Equal(t, tc.ok, got, "%s -> %s", tc.from, tc.to)
	}
}
