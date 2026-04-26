// Package valueobjects holds the immutable value objects of the jobposting
// bounded context.
package valueobjects

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// ErrInvalidPostingID is returned when a PostingID cannot be parsed.
var ErrInvalidPostingID = errors.New("invalid posting id")

// PostingID is the unique identifier of a JobPosting aggregate.
type PostingID struct {
	value uuid.UUID
}

// NewPostingID generates a fresh PostingID using UUID v7 when available.
func NewPostingID() PostingID {
	if u, err := uuid.NewV7(); err == nil {
		return PostingID{value: u}
	}
	return PostingID{value: uuid.New()}
}

// ParsePostingID validates and constructs a PostingID from a string.
func ParsePostingID(s string) (PostingID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return PostingID{}, ErrInvalidPostingID
	}
	return PostingID{value: u}, nil
}

func (p PostingID) String() string             { return p.value.String() }
func (p PostingID) Equals(o PostingID) bool    { return p.value == o.value }
func (p PostingID) IsZero() bool               { return p.value == uuid.Nil }
func (p PostingID) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.value.String())
}
func (p *PostingID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	u, err := uuid.Parse(s)
	if err != nil {
		return ErrInvalidPostingID
	}
	p.value = u
	return nil
}
