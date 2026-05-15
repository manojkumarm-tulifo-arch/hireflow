package queries

import (
	"context"
	"errors"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/application/commands"
	"github.com/hustle/hireflow/internal/interview/application/dto"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
)

type GetLoopTemplateHandler struct {
	templates repositories.LoopTemplateRepository
}

func NewGetLoopTemplateHandler(templates repositories.LoopTemplateRepository) *GetLoopTemplateHandler {
	return &GetLoopTemplateHandler{templates: templates}
}

func (h *GetLoopTemplateHandler) Handle(ctx context.Context, tenant shared.TenantID, intentID uuid.UUID) (dto.LoopTemplateDTO, error) {
	tmpl, err := h.templates.FindByIntent(ctx, tenant, intentID)
	if err != nil {
		if errors.Is(err, repositories.ErrLoopTemplateNotFound) {
			rounds := commands.DefaultLoop
			out := dto.LoopTemplateDTO{IntentID: intentID, IsDefault: true}
			for _, r := range rounds {
				out.Rounds = append(out.Rounds, dto.LoopTemplateRoundDTO{
					Kind: string(r.Kind), Sequence: r.Sequence,
				})
			}
			return out, nil
		}
		return dto.LoopTemplateDTO{}, err
	}
	out := dto.LoopTemplateDTO{IntentID: intentID, IsDefault: false}
	for _, r := range tmpl.Rounds() {
		out.Rounds = append(out.Rounds, dto.LoopTemplateRoundDTO{
			Kind: string(r.Kind), Sequence: r.Sequence,
		})
	}
	return out, nil
}
