//go:build integration

package persistence_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/persistence"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

func newCandidate(t *testing.T, tenant shared.TenantID, hash string) *entities.Candidate {
	t.Helper()
	h, err := vo.NewContentHash(hash)
	require.NoError(t, err)
	profile := vo.NewParsedProfile()
	profile.Personal.FullName = "Alice"
	c, err := entities.NewCandidate(entities.NewCandidateInput{
		TenantID:    tenant,
		ContentHash: h,
		Profile:     profile,
		Encrypted:   entities.EncryptedPersonal{FullName: "enc:full", Email: "enc:em", Phone: "enc:ph"},
		Location:    "Bangalore",
		Headline:    "SBE",
		Source:      "manual_upload",
	})
	require.NoError(t, err)
	return c
}

func TestCandidateSave_PersistsRow(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresCandidateRepository(pool)
	tenant := shared.NewTenantID()
	c := newCandidate(t, tenant, uuidHex(t))

	got, err := repo.Save(context.Background(), c)
	require.NoError(t, err)
	assert.Equal(t, c.ID(), got.ID())

	fetched, err := repo.FindByID(context.Background(), tenant, c.ID())
	require.NoError(t, err)
	assert.Equal(t, "enc:em", fetched.EncryptedEmail())
	assert.Equal(t, "Bangalore", fetched.Location())
	assert.Equal(t, 1, fetched.ProfileSchema())
}

func TestCandidateSave_DuplicateContentHashReturnsExisting(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresCandidateRepository(pool)
	tenant := shared.NewTenantID()
	hash := uuidHex(t)
	c1 := newCandidate(t, tenant, hash)
	first, err := repo.Save(context.Background(), c1)
	require.NoError(t, err)

	// New aggregate, same hash — Save should return the original.
	c2 := newCandidate(t, tenant, hash)
	second, err := repo.Save(context.Background(), c2)
	require.NoError(t, err)
	assert.Equal(t, first.ID(), second.ID(), "second save must attach to existing candidate")
}

func TestCandidateFindByContentHash_ReturnsRow(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresCandidateRepository(pool)
	tenant := shared.NewTenantID()
	hash := uuidHex(t)
	c := newCandidate(t, tenant, hash)
	_, err := repo.Save(context.Background(), c)
	require.NoError(t, err)

	got, err := repo.FindByContentHash(context.Background(), tenant, hash)
	require.NoError(t, err)
	assert.Equal(t, c.ID(), got.ID())
}
