// Package parsing holds ResumeParser adapters. Stub is a deterministic
// implementation for local development and demo use (STUB_LLMS=true).
// No real Anthropic call is made; the profile is derived from sha256 of the
// input text so:
//   - The same text always yields the same profile (determinism).
//   - Different texts yield different email/phone (uniqueness for dedup).
//   - No API key is needed.
package parsing

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// Stub is a deterministic ResumeParser for use when STUB_LLMS=true.
// It derives all variable fields from sha256(text) so every unique upload
// produces a unique profile, exercising the candidate-dedup path end-to-end.
type Stub struct{}

// compile-time interface check.
var _ services.ResumeParser = (*Stub)(nil)

// NewStub returns a ready-to-use Stub parser.
func NewStub() *Stub {
	return &Stub{}
}

// Parse returns a deterministic ParsedProfile derived from sha256 of text.
// Personal fields include a hash-derived suffix so each distinct input
// produces a unique candidate (dedup still exercises real DB uniqueness logic).
func (Stub) Parse(_ context.Context, text string) (vo.ParsedProfile, error) {
	h := sha256.Sum256([]byte(text))
	// Use the first 8 hex chars as a short unique tag.
	tag := fmt.Sprintf("%x", h[:4])

	return vo.ParsedProfile{
		SchemaVersion: 1,
		Personal: vo.ParsedPersonal{
			FullName: "Demo Candidate",
			Email:    fmt.Sprintf("demo+%s@example.com", tag),
			Phone:    "+91-9999999999",
			Location: "Bangalore, India",
		},
		Headline: "Senior Backend Engineer (stub)",
		Summary:  fmt.Sprintf("Stub profile derived from input hash %s. Suitable for local development only.", tag),
		Skills: []vo.ParsedSkill{
			{Name: "Go", Years: 5, EvidenceRef: "exp-001"},
			{Name: "Kafka", Years: 3, EvidenceRef: "exp-001"},
			{Name: "Postgres", Years: 4, EvidenceRef: "exp-001"},
		},
		Experiences: []vo.ParsedExperience{
			{
				ID:          "exp-001",
				Company:     "Demo Corp",
				Title:       "Senior Backend Engineer",
				Start:       "2020-01",
				End:         "2025-01",
				Description: "Built distributed systems using Go, Kafka, and Postgres.",
				SkillsUsed:  []string{"Go", "Kafka", "Postgres"},
			},
		},
		Education: []vo.ParsedEducation{
			{
				Institution: "IIT Bombay",
				Degree:      "BTech",
				Field:       "Computer Science",
				Start:       "2014-08",
				End:         "2018-05",
			},
		},
	}, nil
}
