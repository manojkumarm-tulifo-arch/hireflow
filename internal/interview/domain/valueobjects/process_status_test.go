package valueobjects_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

func TestParseProcessStatus_KnownValues(t *testing.T) {
	cases := []vo.ProcessStatus{
		vo.ProcessStatusNew,
		vo.ProcessStatusInProgress,
		vo.ProcessStatusCompleted,
		vo.ProcessStatusCancelled,
	}
	for _, c := range cases {
		got, err := vo.ParseProcessStatus(string(c))
		require.NoError(t, err)
		assert.Equal(t, c, got)
	}
}

func TestParseProcessStatus_RejectsInvalid(t *testing.T) {
	for _, s := range []string{"invalid", "", "new", "completed"} {
		_, err := vo.ParseProcessStatus(s)
		assert.ErrorIs(t, err, vo.ErrInvalidProcessStatus, "expected error for %q", s)
	}
}

func TestProcessStatus_IsTerminal(t *testing.T) {
	cases := []struct {
		status   vo.ProcessStatus
		terminal bool
	}{
		{vo.ProcessStatusNew, false},
		{vo.ProcessStatusInProgress, false},
		{vo.ProcessStatusCompleted, true},
		{vo.ProcessStatusCancelled, true},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.terminal, tc.status.IsTerminal(), "IsTerminal(%s)", tc.status)
	}
}
