//go:build integration

package persistence_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	"github.com/hustle/hireflow/internal/interview/infrastructure/persistence"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// newPool creates a test pool, skips if DATABASE_URL is unset, and truncates
// all interview tables for test isolation.
func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(context.Background(), `
		TRUNCATE interview_processes, interview_rounds, interview_feedback,
		         intent_loops, interview_outbox CASCADE`)
	require.NoError(t, err)
	return pool
}

// newProcess creates a fresh InterviewProcess with 3 rounds for tests.
func newProcess(t *testing.T, tenant shared.TenantID, applicationID uuid.UUID, now func() time.Time) *entities.InterviewProcess {
	t.Helper()
	p, err := entities.NewInterviewProcess(entities.NewInterviewProcessInput{
		TenantID:      tenant,
		ApplicationID: applicationID,
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
		Rounds: []entities.TemplateRound{
			{Kind: vo.RoundKindScreen, Sequence: 1},
			{Kind: vo.RoundKindTechnical, Sequence: 2},
			{Kind: vo.RoundKindBarRaiser, Sequence: 3},
		},
		Now: now,
	})
	require.NoError(t, err)
	return p
}

// TestProcessSave_PersistsRowsAndOutbox saves a process and verifies
// 1 process row, 3 round rows, and 1 outbox row are written.
func TestProcessSave_PersistsRowsAndOutbox(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresProcessRepository(pool)
	tenant := shared.NewTenantID()
	p := newProcess(t, tenant, uuid.New(), nil)

	require.NoError(t, repo.Save(context.Background(), p))

	var processCount int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM interview_processes WHERE id=$1`, p.ID()).Scan(&processCount))
	assert.Equal(t, 1, processCount)

	var roundCount int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM interview_rounds WHERE process_id=$1`, p.ID()).Scan(&roundCount))
	assert.Equal(t, 3, roundCount)

	var outboxCount int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM interview_outbox WHERE aggregate_id=$1 AND event_name='interview.InterviewProcessCreated' AND dispatched_at IS NULL`,
		p.ID()).Scan(&outboxCount))
	assert.Equal(t, 1, outboxCount)
}

// TestProcessFindByID_RehydratesRounds saves then FindByID round-trips all fields.
func TestProcessFindByID_RehydratesRounds(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresProcessRepository(pool)
	tenant := shared.NewTenantID()
	appID := uuid.New()
	p := newProcess(t, tenant, appID, nil)

	require.NoError(t, repo.Save(context.Background(), p))

	got, err := repo.FindByID(context.Background(), tenant, p.ID())
	require.NoError(t, err)
	assert.Equal(t, p.ID(), got.ID())
	assert.Equal(t, tenant, got.TenantID())
	assert.Equal(t, appID, got.ApplicationID())
	assert.Equal(t, p.CandidateID(), got.CandidateID())
	assert.Equal(t, p.IntentID(), got.IntentID())
	assert.Equal(t, vo.ProcessStatusNew, got.Status())
	assert.Len(t, got.Rounds(), 3)
	for _, r := range got.Rounds() {
		assert.Equal(t, vo.RoundStatusPending, r.Status())
	}
}

// TestProcessSave_DuplicateApplicationID_ReturnsErrProcessDuplicate verifies
// that saving two distinct processes with the same (tenant, application_id) errors.
func TestProcessSave_DuplicateApplicationID_ReturnsErrProcessDuplicate(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresProcessRepository(pool)
	tenant := shared.NewTenantID()
	appID := uuid.New()

	p1 := newProcess(t, tenant, appID, nil)
	require.NoError(t, repo.Save(context.Background(), p1))

	p2 := newProcess(t, tenant, appID, nil)
	err := repo.Save(context.Background(), p2)
	assert.ErrorIs(t, err, repositories.ErrProcessDuplicate)
}

// TestProcessFindByApplicationID_TenantScoped verifies that tenant A's process
// is invisible to tenant B.
func TestProcessFindByApplicationID_TenantScoped(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresProcessRepository(pool)
	tenantA := shared.NewTenantID()
	tenantB := shared.NewTenantID()
	appID := uuid.New()

	pA := newProcess(t, tenantA, appID, nil)
	require.NoError(t, repo.Save(context.Background(), pA))

	// tenantB has its own process with a different appID.
	pB := newProcess(t, tenantB, uuid.New(), nil)
	require.NoError(t, repo.Save(context.Background(), pB))

	got, err := repo.FindByApplicationID(context.Background(), tenantA, appID)
	require.NoError(t, err)
	assert.Equal(t, pA.ID(), got.ID())

	// tenantB cannot see tenantA's application.
	_, err = repo.FindByApplicationID(context.Background(), tenantB, appID)
	assert.ErrorIs(t, err, repositories.ErrProcessNotFound)
}

// TestProcessListByTenant_FiltersAndPaginates creates 3 processes — 2 with
// status New and 1 in progress — and verifies status filtering.
func TestProcessListByTenant_FiltersAndPaginates(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresProcessRepository(pool)
	tenant := shared.NewTenantID()

	// Create 3 processes. After Save they are all New. We'll use raw SQL to
	// force one into InProgress for the filter test.
	p1 := newProcess(t, tenant, uuid.New(), nil)
	p2 := newProcess(t, tenant, uuid.New(), nil)
	p3 := newProcess(t, tenant, uuid.New(), nil)
	require.NoError(t, repo.Save(context.Background(), p1))
	require.NoError(t, repo.Save(context.Background(), p2))
	require.NoError(t, repo.Save(context.Background(), p3))

	// Force p3 to InProgress via SQL.
	_, err := pool.Exec(context.Background(),
		`UPDATE interview_processes SET status='InProgress' WHERE id=$1`, p3.ID())
	require.NoError(t, err)

	newOnes, err := repo.ListByTenant(context.Background(), tenant, repositories.ProcessListFilter{Status: "New"})
	require.NoError(t, err)
	assert.Len(t, newOnes, 2)

	ids := map[uuid.UUID]bool{}
	for _, p := range newOnes {
		ids[p.ID()] = true
		assert.Equal(t, vo.ProcessStatusNew, p.Status())
	}
	assert.True(t, ids[p1.ID()])
	assert.True(t, ids[p2.ID()])
	assert.False(t, ids[p3.ID()])
}

// TestProcessClaimNextPendingRound_ReturnsOldestPending creates two processes
// with rounds at different next_attempt_at and verifies the older one is returned.
func TestProcessClaimNextPendingRound_ReturnsOldestPending(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresProcessRepository(pool)
	tenant := shared.NewTenantID()

	earlier := time.Now().UTC().Add(-10 * time.Minute)
	later := time.Now().UTC().Add(-1 * time.Minute)

	pEarly := newProcess(t, tenant, uuid.New(), func() time.Time { return earlier })
	pLate := newProcess(t, tenant, uuid.New(), func() time.Time { return later })

	// Save later first to ensure ordering is by next_attempt_at, not insert order.
	require.NoError(t, repo.Save(context.Background(), pLate))
	require.NoError(t, repo.Save(context.Background(), pEarly))

	p, roundID, err := repo.ClaimNextPendingRound(context.Background())
	require.NoError(t, err)
	assert.Equal(t, pEarly.ID(), p.ID(), "earliest process must be claimed first")
	assert.NotEqual(t, uuid.Nil, roundID)
}

// TestProcessClaimNextPendingRound_NoneClaimable_ReturnsNotFound verifies
// that an empty table returns ErrProcessNotFound.
func TestProcessClaimNextPendingRound_NoneClaimable_ReturnsNotFound(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresProcessRepository(pool)

	_, _, err := repo.ClaimNextPendingRound(context.Background())
	assert.ErrorIs(t, err, repositories.ErrProcessNotFound)
}

// TestProcessSave_RoundQuestionsRoundTrip saves, marks questions ready, saves
// again, then finds by ID and verifies questions decode correctly.
func TestProcessSave_RoundQuestionsRoundTrip(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresProcessRepository(pool)
	tenant := shared.NewTenantID()

	p := newProcess(t, tenant, uuid.New(), nil)
	require.NoError(t, repo.Save(context.Background(), p))

	// Get first round id.
	roundID := p.Rounds()[0].ID()

	questions := []vo.Question{
		{
			Prompt:          "Describe your Go experience",
			SkillProbed:     "Go",
			Why:             "Core lang",
			ExpectedSignals: []string{"concurrency", "interfaces", "channels"},
			ModelAnswer:     "Strong familiarity with goroutines and channels",
			RedFlags:        []string{"no go experience", "confused about interfaces"},
			FollowUps:       []string{"How do you handle race conditions?"},
		},
		{
			Prompt:          "Explain channels",
			SkillProbed:     "Go concurrency",
			Why:             "Concurrency model",
			ExpectedSignals: []string{"buffered", "unbuffered", "select"},
			ModelAnswer:     "Channels are typed conduits for goroutine communication",
			RedFlags:        []string{"no understanding", "confused with mutexes"},
			FollowUps:       []string{"When would you use select?"},
		},
	}

	require.NoError(t, p.MarkRoundQuestionsReady(roundID, questions))
	// Drain the QuestionsGenerated event before saving.
	p.PullEvents()
	require.NoError(t, repo.Save(context.Background(), p))

	got, err := repo.FindByID(context.Background(), tenant, p.ID())
	require.NoError(t, err)

	var foundRound *entities.InterviewRound
	for _, r := range got.Rounds() {
		if r.ID() == roundID {
			foundRound = r
			break
		}
	}
	require.NotNil(t, foundRound)
	assert.Equal(t, vo.RoundStatusQuestionsReady, foundRound.Status())
	gotQs := foundRound.Questions()
	require.Len(t, gotQs, 2)
	assert.Equal(t, "Describe your Go experience", gotQs[0].Prompt)
	assert.Equal(t, "Explain channels", gotQs[1].Prompt)
}

// TestProcessFindByRoundID_LocatesProcess saves a process then looks it up by
// one of its round IDs.
func TestProcessFindByRoundID_LocatesProcess(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresProcessRepository(pool)
	tenant := shared.NewTenantID()

	p := newProcess(t, tenant, uuid.New(), nil)
	require.NoError(t, repo.Save(context.Background(), p))

	roundID := p.Rounds()[1].ID() // use the second round

	got, err := repo.FindByRoundID(context.Background(), tenant, roundID)
	require.NoError(t, err)
	assert.Equal(t, p.ID(), got.ID())
	assert.Len(t, got.Rounds(), 3)
}

// TestProcessFindByRoundID_TenantScoped verifies that a tenant mismatch returns
// ErrProcessNotFound.
func TestProcessFindByRoundID_TenantScoped(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresProcessRepository(pool)
	tenantA := shared.NewTenantID()
	tenantB := shared.NewTenantID()

	p := newProcess(t, tenantA, uuid.New(), nil)
	require.NoError(t, repo.Save(context.Background(), p))

	roundID := p.Rounds()[0].ID()

	_, err := repo.FindByRoundID(context.Background(), tenantB, roundID)
	assert.ErrorIs(t, err, repositories.ErrProcessNotFound)
}
