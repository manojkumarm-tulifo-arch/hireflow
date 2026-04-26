// Package queries holds query handlers for the jobposting context.
package queries

import (
	"context"
	"fmt"

	"github.com/hustle/hireflow/internal/jobposting/application/dto"
	"github.com/hustle/hireflow/internal/jobposting/domain/repositories"
	"github.com/hustle/hireflow/internal/jobposting/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// GetPostingInput identifies a posting within a tenant.
type GetPostingInput struct {
	TenantID  string
	PostingID string
}

// GetPostingHandler returns a single posting by id.
type GetPostingHandler struct {
	repo repositories.PostingRepository
}

// NewGetPostingHandler wires the handler.
func NewGetPostingHandler(repo repositories.PostingRepository) *GetPostingHandler {
	return &GetPostingHandler{repo: repo}
}

// Handle executes the query.
func (h *GetPostingHandler) Handle(ctx context.Context, in GetPostingInput) (dto.PostingDTO, error) {
	tenantID, err := shared.ParseTenantID(in.TenantID)
	if err != nil {
		return dto.PostingDTO{}, fmt.Errorf("get posting: %w", err)
	}
	postingID, err := valueobjects.ParsePostingID(in.PostingID)
	if err != nil {
		return dto.PostingDTO{}, fmt.Errorf("get posting: %w", err)
	}
	posting, err := h.repo.FindByID(ctx, tenantID, postingID)
	if err != nil {
		return dto.PostingDTO{}, fmt.Errorf("get posting: %w", err)
	}
	return dto.FromEntity(posting), nil
}
