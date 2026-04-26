package queries

import (
	"context"
	"fmt"

	"github.com/hustle/hireflow/internal/hiringintent/application/dto"
	"github.com/hustle/hireflow/internal/hiringintent/domain/repositories"
	"github.com/hustle/hireflow/internal/hiringintent/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// IntentSummaryInput scopes the summary query to a single tenant.
type IntentSummaryInput struct {
	TenantID string
}

// IntentSummaryHandler returns per-status counts for a tenant. Used by the FE
// to render filter chips with badge counts on the intent list page.
type IntentSummaryHandler struct {
	repo repositories.IntentRepository
}

// NewIntentSummaryHandler wires the handler.
func NewIntentSummaryHandler(repo repositories.IntentRepository) *IntentSummaryHandler {
	return &IntentSummaryHandler{repo: repo}
}

// Handle executes the query.
func (h *IntentSummaryHandler) Handle(ctx context.Context, in IntentSummaryInput) (dto.IntentSummaryDTO, error) {
	tenantID, err := shared.ParseTenantID(in.TenantID)
	if err != nil {
		return dto.IntentSummaryDTO{}, fmt.Errorf("intent summary: %w", err)
	}
	counts, err := h.repo.Counts(ctx, tenantID)
	if err != nil {
		return dto.IntentSummaryDTO{}, fmt.Errorf("intent summary: %w", err)
	}
	out := dto.IntentSummaryDTO{Counts: dto.StatusCountsDTO{
		Drafted:   counts[valueobjects.StatusDrafted],
		Confirmed: counts[valueobjects.StatusConfirmed],
		Cancelled: counts[valueobjects.StatusCancelled],
		Closed:    counts[valueobjects.StatusClosed],
	}}
	out.Counts.Total = out.Counts.Drafted + out.Counts.Confirmed + out.Counts.Cancelled + out.Counts.Closed
	return out, nil
}
