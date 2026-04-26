package subscribers_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	intentevents "github.com/hustle/hireflow/internal/hiringintent/domain/events"
	intentvo "github.com/hustle/hireflow/internal/hiringintent/domain/valueobjects"
	"github.com/hustle/hireflow/internal/jobposting/application/commands"
	"github.com/hustle/hireflow/internal/jobposting/domain/entities"
	"github.com/hustle/hireflow/internal/jobposting/domain/repositories"
	"github.com/hustle/hireflow/internal/jobposting/domain/valueobjects"
	"github.com/hustle/hireflow/internal/jobposting/infrastructure/subscribers"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// fakeReader returns a canned IntentSnapshot for any (tenant, intent) pair.
type fakeReader struct {
	calls int
	snap  subscribers.IntentSnapshot
}

func (r *fakeReader) ReadConfirmed(_ context.Context, tenant, intent string) (subscribers.IntentSnapshot, error) {
	r.calls++
	r.snap.TenantID = tenant
	r.snap.IntentID = intent
	return r.snap, nil
}

// fakeRepo: minimal in-memory PostingRepository (mirrors the one used in commands tests).
type fakeRepo struct {
	mu      sync.Mutex
	byIntID map[string]*entities.JobPosting
	saves   int
}

func newFakeRepo() *fakeRepo { return &fakeRepo{byIntID: map[string]*entities.JobPosting{}} }

func (r *fakeRepo) Save(_ context.Context, p *entities.JobPosting) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byIntID[p.TenantID().String()+":"+p.IntentID()] = p
	r.saves++
	_ = p.PullEvents()
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, _ shared.TenantID, _ valueobjects.PostingID) (*entities.JobPosting, error) {
	return nil, repositories.ErrPostingNotFound
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

func TestIntentConfirmedConsumer_CreatesDraftPosting(t *testing.T) {
	repo := newFakeRepo()
	createH := commands.NewCreateFromIntentHandler(repo)
	reader := &fakeReader{snap: subscribers.IntentSnapshot{
		RoleTitle:      "Senior Backend Engineer",
		RoleSummary:    "Build payments infra.",
		RequiredSkills: []string{"Go", "Postgres"},
		Headcount:      2,
		MinYears:       3,
		MaxYears:       7,
	}}
	consumer := subscribers.NewIntentConfirmedConsumer(reader, createH)

	intentID := intentvo.NewIntentID()
	tenantID := shared.NewTenantID()
	recruiterID := shared.NewRecruiterID()
	event := intentevents.NewIntentConfirmed(intentID, tenantID, recruiterID, intentvo.PriorityHigh, time.Now().UTC())

	err := consumer.Consume(context.Background(), event)
	require.NoError(t, err)

	assert.Equal(t, 1, reader.calls)
	assert.Equal(t, 1, repo.saves)
}

func TestIntentConfirmedConsumer_IsIdempotent(t *testing.T) {
	repo := newFakeRepo()
	createH := commands.NewCreateFromIntentHandler(repo)
	reader := &fakeReader{snap: subscribers.IntentSnapshot{
		RoleTitle:      "Staff Engineer",
		RoleSummary:    "Lead architecture.",
		RequiredSkills: []string{"Go"},
		Headcount:      1,
	}}
	consumer := subscribers.NewIntentConfirmedConsumer(reader, createH)

	intentID := intentvo.NewIntentID()
	tenantID := shared.NewTenantID()
	recruiterID := shared.NewRecruiterID()
	event := intentevents.NewIntentConfirmed(intentID, tenantID, recruiterID, intentvo.PriorityMedium, time.Now().UTC())

	require.NoError(t, consumer.Consume(context.Background(), event))
	require.NoError(t, consumer.Consume(context.Background(), event))

	// CreateFromIntent looks up by intent first, so the second consume must NOT save.
	assert.Equal(t, 1, repo.saves, "redelivery must not create a duplicate posting")
}

// readerErr forwards an error from ReadConfirmed.
type readerErr struct{}

func (readerErr) ReadConfirmed(_ context.Context, _, _ string) (subscribers.IntentSnapshot, error) {
	return subscribers.IntentSnapshot{}, errors.New("boom")
}

func TestIntentConfirmedConsumer_PropagatesReaderError(t *testing.T) {
	repo := newFakeRepo()
	createH := commands.NewCreateFromIntentHandler(repo)
	consumer := subscribers.NewIntentConfirmedConsumer(readerErr{}, createH)

	intentID := intentvo.NewIntentID()
	tenantID := shared.NewTenantID()
	recruiterID := shared.NewRecruiterID()
	event := intentevents.NewIntentConfirmed(intentID, tenantID, recruiterID, intentvo.PriorityLow, time.Now().UTC())

	err := consumer.Consume(context.Background(), event)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read intent")
	assert.Equal(t, 0, repo.saves)
}
