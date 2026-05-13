package valueobjects

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// ErrInvalidContentHash is returned when a string isn't a valid sha256 hex digest.
var ErrInvalidContentHash = errors.New("invalid content hash")

// ContentHash is the sha256 digest of resume bytes, hex-encoded.
// Used for tenant-scoped idempotency and as the storage key.
type ContentHash struct {
	value string
}

// NewContentHash validates a hex sha256 string and returns a ContentHash.
func NewContentHash(s string) (ContentHash, error) {
	if len(s) != 64 {
		return ContentHash{}, ErrInvalidContentHash
	}
	if _, err := hex.DecodeString(s); err != nil {
		return ContentHash{}, ErrInvalidContentHash
	}
	return ContentHash{value: s}, nil
}

// ComputeContentHash hashes the given bytes with sha256 and returns a ContentHash.
func ComputeContentHash(b []byte) ContentHash {
	sum := sha256.Sum256(b)
	return ContentHash{value: hex.EncodeToString(sum[:])}
}

// String returns the hex digest.
func (h ContentHash) String() string { return h.value }
