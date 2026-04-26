package commands

import (
	"context"
	"fmt"

	"github.com/hustle/hireflow/internal/jobposting/application/dto"
	"github.com/hustle/hireflow/internal/jobposting/domain/repositories"
	"github.com/hustle/hireflow/internal/jobposting/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ClosePostingInput identifies the posting and a free-text reason.
type ClosePostingInput struct {
	TenantID  string
	PostingID string
	Reason    string
}

// ClosePostingHandler closes a job posting (filled or cancelled).
type ClosePostingHandler struct {
	repo repositories.PostingRepository
}

// NewClosePostingHandler wires the handler.
func NewClosePostingHandler(repo repositories.PostingRepository) *ClosePostingHandler {
	return &ClosePostingHandler{repo: repo}
}

// Handle executes the use case.
func (h *ClosePostingHandler) Handle(ctx context.Context, in ClosePostingInput) (dto.PostingDTO, error) {
	tenantID, err := shared.ParseTenantID(in.TenantID)
	if err != nil {
		return dto.PostingDTO{}, fmt.Errorf("close posting: %w", err)
	}
	postingID, err := valueobjects.ParsePostingID(in.PostingID)
	if err != nil {
		return dto.PostingDTO{}, fmt.Errorf("close posting: %w", err)
	}

	posting, err := h.repo.FindByID(ctx, tenantID, postingID)
	if err != nil {
		return dto.PostingDTO{}, fmt.Errorf("close posting: %w", err)
	}
	if err := posting.Close(in.Reason); err != nil {
		return dto.PostingDTO{}, fmt.Errorf("close posting: %w", err)
	}
	if err := h.repo.Save(ctx, posting); err != nil {
		return dto.PostingDTO{}, fmt.Errorf("close posting: save: %w", err)
	}
	return dto.FromEntity(posting), nil
}
