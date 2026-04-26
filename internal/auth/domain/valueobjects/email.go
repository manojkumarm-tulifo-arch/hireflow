package valueobjects

import (
	"errors"
	"regexp"
	"strings"
)

// ErrInvalidEmail is returned when an email fails normalization or format check.
var ErrInvalidEmail = errors.New("invalid email address")

// Tightened email regex — RFC-5322 is way over-permissive for product use.
// Accepts most real addresses while rejecting things like "foo@bar" or "a..b@x.com".
var emailRe = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// Email is a normalized, validated email address.
// Normalization: lowercase + trimmed. Equality is exact-string after normalize.
type Email struct {
	value string
}

// NewEmail validates and constructs an Email.
func NewEmail(s string) (Email, error) {
	n := strings.ToLower(strings.TrimSpace(s))
	if n == "" || len(n) > 254 || !emailRe.MatchString(n) {
		return Email{}, ErrInvalidEmail
	}
	if strings.Contains(n, "..") {
		return Email{}, ErrInvalidEmail
	}
	return Email{value: n}, nil
}

func (e Email) String() string         { return e.value }
func (e Email) Equals(o Email) bool    { return e.value == o.value }
func (e Email) IsZero() bool           { return e.value == "" }
