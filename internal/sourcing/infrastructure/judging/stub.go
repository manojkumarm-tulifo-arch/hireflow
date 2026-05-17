// Package judging holds LLMJudge adapters. Stub is a deterministic
// implementation for local development and demo use (STUB_LLMS=true).
// No real Anthropic call is made; the judgment score is derived from
// sha256 of the serialised profile so different candidates get different
// scores (range 50-95), exercising the ranking pipeline end-to-end.
package judging

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"

	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// Stub is a deterministic LLMJudge for use when STUB_LLMS=true.
// Score is sha256-seeded in the range [50, 95] so multiple candidates
// receive meaningfully different scores, exercising recruiter ranking UI.
type Stub struct{}

// compile-time interface check.
var _ services.LLMJudge = (*Stub)(nil)

// NewStub returns a ready-to-use Stub judge.
func NewStub() *Stub {
	return &Stub{}
}

// Judge returns a deterministic LLMJudgment derived from sha256 of the
// serialised profile. The role and rules arguments are accepted but unused.
func (Stub) Judge(
	_ context.Context,
	profile vo.ParsedProfile,
	_ services.RoleSpec,
	_ vo.RuleMatchReport,
) (vo.LLMJudgment, error) {
	// Seed from sha256 of the JSON-serialised profile so each unique
	// candidate consistently gets the same score across repeated judge runs.
	b, _ := json.Marshal(profile)
	h := sha256.Sum256(b)
	seed := binary.LittleEndian.Uint64(h[:8])

	// Map seed into [50, 95] — a 46-point window that exercises ranking.
	score := int(50 + (seed % 46))

	return vo.LLMJudgment{
		Score: score,
		Evidence: []vo.JudgmentEvidence{
			{
				Kind:    "skill",
				Skill:   "Go",
				Claim:   "5 years Go experience",
				Support: "Built distributed systems using Go, Kafka, and Postgres.",
			},
		},
		Summary:       "Candidate demonstrates strong backend engineering skills. Go and distributed systems experience is the strongest factor.",
		Concerns:      nil,
		PromptVersion: "stub-v1",
	}, nil
}
