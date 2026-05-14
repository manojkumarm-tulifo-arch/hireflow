//go:build integration

package persistence_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/persistence"
)

// newApplication creates a fresh Application with status=New for tests.
func newApplication(t *testing.T, tenant shared.TenantID, intentID uuid.UUID) *entities.Application {
	t.Helper()
	a, err := entities.NewApplication(entities.NewApplicationInput{
		TenantID:             tenant,
		CandidateID:          uuid.New(),
		IntentID:             intentID,
		IntentSpecVersion:    1,
		ProfileSchemaVersion: 1,
	})
	require.NoError(t, err)
	return a
}

// recordFullScore moves an application through the scoring pipeline:
// RecordRuleMatch → RecordEmbeddingScore → MarkScored.
func recordFullScore(t *testing.T, a *entities.Application, embScore float64) {
	t.Helper()
	report := vo.RuleMatchReport{Results: []vo.RuleResult{
		{Criterion: vo.RuleCriterion{Type: "skill", Name: "Go", Required: true}, Passed: true},
	}}
	require.NoError(t, a.RecordRuleMatch(report))
	require.NoError(t, a.RecordEmbeddingScore(embScore))
	overall := embScore * 20
	require.NoError(t, a.MarkScored(&overall))
}

// TestApplicationSave_PersistsRow saves an application and reads it back via FindByID.
func TestApplicationSave_PersistsRow(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresApplicationRepository(pool)
	tenant := shared.NewTenantID()
	intentID := uuid.New()

	a := newApplication(t, tenant, intentID)
	require.NoError(t, repo.Save(context.Background(), a))

	got, err := repo.FindByID(context.Background(), tenant, a.ID())
	require.NoError(t, err)
	assert.Equal(t, a.ID(), got.ID())
	assert.Equal(t, tenant, got.TenantID())
	assert.Equal(t, a.CandidateID(), got.CandidateID())
	assert.Equal(t, intentID, got.IntentID())
	assert.Equal(t, vo.AppStatusNew, got.Status())
	assert.Equal(t, 1, got.IntentSpecVersion())
	assert.Equal(t, 1, got.ProfileSchemaVersion())
}

// TestApplicationSave_UpsertOnConflict saves the same (tenant, candidate, intent)
// twice and verifies only one row exists with the updated fields.
func TestApplicationSave_UpsertOnConflict(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresApplicationRepository(pool)
	tenant := shared.NewTenantID()
	intentID := uuid.New()
	candidateID := uuid.New()

	a, err := entities.NewApplication(entities.NewApplicationInput{
		TenantID:             tenant,
		CandidateID:          candidateID,
		IntentID:             intentID,
		IntentSpecVersion:    1,
		ProfileSchemaVersion: 1,
	})
	require.NoError(t, err)
	require.NoError(t, repo.Save(context.Background(), a))

	// Simulate an update: move to Excluded, save again.
	require.NoError(t, a.Exclude("test reason"))
	require.NoError(t, repo.Save(context.Background(), a))

	// Only one row must exist.
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM applications WHERE tenant_id=$1 AND candidate_id=$2 AND intent_id=$3`,
		tenant.String(), candidateID, intentID,
	).Scan(&n))
	assert.Equal(t, 1, n)

	// Fetched row must reflect the second save.
	got, err := repo.FindByCandidateAndIntent(context.Background(), tenant, candidateID, intentID)
	require.NoError(t, err)
	assert.Equal(t, vo.AppStatusExcluded, got.Status())
}

// TestApplicationSave_WritesOutbox saves an application that emits an event and
// checks the sourcing_outbox row is written.
func TestApplicationSave_WritesOutbox(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresApplicationRepository(pool)
	tenant := shared.NewTenantID()
	intentID := uuid.New()

	a := newApplication(t, tenant, intentID)
	// Exclude triggers ApplicationExcluded event.
	require.NoError(t, a.Exclude("test exclusion"))

	require.NoError(t, repo.Save(context.Background(), a))

	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM sourcing_outbox
		 WHERE aggregate_id=$1 AND dispatched_at IS NULL`, a.ID()).Scan(&n))
	assert.Equal(t, 1, n)
}

// TestApplicationListByIntent_TenantScoped verifies that tenant A's applications
// are not visible to tenant B.
func TestApplicationListByIntent_TenantScoped(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresApplicationRepository(pool)
	tenantA := shared.NewTenantID()
	tenantB := shared.NewTenantID()
	intentID := uuid.New()

	aA := newApplication(t, tenantA, intentID)
	require.NoError(t, repo.Save(context.Background(), aA))

	aB := newApplication(t, tenantB, intentID)
	require.NoError(t, repo.Save(context.Background(), aB))

	listA, err := repo.ListByIntent(context.Background(), tenantA, intentID, repositories.ApplicationListFilter{})
	require.NoError(t, err)
	require.Len(t, listA, 1)
	assert.Equal(t, aA.ID(), listA[0].ID())

	listB, err := repo.ListByIntent(context.Background(), tenantB, intentID, repositories.ApplicationListFilter{})
	require.NoError(t, err)
	require.Len(t, listB, 1)
	assert.Equal(t, aB.ID(), listB[0].ID())
}

