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

// UpsertLoopTemplateInput carries all inputs for the command.
type UpsertLoopTemplateInput struct {
	TenantID    shared.TenantID
	ActorUserID uuid.UUID
	IntentID    uuid.UUID
	Rounds      []entities.TemplateRound
}

// UpsertLoopTemplateHandler creates or replaces a per-intent loop template.
// Existing processes are NOT retroactively mutated (spec decision I1-D7).
type UpsertLoopTemplateHandler struct {
	templates repositories.LoopTemplateRepository
	audit     auditdomain.AuditWriter
}

// NewUpsertLoopTemplateHandler constructs the handler.
func NewUpsertLoopTemplateHandler(templates repositories.LoopTemplateRepository, audit auditdomain.AuditWriter) *UpsertLoopTemplateHandler {
	return &UpsertLoopTemplateHandler{templates: templates, audit: audit}
}

// Handle executes the command.
func (h *UpsertLoopTemplateHandler) Handle(ctx context.Context, in UpsertLoopTemplateInput) error {
	now := func() time.Time { return time.Now().UTC() }

	var tmpl *entities.LoopTemplate
	existing, err := h.templates.FindByIntent(ctx, in.TenantID, in.IntentID)
	switch {
	case err == nil:
		if err := existing.Replace(in.Rounds, now); err != nil {
			return fmt.Errorf("replace rounds: %w", err)
		}
		tmpl = existing
	case errors.Is(err, repositories.ErrLoopTemplateNotFound):
		tmpl, err = entities.NewLoopTemplate(entities.NewLoopTemplateInput{
			TenantID: in.TenantID,
			IntentID: in.IntentID,
			Rounds:   in.Rounds,
			Now:      now,
		})
		if err != nil {
			return fmt.Errorf("construct template: %w", err)
		}
	default:
		return fmt.Errorf("load template: %w", err)
	}

	if err := h.templates.Save(ctx, tmpl); err != nil {
		return fmt.Errorf("save template: %w", err)
	}

	// Audit. Load-bearing per slice 4 conventions.
	if err := h.audit.Write(ctx, auditdomain.AuditEvent{
		ActorUserID:  in.ActorUserID,
		TenantID:     in.TenantID,
		Action:       "interview_loop_template_upserted",
		ResourceKind: "intent",
		ResourceID:   in.IntentID,
		Payload:      map[string]any{"round_count": len(in.Rounds)},
		OccurredAt:   now(),
	}); err != nil {
		return err
	}
	return nil
}
