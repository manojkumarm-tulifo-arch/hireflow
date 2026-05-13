package entities_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

func mustCHash(t *testing.T) vo.ContentHash {
	h, err := vo.NewContentHash("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	require.NoError(t, err)
	return h
}

func newCandidateInput(t *testing.T) entities.NewCandidateInput {
	t.Helper()
	profile := vo.NewParsedProfile()
	profile.Personal.FullName = "Alice"
	profile.Personal.Email = "alice@example.com"
	profile.Headline = "Senior Backend Engineer"

	return entities.NewCandidateInput{
		TenantID:    shared.NewTenantID(),
		ContentHash: mustCHash(t),
		Profile:     profile,
		Encrypted: entities.EncryptedPersonal{
			FullName: "enc:full_name",
			Email:    "enc:email",
			Phone:    "enc:phone",
		},
		Location: "Bangalore",
		Headline: "Senior Backend Engineer",
		Source:   "manual_upload",
	}
}

func TestNewCandidate_HappyPath_EmitsParsedEvent(t *testing.T) {
	c, err := entities.NewCandidate(newCandidateInput(t))
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, c.ID())
	assert.Equal(t, "enc:email", c.EncryptedEmail())
	assert.Equal(t, "Bangalore", c.Location())
	assert.Equal(t, 1, c.ProfileSchema())

	evs := c.PullEvents()
	require.Len(t, evs, 1)
	assert.Equal(t, "sourcing.CandidateParsed", evs[0].EventName())
	assert.Empty(t, c.PullEvents(), "PullEvents must drain")
}

func TestNewCandidate_RejectsInvalidProfile(t *testing.T) {
	in := newCandidateInput(t)
	in.Profile = vo.ParsedProfile{} // schema_version=0
	_, err := entities.NewCandidate(in)
	assert.ErrorIs(t, err, vo.ErrInvalidProfile)
}

func TestNewCandidate_RejectsEmptyContentHash(t *testing.T) {
	in := newCandidateInput(t)
	in.ContentHash = vo.ContentHash{}
	_, err := entities.NewCandidate(in)
	assert.Error(t, err)
}

func TestRehydrateCandidate_BypassesEvents(t *testing.T) {
	c, err := entities.NewCandidate(newCandidateInput(t))
	require.NoError(t, err)
	_ = c.PullEvents()

	rh := entities.RehydrateCandidate(entities.RehydrateCandidateInput{
		ID:                c.ID(),
		TenantID:          c.TenantID(),
		ContentHash:       c.ContentHash(),
		EncryptedFullName: c.EncryptedFullName(),
		EncryptedEmail:    c.EncryptedEmail(),
		EncryptedPhone:    c.EncryptedPhone(),
		Location:          c.Location(),
		Headline:          c.Headline(),
		Profile:           c.Profile(),
		Source:            c.Source(),
		CreatedAt:         c.CreatedAt(),
		UpdatedAt:         c.UpdatedAt(),
	})
	assert.Equal(t, c.ID(), rh.ID())
	assert.Empty(t, rh.PullEvents(), "rehydrate must not emit events")
}
