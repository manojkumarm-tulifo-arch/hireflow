package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	auditdomain "github.com/hustle/hireflow/internal/shared/audit/domain"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
)

type CancelProcessInput struct {
	TenantID    shared.TenantID
	ActorUserID uuid.UUID
	ProcessID   uuid.UUID
}

type CancelProcessHandler struct {
	processes repositories.ProcessRepository
	audit     auditdomain.AuditWriter
}

func NewCancelProcessHandler(processes repositories.ProcessRepository, audit auditdomain.AuditWriter) *CancelProcessHandler {
	return &CancelProcessHandler{processes: processes, audit: audit}
}

func (h *CancelProcessHandler) Handle(ctx context.Context, in CancelProcessInput) error {
	process, err := h.processes.FindByID(ctx, in.TenantID, in.ProcessID)
	if err != nil {
		return err
	}
	if err := process.Cancel(); err != nil {
		if errors.Is(err, entities.ErrInvalidTransition) {
			return ErrProcessInvalidTransition
		}
		return fmt.Errorf("cancel: %w", err)
	}
	if err := h.processes.Save(ctx, process); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	return h.audit.Write(ctx, auditdomain.AuditEvent{
		ActorUserID:  in.ActorUserID,
		TenantID:     in.TenantID,
		Action:       "interview_process_cancelled",
		ResourceKind: "interview_process",
		ResourceID:   in.ProcessID,
		OccurredAt:   time.Now().UTC(),
	})
}
