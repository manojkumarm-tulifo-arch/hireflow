package commands_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/jobposting/application/commands"
	"github.com/hustle/hireflow/internal/jobposting/application/dto"
	"github.com/hustle/hireflow/internal/jobposting/domain/entities"
	"github.com/hustle/hireflow/internal/jobposting/domain/repositories"
	"github.com/hustle/hireflow/internal/jobposting/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// fakeRepo is an in-memory PostingRepository for unit-testing handlers.
type fakeRepo struct {
	mu      sync.Mutex
	byID    map[string]*entities.JobPosting
	byIntID map[string]*entities.JobPosting
	saves   int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		byID:    map[string]*entities.JobPosting{},
		byIntID: map[string]*entities.JobPosting{},
	}
}

func (r *fakeRepo) Save(_ context.Context, p *entities.JobPosting) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[p.ID().String()] = p
	r.byIntID[p.TenantID().String()+":"+p.IntentID()] = p
	r.saves++
	_ = p.PullEvents()
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, tenant shared.TenantID, id valueobjects.PostingID) (*entities.JobPosting, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byID[id.String()]
	if !ok || !p.TenantID().Equals(tenant) {
		return nil, repositories.ErrPostingNotFound
	}
	return p, nil
}

func (r *fakeRepo) FindByIntentID(_ context.Context, tenant shared.TenantID, intentID string) (*entities.JobPosting, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byIntID[tenant.String()+":"+intentID]
	if !ok {
		return nil, repositories.ErrPostingNotFound
	}
	return p, nil
}

func (r *fakeRepo) List(_ context.Context, _ shared.TenantID, _ repositories.PostingFilter) ([]*entities.JobPosting, error) {
	return nil, nil
}

func validInput(tenantID string) dto.CreateFromIntentInput {
	return dto.CreateFromIntentInput{
		TenantID:         tenantID,
		IntentID:         "intent-abc",
		Title:            "Senior Backend Engineer",
		Summary:          "Join our payments platform team.",
		Responsibilities: []string{"Design APIs", "Mentor juniors"},
		Requirements:     []string{"5+ years Go", "Postgres expertise"},
	}
}

func TestCreateFromIntent_HappyPath(t *testing.T) {
	repo := newFakeRepo()
	h := commands.NewCreateFromIntentHandler(repo)
	tenantID := shared.NewTenantID().String()

	out, err := h.Handle(context.Background(), validInput(tenantID))
	require.NoError(t, err)

	assert.Equal(t, "DRAFT", out.Status)
	assert.Equal(t, "Senior Backend Engineer", out.JD.Title)
	assert.Equal(t, 1, out.JD.Version)
	assert.Equal(t, 1, repo.saves)
}

func TestCreateFromIntent_IsIdempotent(t *testing.T) {
	repo := newFakeRepo()
	h := commands.NewCreateFromIntentHandler(repo)
	tenantID := shared.NewTenantID().String()

	first, err := h.Handle(context.Background(), validInput(tenantID))
	require.NoError(t, err)

	second, err := h.Handle(context.Background(), validInput(tenantID))
	require.NoError(t, err)

	assert.Equal(t, first.ID, second.ID, "same intent must return same posting")
	assert.Equal(t, 1, repo.saves, "second call must not persist")
}

func TestCreateFromIntent_RejectsBadInput(t *testing.T) {
	repo := newFakeRepo()
	h := commands.NewCreateFromIntentHandler(repo)

	tests := []struct {
		name   string
		mutate func(in *dto.CreateFromIntentInput)
	}{
		{"invalid tenant", func(in *dto.CreateFromIntentInput) { in.TenantID = "not-a-uuid" }},
		{"empty intent id", func(in *dto.CreateFromIntentInput) { in.IntentID = "" }},
		{"empty title", func(in *dto.CreateFromIntentInput) { in.Title = "  " }},
		{"empty summary", func(in *dto.CreateFromIntentInput) { in.Summary = "" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput(shared.NewTenantID().String())
			tc.mutate(&in)
			_, err := h.Handle(context.Background(), in)
			require.Error(t, err)
		})
	}
}

func TestPublishPosting_HappyPath(t *testing.T) {
	repo := newFakeRepo()
	createH := commands.NewCreateFromIntentHandler(repo)
	publishH := commands.NewPublishPostingHandler(repo)
	tenantID := shared.NewTenantID().String()

	created, err := createH.Handle(context.Background(), validInput(tenantID))
	require.NoError(t, err)

	out, err := publishH.Handle(context.Background(), commands.PublishPostingInput{
		TenantID:  tenantID,
		PostingID: created.ID,
		Channels:  []string{"LINKEDIN", "CAREER_PAGE"},
	})
	require.NoError(t, err)
	assert.Equal(t, "PUBLISHED", out.Status)
	assert.Len(t, out.Sources, 2)
}

func TestPublishPosting_RejectsUnknownChannel(t *testing.T) {
	repo := newFakeRepo()
	createH := commands.NewCreateFromIntentHandler(repo)
	publishH := commands.NewPublishPostingHandler(repo)
	tenantID := shared.NewTenantID().String()

	created, err := createH.Handle(context.Background(), validInput(tenantID))
	require.NoError(t, err)

	_, err = publishH.Handle(context.Background(), commands.PublishPostingInput{
		TenantID:  tenantID,
		PostingID: created.ID,
		Channels:  []string{"PIGEON"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, valueobjects.ErrInvalidSourceChannel))
}

func TestClosePosting_HappyPath(t *testing.T) {
	repo := newFakeRepo()
	createH := commands.NewCreateFromIntentHandler(repo)
	closeH := commands.NewClosePostingHandler(repo)
	tenantID := shared.NewTenantID().String()

	created, err := createH.Handle(context.Background(), validInput(tenantID))
	require.NoError(t, err)

	out, err := closeH.Handle(context.Background(), commands.ClosePostingInput{
		TenantID:  tenantID,
		PostingID: created.ID,
		Reason:    "filled internally",
	})
	require.NoError(t, err)
	assert.Equal(t, "CLOSED", out.Status)
	assert.Equal(t, "filled internally", out.CloseReason)
}
