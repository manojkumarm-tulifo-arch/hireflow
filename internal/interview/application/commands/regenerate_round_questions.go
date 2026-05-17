package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	auditdomain "github.com/hustle/hireflow/internal/shared/audit/domain"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ErrRoundNotRegenerable is returned when the round is in a terminal state
// (Completed or Skipped) — HTTP 409 territory.
var ErrRoundNotRegenerable = errors.New("interview: round not regenerable")

type RegenerateRoundQuestionsInput struct {
	TenantID    shared.TenantID
	ActorUserID uuid.UUID
	RoundID     uuid.UUID
	// Steering: slice-1 accepts but the worker doesn't persist between
	// regenerations. Field present for HTTP DTO compatibility.
	Steering string
}

// RegenerateRoundQuestionsHandler resets a round to Pending so the worker
// pool picks it up again. Only valid from QuestionsReady or GenerationFailed.
type RegenerateRoundQuestionsHandler struct {
	processes repositories.ProcessRepository
	audit     auditdomain.AuditWriter
}

func NewRegenerateRoundQuestionsHandler(processes repositories.ProcessRepository, audit auditdomain.AuditWriter) *RegenerateRoundQuestionsHandler {
	return &RegenerateRoundQuestionsHandler{processes: processes, audit: audit}
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
	return h.audit.Write(ctx, auditdomain.AuditEvent{
		ActorUserID:  in.ActorUserID,
		TenantID:     in.TenantID,
		Action:       "interview_round_regenerated",
		ResourceKind: "interview_round",
		ResourceID:   in.RoundID,
		OccurredAt:   time.Now().UTC(),
	})
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
