// Package scoring contains infrastructure helpers for the match-scoring pipeline.
package scoring

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// InProcMatchScorer is a pure-Go implementation of the MatchScorer port.
// It runs the rule engine and computes cosine similarity entirely in-process
// without any I/O. The context argument satisfies the interface contract but is
// never used for cancellation or timeouts — the scorer is deterministic and
// completes in microseconds.
type InProcMatchScorer struct{}

// NewInProcMatchScorer returns a ready-to-use InProcMatchScorer.
func NewInProcMatchScorer() *InProcMatchScorer {
	return &InProcMatchScorer{}
}

// Score evaluates a (Candidate, Intent) pair against the role's criteria and
// the pre-computed embedding vectors.
//
// Algorithm (see docs/modules/sourcing/scoring.md §3):
//  1. Build a RuleMatchReport by evaluating each criterion in the RoleSpec.
//  2. If any required criterion fails (PassedRequired() == false), return
//     MatchOutput{Rules: report} with nil EmbeddingScore and nil CoarseScore.
//     The caller MUST call Application.Exclude in this case.
//  3. Compute cosine similarity between in.CandidateVec and in.RoleVec.
//  4. Compute coarse_score = required_pass_rate×100 + cosine×20.
//  5. Return the full MatchOutput.
func (s *InProcMatchScorer) Score(_ context.Context, in services.MatchInput) (services.MatchOutput, error) {
	report := buildRuleMatch(in.Profile, in.Role)

	if !report.PassedRequired() {
		return services.MatchOutput{Rules: report}, nil
	}

	sim := cosineSimilarity(in.CandidateVec, in.RoleVec)
	coarse := report.RequiredPassRate()*100 + sim*20

	return services.MatchOutput{
		Rules:          report,
		EmbeddingScore: &sim,
		CoarseScore:    &coarse,
	}, nil
}

// ─── rule engine ──────────────────────────────────────────────────────────────

// buildRuleMatch iterates every criterion in the RoleSpec and produces a
// RuleMatchReport containing one RuleResult per criterion.
func buildRuleMatch(profile vo.ParsedProfile, role services.RoleSpec) vo.RuleMatchReport {
	var results []vo.RuleResult

	// Required skills.
	for _, skill := range role.RequiredSkills {
		results = append(results, evaluateSkill(profile, skill, true))
	}

	// Optional skills.
	for _, skill := range role.OptionalSkills {
		results = append(results, evaluateSkill(profile, skill, false))
	}

	// Experience range.
	if role.MinYears > 0 {
		totalYears := totalExperienceYears(profile.Experiences)
		actual := fmt.Sprintf("%.1fy", totalYears)

		passed := totalYears >= float64(role.MinYears)
		results = append(results, vo.RuleResult{
			Criterion: vo.RuleCriterion{
				Type:     "experience",
				Name:     fmt.Sprintf("%dy-%dy", role.MinYears, role.MaxYears),
				Required: true,
			},
			Passed: passed,
			Actual: actual,
		})
	}
	if role.MaxYears > 0 {
		totalYears := totalExperienceYears(profile.Experiences)
		actual := fmt.Sprintf("%.1fy", totalYears)

		passed := totalYears <= float64(role.MaxYears)
		results = append(results, vo.RuleResult{
			Criterion: vo.RuleCriterion{
				Type:     "experience",
				Name:     fmt.Sprintf("max-%dy", role.MaxYears),
				Required: false, // upper-bound is soft per the spec
			},
			Passed: passed,
			Actual: actual,
		})
	}

	// Location (soft) — skipped entirely when work_mode is "remote".
	if len(role.Locations) > 0 && !strings.EqualFold(role.WorkMode, "remote") {
		candidateLocation := profile.Personal.Location
		passed := false
		for _, loc := range role.Locations {
			if strings.Contains(
				strings.ToLower(candidateLocation),
				strings.ToLower(loc),
			) || strings.Contains(
				strings.ToLower(loc),
				strings.ToLower(candidateLocation),
			) {
				passed = true
				break
			}
		}
		results = append(results, vo.RuleResult{
			Criterion: vo.RuleCriterion{
				Type:     "location",
				Name:     strings.Join(role.Locations, "|"),
				Required: false, // locations are soft per spec
			},
			Passed: passed,
			Actual: candidateLocation,
		})
	}

	// Degree (soft).
	if role.Degree != "" {
		passed := false
		for _, edu := range profile.Education {
			if strings.Contains(strings.ToLower(edu.Degree), strings.ToLower(role.Degree)) {
				passed = true
				break
			}
		}
		results = append(results, vo.RuleResult{
			Criterion: vo.RuleCriterion{
				Type:     "education",
				Name:     role.Degree,
				Required: false,
			},
			Passed: passed,
		})
	}

	// Languages (soft).
	for _, lang := range role.Languages {
		passed := false
		for _, pl := range profile.Languages {
			if strings.EqualFold(pl.Name, lang) {
				passed = true
				break
			}
		}
		results = append(results, vo.RuleResult{
			Criterion: vo.RuleCriterion{
				Type:     "language",
				Name:     lang,
				Required: false,
			},
			Passed: passed,
		})
	}

	return vo.RuleMatchReport{Results: results}
}

