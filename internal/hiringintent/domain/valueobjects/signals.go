package valueobjects

import (
	"errors"
	"strings"
)

// ErrInvalidSignalLevel is returned when a level string cannot be parsed.
var ErrInvalidSignalLevel = errors.New("invalid signal level")

// SignalLevel is a qualitative readiness rating on an IntentSignal.
type SignalLevel string

const (
	SignalLevelLow    SignalLevel = "LOW"
	SignalLevelMedium SignalLevel = "MEDIUM"
	SignalLevelHigh   SignalLevel = "HIGH"
)

// ParseSignalLevel validates a string and returns the matching level.
func ParseSignalLevel(s string) (SignalLevel, error) {
	switch SignalLevel(s) {
	case SignalLevelLow, SignalLevelMedium, SignalLevelHigh:
		return SignalLevel(s), nil
	default:
		return "", ErrInvalidSignalLevel
	}
}

// IntentSignal captures readiness indicators on a hiring intent —
// e.g., urgency, budget approved, hiring manager involvement.
type IntentSignal struct {
	Label string
	Value string
	Level SignalLevel
}

// NewIntentSignal validates inputs.
func NewIntentSignal(label, value string, level SignalLevel) (IntentSignal, error) {
	l := strings.TrimSpace(label)
	v := strings.TrimSpace(value)
	if l == "" || v == "" {
		return IntentSignal{}, errors.New("intent signal label and value must not be empty")
	}
	if _, err := ParseSignalLevel(string(level)); err != nil {
		return IntentSignal{}, err
	}
	return IntentSignal{Label: l, Value: v, Level: level}, nil
}

// TrustSignal captures a candidate verification requirement —
// e.g., ID verification, liveness, BGV, NDA, references.
type TrustSignal struct {
	Label    string
	Value    string
	Required bool
}

// NewTrustSignal validates inputs.
func NewTrustSignal(label, value string, required bool) (TrustSignal, error) {
	l := strings.TrimSpace(label)
	v := strings.TrimSpace(value)
	if l == "" || v == "" {
		return TrustSignal{}, errors.New("trust signal label and value must not be empty")
	}
	return TrustSignal{Label: l, Value: v, Required: required}, nil
}
