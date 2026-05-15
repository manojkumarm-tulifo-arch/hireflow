package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	"github.com/hustle/hireflow/internal/interview/domain/services"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	"github.com/hustle/hireflow/internal/interview/infrastructure/generation"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

type GenerateRoundQuestionsInput struct {
	TenantID  shared.TenantID
	ProcessID uuid.UUID
	RoundID   uuid.UUID
}

// GenerateRoundQuestionsHandler runs one generation attempt for one round.
// Fired by QuestionGenerationPool. Returns nil on success (questions saved,
// round → QuestionsReady) AND on retry-scheduled (round stays Pending with
// updated next_attempt_at) AND on abort (round → GenerationFailed). A non-nil
// return indicates an unexpected infrastructure error (DB write failed, etc.).
type GenerateRoundQuestionsHandler struct {
	processes  repositories.ProcessRepository
	intents    services.IntentReader
	candidates services.CandidateReader
	generator  services.QuestionGenerator
}

func NewGenerateRoundQuestionsHandler(
	processes repositories.ProcessRepository,
	intents services.IntentReader,
	candidates services.CandidateReader,
	generator services.QuestionGenerator,
) *GenerateRoundQuestionsHandler {
	return &GenerateRoundQuestionsHandler{
		processes:  processes,
		intents:    intents,
		candidates: candidates,
		generator:  generator,
	}
}

func (h *GenerateRoundQuestionsHandler) Handle(ctx context.Context, in GenerateRoundQuestionsInput) error {
	process, err := h.processes.FindByID(ctx, in.TenantID, in.ProcessID)
	if err != nil {
		return fmt.Errorf("load process: %w", err)
	}

	var round *entities.InterviewRound
	for _, r := range process.Rounds() {
		if r.ID() == in.RoundID {
			round = r
			break
		}
	}
	if round == nil {
		return entities.ErrRoundNotFound
	}
	if round.Status() != vo.RoundStatusPending {
		// Another worker already advanced it; idempotent no-op.
		return nil
	}

	roleSpec, err := h.intents.GetRoleSpec(ctx, in.TenantID, process.IntentID())
	if err != nil {
		return h.handleFailure(ctx, process, in.RoundID, classifyError(err), err.Error())
	}
	candidateProfile, err := h.candidates.GetProfileForQuestions(ctx, in.TenantID, process.CandidateID())
	if err != nil {
		return h.handleFailure(ctx, process, in.RoundID, classifyError(err), err.Error())
	}

	questions, err := h.generator.Generate(ctx, services.GenerationInput{
		RoundKind:        round.Kind(),
		RoleSpec:         roleSpec,
		CandidateProfile: candidateProfile,
	})
	if err != nil {
		return h.handleFailure(ctx, process, in.RoundID, classifyError(err), err.Error())
	}

	if err := process.MarkRoundQuestionsReady(in.RoundID, questions); err != nil {
		return fmt.Errorf("mark ready: %w", err)
	}
	if err := h.processes.Save(ctx, process); err != nil {
		return fmt.Errorf("save process: %w", err)
	}
	return nil
}

// classifyError maps a concrete error to a FailureKind for retry decisions.
func classifyError(err error) vo.FailureKind {
	if errors.Is(err, generation.ErrLLMAuthFailed) {
		return vo.FailureKindLLMAuth
	}
	if errors.Is(err, generation.ErrInvalidLLMOutput) {
		return vo.FailureKindInvalidJSON
	}
	if errors.Is(err, services.ErrIntentNotFound) || errors.Is(err, services.ErrCandidateNotFound) {
		// Cross-context dependency permanently missing — treat as auth-class.
		return vo.FailureKindLLMAuth
	}
	return vo.FailureKindTransient
}

// handleFailure applies the retry decision: either re-save the process with
// an incremented attempt count + scheduled next_attempt_at, or mark the
// round GenerationFailed.
func (h *GenerateRoundQuestionsHandler) handleFailure(
	ctx context.Context,
	process *entities.InterviewProcess,
	roundID uuid.UUID,
	kind vo.FailureKind,
	detail string,
) error {
	var attempt int
	for _, r := range process.Rounds() {
		if r.ID() == roundID {
			attempt = r.AttemptCount() + 1
			break
		}
	}
	decision := vo.DecideRetry(kind, attempt, detail)
	switch decision.Action {
	case vo.RetryActionRetry:
		nextAt := time.Now().UTC().Add(decision.Backoff)
		if err := process.RecordGenerationAttempt(roundID, decision.Detail, nextAt); err != nil {
			return fmt.Errorf("record attempt: %w", err)
		}
	case vo.RetryActionAbort:
		if err := process.MarkRoundGenerationFailed(roundID, decision.Detail); err != nil {
			return fmt.Errorf("mark failed: %w", err)
		}
	}
	if err := h.processes.Save(ctx, process); err != nil {
		return fmt.Errorf("save process: %w", err)
	}
	return nil
}
