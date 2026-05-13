// Package encryption holds adapters implementing the PIIEncryptor port.
// LocalDevDEK is the development adapter: a single 256-bit AES key from env,
// shared across all tenants. Prod uses a KMS-backed adapter (future task).
package encryption

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ErrInvalidKey is returned when the configured DEK isn't 32 bytes (after hex decode).
var ErrInvalidKey = errors.New("local-dev DEK must be 64 hex chars / 32 bytes")

// LocalDevDEK uses a single shared AES-256-GCM key for all tenants.
// Ciphertext format: base64( nonce || aes-gcm-ciphertext ).
type LocalDevDEK struct {
	gcm cipher.AEAD
}

// NewLocalDevDEK validates and parses a 64-hex-char key string and returns
// the adapter. Pass via SOURCING_PII_DEK in prod-of-dev environments.
func NewLocalDevDEK(hexKey string) (*LocalDevDEK, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != 32 {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}
	return &LocalDevDEK{gcm: gcm}, nil
}

// Encrypt produces base64(nonce || ciphertext). Empty plaintext is round-trippable.
func (e *LocalDevDEK) Encrypt(_ context.Context, _ shared.TenantID, plaintext string) (string, error) {
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}
	ct := e.gcm.Seal(nil, nonce, []byte(plaintext), nil)
	out := append(nonce, ct...)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt round-trips the base64 string back to plaintext.
func (e *LocalDevDEK) Decrypt(_ context.Context, _ shared.TenantID, ciphertext string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	ns := e.gcm.NonceSize()
	if len(raw) < ns {
		return "", errors.New("ciphertext shorter than nonce")
	}
	nonce, ct := raw[:ns], raw[ns:]
	pt, err := e.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("gcm open: %w", err)
	}
	return string(pt), nil
}
