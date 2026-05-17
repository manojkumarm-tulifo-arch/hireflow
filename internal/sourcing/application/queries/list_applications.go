package queries

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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

		// Top 3 skills by years desc from the candidate's parsed profile.
		topSkills := buildTopSkills(cand.Profile())

		// First sentence of llm_judgment.summary.
		judgeSummary := ""
		if j := app.LLMJudgment(); j != nil {
			judgeSummary = firstSentence(j.Summary)
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
			TopSkills:      topSkills,
			JudgeSummary:   judgeSummary,
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

// buildTopSkills returns up to 3 skills from the profile, ordered by Years
// descending. Skills with the same years preserve their original order.
func buildTopSkills(profile vo.ParsedProfile) []dto.SkillSummary {
	skills := make([]vo.ParsedSkill, len(profile.Skills))
	copy(skills, profile.Skills)
	sort.SliceStable(skills, func(i, j int) bool {
		return skills[i].Years > skills[j].Years
	})
	top := make([]dto.SkillSummary, 0, 3)
	for i, s := range skills {
		if i >= 3 {
			break
		}
		top = append(top, dto.SkillSummary{Name: s.Name, Years: s.Years})
	}
	return top
}

// firstSentence returns the first sentence of s. A sentence boundary is
// defined as ". " (period + space) or a trailing "." with nothing after it.
// If s is empty, "" is returned. If no sentence boundary is found, the full
// string is returned.
func firstSentence(s string) string {
	if s == "" {
		return ""
	}
	if idx := strings.Index(s, ". "); idx >= 0 {
		return s[:idx+1]
	}
	// Trailing period with no following space (e.g. single-sentence summary).
	if strings.HasSuffix(s, ".") {
		return s
	}
	return s
}
