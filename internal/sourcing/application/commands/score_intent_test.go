package commands_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
)

func TestScoreIntent_HappyPath_CreatesApplicationsForAllTenantCandidates(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	intent := makeIntent(tenantID)

	candidateRepo := newFakeExtendedCandidateRepo()
	for i := 0; i < 3; i++ {
		candidateRepo.addCandidate(makeCandidate(t, tenantID))
	}

	intentReader := newFakeIntentReader()
	intentReader.addIntent(intent)

	appRepo := newFakeApplicationRepo()
	judgeJobRepo := newFakeJudgeJobRepo()

	handler := commands.NewScoreIntentHandler(
		intentReader, appRepo, candidateRepo, judgeJobRepo,
		commands.ScoreIntentConfig{JudgeTopK: 20},
	)
	err := handler.Handle(ctx, commands.ScoreIntentInput{
		TenantID: tenantID,
		IntentID: intent.ID,
	})

	require.NoError(t, err)
	assert.Equal(t, 3, appRepo.savedCount(), "expected one Application per tenant candidate")
}

func TestScoreIntent_IntentNotFound_ReturnsError(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	intentReader := newFakeIntentReader()
	// intent not added — FindByID returns errIntentNotFound

	appRepo := newFakeApplicationRepo()
	candidateRepo := newFakeExtendedCandidateRepo()
	judgeJobRepo := newFakeJudgeJobRepo()

	handler := commands.NewScoreIntentHandler(
		intentReader, appRepo, candidateRepo, judgeJobRepo,
		commands.ScoreIntentConfig{},
	)
	err := handler.Handle(ctx, commands.ScoreIntentInput{
		TenantID: tenantID,
		IntentID: uuid.New(),
	})

	require.Error(t, err)
}

func TestScoreIntent_EmptyCandidatePool_NoApps_NoError(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	intent := makeIntent(tenantID)

	intentReader := newFakeIntentReader()
	intentReader.addIntent(intent)

	candidateRepo := newFakeExtendedCandidateRepo()
	// No candidates added.

	appRepo := newFakeApplicationRepo()
	judgeJobRepo := newFakeJudgeJobRepo()

	handler := commands.NewScoreIntentHandler(
		intentReader, appRepo, candidateRepo, judgeJobRepo,
		commands.ScoreIntentConfig{JudgeTopK: 20},
	)
	err := handler.Handle(ctx, commands.ScoreIntentInput{
		TenantID: tenantID,
		IntentID: intent.ID,
	})

	require.NoError(t, err)
	assert.Equal(t, 0, appRepo.savedCount(), "no applications expected when candidate pool is empty")
	assert.Equal(t, 0, judgeJobRepo.savedCount(), "no judge jobs expected when candidate pool is empty")
}

func TestScoreIntent_EnqueuesJudgeJobsForTopKScoredApps(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	intent := makeIntent(tenantID)

	intentReader := newFakeIntentReader()
	intentReader.addIntent(intent)

	candidateRepo := newFakeExtendedCandidateRepo()
	// No new candidates — topK will drive judge-job creation.

	appRepo := newFakeApplicationRepo()
	// Seed two already-scored apps in topK.
	cand1 := makeCandidate(t, tenantID)
	cand2 := makeCandidate(t, tenantID)
	scored1 := makeScoredApplication(t, tenantID, cand1.ID(), intent.ID)
	scored2 := makeScoredApplication(t, tenantID, cand2.ID(), intent.ID)
	appRepo.topK = []*entities.Application{scored1, scored2}

	judgeJobRepo := newFakeJudgeJobRepo()

	handler := commands.NewScoreIntentHandler(
		intentReader, appRepo, candidateRepo, judgeJobRepo,
		commands.ScoreIntentConfig{JudgeTopK: 5},
	)
	err := handler.Handle(ctx, commands.ScoreIntentInput{
		TenantID: tenantID,
		IntentID: intent.ID,
	})

	require.NoError(t, err)
	assert.Equal(t, 2, judgeJobRepo.savedCount(), "expected one JudgeJob per topK application")
}
