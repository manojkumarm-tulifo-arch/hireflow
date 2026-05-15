package queries

import (
	"context"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/application/dto"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ListInterviewProcessesHandler returns processes for an intent, optionally
// filtered by status. No audit (high volume; no PII).
type ListInterviewProcessesHandler struct {
	processes repositories.ProcessRepository
}

func NewListInterviewProcessesHandler(processes repositories.ProcessRepository) *ListInterviewProcessesHandler {
	return &ListInterviewProcessesHandler{processes: processes}
}

type ListInput struct {
	TenantID shared.TenantID
	IntentID uuid.UUID
	Status   string
	Limit    int
	Offset   int
}

func (h *ListInterviewProcessesHandler) Handle(ctx context.Context, in ListInput) ([]dto.InterviewProcessDTO, error) {
	processes, err := h.processes.ListByTenant(ctx, in.TenantID, repositories.ProcessListFilter{
		IntentID: in.IntentID,
		Status:   in.Status,
		Limit:    in.Limit,
		Offset:   in.Offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]dto.InterviewProcessDTO, 0, len(processes))
	for _, p := range processes {
		out = append(out, dto.InterviewProcessDTO{
			ID:            p.ID(),
			TenantID:      p.TenantID().String(),
			ApplicationID: p.ApplicationID(),
			CandidateID:   p.CandidateID(),
			IntentID:      p.IntentID(),
			Status:        string(p.Status()),
			CreatedAt:     p.CreatedAt(),
			UpdatedAt:     p.UpdatedAt(),
		})
	}
	return out, nil
}
