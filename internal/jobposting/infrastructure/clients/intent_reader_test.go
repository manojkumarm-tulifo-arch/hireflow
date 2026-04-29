package clients

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	intentdto "github.com/hustle/hireflow/internal/hiringintent/application/dto"
	intentqueries "github.com/hustle/hireflow/internal/hiringintent/application/queries"
)

type fakeQuery struct {
	out intentdto.IntentDTO
	err error
}

func (f fakeQuery) Handle(_ context.Context, _ intentqueries.GetIntentInput) (intentdto.IntentDTO, error) {
	return f.out, f.err
}

func newReader(q intentQuery) *IntentReader { return &IntentReader{query: q} }

func confirmedDTO() intentdto.IntentDTO {
	return intentdto.IntentDTO{
		ID:       "i-1",
		TenantID: "t-1",
		Status:   "CONFIRMED",
		Role: intentdto.RoleSpecDTO{
			Title:     "Senior Backend Engineer",
			Headcount: 2,
			Experience: intentdto.ExperienceRangeDTO{
				MinYears: 3,
				MaxYears: 7,
			},
			Skills: []intentdto.SkillDTO{
				{Name: "Go", Required: true},
				{Name: "Postgres", Required: true},
				{Name: "GraphQL", Required: false},
			},
		},
	}
}

func TestReadConfirmed_ProjectsSnapshot(t *testing.T) {
	r := newReader(fakeQuery{out: confirmedDTO()})

	snap, err := r.ReadConfirmed(context.Background(), "t-1", "i-1")
	require.NoError(t, err)

	assert.Equal(t, "i-1", snap.IntentID)
	assert.Equal(t, "t-1", snap.TenantID)
	assert.Equal(t, "Senior Backend Engineer", snap.RoleTitle)
	assert.Equal(t, 2, snap.Headcount)
	assert.Equal(t, 3, snap.MinYears)
	assert.Equal(t, 7, snap.MaxYears)
	assert.Equal(t, []string{"Go", "Postgres"}, snap.RequiredSkills, "non-required skills must be filtered out")
}

func TestReadConfirmed_RejectsNonConfirmed(t *testing.T) {
	dto := confirmedDTO()
	dto.Status = "DRAFTED"
	r := newReader(fakeQuery{out: dto})

	_, err := r.ReadConfirmed(context.Background(), "t-1", "i-1")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrIntentNotConfirmed)
}

func TestReadConfirmed_PropagatesQueryError(t *testing.T) {
	boom := errors.New("db down")
	r := newReader(fakeQuery{err: boom})

	_, err := r.ReadConfirmed(context.Background(), "t-1", "i-1")
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
}
