package entities_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
)

func validJudgeJobInput() entities.NewJudgeJobInput {
	return entities.NewJudgeJobInput{
		TenantID:      shared.NewTenantID(),
		ApplicationID: uuid.New(),
		IntentID:      uuid.New(),
		CoarseScore:   85.5,
	}
}

func TestNewJudgeJob_ProducesPending(t *testing.T) {
	j := entities.NewJudgeJob(validJudgeJobInput())
	assert.NotEqual(t, uuid.Nil, j.ID())
	assert.Equal(t, entities.JobPending, j.Status())
	assert.Equal(t, 0, j.AttemptCount())
	assert.Nil(t, j.CompletedAt())
}

func TestBeginRunning_FromPending_Succeeds(t *testing.T) {
	j := entities.NewJudgeJob(validJudgeJobInput())
	err := j.BeginRunning()
	require.NoError(t, err)
	assert.Equal(t, entities.JobRunning, j.Status())
}

func TestBeginRunning_RejectsWhenNotPending(t *testing.T) {
	j := entities.NewJudgeJob(validJudgeJobInput())
	_ = j.BeginRunning()
	err := j.BeginRunning()
	require.Error(t, err)
}

func TestComplete_FromRunning_SetsDoneAndCompletedAt(t *testing.T) {
	j := entities.NewJudgeJob(validJudgeJobInput())
	_ = j.BeginRunning()
	before := time.Now()
	j.Complete()
	assert.Equal(t, entities.JobDone, j.Status())
	require.NotNil(t, j.CompletedAt())
	assert.True(t, !j.CompletedAt().Before(before))
}

func TestFail_WithinSchedule_GoesBackToPending(t *testing.T) {
	j := entities.NewJudgeJob(validJudgeJobInput())
	_ = j.BeginRunning()
	now := time.Now()
	schedule := []time.Duration{10 * time.Second, 30 * time.Second, 60 * time.Second}
	j.Fail("timeout", now, schedule)
	assert.Equal(t, entities.JobPending, j.Status())
	assert.Equal(t, 1, j.AttemptCount())
	assert.Equal(t, "timeout", j.LastError())
	assert.Equal(t, now.Add(10*time.Second), j.NextAttemptAt())
}

func TestFail_ExceedsSchedule_TransitionsToFailed(t *testing.T) {
	j := entities.NewJudgeJob(validJudgeJobInput())
	now := time.Now()
	schedule := []time.Duration{5 * time.Second}
	// First fail — within schedule, goes back to Pending.
	_ = j.BeginRunning()
	j.Fail("err1", now, schedule)
	assert.Equal(t, entities.JobPending, j.Status())

	// Second fail — exceeds schedule cap, becomes Failed.
	_ = j.BeginRunning()
	j.Fail("err2", now, schedule)
	assert.Equal(t, entities.JobFailed, j.Status())
	assert.Equal(t, 2, j.AttemptCount())
}

func TestRehydrateJudgeJob_RehydratesState(t *testing.T) {
	orig := entities.NewJudgeJob(validJudgeJobInput())
	completedAt := time.Now().UTC()
	rh := entities.RehydrateJudgeJob(entities.RehydrateJudgeJobInput{
		ID:            orig.ID(),
		TenantID:      orig.TenantID(),
		ApplicationID: orig.ApplicationID(),
		IntentID:      orig.IntentID(),
		CoarseScore:   orig.CoarseScore(),
		Status:        entities.JobDone,
		AttemptCount:  3,
		LastError:     "some error",
		NextAttemptAt: orig.NextAttemptAt(),
		EnqueuedAt:    orig.EnqueuedAt(),
		CompletedAt:   &completedAt,
	})
	assert.Equal(t, orig.ID(), rh.ID())
	assert.Equal(t, entities.JobDone, rh.Status())
	assert.Equal(t, 3, rh.AttemptCount())
	require.NotNil(t, rh.CompletedAt())
	assert.Equal(t, completedAt, *rh.CompletedAt())
}
