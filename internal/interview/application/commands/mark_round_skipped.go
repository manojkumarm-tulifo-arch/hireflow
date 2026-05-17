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

type MarkRoundSkippedInput struct {
	TenantID    shared.TenantID
	ActorUserID uuid.UUID
	RoundID     uuid.UUID
}

type MarkRoundSkippedHandler struct {
	processes repositories.ProcessRepository
	audit     auditdomain.AuditWriter
}

func NewMarkRoundSkippedHandler(processes repositories.ProcessRepository, audit auditdomain.AuditWriter) *MarkRoundSkippedHandler {
	return &MarkRoundSkippedHandler{processes: processes, audit: audit}
}

func (h *MarkRoundSkippedHandler) Handle(ctx context.Context, in MarkRoundSkippedInput) error {
	process, err := h.processes.FindByRoundID(ctx, in.TenantID, in.RoundID)
	if err != nil {
		return err
	}
	if err := process.MarkRoundSkipped(in.RoundID); err != nil {
		if errors.Is(err, entities.ErrInvalidTransition) {
			return ErrRoundInvalidTransition
		}
		return fmt.Errorf("mark skipped: %w", err)
	}
	if err := h.processes.Save(ctx, process); err != nil {
		return fmt.Errorf("save process: %w", err)
	}
	return h.audit.Write(ctx, auditdomain.AuditEvent{
		ActorUserID:  in.ActorUserID,
		TenantID:     in.TenantID,
		Action:       "interview_round_skipped",
		ResourceKind: "interview_round",
		ResourceID:   in.RoundID,
		OccurredAt:   time.Now().UTC(),
	})
}
