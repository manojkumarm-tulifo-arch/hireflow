package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/scoring"
)

// ScoreApplicationConfig holds configuration for ScoreApplicationHandler.
type ScoreApplicationConfig struct {
	// RetryBackoff is the sequence of delays for embedding retries.
	// Attempt n uses schedule[n-1]; schedule exhaustion → MarkEmbedFailed.
	RetryBackoff []time.Duration
	// JudgeTopK is kept here for documentation consistency but is NOT used by
	// ScoreApplication — JudgeJob enqueuing is ScoreIntent's responsibility
	// after the intent fan-out completes.
	JudgeTopK int
}

// ScoreApplicationHandler is the per-Application worker entry.
// It is invoked by the match worker pool (T17 MatchPool) for each Application
// in New status.
//
// Responsibilities:
//  1. Load the Application + Candidate + Intent.
//  2. Embed the candidate profile (skip if already embedded).
//  3. Find-or-create the intent embedding.
//  4. Run rule + cosine scoring via MatchScorer.
//  5. Apply results to the Application aggregate and persist.
//
// NOTE: ScoreApplication does NOT enqueue JudgeJobs. That is ScoreIntent's
// responsibility after the intent fan-out completes and the top-K can be
// computed across all scored candidates for that intent.
type ScoreApplicationHandler struct {
	appRepo              repositories.ApplicationRepository
	candidateRepo        repositories.CandidateRepository
	intentReader         services.IntentReader
	embedder             services.Embedder
	matchScorer          services.MatchScorer
	intentEmbeddingRepo  repositories.IntentEmbeddingRepository
	cfg                  ScoreApplicationConfig
}

// NewScoreApplicationHandler wires the handler.
func NewScoreApplicationHandler(
	appRepo repositories.ApplicationRepository,
	candidateRepo repositories.CandidateRepository,
	intentReader services.IntentReader,
	embedder services.Embedder,
	matchScorer services.MatchScorer,
	intentEmbeddingRepo repositories.IntentEmbeddingRepository,
	cfg ScoreApplicationConfig,
) *ScoreApplicationHandler {
	if len(cfg.RetryBackoff) == 0 {
		cfg.RetryBackoff = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute}
	}
	return &ScoreApplicationHandler{
		appRepo:             appRepo,
		candidateRepo:       candidateRepo,
		intentReader:        intentReader,
		embedder:            embedder,
		matchScorer:         matchScorer,
		intentEmbeddingRepo: intentEmbeddingRepo,
		cfg:                 cfg,
	}
}

// Handle scores one Application aggregate.
func (h *ScoreApplicationHandler) Handle(ctx context.Context, app *entities.Application) error {
	// Step 1: load candidate.
	candidate, err := h.candidateRepo.FindByID(ctx, app.TenantID(), app.CandidateID())
	if err != nil {
		return fmt.Errorf("score_application: find candidate: %w", err)
	}

	// Step 2: embed candidate profile (cached on candidate row).
	candidateVec := candidate.ProfileEmbedding()
	if len(candidateVec) == 0 {
		profileText := scoring.SerializeProfile(candidate.Profile())
		vec, embedErr := h.embedder.EmbedDocument(ctx, profileText)
		if embedErr != nil {
			return h.handleEmbedError(ctx, app, embedErr)
		}
		candidateVec = vec
		// Persist the embedding so future scoring passes skip this call.
		if perr := h.candidateRepo.UpdateProfileEmbedding(ctx, candidate.ID(), app.TenantID(), vec); perr != nil {
			// Best-effort — we can continue scoring without persisting the cache.
			// The next run will simply re-embed.
			_ = perr
		}
	}

	// Step 3: load intent snapshot.
	intent, err := h.intentReader.FindByID(ctx, app.TenantID(), app.IntentID())
	if err != nil {
		return fmt.Errorf("score_application: find intent: %w", err)
	}

	// Step 4: find-or-create intent embedding.
	roleVec, err := h.intentEmbeddingRepo.Find(ctx, app.IntentID(), app.IntentSpecVersion())
	if err != nil {
		if !errors.Is(err, repositories.ErrIntentEmbeddingNotFound) {
			return fmt.Errorf("score_application: find intent embedding: %w", err)
		}
		// Embed and cache.
		roleText := scoring.SerializeRole(intent.Role)
		vec, embedErr := h.embedder.EmbedDocument(ctx, roleText)
		if embedErr != nil {
			return h.handleEmbedError(ctx, app, embedErr)
		}
		roleVec = vec
		if saveErr := h.intentEmbeddingRepo.Save(ctx, app.IntentID(), app.TenantID(), app.IntentSpecVersion(), vec); saveErr != nil {
			// Best-effort — continue without caching.
			_ = saveErr
		}
	}

	// Step 5: run match scoring.
	result, err := h.matchScorer.Score(ctx, services.MatchInput{
		Profile:      candidate.Profile(),
		Role:         intent.Role,
		CandidateVec: candidateVec,
		RoleVec:      roleVec,
	})
	if err != nil {
		return fmt.Errorf("score_application: match scorer: %w", err)
	}

	// Step 6: apply results to the Application.
	if err := app.RecordRuleMatch(result.Rules); err != nil {
		return fmt.Errorf("score_application: record rule match: %w", err)
	}

	if !result.Rules.PassedRequired() {
		// Required rule criteria not met — exclude this application.
		if err := app.Exclude("rule_failed"); err != nil {
			return fmt.Errorf("score_application: exclude: %w", err)
		}
		return h.appRepo.Save(ctx, app)
	}

	// Record embedding score.
	if result.EmbeddingScore != nil {
		if err := app.RecordEmbeddingScore(*result.EmbeddingScore); err != nil {
			return fmt.Errorf("score_application: record embedding score: %w", err)
		}
	}

	// Mark scored (overallScore is nil here — ScoreIntent enqueues JudgeJobs
	// separately; overall_score is only set after LLM judging).
	if err := app.MarkScored(nil); err != nil {
		return fmt.Errorf("score_application: mark scored: %w", err)
	}

	return h.appRepo.Save(ctx, app)
}

// handleEmbedError classifies an embedding error and either schedules a retry
// or marks the application as EmbedFailed.
func (h *ScoreApplicationHandler) handleEmbedError(ctx context.Context, app *entities.Application, embedErr error) error {
	var embErr services.EmbeddingError
	if errors.As(embedErr, &embErr) {
		if embErr.Retryable {
			app.ScheduleRetry(embErr.Reason, time.Now().UTC(), h.cfg.RetryBackoff)
			return h.appRepo.Save(ctx, app)
		}
		// Non-retryable — mark permanently failed.
		if err := app.MarkEmbedFailed(embErr.Reason); err != nil {
			return fmt.Errorf("score_application: mark embed failed: %w", err)
		}
		return h.appRepo.Save(ctx, app)
	}
	// Unknown error type — treat as retryable.
	app.ScheduleRetry("embed_unknown: "+embedErr.Error(), time.Now().UTC(), h.cfg.RetryBackoff)
	return h.appRepo.Save(ctx, app)
}
