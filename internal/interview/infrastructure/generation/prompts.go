// Package generation holds the AnthropicQuestionGenerator + per-round prompt
// templates.
package generation

import (
	"fmt"
	"strings"

	"github.com/hustle/hireflow/internal/interview/domain/services"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

// QuestionCounts is the per-round-kind target question count.
var QuestionCounts = map[vo.RoundKind]int{
	vo.RoundKindScreen:       4,
	vo.RoundKindTechnical:    6,
	vo.RoundKindSystemDesign: 6,
	vo.RoundKindBehavioral:   4,
	vo.RoundKindBarRaiser:    4,
}

var roundKindBriefs = map[vo.RoundKind]string{
	vo.RoundKindScreen: `An initial screen call between the recruiter / hiring manager and the candidate.
Focus: role-fit, motivation, level alignment, deal-breaker questions on location / comp / start-date.
Avoid deep technical drilling — that belongs in later rounds.`,
	vo.RoundKindTechnical: `A hands-on technical round probing the candidate's craft in the role's primary skills.
Focus: depth on the skills listed as required in the role spec. Coding-by-discussion is fine; design at
implementation level. Each question should connect to a specific skill the candidate claims experience in.`,
	vo.RoundKindSystemDesign: `An architecture / scaling round.
Focus: how the candidate decomposes a non-trivial system, makes trade-offs (consistency vs availability,
latency vs durability), reasons about failure modes, scale, and operational concerns. Tailor to the
candidate's prior systems experience.`,
	vo.RoundKindBehavioral: `A STAR-style past-experience round.
Focus: how the candidate handled past situations — conflict, ambiguity, ownership, failure, mentoring.
Each question should target a specific competency the role spec implies (e.g., leadership for a staff role).`,
	vo.RoundKindBarRaiser: `A broader judgment / leadership / culture round.
Focus: the candidate's principles, decisions under uncertainty, hiring bar, cross-functional collaboration,
how they raise the level of teams they join. This is the "would we hire them again" lens.`,
}

func BuildPrompt(in services.GenerationInput) (system string, user string) {
	count := QuestionCounts[in.RoundKind]
	if count == 0 {
		count = 4
	}
	brief := roundKindBriefs[in.RoundKind]
	roleBrief := formatRoleSpec(in.RoleSpec)
	candidateBrief := formatCandidateProfile(in.CandidateProfile)

	system = `You are designing an interview round for a specific role and candidate.
You will return a JSON array of question objects. Each question must be tailored to BOTH the role spec
and the candidate's actual experience — generic questions are not acceptable.

The interviewer running this round may not be a deep expert in every domain the candidate claims.
Your model_answer paragraph MUST be concrete and specific enough that a domain-generalist interviewer can
use it as a real-time reference to evaluate the candidate's response.`

	steering := ""
	if strings.TrimSpace(in.Steering) != "" {
		steering = "\n\nAdditional recruiter steering for this regeneration:\n" + in.Steering
	}

	user = fmt.Sprintf(`Round type: %s

%s

Role spec:
%s

Candidate profile:
%s
%s

Produce exactly %d question objects as a JSON array. Each object must have these fields, all required:

- prompt: string — the question the interviewer asks.
- skill_probed: string — which skill from the role spec this targets.
- why: string — one sentence tying this question to something in the candidate's profile.
- expected_signals: string[] (>= 3) — what a strong answer demonstrates.
- model_answer: string (one paragraph) — a concrete sketch of what a strong answer looks like, written
  so a domain-generalist interviewer can compare it to the candidate's response in real time.
- red_flags: string[] (>= 2) — specific weak-answer patterns to watch for.
- follow_ups: string[] (>= 1) — deeper probes if the candidate's first pass is shallow.

Return ONLY the JSON array. No prose, no commentary.`,
		in.RoundKind, brief, roleBrief, candidateBrief, steering, count)

	return system, user
}

func formatRoleSpec(s services.RoleSpec) string {
	var sb strings.Builder
	if s.Title != "" {
		sb.WriteString("- Title: " + s.Title + "\n")
	}
	if s.Seniority != "" {
		sb.WriteString("- Seniority: " + s.Seniority + "\n")
	}
	if s.YearsMin > 0 || s.YearsMax > 0 {
		sb.WriteString(fmt.Sprintf("- Years experience: %d-%d\n", s.YearsMin, s.YearsMax))
	}
	if s.Team != "" {
		sb.WriteString("- Team: " + s.Team + "\n")
	}
	if s.Reports != "" {
		sb.WriteString("- Reports to: " + s.Reports + "\n")
	}
	if len(s.Skills) > 0 {
		sb.WriteString("- Skills:\n")
		for _, sk := range s.Skills {
			marker := ""
			if sk.Required {
				marker = " (required)"
			}
			sb.WriteString("  - " + sk.Name + marker + "\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func formatCandidateProfile(p services.CandidateProfile) string {
	var sb strings.Builder
	if p.Headline != "" {
		sb.WriteString("- Headline: " + p.Headline + "\n")
	}
	if p.Location != "" {
		sb.WriteString("- Location: " + p.Location + "\n")
	}
	if len(p.Skills) > 0 {
		sb.WriteString("- Skills: " + strings.Join(p.Skills, ", ") + "\n")
	}
	if len(p.Experiences) > 0 {
		sb.WriteString("- Experience:\n")
		for _, e := range p.Experiences {
			line := "  - " + e.Title
			if e.Company != "" {
				line += " at " + e.Company
			}
			if e.Duration != "" {
				line += " (" + e.Duration + ")"
			}
			sb.WriteString(line + "\n")
			if e.Summary != "" {
				sb.WriteString("    " + e.Summary + "\n")
			}
		}
	}
	if len(p.Education) > 0 {
		sb.WriteString("- Education:\n")
		for _, e := range p.Education {
			sb.WriteString("  - " + e.Degree + " in " + e.Field + ", " + e.Institution + " (" + e.Year + ")\n")
		}
	}
	if len(p.Certifications) > 0 {
		sb.WriteString("- Certifications: " + strings.Join(p.Certifications, ", ") + "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}
