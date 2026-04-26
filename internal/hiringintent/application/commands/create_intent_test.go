package commands_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/hiringintent/application/commands"
	"github.com/hustle/hireflow/internal/hiringintent/domain/entities"
	"github.com/hustle/hireflow/internal/hiringintent/domain/repositories"
	"github.com/hustle/hireflow/internal/hiringintent/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// fakeRepo is an in-memory IntentRepository for unit-testing application handlers.
type fakeRepo struct {
	mu    sync.Mutex
	items map[string]*entities.HiringIntent
	saves int
}

func newFakeRepo() *fakeRepo { return &fakeRepo{items: map[string]*entities.HiringIntent{}} }

func (r *fakeRepo) Save(_ context.Context, intent *entities.HiringIntent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[intent.ID().String()] = intent
	r.saves++
	_ = intent.PullEvents() // simulate outbox drain
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, tenantID shared.TenantID, id valueobjects.IntentID) (*entities.HiringIntent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	intent, ok := r.items[id.String()]
	if !ok || !intent.TenantID().Equals(tenantID) {
		return nil, repositories.ErrIntentNotFound
	}
	return intent, nil
}

func (r *fakeRepo) List(_ context.Context, _ shared.TenantID, _ repositories.IntentFilter) ([]*entities.HiringIntent, error) {
	return nil, nil
}

func (r *fakeRepo) Counts(_ context.Context, _ shared.TenantID) (repositories.StatusCounts, error) {
	return repositories.StatusCounts{}, nil
}

func validInput(tenantID, recruiterID string) commands.CreateIntentInput {
	return commands.CreateIntentInput{
		TenantID:    tenantID,
		RecruiterID: recruiterID,
		RoleTitle:   "Senior Backend Engineer",
		Skills: []commands.SkillInput{
			{Name: "Go", Required: true},
			{Name: "Postgres", Required: false},
		},
		MinYears:  3,
		MaxYears:  7,
		Headcount: 2,
		Locations: []string{"Bangalore", "Remote"},
		WorkMode:  "HYBRID",
		Priority:  "HIGH",
	}
}

func TestCreateIntent_HappyPath(t *testing.T) {
	repo := newFakeRepo()
	h := commands.NewCreateIntentHandler(repo)
	tenantID := shared.NewTenantID().String()
	recruiterID := shared.NewRecruiterID().String()

	out, err := h.Handle(context.Background(), validInput(tenantID, recruiterID))
	require.NoError(t, err)

	assert.Equal(t, "DRAFTED", out.Status)
	assert.Equal(t, "Senior Backend Engineer", out.Role.Title)
	assert.Equal(t, 2, out.Role.Headcount)
	assert.Equal(t, "HIGH", out.Priority)
	assert.Equal(t, 1, repo.saves)
	assert.NotEmpty(t, out.ID)
}

func TestCreateIntent_RejectsBadInput(t *testing.T) {
	repo := newFakeRepo()
	h := commands.NewCreateIntentHandler(repo)
	tenantID := shared.NewTenantID().String()
	recruiterID := shared.NewRecruiterID().String()

	tests := []struct {
		name    string
		mutate  func(in *commands.CreateIntentInput)
		wantErr error
	}{
		{
			name:    "invalid tenant id",
			mutate:  func(in *commands.CreateIntentInput) { in.TenantID = "not-a-uuid" },
			wantErr: shared.ErrInvalidTenantID,
		},
		{
			name:    "invalid recruiter id",
			mutate:  func(in *commands.CreateIntentInput) { in.RecruiterID = "not-a-uuid" },
			wantErr: shared.ErrInvalidRecruiterID,
		},
		{
			name:    "experience min greater than max",
			mutate:  func(in *commands.CreateIntentInput) { in.MinYears = 8; in.MaxYears = 3 },
			wantErr: valueobjects.ErrInvalidExperienceRange,
		},
		{
			name:    "zero headcount",
			mutate:  func(in *commands.CreateIntentInput) { in.Headcount = 0 },
			wantErr: valueobjects.ErrInvalidHeadcount,
		},
		{
			name:    "empty role title",
			mutate:  func(in *commands.CreateIntentInput) { in.RoleTitle = "  " },
			wantErr: valueobjects.ErrEmptyRoleTitle,
		},
		{
			name:    "invalid work mode",
			mutate:  func(in *commands.CreateIntentInput) { in.WorkMode = "BIWEEKLY" },
			wantErr: valueobjects.ErrInvalidWorkMode,
		},
		{
			name:    "invalid priority",
			mutate:  func(in *commands.CreateIntentInput) { in.Priority = "URGENT" },
			wantErr: valueobjects.ErrInvalidPriority,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput(tenantID, recruiterID)
			tc.mutate(&in)
			_, err := h.Handle(context.Background(), in)
			require.Error(t, err)
			assert.True(t, errors.Is(err, tc.wantErr), "got %v want %v", err, tc.wantErr)
		})
	}
}

func TestConfirmIntent_HappyPath(t *testing.T) {
	repo := newFakeRepo()
	createH := commands.NewCreateIntentHandler(repo)
	confirmH := commands.NewConfirmIntentHandler(repo)
	tenantID := shared.NewTenantID().String()
	recruiterID := shared.NewRecruiterID().String()

	created, err := createH.Handle(context.Background(), validInput(tenantID, recruiterID))
	require.NoError(t, err)

	confirmed, err := confirmH.Handle(context.Background(), commands.ConfirmIntentInput{
		TenantID: tenantID,
		IntentID: created.ID,
	})
	require.NoError(t, err)

	assert.Equal(t, "CONFIRMED", confirmed.Status)
	assert.NotNil(t, confirmed.ConfirmedAt)
	assert.Equal(t, 2, repo.saves)
}

func TestConfirmIntent_TenantMismatchReturnsNotFound(t *testing.T) {
	repo := newFakeRepo()
	createH := commands.NewCreateIntentHandler(repo)
	confirmH := commands.NewConfirmIntentHandler(repo)
	tenantA := shared.NewTenantID().String()
	tenantB := shared.NewTenantID().String()
	recruiter := shared.NewRecruiterID().String()

	created, err := createH.Handle(context.Background(), validInput(tenantA, recruiter))
	require.NoError(t, err)

	_, err = confirmH.Handle(context.Background(), commands.ConfirmIntentInput{
		TenantID: tenantB,
		IntentID: created.ID,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, repositories.ErrIntentNotFound))
}

func TestConfirmIntent_BadIDFormat(t *testing.T) {
	repo := newFakeRepo()
	confirmH := commands.NewConfirmIntentHandler(repo)
	_, err := confirmH.Handle(context.Background(), commands.ConfirmIntentInput{
		TenantID: shared.NewTenantID().String(),
		IntentID: "not-a-uuid",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, valueobjects.ErrInvalidIntentID))
}
