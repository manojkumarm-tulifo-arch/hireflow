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

// ScoreCandidateInput is the input for the ScoreCandidateHandler.
type ScoreCandidateInput struct {
	TenantID    shared.TenantID
	CandidateID uuid.UUID
}

// ScoreCandidateHandler is the fan-out command triggered by a CandidateParsed event.
// Its sole responsibility is to create Application rows (status=New) for every
// confirmed intent in the tenant — the actual embedding + rule + cosine work is
// done asynchronously by the match worker (ScoreApplicationHandler, T17).
//
// This separation keeps the event-consumer path fast and unbounded from the
// Voyage API latency.
type ScoreCandidateHandler struct {
	candidateRepo   repositories.CandidateRepository
	intentReader    services.IntentReader
	applicationRepo repositories.ApplicationRepository
}

// NewScoreCandidateHandler wires the handler.
func NewScoreCandidateHandler(
	cr repositories.CandidateRepository,
	ir services.IntentReader,
	ar repositories.ApplicationRepository,
) *ScoreCandidateHandler {
	return &ScoreCandidateHandler{
		candidateRepo:   cr,
		intentReader:    ir,
		applicationRepo: ar,
	}
}

// Handle fans out Application rows for all confirmed intents in the tenant.
// Step 1: confirm the candidate exists.
// Step 2: list all confirmed intents.
// Step 3: for each intent, find-or-create Application in New status and save.
func (h *ScoreCandidateHandler) Handle(ctx context.Context, in ScoreCandidateInput) error {
	candidate, err := h.candidateRepo.FindByID(ctx, in.TenantID, in.CandidateID)
	if err != nil {
		return fmt.Errorf("score_candidate: find candidate: %w", err)
	}

	intents, err := h.intentReader.ListConfirmedIntents(ctx, in.TenantID)
	if err != nil {
		return fmt.Errorf("score_candidate: list confirmed intents: %w", err)
	}

	for _, intent := range intents {
		app, err := h.applicationRepo.FindByCandidateAndIntent(ctx, in.TenantID, in.CandidateID, intent.ID)
		if err != nil && err != repositories.ErrApplicationNotFound {
			return fmt.Errorf("score_candidate: find application: %w", err)
		}
		if app != nil {
			// Already exists — no-op (idempotent fan-out).
			continue
		}

		// Create a new Application in New status.
		newApp, err := entities.NewApplication(entities.NewApplicationInput{
			TenantID:             in.TenantID,
			CandidateID:          in.CandidateID,
			IntentID:             intent.ID,
			IntentSpecVersion:    intent.SpecVersion,
			ProfileSchemaVersion: candidate.ProfileSchema(),
		})
		if err != nil {
			return fmt.Errorf("score_candidate: new application: %w", err)
		}

		if err := h.applicationRepo.Save(ctx, newApp); err != nil {
			return fmt.Errorf("score_candidate: save application: %w", err)
		}
	}

	return nil
}
