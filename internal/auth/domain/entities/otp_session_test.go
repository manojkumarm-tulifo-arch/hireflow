package entities_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/auth/domain/entities"
	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// fakeMatch is an OTPHasher.Matches stand-in: matches iff hash == "hash:"+candidate.
func fakeMatch(hash, candidate string) bool { return hash == "hash:"+candidate }

func newSession(t *testing.T) *entities.OTPSession {
	t.Helper()
	email, err := valueobjects.NewEmail("alice@example.com")
	require.NoError(t, err)
	s, err := entities.NewOTPSession(shared.NewTenantID(), email, valueobjects.OTPPurposeSignup, "hash:123456")
	require.NoError(t, err)
	return s
}

func TestOTPSession_VerifyHappyPath(t *testing.T) {
	s := newSession(t)
	code, _ := valueobjects.NewOTPCode("123456")
	require.NoError(t, s.Verify(code, fakeMatch))
	assert.True(t, s.IsVerified())
	assert.Equal(t, entities.MaxOTPAttempts-1, s.AttemptsLeft())
}

func TestOTPSession_VerifyConsumesAttemptOnFailure(t *testing.T) {
	s := newSession(t)
	wrong, _ := valueobjects.NewOTPCode("999999")
	err := s.Verify(wrong, fakeMatch)
	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrOTPCodeMismatch))
	assert.False(t, s.IsVerified())
	assert.Equal(t, entities.MaxOTPAttempts-1, s.AttemptsLeft())
}

func TestOTPSession_LocksAfterMaxAttempts(t *testing.T) {
	s := newSession(t)
	wrong, _ := valueobjects.NewOTPCode("999999")
	for i := 0; i < entities.MaxOTPAttempts; i++ {
		_ = s.Verify(wrong, fakeMatch)
	}
	err := s.Verify(wrong, fakeMatch)
	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrOTPNoAttemptsLeft))
}

func TestOTPSession_RejectsExpired(t *testing.T) {
	email, _ := valueobjects.NewEmail("alice@example.com")
	expired := entities.HydrateOTPSession(
		valueobjects.NewOTPSessionID(), shared.NewTenantID(), email,
		valueobjects.OTPPurposeSignup, "hash:123456",
		entities.MaxOTPAttempts,
		time.Now().Add(-1*time.Minute), nil,
		time.Now().Add(-time.Hour), time.Now().Add(-time.Hour),
	)
	code, _ := valueobjects.NewOTPCode("123456")
	err := expired.Verify(code, fakeMatch)
	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrOTPExpired))
}

func TestOTPSession_RejectsAlreadyVerified(t *testing.T) {
	s := newSession(t)
	code, _ := valueobjects.NewOTPCode("123456")
	require.NoError(t, s.Verify(code, fakeMatch))
	err := s.Verify(code, fakeMatch)
	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrOTPAlreadyVerified))
}