// TestApplicationClaimNextNew_ReturnsNewRowOldestFirst inserts two New applications
// with different next_attempt_at and verifies ClaimNextNew returns the oldest one.
func TestApplicationClaimNextNew_ReturnsNewRowOldestFirst(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresApplicationRepository(pool)
	tenant := shared.NewTenantID()
	intentID := uuid.New()

	earlier := time.Now().UTC().Add(-10 * time.Minute)
	later := time.Now().UTC().Add(-1 * time.Minute)

	aEarly, err := entities.NewApplication(entities.NewApplicationInput{
		TenantID:             tenant,
		CandidateID:          uuid.New(),
		IntentID:             intentID,
		IntentSpecVersion:    1,
		ProfileSchemaVersion: 1,
		Now:                  func() time.Time { return earlier },
	})
	require.NoError(t, err)

	aLate, err := entities.NewApplication(entities.NewApplicationInput{
		TenantID:             tenant,
		CandidateID:          uuid.New(),
		IntentID:             intentID,
		IntentSpecVersion:    1,
		ProfileSchemaVersion: 1,
		Now:                  func() time.Time { return later },
	})
	require.NoError(t, err)

	// Save later first to ensure ordering is by next_attempt_at not insert order.
	require.NoError(t, repo.Save(context.Background(), aLate))
	require.NoError(t, repo.Save(context.Background(), aEarly))

	claimed, err := repo.ClaimNextNew(context.Background())
	require.NoError(t, err)
	assert.Equal(t, aEarly.ID(), claimed.ID(), "earliest next_attempt_at must be claimed first")
}

// TestApplicationInvalidateJudgments_NullsThreeFields seeds two applications for
// the same tenant — one for the target intent and one for a different intent —
// then calls InvalidateJudgmentsForIntent and asserts:
//  1. The three LLM fields (llm_judgment, overall_score, score_band) are NULL
//     for the target application.
//  2. The bystander application (different intent_id) is left untouched.
//  3. No other columns (status, embedding_score, rule_match) are modified.
func TestApplicationInvalidateJudgments_NullsThreeFields(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresApplicationRepository(pool)
	tenant := shared.NewTenantID()
	targetIntentID := uuid.New()
	otherIntentID := uuid.New()

	// Build and fully score the target application.
	target := newApplication(t, tenant, targetIntentID)
	recordFullScore(t, target, 0.75)
	require.NoError(t, repo.Save(context.Background(), target))

	// Build and fully score the bystander application.
	bystander := newApplication(t, tenant, otherIntentID)
	recordFullScore(t, bystander, 0.65)
	require.NoError(t, repo.Save(context.Background(), bystander))

	// Verify both apps have overall_score populated before the call.
	targetBefore, err := repo.FindByID(context.Background(), tenant, target.ID())
	require.NoError(t, err)
	require.NotNil(t, targetBefore.OverallScore(), "target should have overall_score before invalidation")

	bystanderBefore, err := repo.FindByID(context.Background(), tenant, bystander.ID())
	require.NoError(t, err)
	require.NotNil(t, bystanderBefore.OverallScore(), "bystander should have overall_score before invalidation")

	// Invalidate only the target intent.
	require.NoError(t, repo.InvalidateJudgmentsForIntent(context.Background(), tenant, targetIntentID))

	// Assert target's three fields are now NULL.
	var overallScore *float64
	var scoreBand *string
	var llmJudgment *string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT overall_score, score_band, llm_judgment::text
		   FROM applications
		  WHERE tenant_id=$1 AND id=$2`,
		tenant.String(), target.ID(),
	).Scan(&overallScore, &scoreBand, &llmJudgment))
	assert.Nil(t, overallScore, "overall_score must be NULL after invalidation")
	assert.Nil(t, scoreBand, "score_band must be NULL after invalidation")
	assert.Nil(t, llmJudgment, "llm_judgment must be NULL after invalidation")

	// Assert target's status and embedding_score are unchanged.
	targetAfter, err := repo.FindByID(context.Background(), tenant, target.ID())
	require.NoError(t, err)
	assert.Equal(t, targetBefore.Status(), targetAfter.Status(), "status must not change")
	require.NotNil(t, targetAfter.EmbeddingScore(), "embedding_score must survive invalidation")
	assert.InDelta(t, 0.75, *targetAfter.EmbeddingScore(), 1e-4)

	// Assert bystander is untouched.
	bystanderAfter, err := repo.FindByID(context.Background(), tenant, bystander.ID())
	require.NoError(t, err)
	require.NotNil(t, bystanderAfter.OverallScore(), "bystander overall_score must survive")
	assert.Equal(t, bystanderBefore.Status(), bystanderAfter.Status())
}

// TestApplicationTopByCoarseScore_OrdersCorrectly inserts three scored applications
// with different embedding scores and verifies top-2 are returned in correct order.
func TestApplicationTopByCoarseScore_OrdersCorrectly(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresApplicationRepository(pool)
	tenant := shared.NewTenantID()
	intentID := uuid.New()

	// All have PassedRequired=true (required_pass_rate=1.0), so coarse score
	// = 1.0*100 + embedding*20. Higher embedding wins.
	embScores := []float64{0.5, 0.9, 0.3}
	apps := make([]*entities.Application, 3)
	for i, emb := range embScores {
		a := newApplication(t, tenant, intentID)
		recordFullScore(t, a, emb)
		require.NoError(t, repo.Save(context.Background(), a))
		apps[i] = a
	}

	top2, err := repo.TopByCoarseScoreForIntent(context.Background(), tenant, intentID, 2)
	require.NoError(t, err)
	require.Len(t, top2, 2)

	// Highest embedding_score is 0.9 (apps[1]), second is 0.5 (apps[0]).
	assert.Equal(t, apps[1].ID(), top2[0].ID(), "highest embedding score must come first")
	assert.Equal(t, apps[0].ID(), top2[1].ID(), "second highest must come second")
}
