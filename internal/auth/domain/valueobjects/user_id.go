// Package valueobjects holds the immutable value objects of the auth context.
package valueobjects

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// ErrInvalidUserID is returned when a UserID cannot be parsed.
var ErrInvalidUserID = errors.New("invalid user id")

// UserID is the unique identifier of a User aggregate.
type UserID struct {
	value uuid.UUID
}

// NewUserID generates a fresh UserID using UUID v7 when available.
func NewUserID() UserID {
	if u, err := uuid.NewV7(); err == nil {
		return UserID{value: u}
	}
	return UserID{value: uuid.New()}
}

// ParseUserID validates and constructs a UserID from a string.
func ParseUserID(s string) (UserID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return UserID{}, ErrInvalidUserID
	}
	return UserID{value: u}, nil
}

func (u UserID) String() string             { return u.value.String() }
func (u UserID) Equals(o UserID) bool       { return u.value == o.value }
func (u UserID) IsZero() bool               { return u.value == uuid.Nil }
func (u UserID) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.value.String())
}
func (u *UserID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := uuid.Parse(s)
	if err != nil {
		return ErrInvalidUserID
	}
	u.value = parsed
	return nil
}
