// Package generation holds QuestionGenerator adapters. Stub is a deterministic
// implementation for local development and demo use (STUB_LLMS=true).
// No real Anthropic call is made; three canned Question entries are returned.
// Skill references use the first skill from the role spec when available,
// falling back to "Backend Engineering".
package generation

import (
	"context"

	"github.com/hustle/hireflow/internal/interview/domain/services"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

// Stub is a deterministic QuestionGenerator for use when STUB_LLMS=true.
type Stub struct{}

// compile-time interface check.
var _ services.QuestionGenerator = (*Stub)(nil)

// NewStub returns a ready-to-use Stub question generator.
func NewStub() *Stub {
	return &Stub{}
}

// Generate returns 3 canned Question entries. The skill_probed and prompt
// fields reference the first skill from the role spec if one is present,
// otherwise they fall back to "Backend Engineering". All required Question
// invariants (≥3 expected_signals, ≥2 red_flags, ≥1 follow_up) are met.
func (Stub) Generate(_ context.Context, in services.GenerationInput) ([]vo.Question, error) {
	skill := firstSkill(in.RoleSpec.Skills)

	return []vo.Question{
		{
			Prompt:      "Walk me through a time you designed a system using " + skill + ". What trade-offs did you make?",
			SkillProbed: skill,
			Why:         "Assesses depth of practical experience and ability to reason about design trade-offs under real constraints.",
			ExpectedSignals: []string{
				"Names a concrete system (not hypothetical)",
				"Articulates clear trade-offs (e.g. latency vs consistency)",
				"Explains decision rationale tied to business context",
			},
			ModelAnswer: "A strong answer describes a specific system, explains why " + skill +
				" was chosen, identifies 2-3 meaningful trade-offs made, and reflects on what they'd do differently.",
			RedFlags: []string{
				"Cannot name a specific system or project",
				"Describes only happy-path design with no trade-offs",
			},
			FollowUps: []string{
				"What would you change if you were starting that system from scratch today?",
			},
		},
		{
			Prompt:      "Describe how you would debug a production latency regression in a " + skill + " service.",
			SkillProbed: skill,
			Why:         "Tests systematic debugging skills, observability awareness, and calm under production pressure.",
			ExpectedSignals: []string{
				"Mentions metrics, tracing, or logs as first steps",
				"Proposes hypothesis-driven investigation",
				"Describes rollback or mitigation strategy",
			},
			ModelAnswer: "A strong answer outlines an observability-first approach: check dashboards/alerts, form a hypothesis, isolate the change that caused the regression (deploy diff, config change, data volume), and apply a targeted fix or rollback.",
			RedFlags: []string{
				"Jumps straight to code changes without gathering data",
				"Cannot describe any observability tooling they have used",
			},
			FollowUps: []string{
				"How would you prevent this class of regression in the future?",
			},
		},
		{
			Prompt:      "How do you approach code review for a pull request that touches core business logic?",
			SkillProbed: "Code Quality",
			Why:         "Reveals attitude toward quality, team collaboration, and understanding of correctness vs style concerns.",
			ExpectedSignals: []string{
				"Prioritises correctness over style",
				"Mentions test coverage as a review criterion",
				"Describes how they give actionable, non-blocking feedback",
			},
			ModelAnswer: "A strong answer separates blocking correctness issues from non-blocking style suggestions, checks for test coverage, and frames feedback as questions or alternatives rather than mandates.",
			RedFlags: []string{
				"Treats all feedback as equally blocking",
				"Does not mention testing or observability in reviews",
			},
			FollowUps: []string{
				"How do you handle a situation where you strongly disagree with a reviewer's feedback?",
			},
		},
	}, nil
}

// firstSkill returns the name of the first skill in the list, or
// "Backend Engineering" if the list is empty.
func firstSkill(skills []services.SkillRequirement) string {
	for _, s := range skills {
		if s.Name != "" {
			return s.Name
		}
	}
	return "Backend Engineering"
}
