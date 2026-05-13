package valueobjects_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

func TestParseUploadStatus_KnownValues(t *testing.T) {
	cases := []vo.UploadStatus{
		vo.StatusPending, vo.StatusScanning, vo.StatusExtracting,
		vo.StatusExtracted, vo.StatusFailed, vo.StatusQuarantined,
	}
	for _, c := range cases {
		got, err := vo.ParseUploadStatus(string(c))
		require.NoError(t, err)
		assert.Equal(t, c, got)
	}
}

func TestParseUploadStatus_RejectsUnknown(t *testing.T) {
	_, err := vo.ParseUploadStatus("Bogus")
	assert.ErrorIs(t, err, vo.ErrInvalidStatus)
}

func TestUploadStatus_CanTransitionTo(t *testing.T) {
	cases := []struct {
		from, to vo.UploadStatus
		ok       bool
	}{
		{vo.StatusPending, vo.StatusScanning, true},
		{vo.StatusScanning, vo.StatusExtracting, true},
		{vo.StatusScanning, vo.StatusQuarantined, true},
		{vo.StatusExtracting, vo.StatusExtracted, true},
		{vo.StatusExtracting, vo.StatusFailed, true},
		{vo.StatusPending, vo.StatusFailed, true}, // fatal at any stage
		{vo.StatusExtracted, vo.StatusScanning, false},
		{vo.StatusFailed, vo.StatusPending, false},
		{vo.StatusExtracted, vo.StatusParsing, true},
		{vo.StatusParsing, vo.StatusParsed, true},
		{vo.StatusParsing, vo.StatusFailed, true},
		{vo.StatusParsed, vo.StatusParsing, false}, // Parsed is terminal
		{vo.StatusExtracted, vo.StatusExtracted, false},
	}
	for _, tc := range cases {
		got := tc.from.CanTransitionTo(tc.to)
		assert.Equal(t, tc.ok, got, "%s -> %s", tc.from, tc.to)
	}
}

func TestUploadStatus_IsTerminal(t *testing.T) {
	assert.True(t, vo.StatusParsed.IsTerminal())
	assert.True(t, vo.StatusFailed.IsTerminal())
	assert.True(t, vo.StatusQuarantined.IsTerminal())
	assert.False(t, vo.StatusExtracted.IsTerminal())
	assert.False(t, vo.StatusPending.IsTerminal())
	assert.False(t, vo.StatusScanning.IsTerminal())
}
