package encryption_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/infrastructure/encryption"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

func newEncryptor(t *testing.T) *encryption.LocalDevDEK {
	t.Helper()
	// 32-byte hex key (all zeros for determinism; never use in prod).
	key := "0000000000000000000000000000000000000000000000000000000000000000"
	enc, err := encryption.NewLocalDevDEK(key)
	require.NoError(t, err)
	return enc
}

func TestEncrypt_Decrypt_RoundTrip(t *testing.T) {
	enc := newEncryptor(t)
	tenant := shared.NewTenantID()
	plain := "alice@example.com"

	ct, err := enc.Encrypt(context.Background(), tenant, plain)
	require.NoError(t, err)
	assert.NotEqual(t, plain, ct)

	got, err := enc.Decrypt(context.Background(), tenant, ct)
	require.NoError(t, err)
	assert.Equal(t, plain, got)
}

func TestEncrypt_TwoCallsProduceDifferentCiphertexts(t *testing.T) {
	enc := newEncryptor(t)
	tenant := shared.NewTenantID()

	a, err := enc.Encrypt(context.Background(), tenant, "same")
	require.NoError(t, err)
	b, err := enc.Encrypt(context.Background(), tenant, "same")
	require.NoError(t, err)
	assert.NotEqual(t, a, b, "AES-GCM nonces must differ across calls")
}

func TestNewLocalDevDEK_RejectsWrongKeyLength(t *testing.T) {
	_, err := encryption.NewLocalDevDEK("abc")
	assert.Error(t, err)
}

func TestEncrypt_EmptyStringRoundTrips(t *testing.T) {
	enc := newEncryptor(t)
	tenant := shared.NewTenantID()

	ct, err := enc.Encrypt(context.Background(), tenant, "")
	require.NoError(t, err)

	got, err := enc.Decrypt(context.Background(), tenant, ct)
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

func TestDecrypt_RejectsTamperedCiphertext(t *testing.T) {
	enc := newEncryptor(t)
	tenant := shared.NewTenantID()
	ct, err := enc.Encrypt(context.Background(), tenant, "hello")
	require.NoError(t, err)

	// Flip a byte in the middle of the b64 payload.
	tampered := ct[:len(ct)/2] + "X" + ct[len(ct)/2+1:]
	_, err = enc.Decrypt(context.Background(), tenant, tampered)
	assert.Error(t, err)
}
