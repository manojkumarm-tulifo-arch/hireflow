package queries_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	auditdomain "github.com/hustle/hireflow/internal/shared/audit/domain"
	auditinfra "github.com/hustle/hireflow/internal/shared/audit/infrastructure"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/queries"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
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
func (r *stubCandidateRepo) ListByTenant(context.Context, shared.TenantID) ([]*entities.Candidate, error) {
	return nil, nil
}
func (r *stubCandidateRepo) UpdateProfileEmbedding(context.Context, uuid.UUID, shared.TenantID, []float32) error {
	return nil
}
func (r *stubCandidateRepo) EraseCascade(_ context.Context, _ shared.TenantID, _ uuid.UUID) ([]string, error) {
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

// capturingAuditWriter records every Write call so tests can assert on it.
type capturingAuditWriter struct {
	events []auditdomain.AuditEvent
	err    error
}

func (w *capturingAuditWriter) Write(_ context.Context, e auditdomain.AuditEvent) error {
	if w.err != nil {
		return w.err
	}
	w.events = append(w.events, e)
	return nil
}

func TestGetCandidate_ReturnsDecryptedPII(t *testing.T) {
	tenant := shared.NewTenantID()
	c := newCandidateForQuery(t, tenant)
	repo := &stubCandidateRepo{byID: map[string]*entities.Candidate{c.ID().String(): c}}
	h := queries.NewGetCandidateHandler(repo, stubEncryptor{}, auditinfra.NewNoopAuditWriter())

	got, err := h.Handle(context.Background(), tenant, uuid.New(), c.ID())
	require.NoError(t, err)
	assert.Equal(t, c.ID(), got.ID)
	assert.Equal(t, "Alice", got.Personal.FullName)
	assert.Equal(t, "alice@example.com", got.Personal.Email)
	assert.Equal(t, "+91-555", got.Personal.Phone)
	assert.Equal(t, "Bangalore", got.Location)
}

func TestGetCandidate_NotFound(t *testing.T) {
	repo := &stubCandidateRepo{byID: map[string]*entities.Candidate{}}
	h := queries.NewGetCandidateHandler(repo, stubEncryptor{}, auditinfra.NewNoopAuditWriter())

	_, err := h.Handle(context.Background(), shared.NewTenantID(), uuid.New(), uuid.New())
	assert.ErrorIs(t, err, repositories.ErrCandidateNotFound)
}

func TestGetCandidate_AuditWrittenOnHappyPath(t *testing.T) {
	tenant := shared.NewTenantID()
	actorID := uuid.New()
	c := newCandidateForQuery(t, tenant)
	repo := &stubCandidateRepo{byID: map[string]*entities.Candidate{c.ID().String(): c}}
	capture := &capturingAuditWriter{}
	h := queries.NewGetCandidateHandler(repo, stubEncryptor{}, capture)

	_, err := h.Handle(context.Background(), tenant, actorID, c.ID())
	require.NoError(t, err)

	require.Len(t, capture.events, 1, "expected exactly one audit event")
	ev := capture.events[0]
	assert.Equal(t, actorID, ev.ActorUserID)
	assert.Equal(t, tenant, ev.TenantID)
	assert.Equal(t, "candidate_read", ev.Action)
	assert.Equal(t, "candidate", ev.ResourceKind)
	assert.Equal(t, c.ID(), ev.ResourceID)
	assert.False(t, ev.OccurredAt.IsZero(), "OccurredAt must be set")
}

func TestGetCandidate_AuditFailurePropagates(t *testing.T) {
	tenant := shared.NewTenantID()
	c := newCandidateForQuery(t, tenant)
	repo := &stubCandidateRepo{byID: map[string]*entities.Candidate{c.ID().String(): c}}
	auditErr := errors.New("db down")
	capture := &capturingAuditWriter{err: auditErr}
	h := queries.NewGetCandidateHandler(repo, stubEncryptor{}, capture)

	_, err := h.Handle(context.Background(), tenant, uuid.New(), c.ID())
	require.Error(t, err)
	assert.ErrorContains(t, err, "audit candidate read")
}

func TestGetCandidate_NotFoundDoesNotAudit(t *testing.T) {
	repo := &stubCandidateRepo{byID: map[string]*entities.Candidate{}}
	capture := &capturingAuditWriter{}
	h := queries.NewGetCandidateHandler(repo, stubEncryptor{}, capture)

	_, err := h.Handle(context.Background(), shared.NewTenantID(), uuid.New(), uuid.New())
	assert.ErrorIs(t, err, repositories.ErrCandidateNotFound)
	assert.Empty(t, capture.events, "no audit event should be written on 404")
}
