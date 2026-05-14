package domain

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// ErrInvalidRecruiterID is returned when a RecruiterID cannot be parsed.
var ErrInvalidRecruiterID = errors.New("invalid recruiter id")

// RecruiterID identifies a user who creates and owns hiring intents.
type RecruiterID struct {
	value uuid.UUID
}

// NewRecruiterID generates a fresh RecruiterID.
func NewRecruiterID() RecruiterID {
	return RecruiterID{value: uuid.New()}
}

// ParseRecruiterID validates and constructs a RecruiterID from a string.
func ParseRecruiterID(s string) (RecruiterID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return RecruiterID{}, ErrInvalidRecruiterID
	}
	return RecruiterID{value: u}, nil
}

// String returns the canonical string form.
func (r RecruiterID) String() string { return r.value.String() }

// UUID returns the underlying uuid.UUID value.
func (r RecruiterID) UUID() uuid.UUID { return r.value }

// Equals compares two RecruiterIDs.
func (r RecruiterID) Equals(other RecruiterID) bool { return r.value == other.value }

// IsZero reports whether the RecruiterID is the zero value.
func (r RecruiterID) IsZero() bool { return r.value == uuid.Nil }

// MarshalJSON serializes the id as its canonical UUID string.
func (r RecruiterID) MarshalJSON() ([]byte, error) { return json.Marshal(r.value.String()) }

// UnmarshalJSON parses a canonical UUID string into the id.
func (r *RecruiterID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	u, err := uuid.Parse(s)
	if err != nil {
		return ErrInvalidRecruiterID
	}
	r.value = u
	return nil
}
