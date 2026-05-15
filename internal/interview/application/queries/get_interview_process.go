// Package queries holds the interview context's read-side handlers.
package queries

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	auditdomain "github.com/hustle/hireflow/internal/shared/audit/domain"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/application/dto"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
)

// GetInterviewProcessHandler returns the read-model for one process,
// including per-round feedback summaries. Writes an audit row because the
// response includes candidate-derived data.
type GetInterviewProcessHandler struct {
	processes repositories.ProcessRepository
	feedback  repositories.FeedbackRepository
	audit     auditdomain.AuditWriter
}

func NewGetInterviewProcessHandler(processes repositories.ProcessRepository, feedback repositories.FeedbackRepository, audit auditdomain.AuditWriter) *GetInterviewProcessHandler {
	return &GetInterviewProcessHandler{processes: processes, feedback: feedback, audit: audit}
}

func (h *GetInterviewProcessHandler) Handle(ctx context.Context, tenant shared.TenantID, actorUserID, processID uuid.UUID) (dto.InterviewProcessDTO, error) {
	p, err := h.processes.FindByID(ctx, tenant, processID)
	if err != nil {
		return dto.InterviewProcessDTO{}, err
	}

	out := dto.InterviewProcessDTO{
		ID:            p.ID(),
		TenantID:      p.TenantID().String(),
		ApplicationID: p.ApplicationID(),
		CandidateID:   p.CandidateID(),
		IntentID:      p.IntentID(),
		Status:        string(p.Status()),
		CreatedAt:     p.CreatedAt(),
		UpdatedAt:     p.UpdatedAt(),
	}

	for _, r := range p.Rounds() {
		fs, err := h.summarizeFeedback(ctx, tenant, r.ID())
		if err != nil {
			return dto.InterviewProcessDTO{}, fmt.Errorf("summarize feedback: %w", err)
		}
		out.Rounds = append(out.Rounds, dto.InterviewRoundDTO{
			ID:              r.ID(),
			Kind:            string(r.Kind()),
			Sequence:        r.Sequence(),
			Status:          string(r.Status()),
			Questions:       r.Questions(),
			AttemptCount:    r.AttemptCount(),
			LastError:       r.LastError(),
			FeedbackSummary: fs,
			CreatedAt:       r.CreatedAt(),
			UpdatedAt:       r.UpdatedAt(),
		})
	}

	// Audit AFTER the read succeeds. Load-bearing per slice 4.
	if err := h.audit.Write(ctx, auditdomain.AuditEvent{
		ActorUserID:  actorUserID,
		TenantID:     tenant,
		Action:       "interview_process_read",
		ResourceKind: "interview_process",
		ResourceID:   processID,
		OccurredAt:   time.Now().UTC(),
	}); err != nil {
		return dto.InterviewProcessDTO{}, fmt.Errorf("audit read: %w", err)
	}
	return out, nil
}

func (h *GetInterviewProcessHandler) summarizeFeedback(ctx context.Context, tenant shared.TenantID, roundID uuid.UUID) (dto.FeedbackSummaryDTO, error) {
	rows, err := h.feedback.ListByRound(ctx, tenant, roundID)
	if err != nil {
		return dto.FeedbackSummaryDTO{}, err
	}
	out := dto.FeedbackSummaryDTO{}
	for i, r := range rows {
		switch string(r.Decision) {
		case "strong_yes":
			out.StrongYes++
		case "yes":
			out.Yes++
		case "mixed":
			out.Mixed++
		case "no":
			out.No++
		case "strong_no":
			out.StrongNo++
		}
		out.Total++
		if i == 0 {
			// ListByRound returns newest-first; first row is latest.
			out.LatestDecision = string(r.Decision)
		}
	}
	return out, nil
}
