package valueobjects

import (
	"encoding/json"
	"errors"
	"regexp"

	"github.com/google/uuid"
)

var (
	// ErrInvalidOTPCode is returned when an OTP string isn't 6 digits.
	ErrInvalidOTPCode = errors.New("otp code must be 6 digits")
	// ErrInvalidOTPSessionID is returned when an id cannot be parsed.
	ErrInvalidOTPSessionID = errors.New("invalid otp session id")
	// ErrInvalidOTPPurpose is returned for an unrecognized purpose string.
	ErrInvalidOTPPurpose = errors.New("invalid otp purpose")
)

var otpRe = regexp.MustCompile(`^[0-9]{6}$`)

// OTPCode is a 6-digit one-time password value object.
type OTPCode struct {
	value string
}

// NewOTPCode validates and constructs an OTP value.
func NewOTPCode(s string) (OTPCode, error) {
	if !otpRe.MatchString(s) {
		return OTPCode{}, ErrInvalidOTPCode
	}
	return OTPCode{value: s}, nil
}

func (c OTPCode) String() string { return c.value }

// OTPSessionID identifies an in-flight OTP challenge.
type OTPSessionID struct {
	value uuid.UUID
}

// NewOTPSessionID generates a fresh session id.
func NewOTPSessionID() OTPSessionID {
	if u, err := uuid.NewV7(); err == nil {
		return OTPSessionID{value: u}
	}
	return OTPSessionID{value: uuid.New()}
}

// ParseOTPSessionID validates and constructs from a string.
func ParseOTPSessionID(s string) (OTPSessionID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return OTPSessionID{}, ErrInvalidOTPSessionID
	}
	return OTPSessionID{value: u}, nil
}

func (i OTPSessionID) String() string                { return i.value.String() }
func (i OTPSessionID) Equals(o OTPSessionID) bool    { return i.value == o.value }
func (i OTPSessionID) MarshalJSON() ([]byte, error)  { return json.Marshal(i.value.String()) }
func (i *OTPSessionID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := uuid.Parse(s)
	if err != nil {
		return ErrInvalidOTPSessionID
	}
	i.value = parsed
	return nil
}

// OTPPurpose distinguishes a signup challenge from a signin challenge.
// Purpose is encoded so we can't accidentally use a signup OTP to sign in
// (or vice versa) — defense-in-depth on top of the per-session SQL lookup.
type OTPPurpose string

const (
	OTPPurposeSignup OTPPurpose = "SIGNUP"
	OTPPurposeSignin OTPPurpose = "SIGNIN"
)

// ParseOTPPurpose validates a string.
func ParseOTPPurpose(s string) (OTPPurpose, error) {
	switch OTPPurpose(s) {
	case OTPPurposeSignup, OTPPurposeSignin:
		return OTPPurpose(s), nil
	default:
		return "", ErrInvalidOTPPurpose
	}
}

func (p OTPPurpose) String() string { return string(p) }
