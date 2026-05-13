package valueobjects_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

func TestParsedProfile_DefaultSchemaVersion(t *testing.T) {
	p := vo.NewParsedProfile()
	assert.Equal(t, 1, p.SchemaVersion)
}

func TestParsedProfile_RoundTrip(t *testing.T) {
	p := vo.NewParsedProfile()
	p.Personal.FullName = "Alice"
	p.Personal.Email = "alice@example.com"
	p.Headline = "Senior Backend Engineer"
	p.Skills = []vo.ParsedSkill{{Name: "Go", Years: 5}}
	p.Experiences = []vo.ParsedExperience{{
		ID: "exp_0", Company: "Razorpay", Title: "Senior Backend Engineer",
		Start: "2020-04", End: "2025-01",
	}}

	b, err := json.Marshal(p)
	require.NoError(t, err)

	var got vo.ParsedProfile
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, 1, got.SchemaVersion)
	assert.Equal(t, "Alice", got.Personal.FullName)
	assert.Equal(t, "Senior Backend Engineer", got.Headline)
	require.Len(t, got.Skills, 1)
	assert.Equal(t, "Go", got.Skills[0].Name)
	assert.Equal(t, 5.0, got.Skills[0].Years)
	require.Len(t, got.Experiences, 1)
	assert.Equal(t, "Razorpay", got.Experiences[0].Company)
}

func TestParsedProfile_Validate_RequiresSchemaVersion(t *testing.T) {
	var p vo.ParsedProfile // zero-value, schema=0
	err := p.Validate()
	assert.ErrorIs(t, err, vo.ErrInvalidProfile)
}

func TestParsedProfile_Validate_AcceptsMinimal(t *testing.T) {
	p := vo.NewParsedProfile()
	p.Personal.FullName = "Anon Candidate"
	err := p.Validate()
	require.NoError(t, err)
}
