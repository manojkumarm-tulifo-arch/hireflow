package valueobjects

import "encoding/json"

// JudgmentEvidence is one piece of supporting evidence from the LLM judge.
// Kind is "skill" or "experience"; the remaining fields depend on Kind.
//
//   - Kind "skill":      Skill + Claim + Support
//   - Kind "experience": Experience + Support
type JudgmentEvidence struct {
	Kind       string `json:"kind"`
	Skill      string `json:"skill,omitempty"`
	Claim      string `json:"claim,omitempty"`
	Experience string `json:"experience,omitempty"`
	Support    string `json:"support"`
}

// LLMJudgment holds the structured verdict returned by the LLM judge.
// It is stored as JSONB in applications.llm_judgment.
//
//   - Score         is the 0–100 integer score.
//   - Evidence      is the list of grounded evidence items.
//   - Summary       is a 2-sentence narrative.
//   - Concerns      lists recruiter-visible red flags.
//   - PromptVersion tracks which judge prompt produced this judgment so
//     historical scores remain reproducible after prompt bumps.
type LLMJudgment struct {
	Score         int                `json:"score"`
	Evidence      []JudgmentEvidence `json:"evidence,omitempty"`
	Summary       string             `json:"summary,omitempty"`
	Concerns      []string           `json:"concerns,omitempty"`
	PromptVersion string             `json:"prompt_version"`
}

// Marshal serialises the judgment to JSON for pgx jsonb storage.
func (j LLMJudgment) Marshal() ([]byte, error) {
	return json.Marshal(j)
}

// UnmarshalLLMJudgment deserialises an LLMJudgment from JSON (pgx jsonb retrieval).
func UnmarshalLLMJudgment(b []byte) (LLMJudgment, error) {
	var j LLMJudgment
	if err := json.Unmarshal(b, &j); err != nil {
		return LLMJudgment{}, err
	}
	return j, nil
}
