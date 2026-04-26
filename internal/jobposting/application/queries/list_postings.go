package queries

import (
	"context"
	"fmt"

	"github.com/hustle/hireflow/internal/jobposting/application/dto"
	"github.com/hustle/hireflow/internal/jobposting/domain/repositories"
	"github.com/hustle/hireflow/internal/jobposting/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ListPostingsInput narrows a list query within a tenant.
type ListPostingsInput struct {
	TenantID string
	Status   string
	IntentID string
	Limit    int
	Offset   int
}

// ListPostingsHandler returns postings for a tenant with optional filters.
type ListPostingsHandler struct {
	repo repositories.PostingRepository
}

// NewListPostingsHandler wires the handler.
func NewListPostingsHandler(repo repositories.PostingRepository) *ListPostingsHandler {
	return &ListPostingsHandler{repo: repo}
}

// Handle executes the query.
func (h *ListPostingsHandler) Handle(ctx context.Context, in ListPostingsInput) ([]dto.PostingDTO, error) {
	tenantID, err := shared.ParseTenantID(in.TenantID)
	if err != nil {
		return nil, fmt.Errorf("list postings: %w", err)
	}
	filter := repositories.PostingFilter{IntentID: in.IntentID, Limit: in.Limit, Offset: in.Offset}
	if in.Status != "" {
		s, err := valueobjects.ParsePostingStatus(in.Status)
		if err != nil {
			return nil, fmt.Errorf("list postings: %w", err)
		}
		filter.Status = &s
	}
	postings, err := h.repo.List(ctx, tenantID, filter)
	if err != nil {
		return nil, fmt.Errorf("list postings: %w", err)
	}
	out := make([]dto.PostingDTO, len(postings))
	for i, p := range postings {
		out[i] = dto.FromEntity(p)
	}
	return out, nil
}
