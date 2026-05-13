//go:build integration

package persistence_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/persistence"
)

func newJudgeJob(t *testing.T, tenant shared.TenantID, coarseScore float64) *entities.JudgeJob {
	t.Helper()
	return entities.NewJudgeJob(entities.NewJudgeJobInput{
		TenantID:      tenant,
		ApplicationID: uuid.New(),
		IntentID:      uuid.New(),
		CoarseScore:   coarseScore,
	})
}

// TestJudgeJobSave_RoundTrip saves a job and reads it back via FindByID.
func TestJudgeJobSave_RoundTrip(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresJudgeJobRepository(pool)
	tenant := shared.NewTenantID()

	j := newJudgeJob(t, tenant, 75.5)
	require.NoError(t, repo.Save(context.Background(), j))

	got, err := repo.FindByID(context.Background(), j.ID())
	require.NoError(t, err)
	assert.Equal(t, j.ID(), got.ID())
	assert.Equal(t, tenant, got.TenantID())
	assert.Equal(t, j.ApplicationID(), got.ApplicationID())
	assert.InDelta(t, 75.5, got.CoarseScore(), 0.001)
	assert.Equal(t, entities.JobPending, got.Status())
}

// TestJudgeJobClaimNextPending_OrdersByCoarseScoreDesc inserts three jobs with
// varying coarse scores and verifies ClaimNextPending returns the highest.
func TestJudgeJobClaimNextPending_OrdersByCoarseScoreDesc(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresJudgeJobRepository(pool)
	tenant := shared.NewTenantID()

	j1 := newJudgeJob(t, tenant, 50.0)
	j2 := newJudgeJob(t, tenant, 90.0) // highest
	j3 := newJudgeJob(t, tenant, 30.0)

	require.NoError(t, repo.Save(context.Background(), j1))
	require.NoError(t, repo.Save(context.Background(), j2))
	require.NoError(t, repo.Save(context.Background(), j3))

	claimed, err := repo.ClaimNextPending(context.Background())
	require.NoError(t, err)
	assert.Equal(t, j2.ID(), claimed.ID(), "highest coarse_score must be claimed first")
	assert.Equal(t, entities.JobRunning, claimed.Status())

	// Second claim should return j1 (score 50).
	second, err := repo.ClaimNextPending(context.Background())
	require.NoError(t, err)
	assert.Equal(t, j1.ID(), second.ID())
}

// TestJudgeJobClaimNextPending_ReturnsErrNotFoundWhenEmpty expects ErrJudgeJobNotFound
// when there are no pending jobs ready to run.
func TestJudgeJobClaimNextPending_ReturnsErrNotFoundWhenEmpty(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresJudgeJobRepository(pool)

	// Use a random tenant so we don't accidentally pick up rows from other tests.
	// (There is no WHERE tenant_id clause in ClaimNextPending, so we test against
	// an empty table state by relying on the test DB being cleared or jobs not
	// having next_attempt_at in the past by default.)
	// Drain any pending/running jobs left over from sibling tests before asserting.
	_, _ = pool.Exec(context.Background(), `UPDATE judge_jobs SET status='Done' WHERE status IN ('Pending','Running')`)

	_, err := repo.ClaimNextPending(context.Background())
	assert.ErrorIs(t, err, repositories.ErrJudgeJobNotFound)
}
