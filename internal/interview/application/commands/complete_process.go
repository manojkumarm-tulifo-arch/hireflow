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

var ErrProcessInvalidTransition = errors.New("interview: invalid process state for transition")

type CompleteProcessInput struct {
	TenantID    shared.TenantID
	ActorUserID uuid.UUID
	ProcessID   uuid.UUID
}

type CompleteProcessHandler struct {
	processes repositories.ProcessRepository
	audit     auditdomain.AuditWriter
}

func NewCompleteProcessHandler(processes repositories.ProcessRepository, audit auditdomain.AuditWriter) *CompleteProcessHandler {
	return &CompleteProcessHandler{processes: processes, audit: audit}
}

func (h *CompleteProcessHandler) Handle(ctx context.Context, in CompleteProcessInput) error {
	process, err := h.processes.FindByID(ctx, in.TenantID, in.ProcessID)
	if err != nil {
		return err
	}
	if err := process.Complete(); err != nil {
		if errors.Is(err, entities.ErrInvalidTransition) {
			return ErrProcessInvalidTransition
		}
		// "cannot complete; round X is ..." errors fall through as plain errors
		return fmt.Errorf("complete: %w", err)
	}
	if err := h.processes.Save(ctx, process); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	return h.audit.Write(ctx, auditdomain.AuditEvent{
		ActorUserID:  in.ActorUserID,
		TenantID:     in.TenantID,
		Action:       "interview_process_completed",
		ResourceKind: "interview_process",
		ResourceID:   in.ProcessID,
		OccurredAt:   time.Now().UTC(),
	})
}
