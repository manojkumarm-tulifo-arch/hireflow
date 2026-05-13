package valueobjects_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

func TestStageArtifacts_RoundTrip(t *testing.T) {
	a := vo.NewStageArtifacts()
	a.SetExtractedText("hello world", 1)
	out, err := a.Marshal()
	require.NoError(t, err)

	got, err := vo.UnmarshalStageArtifacts(out)
	require.NoError(t, err)
	text, pages, ok := got.ExtractedText()
	require.True(t, ok)
	assert.Equal(t, "hello world", text)
	assert.Equal(t, 1, pages)
}

func TestStageArtifacts_EmptyByDefault(t *testing.T) {
	a := vo.NewStageArtifacts()
	_, _, ok := a.ExtractedText()
	assert.False(t, ok)
}

func TestUnmarshalStageArtifacts_AcceptsEmptyJSON(t *testing.T) {
	a, err := vo.UnmarshalStageArtifacts([]byte("{}"))
	require.NoError(t, err)
	_, _, ok := a.ExtractedText()
	assert.False(t, ok)
}

func TestStageArtifacts_ParsedProfile_RoundTrip(t *testing.T) {
	a := vo.NewStageArtifacts()
	a.SetParsedProfile([]byte(`{"schema_version":1}`))

	out, err := a.Marshal()
	require.NoError(t, err)

	got, err := vo.UnmarshalStageArtifacts(out)
	require.NoError(t, err)
	b, ok := got.ParsedProfile()
	require.True(t, ok)
	assert.Contains(t, string(b), `"schema_version":1`)
}

func TestStageArtifacts_ParsedProfile_EmptyByDefault(t *testing.T) {
	a := vo.NewStageArtifacts()
	_, ok := a.ParsedProfile()
	assert.False(t, ok)
}
