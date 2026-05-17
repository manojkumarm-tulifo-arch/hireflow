//go:build integration

package persistence_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	"github.com/hustle/hireflow/internal/interview/infrastructure/persistence"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// newLoopTemplate creates a fresh LoopTemplate for tests.
func newLoopTemplate(t *testing.T, tenant shared.TenantID, intentID uuid.UUID, rounds []entities.TemplateRound) *entities.LoopTemplate {
	t.Helper()
	lt, err := entities.NewLoopTemplate(entities.NewLoopTemplateInput{
		TenantID: tenant,
		IntentID: intentID,
		Rounds:   rounds,
	})
	require.NoError(t, err)
	return lt
}

var defaultRounds = []entities.TemplateRound{
	{Kind: vo.RoundKindScreen, Sequence: 1},
	{Kind: vo.RoundKindTechnical, Sequence: 2},
	{Kind: vo.RoundKindBarRaiser, Sequence: 3},
}

// TestLoopTemplateSave_PersistsRow saves a template and verifies it exists.
func TestLoopTemplateSave_PersistsRow(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresLoopTemplateRepository(pool)
	tenant := shared.NewTenantID()
	intentID := uuid.New()

	lt := newLoopTemplate(t, tenant, intentID, defaultRounds)
	require.NoError(t, repo.Save(context.Background(), lt))

	got, err := repo.FindByIntent(context.Background(), tenant, intentID)
	require.NoError(t, err)
	assert.Equal(t, lt.ID(), got.ID())
	assert.Equal(t, tenant, got.TenantID())
	assert.Equal(t, intentID, got.IntentID())
	assert.Len(t, got.Rounds(), 3)
}

// TestLoopTemplateSave_UpsertOnConflict verifies that saving the same
// (tenant, intent) twice results in a single row with the updated shape.
func TestLoopTemplateSave_UpsertOnConflict(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresLoopTemplateRepository(pool)
	tenant := shared.NewTenantID()
	intentID := uuid.New()

	lt1 := newLoopTemplate(t, tenant, intentID, defaultRounds)
	require.NoError(t, repo.Save(context.Background(), lt1))

	// Replace rounds with a shorter list.
	lt2 := newLoopTemplate(t, tenant, intentID, []entities.TemplateRound{
		{Kind: vo.RoundKindScreen, Sequence: 1},
		{Kind: vo.RoundKindBehavioral, Sequence: 2},
	})
	require.NoError(t, repo.Save(context.Background(), lt2))

	// Only one row should exist.
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM intent_loops WHERE tenant_id=$1 AND intent_id=$2`,
		tenant.String(), intentID).Scan(&n))
	assert.Equal(t, 1, n)

	// FindByIntent returns the second shape.
	got, err := repo.FindByIntent(context.Background(), tenant, intentID)
	require.NoError(t, err)
	assert.Len(t, got.Rounds(), 2)
	assert.Equal(t, vo.RoundKindBehavioral, got.Rounds()[1].Kind)
}

// TestLoopTemplateFindByIntent_NotFound verifies ErrLoopTemplateNotFound is
// returned when no row exists.
func TestLoopTemplateFindByIntent_NotFound(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresLoopTemplateRepository(pool)

	_, err := repo.FindByIntent(context.Background(), shared.NewTenantID(), uuid.New())
	assert.ErrorIs(t, err, repositories.ErrLoopTemplateNotFound)
}

// TestLoopTemplateFindByIntent_TenantScoped verifies that tenant A's template
// is not visible to tenant B.
func TestLoopTemplateFindByIntent_TenantScoped(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresLoopTemplateRepository(pool)
	tenantA := shared.NewTenantID()
	tenantB := shared.NewTenantID()
	intentID := uuid.New()

	ltA := newLoopTemplate(t, tenantA, intentID, defaultRounds)
	require.NoError(t, repo.Save(context.Background(), ltA))

	got, err := repo.FindByIntent(context.Background(), tenantA, intentID)
	require.NoError(t, err)
	assert.Equal(t, ltA.ID(), got.ID())

	_, err = repo.FindByIntent(context.Background(), tenantB, intentID)
	assert.ErrorIs(t, err, repositories.ErrLoopTemplateNotFound)
}
