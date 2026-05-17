package valueobjects

import (
	"errors"
	"strings"
)

// Question is one generated probe for an interview round. Immutable value object.
type Question struct {
	Prompt          string   `json:"prompt"`
	SkillProbed     string   `json:"skill_probed"`
	Why             string   `json:"why"`
	ExpectedSignals []string `json:"expected_signals"`
	ModelAnswer     string   `json:"model_answer"`
	RedFlags        []string `json:"red_flags"`
	FollowUps       []string `json:"follow_ups"`
}

// ErrInvalidQuestion is returned by Validate when a question fails its shape
// requirements.
var ErrInvalidQuestion = errors.New("invalid question")

// Validate enforces minimum invariants. Used both by the LLM-output parser in
// AnthropicQuestionGenerator and as a sanity check on round persistence.
func (q Question) Validate() error {
	if strings.TrimSpace(q.Prompt) == "" {
		return errors.New("question: prompt required")
	}
	if strings.TrimSpace(q.SkillProbed) == "" {
		return errors.New("question: skill_probed required")
	}
	if strings.TrimSpace(q.Why) == "" {
		return errors.New("question: why required")
	}
	if len(q.ExpectedSignals) < 3 {
		return errors.New("question: expected_signals must have at least 3 entries")
	}
	if strings.TrimSpace(q.ModelAnswer) == "" {
		return errors.New("question: model_answer required")
	}
	if len(q.RedFlags) < 2 {
		return errors.New("question: red_flags must have at least 2 entries")
	}
	if len(q.FollowUps) < 1 {
		return errors.New("question: follow_ups must have at least 1 entry")
	}
	return nil
}
