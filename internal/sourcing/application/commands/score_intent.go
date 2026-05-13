package commands

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
)

// ScoreIntentInput is the input for the ScoreIntentHandler.
type ScoreIntentInput struct {
	TenantID shared.TenantID
	IntentID uuid.UUID
}

// ScoreIntentConfig holds configuration for ScoreIntentHandler.
type ScoreIntentConfig struct {
	// JudgeTopK is the maximum number of Applications to enqueue for LLM judging
	// after the fan-out completes. Default: 20.
	JudgeTopK int
}

// ScoreIntentHandler is the fan-out command triggered by an IntentConfirmed event.
// Its responsibilities are:
//  1. Confirm the intent is in Confirmed status.
//  2. Create Application rows (status=New) for every parsed candidate in the tenant.
//  3. After fan-out, enqueue JudgeJobs for the top-K applications ordered by coarse
//     score (= required_pass_rate*100 + embedding_score*20) for LLM judging.
//
// Steps 1+2 are fast (no Voyage calls). Step 3 is lightweight — it just inserts
// JudgeJob rows that the judge worker pool will claim asynchronously.
//
// NOTE: The actual embedding + rule + cosine scoring of the new Application rows is
// done by the match worker (ScoreApplicationHandler, T17). ScoreIntent creates the
// rows in New status and later triggers the judge queue once scoring completes.
type ScoreIntentHandler struct {
	intentReader        services.IntentReader
	applicationRepo     repositories.ApplicationRepository
	candidateRepo       repositories.CandidateRepository
	judgeJobRepo        repositories.JudgeJobRepository
	cfg                 ScoreIntentConfig
}

// NewScoreIntentHandler wires the handler.
func NewScoreIntentHandler(
	ir services.IntentReader,
	ar repositories.ApplicationRepository,
	cr repositories.CandidateRepository,
	jr repositories.JudgeJobRepository,
	cfg ScoreIntentConfig,
) *ScoreIntentHandler {
	if cfg.JudgeTopK <= 0 {
		cfg.JudgeTopK = 20
	}
	return &ScoreIntentHandler{
		intentReader:    ir,
		applicationRepo: ar,
		candidateRepo:   cr,
		judgeJobRepo:    jr,
		cfg:             cfg,
	}
}

// Handle fans out Application rows for all parsed candidates in the tenant, then
// enqueues JudgeJobs for the top-K scored applications.
func (h *ScoreIntentHandler) Handle(ctx context.Context, in ScoreIntentInput) error {
	intent, err := h.intentReader.FindByID(ctx, in.TenantID, in.IntentID)
	if err != nil {
		return fmt.Errorf("score_intent: find intent: %w", err)
	}
	if intent.Status != "Confirmed" {
		return fmt.Errorf("score_intent: intent %s is not Confirmed (status=%s)", in.IntentID, intent.Status)
	}

	candidates, err := h.candidateRepo.ListByTenant(ctx, in.TenantID)
	if err != nil {
		return fmt.Errorf("score_intent: list candidates: %w", err)
	}

	for _, cand := range candidates {
		app, err := h.applicationRepo.FindByCandidateAndIntent(ctx, in.TenantID, cand.ID(), in.IntentID)
		if err != nil && err != repositories.ErrApplicationNotFound {
			return fmt.Errorf("score_intent: find application: %w", err)
		}
		if app != nil {
			// Already exists — idempotent.
			continue
		}

		newApp, err := entities.NewApplication(entities.NewApplicationInput{
			TenantID:             in.TenantID,
			CandidateID:          cand.ID(),
			IntentID:             in.IntentID,
			IntentSpecVersion:    intent.SpecVersion,
			ProfileSchemaVersion: cand.ProfileSchema(),
		})
		if err != nil {
			return fmt.Errorf("score_intent: new application: %w", err)
		}
		if err := h.applicationRepo.Save(ctx, newApp); err != nil {
			return fmt.Errorf("score_intent: save application: %w", err)
		}
	}

	// Enqueue JudgeJobs for top-K already-scored applications.
	// Applications that were just created (New status) will not appear in
	// TopByCoarseScoreForIntent because they have no embedding_score yet —
	// they become judge candidates after the match worker scores them.
	// This handles the re-confirm case: when an intent is re-confirmed,
	// previously-scored applications for earlier spec_versions may already
	// exist and can be judged immediately.
	topK, err := h.applicationRepo.TopByCoarseScoreForIntent(ctx, in.TenantID, in.IntentID, h.cfg.JudgeTopK)
	if err != nil {
		return fmt.Errorf("score_intent: top by coarse score: %w", err)
	}

	for _, app := range topK {
		job := entities.NewJudgeJob(entities.NewJudgeJobInput{
			TenantID:      in.TenantID,
			ApplicationID: app.ID(),
			IntentID:      in.IntentID,
			CoarseScore:   coarseScore(app),
		})
		if err := h.judgeJobRepo.Save(ctx, job); err != nil {
			return fmt.Errorf("score_intent: save judge job: %w", err)
		}
	}

	return nil
}

// coarseScore computes required_pass_rate*100 + embedding_score*20 for an Application.
// Returns 0 when embedding_score is nil (should not happen after TopByCoarseScoreForIntent
// which filters for non-nil scores, but we guard defensively).
func coarseScore(app *entities.Application) float64 {
	var embScore float64
	if app.EmbeddingScore() != nil {
		embScore = *app.EmbeddingScore()
	}
	rulePassRate := app.RuleMatch().RequiredPassRate()
	return rulePassRate*100 + embScore*20
}
