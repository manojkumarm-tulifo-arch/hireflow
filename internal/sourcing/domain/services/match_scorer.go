package services

import (
	"context"

	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// MatchInput bundles all inputs required to score one (Candidate, Intent) pair.
// RoleSpec is the anti-corruption projection from the hiringintent context.
// CandidateVec and RoleVec are the L2-normalised 1024-dim embeddings produced
// by the Embedder port.
type MatchInput struct {
	Profile      vo.ParsedProfile
	Role         RoleSpec // anti-corruption type defined in intent_reader.go
	CandidateVec []float32
	RoleVec      []float32
}

// MatchOutput carries the scoring results for one (Candidate, Intent) pair.
//
// If any required rule criterion fails, EmbeddingScore and CoarseScore are nil —
// the caller MUST call Application.Exclude in that case rather than
// Application.RecordEmbeddingScore.
type MatchOutput struct {
	Rules          vo.RuleMatchReport
	EmbeddingScore *float64 // nil when rule-failed; cosine similarity otherwise
	CoarseScore    *float64 // nil when rule-failed; used for top-K judge selection
}

// MatchScorer is the port for per-(Candidate, Intent) rule + embedding scoring.
// The canonical implementation (InProcMatchScorer) is pure-Go: it runs the
// rule engine and computes cosine similarity in-process without any I/O.
type MatchScorer interface {
	// Score evaluates in against the rule criteria and the embedding vectors.
	// It never calls external services; embedding is the caller's responsibility.
	Score(ctx context.Context, in MatchInput) (MatchOutput, error)
}
