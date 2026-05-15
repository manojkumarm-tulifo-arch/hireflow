// Package commands holds the interview context's write-side handlers.
package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// DefaultLoop is the hardcoded fallback loop when no per-intent template
// exists at process-creation time. Spec decision I1-D7.
var DefaultLoop = []entities.TemplateRound{
	{Kind: vo.RoundKindScreen, Sequence: 1},
	{Kind: vo.RoundKindTechnical, Sequence: 2},
	{Kind: vo.RoundKindBarRaiser, Sequence: 3},
}

// StartInterviewProcessInput carries all inputs for the command.
type StartInterviewProcessInput struct {
	TenantID      shared.TenantID
	ApplicationID uuid.UUID
	CandidateID   uuid.UUID
	IntentID      uuid.UUID
}

// StartInterviewProcessHandler creates an InterviewProcess for a newly-
// shortlisted application. Fired by ApplicationShortlistedConsumer.
//
// Idempotent: if a process already exists for (tenant, application_id), the
// pre-check returns nil. The DB-level UNIQUE constraint is a backstop for
// the race condition.
type StartInterviewProcessHandler struct {
	processes repositories.ProcessRepository
	templates repositories.LoopTemplateRepository
}

// NewStartInterviewProcessHandler constructs the handler.
func NewStartInterviewProcessHandler(
	processes repositories.ProcessRepository,
	templates repositories.LoopTemplateRepository,
) *StartInterviewProcessHandler {
	return &StartInterviewProcessHandler{processes: processes, templates: templates}
}

// Handle executes the command.
func (h *StartInterviewProcessHandler) Handle(ctx context.Context, in StartInterviewProcessInput) error {
	// Idempotency pre-check.
	if _, err := h.processes.FindByApplicationID(ctx, in.TenantID, in.ApplicationID); err == nil {
		return nil
	} else if !errors.Is(err, repositories.ErrProcessNotFound) {
		return fmt.Errorf("idempotency check: %w", err)
	}

	rounds := DefaultLoop
	tmpl, err := h.templates.FindByIntent(ctx, in.TenantID, in.IntentID)
	if err == nil {
		rounds = tmpl.Rounds()
	} else if !errors.Is(err, repositories.ErrLoopTemplateNotFound) {
		return fmt.Errorf("load template: %w", err)
	}

	process, err := entities.NewInterviewProcess(entities.NewInterviewProcessInput{
		TenantID:      in.TenantID,
		ApplicationID: in.ApplicationID,
		CandidateID:   in.CandidateID,
		IntentID:      in.IntentID,
		Rounds:        rounds,
		Now:           func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return fmt.Errorf("construct process: %w", err)
	}

	if err := h.processes.Save(ctx, process); err != nil {
		if errors.Is(err, repositories.ErrProcessDuplicate) {
			return nil
		}
		return fmt.Errorf("save process: %w", err)
	}
	return nil
}
