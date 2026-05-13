package services

import (
	"context"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// PIIEncryptor is the port for envelope encryption of personal fields.
// The tenant parameter scopes the key (per-tenant DEK in the future
// KMS-backed adapter; ignored by the dev adapter that uses a single key).
//
// Encrypt returns an opaque base64 string the aggregate stores as-is.
// Decrypt round-trips it back.
type PIIEncryptor interface {
	Encrypt(ctx context.Context, tenant shared.TenantID, plaintext string) (string, error)
	Decrypt(ctx context.Context, tenant shared.TenantID, ciphertext string) (string, error)
}
