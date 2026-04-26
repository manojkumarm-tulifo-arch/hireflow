package valueobjects

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// ErrInvalidRefreshTokenID is returned when an id cannot be parsed.
var ErrInvalidRefreshTokenID = errors.New("invalid refresh token id")

// RefreshTokenID identifies a stored refresh token row.
// The opaque token returned to the client is a separate value (a random
// secret), and only its hash plus this id are persisted.
type RefreshTokenID struct {
	value uuid.UUID
}

// NewRefreshTokenID generates a fresh id.
func NewRefreshTokenID() RefreshTokenID {
	if u, err := uuid.NewV7(); err == nil {
		return RefreshTokenID{value: u}
	}
	return RefreshTokenID{value: uuid.New()}
}

// ParseRefreshTokenID validates and constructs from a string.
func ParseRefreshTokenID(s string) (RefreshTokenID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return RefreshTokenID{}, ErrInvalidRefreshTokenID
	}
	return RefreshTokenID{value: u}, nil
}

func (r RefreshTokenID) String() string                  { return r.value.String() }
func (r RefreshTokenID) Equals(o RefreshTokenID) bool    { return r.value == o.value }
func (r RefreshTokenID) MarshalJSON() ([]byte, error)    { return json.Marshal(r.value.String()) }
func (r *RefreshTokenID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := uuid.Parse(s)
	if err != nil {
		return ErrInvalidRefreshTokenID
	}
	r.value = parsed
	return nil
}
