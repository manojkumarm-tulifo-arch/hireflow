package commands

import (
	"context"
	"fmt"

	"github.com/hustle/hireflow/internal/jobposting/application/dto"
	"github.com/hustle/hireflow/internal/jobposting/domain/repositories"
	"github.com/hustle/hireflow/internal/jobposting/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// PublishPostingInput identifies the posting and its target channels.
type PublishPostingInput struct {
	TenantID  string
	PostingID string
	Channels  []string
}

// PublishPostingHandler distributes a posting to one or more channels.
type PublishPostingHandler struct {
	repo repositories.PostingRepository
}

// NewPublishPostingHandler wires the handler.
func NewPublishPostingHandler(repo repositories.PostingRepository) *PublishPostingHandler {
	return &PublishPostingHandler{repo: repo}
}

// Handle executes the use case.
func (h *PublishPostingHandler) Handle(ctx context.Context, in PublishPostingInput) (dto.PostingDTO, error) {
	tenantID, err := shared.ParseTenantID(in.TenantID)
	if err != nil {
		return dto.PostingDTO{}, fmt.Errorf("publish posting: %w", err)
	}
	postingID, err := valueobjects.ParsePostingID(in.PostingID)
	if err != nil {
		return dto.PostingDTO{}, fmt.Errorf("publish posting: %w", err)
	}

	channels := make([]valueobjects.SourceChannel, 0, len(in.Channels))
	for _, c := range in.Channels {
		ch, err := valueobjects.ParseSourceChannel(c)
		if err != nil {
			return dto.PostingDTO{}, fmt.Errorf("publish posting: %w", err)
		}
		channels = append(channels, ch)
	}

	posting, err := h.repo.FindByID(ctx, tenantID, postingID)
	if err != nil {
		return dto.PostingDTO{}, fmt.Errorf("publish posting: %w", err)
	}
	if err := posting.Publish(channels); err != nil {
		return dto.PostingDTO{}, fmt.Errorf("publish posting: %w", err)
	}
	if err := h.repo.Save(ctx, posting); err != nil {
		return dto.PostingDTO{}, fmt.Errorf("publish posting: save: %w", err)
	}
	return dto.FromEntity(posting), nil
}
