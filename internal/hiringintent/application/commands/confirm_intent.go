package commands

import (
	"context"
	"fmt"

	"github.com/hustle/hireflow/internal/hiringintent/application/dto"
	"github.com/hustle/hireflow/internal/hiringintent/domain/repositories"
	"github.com/hustle/hireflow/internal/hiringintent/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ConfirmIntentInput is the input shape for confirming an intent.
type ConfirmIntentInput struct {
	TenantID string
	IntentID string
}

// ConfirmIntentHandler transitions a Drafted intent to Confirmed.
type ConfirmIntentHandler struct {
	repo repositories.IntentRepository
}

// NewConfirmIntentHandler wires the handler.
func NewConfirmIntentHandler(repo repositories.IntentRepository) *ConfirmIntentHandler {
	return &ConfirmIntentHandler{repo: repo}
}

// Handle executes the use case.
func (h *ConfirmIntentHandler) Handle(ctx context.Context, in ConfirmIntentInput) (dto.IntentDTO, error) {
	tenantID, err := shared.ParseTenantID(in.TenantID)
	if err != nil {
		return dto.IntentDTO{}, fmt.Errorf("confirm intent: %w", err)
	}
	intentID, err := valueobjects.ParseIntentID(in.IntentID)
	if err != nil {
		return dto.IntentDTO{}, fmt.Errorf("confirm intent: %w", err)
	}

	intent, err := h.repo.FindByID(ctx, tenantID, intentID)
	if err != nil {
		return dto.IntentDTO{}, fmt.Errorf("confirm intent: %w", err)
	}

	if err := intent.Confirm(); err != nil {
		return dto.IntentDTO{}, fmt.Errorf("confirm intent: %w", err)
	}

	if err := h.repo.Save(ctx, intent); err != nil {
		return dto.IntentDTO{}, fmt.Errorf("confirm intent: save: %w", err)
	}

	return dto.FromEntity(intent), nil
}
