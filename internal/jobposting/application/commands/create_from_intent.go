// Package commands holds command handlers for the jobposting context.
package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/hustle/hireflow/internal/jobposting/application/dto"
	"github.com/hustle/hireflow/internal/jobposting/domain/entities"
	"github.com/hustle/hireflow/internal/jobposting/domain/repositories"
	"github.com/hustle/hireflow/internal/jobposting/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// CreateFromIntentHandler creates a draft JobPosting from a confirmed
// hiringintent. Idempotent on (tenant, intentID): if a posting already
// exists for the intent, the existing one is returned without re-creating.
// This makes the cross-context event consumer safely retry-able.
type CreateFromIntentHandler struct {
	repo repositories.PostingRepository
}

// NewCreateFromIntentHandler wires the handler.
func NewCreateFromIntentHandler(repo repositories.PostingRepository) *CreateFromIntentHandler {
	return &CreateFromIntentHandler{repo: repo}
}

// Handle executes the use case.
func (h *CreateFromIntentHandler) Handle(ctx context.Context, in dto.CreateFromIntentInput) (dto.PostingDTO, error) {
	tenantID, err := shared.ParseTenantID(in.TenantID)
	if err != nil {
		return dto.PostingDTO{}, fmt.Errorf("create from intent: %w", err)
	}
	if in.IntentID == "" {
		return dto.PostingDTO{}, errors.New("create from intent: intentID is required")
	}

	existing, err := h.repo.FindByIntentID(ctx, tenantID, in.IntentID)
	if err == nil && existing != nil {
		return dto.FromEntity(existing), nil
	}
	if err != nil && !errors.Is(err, repositories.ErrPostingNotFound) {
		return dto.PostingDTO{}, fmt.Errorf("create from intent: lookup: %w", err)
	}

	jd, err := valueobjects.NewJDContent(in.Title, in.Summary, in.Responsibilities, in.Requirements, 1)
	if err != nil {
		return dto.PostingDTO{}, fmt.Errorf("create from intent: %w", err)
	}
	posting, err := entities.NewJobPosting(tenantID, in.IntentID, jd)
	if err != nil {
		return dto.PostingDTO{}, fmt.Errorf("create from intent: %w", err)
	}
	if err := h.repo.Save(ctx, posting); err != nil {
		return dto.PostingDTO{}, fmt.Errorf("create from intent: save: %w", err)
	}
	return dto.FromEntity(posting), nil
}
