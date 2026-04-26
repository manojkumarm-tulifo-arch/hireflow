package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
)

// RefreshTokenSecretGenerator implements services.RefreshTokenSecretGenerator.
// Format on the wire: opaque 32-byte random secret base64'd; stored hash is
// SHA-256(secret) — fast verification, fine because the secret has 256 bits
// of entropy (offline brute force is intractable).
type RefreshTokenSecretGenerator struct{}

// NewRefreshTokenSecretGenerator wires the generator.
func NewRefreshTokenSecretGenerator() *RefreshTokenSecretGenerator {
	return &RefreshTokenSecretGenerator{}
}

// Generate returns (raw, hash) — give raw to the client, persist hash.
func (g *RefreshTokenSecretGenerator) Generate() (string, string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("rand: %w", err)
	}
	raw := hex.EncodeToString(buf)
	sum := sha256.Sum256(buf)
	hash := hex.EncodeToString(sum[:])
	return raw, hash, nil
}

// Matches compares a candidate raw secret against the stored hash in constant time.
func (g *RefreshTokenSecretGenerator) Matches(hash, candidate string) bool {
	raw, err := hex.DecodeString(candidate)
	if err != nil {
		return false
	}
	sum := sha256.Sum256(raw)
	expected, err := hex.DecodeString(hash)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(sum[:], expected) == 1
}