// evaluateSkill checks whether the candidate's skills contain a match for the
// given SkillSpec. Matching is case-insensitive on name. If skill.MinYears > 0,
// the candidate skill's years must also meet the threshold.
func evaluateSkill(profile vo.ParsedProfile, skill services.SkillSpec, required bool) vo.RuleResult {
	criterion := vo.RuleCriterion{
		Type:     "skill",
		Name:     skill.Name,
		Required: required,
	}

	for _, ps := range profile.Skills {
		if !strings.EqualFold(ps.Name, skill.Name) {
			continue
		}
		// Name matches. Check years if a minimum is specified.
		if skill.MinYears > 0 && ps.Years < skill.MinYears {
			return vo.RuleResult{
				Criterion: criterion,
				Passed:    false,
				Actual:    fmt.Sprintf("%.1fy (need %.0fy)", ps.Years, skill.MinYears),
			}
		}
		ref := ps.EvidenceRef
		actual := ""
		if ps.Years > 0 {
			actual = fmt.Sprintf("%.1fy", ps.Years)
		}
		return vo.RuleResult{
			Criterion:   criterion,
			Passed:      true,
			Actual:      actual,
			EvidenceRef: ref,
		}
	}

	// No matching skill found.
	return vo.RuleResult{
		Criterion: criterion,
		Passed:    false,
	}
}

// totalExperienceYears sums (end - start) across all experience entries in
// fractional years. Entries with current=true use time.Now() as the end date.
// Dates must be in YYYY-MM format; unparseable dates are skipped silently.
//
// Judgment call for current=true: we use time.Now() at call time so the
// computation is accurate to the day of scoring, not the day of parsing. This
// means the figure can drift slightly between scoring runs, which is acceptable
// — what matters is the order-of-magnitude accuracy needed for the required/soft
// experience gate.
func totalExperienceYears(exps []vo.ParsedExperience) float64 {
	const layout = "2006-01"
	now := time.Now()
	var total float64

	for _, exp := range exps {
		if exp.Start == "" {
			continue
		}
		start, err := time.Parse(layout, exp.Start)
		if err != nil {
			continue
		}

		var end time.Time
		if exp.Current || exp.End == "" {
			end = now
		} else {
			end, err = time.Parse(layout, exp.End)
			if err != nil {
				continue
			}
		}

		if end.Before(start) {
			continue
		}
		// Convert the duration to fractional years (365.25 days/year).
		total += end.Sub(start).Hours() / (365.25 * 24)
	}

	return total
}

// ─── cosine similarity ────────────────────────────────────────────────────────

// cosineSimilarity computes cos(a, b) = (a·b) / (‖a‖ · ‖b‖).
// Returns 0 for empty or mismatched-length vectors instead of panicking.
// The Voyage AI embedder returns L2-normalised vectors so in practice this
// reduces to a dot product, but we keep the full formula for correctness.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
