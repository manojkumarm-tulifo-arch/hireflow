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
// Construct only via NewIntentSignal; fields are private so the zero
// value can't pass for a populated signal.
type IntentSignal struct {
	label string
	value string
	level SignalLevel
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
	return IntentSignal{label: l, value: v, level: level}, nil
}

// Label returns the signal label.
func (s IntentSignal) Label() string { return s.label }

// Value returns the signal's free-text value.
func (s IntentSignal) Value() string { return s.value }

// Level returns the qualitative rating.
func (s IntentSignal) Level() SignalLevel { return s.level }

// TrustSignal captures a candidate verification requirement —
// e.g., ID verification, liveness, BGV, NDA, references. Same
// encapsulation discipline as IntentSignal.
type TrustSignal struct {
	label    string
	value    string
	required bool
}

// NewTrustSignal validates inputs.
func NewTrustSignal(label, value string, required bool) (TrustSignal, error) {
	l := strings.TrimSpace(label)
	v := strings.TrimSpace(value)
	if l == "" || v == "" {
		return TrustSignal{}, errors.New("trust signal label and value must not be empty")
	}
	return TrustSignal{label: l, value: v, required: required}, nil
}

// Label returns the trust-signal label.
func (s TrustSignal) Label() string { return s.label }

// Value returns the trust-signal value.
func (s TrustSignal) Value() string { return s.value }

// Required reports whether the verification is required.
func (s TrustSignal) Required() bool { return s.required }
