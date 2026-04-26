// Package valueobjects holds the immutable value objects of the hiringintent
// bounded context. Value objects have no identity and are equality-by-value.
package valueobjects

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// ErrInvalidIntentID is returned when an IntentID cannot be parsed.
var ErrInvalidIntentID = errors.New("invalid intent id")

// IntentID is the unique identifier of a HiringIntent aggregate.
type IntentID struct {
	value uuid.UUID
}

// NewIntentID generates a fresh IntentID. Uses UUID v7 for sortability when
// the underlying library supports it; falls back to v4 otherwise.
func NewIntentID() IntentID {
	if u, err := uuid.NewV7(); err == nil {
		return IntentID{value: u}
	}
	return IntentID{value: uuid.New()}
}

// ParseIntentID validates and constructs an IntentID from a string.
func ParseIntentID(s string) (IntentID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return IntentID{}, ErrInvalidIntentID
	}
	return IntentID{value: u}, nil
}

// String returns the canonical string form.
func (i IntentID) String() string { return i.value.String() }

// Equals compares two IntentIDs.
func (i IntentID) Equals(other IntentID) bool { return i.value == other.value }

// IsZero reports whether the IntentID is the zero value.
func (i IntentID) IsZero() bool { return i.value == uuid.Nil }

// MarshalJSON serializes the id as its canonical UUID string.
func (i IntentID) MarshalJSON() ([]byte, error) { return json.Marshal(i.value.String()) }

// UnmarshalJSON parses a canonical UUID string into the id.
func (i *IntentID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	u, err := uuid.Parse(s)
	if err != nil {
		return ErrInvalidIntentID
	}
	i.value = u
	return nil
}
