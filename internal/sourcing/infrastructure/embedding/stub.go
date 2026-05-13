package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"math/rand"
)

const stubDim = 1024

// Stub is a deterministic Embedder for use in tests.
// It seeds math/rand from sha256(text) so that:
//   - The same text always produces the same 1024-dim vector.
//   - Different texts produce (with overwhelming probability) different vectors.
//   - The returned vector is L2-normalised, matching Voyage AI's guarantee.
type Stub struct{}

// NewStub returns a ready-to-use Stub embedder.
func NewStub() *Stub {
	return &Stub{}
}

// EmbedDocument returns a deterministic, L2-normalised 1024-dim float32 vector
// derived from the SHA-256 hash of text. The context is unused.
func (Stub) EmbedDocument(_ context.Context, text string) ([]float32, error) {
	// Seed a local rand source from the first 8 bytes of sha256(text).
	// Using a local source ensures concurrent calls don't interfere.
	h := sha256.Sum256([]byte(text))
	seed := int64(binary.LittleEndian.Uint64(h[:8]))
	//nolint:gosec // deterministic seeding for test use only
	rng := rand.New(rand.NewSource(seed))

	vec := make([]float32, stubDim)
	var sumSq float64
	for i := range vec {
		v := rng.NormFloat64()
		vec[i] = float32(v)
		sumSq += v * v
	}

	// L2-normalise.
	norm := math.Sqrt(sumSq)
	if norm > 0 {
		for i := range vec {
			vec[i] = float32(float64(vec[i]) / norm)
		}
	}

	return vec, nil
}
