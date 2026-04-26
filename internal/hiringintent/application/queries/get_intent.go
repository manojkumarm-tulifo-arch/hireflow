// Package queries holds query handlers for the hiringintent context.
// Queries are read-only and do not raise events.
package queries

import (
	"context"
	"fmt"

	"github.com/hustle/hireflow/internal/hiringintent/application/dto"
	"github.com/hustle/hireflow/internal/hiringintent/domain/repositories"
	"github.com/hustle/hireflow/internal/hiringintent/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// GetIntentInput identifies an intent within a tenant.
type GetIntentInput struct {
	TenantID string
	IntentID string
}

// GetIntentHandler returns a single intent by id, scoped to the caller's tenant.
type GetIntentHandler struct {
	repo repositories.IntentRepository
}

// NewGetIntentHandler wires the handler.
func NewGetIntentHandler(repo repositories.IntentRepository) *GetIntentHandler {
	return &GetIntentHandler{repo: repo}
}

// Handle executes the query.
func (h *GetIntentHandler) Handle(ctx context.Context, in GetIntentInput) (dto.IntentDTO, error) {
	tenantID, err := shared.ParseTenantID(in.TenantID)
	if err != nil {
		return dto.IntentDTO{}, fmt.Errorf("get intent: %w", err)
	}
	intentID, err := valueobjects.ParseIntentID(in.IntentID)
	if err != nil {
		return dto.IntentDTO{}, fmt.Errorf("get intent: %w", err)
	}

	intent, err := h.repo.FindByID(ctx, tenantID, intentID)
	if err != nil {
		return dto.IntentDTO{}, fmt.Errorf("get intent: %w", err)
	}
	return dto.FromEntity(intent), nil
}
