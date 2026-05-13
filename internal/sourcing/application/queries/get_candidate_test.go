package queries_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/application/queries"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

type stubCandidateRepo struct {
	byID map[string]*entities.Candidate
}

func (r *stubCandidateRepo) Save(context.Context, *entities.Candidate) (*entities.Candidate, error) {
	return nil, nil
}
func (r *stubCandidateRepo) FindByID(_ context.Context, _ shared.TenantID, id uuid.UUID) (*entities.Candidate, error) {
	if c, ok := r.byID[id.String()]; ok {
		return c, nil
	}
	return nil, repositories.ErrCandidateNotFound
}
func (r *stubCandidateRepo) FindByContentHash(context.Context, shared.TenantID, string) (*entities.Candidate, error) {
	return nil, repositories.ErrCandidateNotFound
}

// Reversible "encryptor" for tests — prepends "ENC:" to plaintext.
type stubEncryptor struct{}

func (stubEncryptor) Encrypt(_ context.Context, _ shared.TenantID, p string) (string, error) {
	return "ENC:" + p, nil
}
func (stubEncryptor) Decrypt(_ context.Context, _ shared.TenantID, ct string) (string, error) {
	if len(ct) < 4 || ct[:4] != "ENC:" {
		return "", nil
	}
	return ct[4:], nil
}

func newCandidateForQuery(t *testing.T, tenant shared.TenantID) *entities.Candidate {
	t.Helper()
	h, err := vo.NewContentHash("dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")
	require.NoError(t, err)
	profile := vo.NewParsedProfile()
	profile.Personal.FullName = "Alice"
	profile.Headline = "SBE"
	c, err := entities.NewCandidate(entities.NewCandidateInput{
		TenantID: tenant, ContentHash: h, Profile: profile,
		Encrypted: entities.EncryptedPersonal{
			FullName: "ENC:Alice", Email: "ENC:alice@example.com", Phone: "ENC:+91-555",
		},
		Location: "Bangalore", Headline: "SBE", Source: "manual_upload",
	})
	require.NoError(t, err)
	return c
}

func TestGetCandidate_ReturnsDecryptedPII(t *testing.T) {
	tenant := shared.NewTenantID()
	c := newCandidateForQuery(t, tenant)
	repo := &stubCandidateRepo{byID: map[string]*entities.Candidate{c.ID().String(): c}}
	h := queries.NewGetCandidateHandler(repo, stubEncryptor{})

	got, err := h.Handle(context.Background(), tenant, c.ID())
	require.NoError(t, err)
	assert.Equal(t, c.ID(), got.ID)
	assert.Equal(t, "Alice", got.Personal.FullName)
	assert.Equal(t, "alice@example.com", got.Personal.Email)
	assert.Equal(t, "+91-555", got.Personal.Phone)
	assert.Equal(t, "Bangalore", got.Location)
}

func TestGetCandidate_NotFound(t *testing.T) {
	repo := &stubCandidateRepo{byID: map[string]*entities.Candidate{}}
	h := queries.NewGetCandidateHandler(repo, stubEncryptor{})

	_, err := h.Handle(context.Background(), shared.NewTenantID(), uuid.New())
	assert.ErrorIs(t, err, repositories.ErrCandidateNotFound)
}
