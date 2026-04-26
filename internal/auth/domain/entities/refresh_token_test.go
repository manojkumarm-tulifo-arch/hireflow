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

func TestRefreshToken_NewIsUsable(t *testing.T) {
	rt, err := entities.NewRefreshToken(valueobjects.NewUserID(), shared.NewTenantID(), "abc")
	require.NoError(t, err)
	require.NoError(t, rt.CheckUsable())
}

func TestRefreshToken_RevokeIsIdempotent(t *testing.T) {
	rt, _ := entities.NewRefreshToken(valueobjects.NewUserID(), shared.NewTenantID(), "abc")
	rt.Revoke()
	first := *rt.RevokedAt()
	rt.Revoke()
	assert.Equal(t, first, *rt.RevokedAt())

	err := rt.CheckUsable()
	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrRefreshTokenRevoked))
}

func TestRefreshToken_ExpiredRejectsUsable(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	rt := entities.HydrateRefreshToken(
		valueobjects.NewRefreshTokenID(),
		valueobjects.NewUserID(), shared.NewTenantID(),
		"abc", past, time.Now().Add(-2*time.Hour), nil,
	)
	err := rt.CheckUsable()
	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrRefreshTokenExpired))
}
