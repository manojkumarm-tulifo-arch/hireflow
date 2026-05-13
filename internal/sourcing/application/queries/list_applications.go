package queries

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/dto"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// ListApplicationsHandler returns ranked Applications for a given intent.
// It fetches each application's Candidate to build a masked candidate name,
// computes per-score-band facets, and returns the assembled DTO.
type ListApplicationsHandler struct {
	repo          repositories.ApplicationRepository
	candidateRepo repositories.CandidateRepository
	encryptor     services.PIIEncryptor
}

// NewListApplicationsHandler wires the handler.
func NewListApplicationsHandler(
	repo repositories.ApplicationRepository,
	candidateRepo repositories.CandidateRepository,
	encryptor services.PIIEncryptor,
) *ListApplicationsHandler {
	return &ListApplicationsHandler{
		repo:          repo,
		candidateRepo: candidateRepo,
		encryptor:     encryptor,
	}
}

// ListApplicationsInput carries the query parameters.
type ListApplicationsInput struct {
	TenantID shared.TenantID
	IntentID uuid.UUID
	Filter   repositories.ApplicationListFilter
}

// Handle fetches and assembles the ApplicationListResponse.
//
// Steps:
//  1. Fetch applications via repo.ListByIntent.
//  2. For each application, fetch the Candidate and derive a masked name by
//     decrypting the encrypted column → take the first rune + "***".
//  3. Build ApplicationListItemDTO for each application.
//  4. Compute facets: count by score_band for rows where ScoreBand is non-nil.
//  5. Return ApplicationListResponse.
func (h *ListApplicationsHandler) Handle(ctx context.Context, in ListApplicationsInput) (dto.ApplicationListResponse, error) {
	apps, err := h.repo.ListByIntent(ctx, in.TenantID, in.IntentID, in.Filter)
	if err != nil {
		return dto.ApplicationListResponse{}, fmt.Errorf("list applications: %w", err)
	}

	items := make([]dto.ApplicationListItemDTO, 0, len(apps))
	facets := dto.ApplicationListFacets{}

	for _, app := range apps {
		// Fetch candidate for name + profile metadata.
		cand, err := h.candidateRepo.FindByID(ctx, in.TenantID, app.CandidateID())
		if err != nil {
			return dto.ApplicationListResponse{}, fmt.Errorf("fetch candidate %s: %w", app.CandidateID(), err)
		}

		// Decrypt name and immediately mask it — never expose cleartext in list view.
		decrypted, err := h.encryptor.Decrypt(ctx, in.TenantID, cand.EncryptedFullName())
		if err != nil {
			return dto.ApplicationListResponse{}, fmt.Errorf("decrypt name for candidate %s: %w", cand.ID(), err)
		}
		masked := maskName(decrypted)

		// Serialise rule_match and llm_judgment to json.RawMessage.
		ruleMatchJSON, err := app.RuleMatch().Marshal()
		if err != nil {
			return dto.ApplicationListResponse{}, fmt.Errorf("marshal rule_match for application %s: %w", app.ID(), err)
		}

		var llmJSON json.RawMessage
		if j := app.LLMJudgment(); j != nil {
			b, err := j.Marshal()
			if err != nil {
				return dto.ApplicationListResponse{}, fmt.Errorf("marshal llm_judgment for application %s: %w", app.ID(), err)
			}
			llmJSON = b
		}

		// Convert *vo.ScoreBand to *string for the DTO.
		var scoreBandStr *string
		if band := app.ScoreBand(); band != nil {
			s := string(*band)
			scoreBandStr = &s
		}

		items = append(items, dto.ApplicationListItemDTO{
			ApplicationID:  app.ID(),
			CandidateID:    cand.ID(),
			CandidateName:  masked,
			Headline:       cand.Headline(),
			Location:       cand.Location(),
			Status:         app.Status().String(),
			OverallScore:   app.OverallScore(),
			ScoreBand:      scoreBandStr,
			EmbeddingScore: app.EmbeddingScore(),
			RuleMatch:      ruleMatchJSON,
			LLMJudgment:    llmJSON,
			ScoredAt:       app.ScoredAt(),
			UpdatedAt:      app.UpdatedAt(),
		})

		// Tally facets — only for rows where ScoreBand is non-nil (judged rows).
		if band := app.ScoreBand(); band != nil {
			switch *band {
			case vo.BandStrong:
				facets.Strong++
			case vo.BandModerate:
				facets.Moderate++
			case vo.BandWeak:
				facets.Weak++
			}
		}
	}

	return dto.ApplicationListResponse{
		Items:  items,
		Total:  len(items),
		Facets: facets,
	}, nil
}

// maskName returns a masked representation of the decrypted full name.
// The first UTF-8 rune is preserved; the rest is replaced with "***".
// Empty or whitespace-only strings are returned as "***".
//
// Examples:
//
//	"Alice"       → "A***"
//	"Bob Smith"   → "B***"
//	""            → "***"
func maskName(name string) string {
	if name == "" {
		return "***"
	}
	r, size := utf8.DecodeRuneInString(name)
	if r == utf8.RuneError && size <= 1 {
		return "***"
	}
	return string(r) + "***"
}
