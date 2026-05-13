package valueobjects

// ScoreBand is the qualitative tier derived from an Application's overall score.
// It mirrors the threshold table in docs/modules/sourcing/scoring.md §4.
type ScoreBand string

const (
	BandStrong   ScoreBand = "strong"
	BandModerate ScoreBand = "moderate"
	BandWeak     ScoreBand = "weak"
	// BandNone indicates that no LLM judgment has been produced yet
	// (the Application has an embedding score but not an overall score).
	BandNone ScoreBand = ""
)

// DeriveBand maps an overall score (0–100) to its ScoreBand.
// Pass -1 (or any negative value) to signal "no judgment yet" — returns BandNone.
//
// Thresholds per scoring.md §4:
//
//	>= 80  → strong
//	>= 60  → moderate
//	<  60  → weak
func DeriveBand(overall float64) ScoreBand {
	if overall < 0 {
		return BandNone
	}
	if overall >= 80 {
		return BandStrong
	}
	if overall >= 60 {
		return BandModerate
	}
	return BandWeak
}

// MatchScore holds the scoring signals attached to an Application.
//
//   - Overall is the LLM judge score (0–100); 0 means not yet judged.
//   - Embedding is the cosine similarity between the candidate and role vectors.
//   - Band is derived from Overall via DeriveBand.
type MatchScore struct {
	Overall   float64
	Embedding float64
	Band      ScoreBand
}
