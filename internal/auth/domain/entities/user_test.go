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

func newUser(t *testing.T) *entities.User {
	t.Helper()
	email, err := valueobjects.NewEmail("alice@example.com")
	require.NoError(t, err)
	u, err := entities.NewUser(shared.NewTenantID(), email, "Alice", nil)
	require.NoError(t, err)
	return u
}

func TestNewUser_DefaultsToPendingAndEmitsRegistered(t *testing.T) {
	u := newUser(t)
	assert.Equal(t, valueobjects.StatusPendingVerification, u.Status())
	assert.Equal(t, []string{"recruiter"}, u.Roles())

	evs := u.PullEvents()
	require.Len(t, evs, 1)
	assert.Equal(t, "auth.UserRegistered", evs[0].EventName())
}

func TestMarkVerified_OnlyFromPending(t *testing.T) {
	u := newUser(t)
	_ = u.PullEvents()

	require.NoError(t, u.MarkVerified())
	assert.Equal(t, valueobjects.StatusActive, u.Status())
	assert.NotNil(t, u.VerifiedAt())

	// Re-verifying must fail.
	err := u.MarkVerified()
	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrCannotVerifyNonPending))
}

func TestRecordSignIn_OnlyWhenActive(t *testing.T) {
	u := newUser(t)
	_ = u.PullEvents()

	// Pending → fails
	err := u.RecordSignIn()
	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrCannotSignInWhenNotActive))

	// Activate
	require.NoError(t, u.MarkVerified())
	_ = u.PullEvents()

	require.NoError(t, u.RecordSignIn())
	assert.NotNil(t, u.LastSignedInAt())
	evs := u.PullEvents()
	require.Len(t, evs, 1)
	assert.Equal(t, "auth.UserSignedIn", evs[0].EventName())
}

func TestRecordFailedAttempt_AutoLocksAtThreshold(t *testing.T) {
	u := newUser(t)
	require.NoError(t, u.MarkVerified())
	_ = u.PullEvents()

	for i := 0; i < entities.MaxFailedAttempts-1; i++ {
		u.RecordFailedAttempt()
		assert.Equal(t, valueobjects.StatusActive, u.Status())
	}
	u.RecordFailedAttempt()
	assert.Equal(t, valueobjects.StatusLocked, u.Status())
	assert.NotNil(t, u.LockedUntil())

	err := u.RecordSignIn()
	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrAccountLocked))
}

func TestCanSignInNow_AutoUnlocksAfterCooldown(t *testing.T) {
	email, _ := valueobjects.NewEmail("bob@example.com")
	past := time.Now().Add(-1 * time.Minute)
	u := entities.HydrateUser(
		valueobjects.NewUserID(), shared.NewTenantID(), email, "Bob",
		valueobjects.StatusLocked, []string{"recruiter"},
		entities.MaxFailedAttempts, &past,
		time.Now().Add(-time.Hour), time.Now().Add(-time.Hour), nil, nil,
	)
	assert.True(t, u.CanSignInNow(), "lockout cooldown elapsed → should auto-unlock")
	assert.Equal(t, valueobjects.StatusActive, u.Status())
}
