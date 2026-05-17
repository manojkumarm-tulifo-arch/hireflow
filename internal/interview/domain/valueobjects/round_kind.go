// Package valueobjects holds the value objects of the interview context.
package valueobjects

import "errors"

// RoundKind enumerates the supported interview round types. Each value has
// a corresponding prompt template in the AnthropicQuestionGenerator; adding
// a value requires a new template + tests.
type RoundKind string

const (
	RoundKindScreen       RoundKind = "screen"
	RoundKindTechnical    RoundKind = "technical"
	RoundKindSystemDesign RoundKind = "system_design"
	RoundKindBehavioral   RoundKind = "behavioral"
	RoundKindBarRaiser    RoundKind = "bar_raiser"
)

// ErrInvalidRoundKind is returned by ParseRoundKind when the value is unknown.
var ErrInvalidRoundKind = errors.New("invalid round kind")

// ParseRoundKind validates and returns a RoundKind for the given string.
func ParseRoundKind(s string) (RoundKind, error) {
	switch RoundKind(s) {
	case RoundKindScreen, RoundKindTechnical, RoundKindSystemDesign,
		RoundKindBehavioral, RoundKindBarRaiser:
		return RoundKind(s), nil
	default:
		return "", ErrInvalidRoundKind
	}
}

// String returns the canonical string form.
func (k RoundKind) String() string { return string(k) }
