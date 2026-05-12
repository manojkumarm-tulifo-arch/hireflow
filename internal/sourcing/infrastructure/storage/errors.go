// Package storage holds adapters implementing the ResumeStorage port.
package storage

import "errors"

// ErrNotFound is returned by Open when the key doesn't exist.
var ErrNotFound = errors.New("storage: key not found")

// ErrUnsafeKey is returned when a key would escape the storage root.
var ErrUnsafeKey = errors.New("storage: unsafe key")
