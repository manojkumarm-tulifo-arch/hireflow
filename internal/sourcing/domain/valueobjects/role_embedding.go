package valueobjects

import "errors"

const roleEmbeddingDim = 1024

// ErrInvalidEmbeddingDim is returned by NewRoleEmbedding when the vector length
// is not exactly roleEmbeddingDim (1024), which is the dimensionality of the
// Voyage AI voyage-3 model used throughout the scoring pipeline.
var ErrInvalidEmbeddingDim = errors.New("role embedding must be exactly 1024 dimensions")

// RoleEmbedding is a validated 1024-dimensional float32 vector representing
// a HiringIntent's RoleSpec in embedding space.
// It wraps the raw []float32 to enforce the dimension invariant at construction time.
type RoleEmbedding struct {
	v []float32
}

// NewRoleEmbedding creates a RoleEmbedding from the given vector.
// Returns ErrInvalidEmbeddingDim if len(v) != 1024.
func NewRoleEmbedding(v []float32) (RoleEmbedding, error) {
	if len(v) != roleEmbeddingDim {
		return RoleEmbedding{}, ErrInvalidEmbeddingDim
	}
	// Copy to prevent external mutation.
	dst := make([]float32, roleEmbeddingDim)
	copy(dst, v)
	return RoleEmbedding{v: dst}, nil
}

// Dim returns the number of dimensions (always 1024 for a valid RoleEmbedding).
func (re RoleEmbedding) Dim() int { return len(re.v) }

// Floats returns the underlying float32 slice.
// The returned slice is a copy; callers may not mutate the embedding.
func (re RoleEmbedding) Floats() []float32 {
	out := make([]float32, len(re.v))
	copy(out, re.v)
	return out
}
