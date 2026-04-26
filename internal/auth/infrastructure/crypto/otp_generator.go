// Package crypto provides cryptographic implementations for the auth context:
// secure OTP generation, Argon2id-based OTP hashing, and refresh-token secrets.
package crypto

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
)

// SecureOTPGenerator produces 6-digit OTPs using crypto/rand. Implements
// services.OTPGenerator.
type SecureOTPGenerator struct{}

// NewSecureOTPGenerator wires the generator.
func NewSecureOTPGenerator() *SecureOTPGenerator { return &SecureOTPGenerator{} }

// Generate returns a uniformly-random 6-digit code.
func (g *SecureOTPGenerator) Generate() (valueobjects.OTPCode, error) {
	const max = int64(1_000_000) // 10^6
	n, err := rand.Int(rand.Reader, big.NewInt(max))
	if err != nil {
		return valueobjects.OTPCode{}, fmt.Errorf("rand: %w", err)
	}
	return valueobjects.NewOTPCode(fmt.Sprintf("%06d", n.Int64()))
}
