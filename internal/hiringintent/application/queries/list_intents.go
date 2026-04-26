package queries

import (
	"context"
	"fmt"

	"github.com/hustle/hireflow/internal/hiringintent/application/dto"
	"github.com/hustle/hireflow/internal/hiringintent/domain/repositories"
	"github.com/hustle/hireflow/internal/hiringintent/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ListIntentsInput narrows a list query within a tenant. Status and
// RecruiterID are optional. Search does an ILIKE match against the role
// title. SortBy accepts "NEWEST" (default) or "URGENT"; anything else falls
// back to NEWEST.
type ListIntentsInput struct {
	TenantID    string
	Status      string
	RecruiterID string
	Search      string
	SortBy      string
	Limit       int
	Offset      int
}

// ListIntentsHandler returns hiring intents for a tenant with optional filters.
type ListIntentsHandler struct {
	repo repositories.IntentRepository
}

// NewListIntentsHandler wires the handler.
func NewListIntentsHandler(repo repositories.IntentRepository) *ListIntentsHandler {
	return &ListIntentsHandler{repo: repo}
}

// Handle executes the query.
func (h *ListIntentsHandler) Handle(ctx context.Context, in ListIntentsInput) ([]dto.IntentDTO, error) {
	tenantID, err := shared.ParseTenantID(in.TenantID)
	if err != nil {
		return nil, fmt.Errorf("list intents: %w", err)
	}

	filter := repositories.IntentFilter{
		Search: in.Search,
		SortBy: parseSortOrder(in.SortBy),
		Limit:  in.Limit,
		Offset: in.Offset,
	}
	if in.Status != "" {
		s, err := valueobjects.ParseIntentStatus(in.Status)
		if err != nil {
			return nil, fmt.Errorf("list intents: %w", err)
		}
		filter.Status = &s
	}
	if in.RecruiterID != "" {
		r, err := shared.ParseRecruiterID(in.RecruiterID)
		if err != nil {
			return nil, fmt.Errorf("list intents: %w", err)
		}
		filter.RecruiterID = &r
	}

	intents, err := h.repo.List(ctx, tenantID, filter)
	if err != nil {
		return nil, fmt.Errorf("list intents: %w", err)
	}

	out := make([]dto.IntentDTO, len(intents))
	for i, it := range intents {
		out[i] = dto.FromEntity(it)
	}
	return out, nil
}

func parseSortOrder(s string) repositories.ListSortOrder {
	switch repositories.ListSortOrder(s) {
	case repositories.SortUrgentFirst:
		return repositories.SortUrgentFirst
	default:
		return repositories.SortNewestFirst
	}
}
