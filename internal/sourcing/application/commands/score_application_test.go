package commands_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

func newScoreApplicationHandler(
	appRepo *fakeApplicationRepo,
	candidateRepo *fakeExtendedCandidateRepo,
	intentReader *fakeIntentReader,
	embedder *fakeEmbedder,
	matchScorer *fakeMatchScorer,
	intentEmbRepo *fakeIntentEmbeddingRepo,
) *commands.ScoreApplicationHandler {
	return commands.NewScoreApplicationHandler(
		appRepo,
		candidateRepo,
		intentReader,
		embedder,
		matchScorer,
		intentEmbRepo,
		commands.ScoreApplicationConfig{},
	)
}

func TestScoreApplication_HappyPath_RulePassEmbedScoreSet(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	intent := makeIntent(tenantID)
	candidate := makeCandidate(t, tenantID)
	app := makeNewApplication(t, tenantID, candidate.ID(), intent.ID)

	candidateRepo := newFakeExtendedCandidateRepo()
	candidateRepo.addCandidate(candidate)

	intentReader := newFakeIntentReader()
	intentReader.addIntent(intent)

	appRepo := newFakeApplicationRepo()

	embedder := newFakeEmbedder()
	matchScorer := &fakeMatchScorer{out: passingMatchOutput()}
	intentEmbRepo := newFakeIntentEmbeddingRepo()

	handler := newScoreApplicationHandler(appRepo, candidateRepo, intentReader, embedder, matchScorer, intentEmbRepo)
	err := handler.Handle(ctx, app)

	require.NoError(t, err)
	assert.Equal(t, vo.AppStatusScored, app.Status(), "expected application status Scored")
	assert.NotNil(t, app.EmbeddingScore(), "expected embedding score to be set")
	assert.Equal(t, 1, appRepo.savedCount())
}

func TestScoreApplication_RuleFailed_Excludes(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	intent := makeIntent(tenantID)
	candidate := makeCandidate(t, tenantID)
	app := makeNewApplication(t, tenantID, candidate.ID(), intent.ID)

	candidateRepo := newFakeExtendedCandidateRepo()
	candidateRepo.addCandidate(candidate)

	intentReader := newFakeIntentReader()
	intentReader.addIntent(intent)

	appRepo := newFakeApplicationRepo()

	embedder := newFakeEmbedder()
	matchScorer := &fakeMatchScorer{out: failingMatchOutput()}
	intentEmbRepo := newFakeIntentEmbeddingRepo()

	handler := newScoreApplicationHandler(appRepo, candidateRepo, intentReader, embedder, matchScorer, intentEmbRepo)
	err := handler.Handle(ctx, app)

	require.NoError(t, err)
	assert.Equal(t, vo.AppStatusExcluded, app.Status(), "expected application status Excluded when required rule fails")
}

func TestScoreApplication_EmbedderRetryable_SchedulesRetry(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	intent := makeIntent(tenantID)
	candidate := makeCandidate(t, tenantID)
	app := makeNewApplication(t, tenantID, candidate.ID(), intent.ID)

	candidateRepo := newFakeExtendedCandidateRepo()
	candidateRepo.addCandidate(candidate)

	intentReader := newFakeIntentReader()
	intentReader.addIntent(intent)

	appRepo := newFakeApplicationRepo()

	embedder := newFakeEmbedder()
	embedder.err = services.EmbeddingError{Retryable: true, Reason: "rate_limit"}
	matchScorer := &fakeMatchScorer{}
	intentEmbRepo := newFakeIntentEmbeddingRepo()

	handler := newScoreApplicationHandler(appRepo, candidateRepo, intentReader, embedder, matchScorer, intentEmbRepo)
	err := handler.Handle(ctx, app)

	require.NoError(t, err, "retryable embed error should not propagate as handler error")
	assert.Equal(t, 1, app.AttemptCount(), "expected attempt count incremented")
	assert.Equal(t, 1, appRepo.savedCount(), "application should be saved with updated retry state")
}

func TestScoreApplication_EmbedderFatal_MarksEmbedFailed(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	intent := makeIntent(tenantID)
	candidate := makeCandidate(t, tenantID)
	app := makeNewApplication(t, tenantID, candidate.ID(), intent.ID)

	candidateRepo := newFakeExtendedCandidateRepo()
	candidateRepo.addCandidate(candidate)

	intentReader := newFakeIntentReader()
	intentReader.addIntent(intent)

	appRepo := newFakeApplicationRepo()

	embedder := newFakeEmbedder()
	embedder.err = services.EmbeddingError{Retryable: false, Reason: "content_policy"}
	matchScorer := &fakeMatchScorer{}
	intentEmbRepo := newFakeIntentEmbeddingRepo()

	handler := newScoreApplicationHandler(appRepo, candidateRepo, intentReader, embedder, matchScorer, intentEmbRepo)
	err := handler.Handle(ctx, app)

	require.NoError(t, err, "fatal embed error should not propagate as handler error")
	assert.Equal(t, vo.AppStatusEmbedFailed, app.Status(), "expected application status EmbedFailed")
	assert.Equal(t, 1, appRepo.savedCount())
}

func TestScoreApplication_ExistingCandidateEmbedding_SkipsEmbedderCallForCandidate(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	intent := makeIntent(tenantID)
	// Use candidate with pre-existing profile embedding.
	candidate := makeCandidateWithEmbedding(t, tenantID)
	app := makeNewApplication(t, tenantID, candidate.ID(), intent.ID)

	candidateRepo := newFakeExtendedCandidateRepo()
	candidateRepo.addCandidate(candidate)

	intentReader := newFakeIntentReader()
	intentReader.addIntent(intent)
	// Intent embedding not cached — embedder will be called once for the intent role text.

	appRepo := newFakeApplicationRepo()
	embedder := newFakeEmbedder()
	matchScorer := &fakeMatchScorer{out: passingMatchOutput()}
	intentEmbRepo := newFakeIntentEmbeddingRepo()

	handler := newScoreApplicationHandler(appRepo, candidateRepo, intentReader, embedder, matchScorer, intentEmbRepo)
	err := handler.Handle(ctx, app)

	require.NoError(t, err)
	assert.Equal(t, 1, embedder.calls(), "embedder should be called exactly once (for intent), not for candidate")
	assert.Equal(t, vo.AppStatusScored, app.Status())
}
