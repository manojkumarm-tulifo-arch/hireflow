package commands

import (
	"context"
	"time"

	"github.com/google/uuid"

	auditdomain "github.com/hustle/hireflow/internal/shared/audit/domain"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
)

// scoreIntentDispatcher is a narrow interface satisfied by *ScoreIntentHandler.
// Defined here so that RescoreIntentHandler tests can pass a mock without
// having to construct a full ScoreIntentHandler (which has 4+ dependencies).
type scoreIntentDispatcher interface {
	Handle(ctx context.Context, in ScoreIntentInput) error
}

// RescoreIntentInput carries the input for a rescore operation.
type RescoreIntentInput struct {
	TenantID    shared.TenantID
	ActorUserID uuid.UUID
	IntentID    uuid.UUID
}

// RescoreIntentHandler invalidates cached LLM judgments for an intent and
// re-dispatches ScoreIntent so the judge worker re-scores the top-K applications.
//
// Rescore is idempotent: the invalidation UPDATE is a no-op when fields are
// already NULL, and ScoreIntent's fan-out skips existing Application rows.
type RescoreIntentHandler struct {
	appRepo     repositories.ApplicationRepository
	scoreIntent scoreIntentDispatcher
	audit       auditdomain.AuditWriter
}

// NewRescoreIntentHandler wires the handler.
//
// scoreIntent should be the application-level *ScoreIntentHandler instance;
// it is accepted as the narrow scoreIntentDispatcher interface so tests can
// substitute a lightweight stub without constructing all of ScoreIntent's deps.
func NewRescoreIntentHandler(
	appRepo repositories.ApplicationRepository,
	scoreIntent scoreIntentDispatcher,
	audit auditdomain.AuditWriter,
) *RescoreIntentHandler {
	return &RescoreIntentHandler{
		appRepo:     appRepo,
		scoreIntent: scoreIntent,
		audit:       audit,
	}
}

// Handle performs the three-step rescore:
//  1. Invalidate cached LLM judgments (llm_judgment / overall_score / score_band)
//     for all applications belonging to the intent.
//  2. Dispatch ScoreIntent, which fans-out Application rows and enqueues JudgeJobs
//     for the top-K apps by coarse score. The judge worker picks these up
//     asynchronously and re-populates the three nulled fields.
//  3. Write an audit event. Audit failure is load-bearing and propagates to the
//     caller (HTTP layer maps it to 500).
func (h *RescoreIntentHandler) Handle(ctx context.Context, in RescoreIntentInput) error {
	// Step 1: Invalidate cached LLM judgments.
	if err := h.appRepo.InvalidateJudgmentsForIntent(ctx, in.TenantID, in.IntentID); err != nil {
		return err
	}

	// Step 2: Fan-out Application rows + enqueue JudgeJobs for top-K.
	if err := h.scoreIntent.Handle(ctx, ScoreIntentInput{
		TenantID: in.TenantID,
		IntentID: in.IntentID,
	}); err != nil {
		return err
	}

	// Step 3: Audit.
	return h.audit.Write(ctx, auditdomain.AuditEvent{
		ActorUserID:  in.ActorUserID,
		TenantID:     in.TenantID,
		Action:       "intent_rescored",
		ResourceKind: "intent",
		ResourceID:   in.IntentID,
		OccurredAt:   time.Now().UTC(),
	})
}
