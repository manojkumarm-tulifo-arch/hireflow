package entities_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/hiringintent/domain/entities"
	"github.com/hustle/hireflow/internal/hiringintent/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

func newRoleSpec(t *testing.T, withRequiredSkill bool) valueobjects.RoleSpec {
	t.Helper()
	skills := []valueobjects.Skill{}
	s, err := valueobjects.NewSkill("Go", withRequiredSkill)
	require.NoError(t, err)
	skills = append(skills, s)

	exp, err := valueobjects.NewExperienceRange(3, 7)
	require.NoError(t, err)
	hc, err := valueobjects.NewHeadcount(2)
	require.NoError(t, err)
	role, err := valueobjects.NewRoleSpec("Senior Backend Engineer", skills, exp, hc, []string{"Bangalore"}, valueobjects.WorkModeHybrid)
	require.NoError(t, err)
	return role
}

func newIntent(t *testing.T, withRequiredSkill bool) *entities.HiringIntent {
	t.Helper()
	intent, err := entities.NewHiringIntent(
		shared.NewTenantID(),
		shared.NewRecruiterID(),
		newRoleSpec(t, withRequiredSkill),
		valueobjects.PriorityHigh,
	)
	require.NoError(t, err)
	return intent
}

func TestNewHiringIntent_DraftedAndEmitsEvent(t *testing.T) {
	intent := newIntent(t, true)

	assert.Equal(t, valueobjects.StatusDrafted, intent.Status())
	assert.True(t, intent.IsModifiable())

	evs := intent.PullEvents()
	require.Len(t, evs, 1)
	assert.Equal(t, "hiringintent.IntentDrafted", evs[0].EventName())

	// PullEvents drains.
	assert.Empty(t, intent.PullEvents())
}

func TestConfirm_Invariants(t *testing.T) {
	tests := []struct {
		name              string
		withRequiredSkill bool
		preCancel         bool
		wantErr           error
	}{
		{"happy path confirms", true, false, nil},
		{"missing required skill blocks", false, false, entities.ErrCannotConfirmWithoutSkills},
		{"already cancelled blocks", true, true, entities.ErrCannotConfirmNonDrafted},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			intent := newIntent(t, tc.withRequiredSkill)
			_ = intent.PullEvents()

			if tc.preCancel {
				require.NoError(t, intent.Cancel("test"))
				_ = intent.PullEvents()
			}

			err := intent.Confirm()
			if tc.wantErr != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tc.wantErr), "got %v want %v", err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, valueobjects.StatusConfirmed, intent.Status())
			assert.NotNil(t, intent.ConfirmedAt())

			evs := intent.PullEvents()
			require.Len(t, evs, 1)
			assert.Equal(t, "hiringintent.IntentConfirmed", evs[0].EventName())
		})
	}
}

func TestUpdateRole_BlockedAfterConfirm(t *testing.T) {
	intent := newIntent(t, true)
	require.NoError(t, intent.Confirm())
	_ = intent.PullEvents()

	err := intent.UpdateRole(newRoleSpec(t, true))
	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrCannotModifyConfirmed))
}

func TestCancel_TerminalStatePreservesError(t *testing.T) {
	intent := newIntent(t, true)
	require.NoError(t, intent.Cancel("changed mind"))
	_ = intent.PullEvents()

	err := intent.Cancel("again")
	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrCannotCancelTerminal))
}

func TestRoleSpec_HasRequiredSkill(t *testing.T) {
	tests := []struct {
		name     string
		required bool
		want     bool
	}{
		{"with required skill", true, true},
		{"only nice-to-have", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			role := newRoleSpec(t, tc.required)
			assert.Equal(t, tc.want, role.HasRequiredSkill())
		})
	}
}
