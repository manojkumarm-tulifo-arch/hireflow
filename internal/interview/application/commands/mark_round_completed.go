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

// ErrRoundInvalidTransition is returned when the round can't be marked done
// from its current state. HTTP 409.
var ErrRoundInvalidTransition = errors.New("interview: invalid round state for transition")

type MarkRoundCompletedInput struct {
	TenantID    shared.TenantID
	ActorUserID uuid.UUID
	RoundID     uuid.UUID
}

type MarkRoundCompletedHandler struct {
	processes repositories.ProcessRepository
	audit     auditdomain.AuditWriter
}

func NewMarkRoundCompletedHandler(processes repositories.ProcessRepository, audit auditdomain.AuditWriter) *MarkRoundCompletedHandler {
	return &MarkRoundCompletedHandler{processes: processes, audit: audit}
}

func (h *MarkRoundCompletedHandler) Handle(ctx context.Context, in MarkRoundCompletedInput) error {
	process, err := h.processes.FindByRoundID(ctx, in.TenantID, in.RoundID)
	if err != nil {
		return err
	}
	if err := process.MarkRoundCompleted(in.RoundID); err != nil {
		if errors.Is(err, entities.ErrInvalidTransition) {
			return ErrRoundInvalidTransition
		}
		return fmt.Errorf("mark completed: %w", err)
	}
	if err := h.processes.Save(ctx, process); err != nil {
		return fmt.Errorf("save process: %w", err)
	}
	return h.audit.Write(ctx, auditdomain.AuditEvent{
		ActorUserID:  in.ActorUserID,
		TenantID:     in.TenantID,
		Action:       "interview_round_completed",
		ResourceKind: "interview_round",
		ResourceID:   in.RoundID,
		OccurredAt:   time.Now().UTC(),
	})
}
