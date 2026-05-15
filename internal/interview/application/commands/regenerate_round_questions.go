package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ErrRoundNotRegenerable is returned when the round is in a terminal state
// (Completed or Skipped) — HTTP 409 territory.
var ErrRoundNotRegenerable = errors.New("interview: round not regenerable")

type RegenerateRoundQuestionsInput struct {
	TenantID shared.TenantID
	RoundID  uuid.UUID
	// Steering: slice-1 accepts but the worker doesn't persist between
	// regenerations. Field present for HTTP DTO compatibility.
	Steering string
}

// RegenerateRoundQuestionsHandler resets a round to Pending so the worker
// pool picks it up again. Only valid from QuestionsReady or GenerationFailed.
type RegenerateRoundQuestionsHandler struct {
	processes repositories.ProcessRepository
}

func NewRegenerateRoundQuestionsHandler(processes repositories.ProcessRepository) *RegenerateRoundQuestionsHandler {
	return &RegenerateRoundQuestionsHandler{processes: processes}
}

func (h *RegenerateRoundQuestionsHandler) Handle(ctx context.Context, in RegenerateRoundQuestionsInput) error {
	process, err := h.findProcessByRoundID(ctx, in.TenantID, in.RoundID)
	if err != nil {
		return err
	}
	if err := process.ResetRoundForRegeneration(in.RoundID); err != nil {
		if errors.Is(err, entities.ErrInvalidTransition) {
			return ErrRoundNotRegenerable
		}
		return fmt.Errorf("reset round: %w", err)
	}
	if err := h.processes.Save(ctx, process); err != nil {
		return fmt.Errorf("save process: %w", err)
	}
	return nil
}

// findProcessByRoundID uses the repository's FindByRoundID method (added in T6).
func (h *RegenerateRoundQuestionsHandler) findProcessByRoundID(ctx context.Context, tenant shared.TenantID, roundID uuid.UUID) (*entities.InterviewProcess, error) {
	p, err := h.processes.FindByRoundID(ctx, tenant, roundID)
	if err != nil {
		if errors.Is(err, repositories.ErrProcessNotFound) {
			return nil, entities.ErrRoundNotFound
		}
		return nil, err
	}
	return p, nil
}
