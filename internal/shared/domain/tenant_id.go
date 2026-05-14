// Package domain holds value objects shared across bounded contexts.
// Use sparingly — only truly cross-context concepts belong here.
package domain

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// ErrInvalidTenantID is returned when a TenantID cannot be parsed.
var ErrInvalidTenantID = errors.New("invalid tenant id")

// TenantID identifies a hiring organization. Multi-tenancy boundary.
type TenantID struct {
	value uuid.UUID
}

// NewTenantID generates a fresh TenantID.
func NewTenantID() TenantID {
	return TenantID{value: uuid.New()}
}

// ParseTenantID validates and constructs a TenantID from a string.
func ParseTenantID(s string) (TenantID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return TenantID{}, ErrInvalidTenantID
	}
	return TenantID{value: u}, nil
}

// String returns the canonical string form.
func (t TenantID) String() string { return t.value.String() }

// UUID returns the underlying uuid.UUID value.
func (t TenantID) UUID() uuid.UUID { return t.value }

// Equals compares two TenantIDs.
func (t TenantID) Equals(other TenantID) bool { return t.value == other.value }

// IsZero reports whether the TenantID is the zero value.
func (t TenantID) IsZero() bool { return t.value == uuid.Nil }

// MarshalJSON serializes the id as its canonical UUID string.
func (t TenantID) MarshalJSON() ([]byte, error) { return json.Marshal(t.value.String()) }

// UnmarshalJSON parses a canonical UUID string into the id.
func (t *TenantID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	u, err := uuid.Parse(s)
	if err != nil {
		return ErrInvalidTenantID
	}
	t.value = u
	return nil
}
