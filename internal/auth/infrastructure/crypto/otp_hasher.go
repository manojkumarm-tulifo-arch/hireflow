package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
)

// Argon2id parameters tuned for short-lived OTP codes — small memory and time
// cost (these aren't passwords; we want signups to be snappy).
// Per OWASP guidance for low-entropy secrets, these values are conservative.
const (
	argonTime    uint32 = 2
	argonMemory  uint32 = 32 * 1024 // 32 MiB
	argonThreads uint8  = 1
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

// Argon2OTPHasher implements services.OTPHasher.
type Argon2OTPHasher struct{}

// NewArgon2OTPHasher wires the hasher.
func NewArgon2OTPHasher() *Argon2OTPHasher { return &Argon2OTPHasher{} }

// Hash returns an encoded form: $argon2id$v=19$m=...,t=...,p=...$<salt>$<key>.
// Standard PHC string — easy to migrate parameters later.
func (h *Argon2OTPHasher) Hash(code valueobjects.OTPCode) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("salt: %w", err)
	}
	key := argon2.IDKey([]byte(code.String()), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
	return encoded, nil
}

// Matches verifies a candidate against a previously-hashed code in constant time.
// Any decode error is treated as a non-match — never panic on bad stored data.
func (h *Argon2OTPHasher) Matches(hash, candidate string) bool {
	salt, key, m, t, p, err := decodeArgon2(hash)
	if err != nil {
		return false
	}
	other := argon2.IDKey([]byte(candidate), salt, t, m, p, uint32(len(key)))
	return subtle.ConstantTimeCompare(key, other) == 1
}

func decodeArgon2(encoded string) ([]byte, []byte, uint32, uint32, uint8, error) {
	parts := strings.Split(encoded, "$")
	// expect: ["", "argon2id", "v=19", "m=...,t=...,p=...", "<salt>", "<key>"]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return nil, nil, 0, 0, 0, errors.New("not argon2id")
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return nil, nil, 0, 0, 0, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return nil, nil, 0, 0, 0, err
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return nil, nil, 0, 0, 0, err
	}
	return salt, key, m, t, p, nil
}
