package entities

import (
	"errors"
	"time"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// JudgeJobStatus is the lifecycle state of a JudgeJob queue row.
// It is intentionally defined here rather than in valueobjects because it is
// an internal scheduler artifact with no cross-package consumers in slice 3.
type JudgeJobStatus string

const (
	JobPending JudgeJobStatus = "Pending"
	JobRunning JudgeJobStatus = "Running"
	JobDone    JudgeJobStatus = "Done"
	JobFailed  JudgeJobStatus = "Failed"
)

// NewJudgeJobInput is the constructor input for NewJudgeJob.
type NewJudgeJobInput struct {
	TenantID      shared.TenantID
	ApplicationID uuid.UUID
	IntentID      uuid.UUID
	CoarseScore   float64
	// Optional overrides for deterministic tests; nil → real values.
	Now func() time.Time
	ID  uuid.UUID
}

// JudgeJob is the tiny queue row that drives the LLM judge worker.
// It does not emit domain events — it is a purely internal scheduling artifact.
type JudgeJob struct {
	id            uuid.UUID
	tenantID      shared.TenantID
	applicationID uuid.UUID
	intentID      uuid.UUID
	coarseScore   float64
	status        JudgeJobStatus
	attemptCount  int
	lastError     string
	nextAttemptAt time.Time
	enqueuedAt    time.Time
	completedAt   *time.Time
}

// NewJudgeJob constructs a fresh JudgeJob in status Pending.
func NewJudgeJob(in NewJudgeJobInput) *JudgeJob {
	now := time.Now().UTC
	if in.Now != nil {
		now = in.Now
	}
	id := in.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	t := now().UTC()
	return &JudgeJob{
		id:            id,
		tenantID:      in.TenantID,
		applicationID: in.ApplicationID,
		intentID:      in.IntentID,
		coarseScore:   in.CoarseScore,
		status:        JobPending,
		nextAttemptAt: t,
		enqueuedAt:    t,
	}
}

// Accessors.
func (j *JudgeJob) ID() uuid.UUID              { return j.id }
func (j *JudgeJob) TenantID() shared.TenantID  { return j.tenantID }
func (j *JudgeJob) ApplicationID() uuid.UUID   { return j.applicationID }
func (j *JudgeJob) IntentID() uuid.UUID        { return j.intentID }
func (j *JudgeJob) CoarseScore() float64       { return j.coarseScore }
func (j *JudgeJob) Status() JudgeJobStatus     { return j.status }
func (j *JudgeJob) AttemptCount() int          { return j.attemptCount }
func (j *JudgeJob) LastError() string          { return j.lastError }
func (j *JudgeJob) NextAttemptAt() time.Time   { return j.nextAttemptAt }
func (j *JudgeJob) EnqueuedAt() time.Time      { return j.enqueuedAt }
func (j *JudgeJob) CompletedAt() *time.Time    { return j.completedAt }

// BeginRunning transitions Pending → Running.
func (j *JudgeJob) BeginRunning() error {
	if j.status != JobPending {
		return errors.New("judge_job: BeginRunning requires Pending status")
	}
	j.status = JobRunning
	return nil
}

// Complete transitions Running → Done and records completedAt.
func (j *JudgeJob) Complete() {
	t := time.Now().UTC()
	j.status = JobDone
	j.completedAt = &t
}

// Fail increments attempt_count and either schedules a retry (going back to
// Pending with an advanced next_attempt_at) or sets status=Failed when the
// schedule is exhausted.
func (j *JudgeJob) Fail(reason string, now time.Time, schedule []time.Duration) {
	j.attemptCount++
	j.lastError = reason
	if j.attemptCount <= len(schedule) {
		j.nextAttemptAt = now.Add(schedule[j.attemptCount-1])
		j.status = JobPending
		return
	}
	j.status = JobFailed
}

// RehydrateJudgeJobInput is for repository reads — bypasses any initialisation logic.
type RehydrateJudgeJobInput struct {
	ID            uuid.UUID
	TenantID      shared.TenantID
	ApplicationID uuid.UUID
	IntentID      uuid.UUID
	CoarseScore   float64
	Status        JudgeJobStatus
	AttemptCount  int
	LastError     string
	NextAttemptAt time.Time
	EnqueuedAt    time.Time
	CompletedAt   *time.Time
}

// RehydrateJudgeJob reconstructs a JudgeJob from a persisted row.
// Repositories use this; application code must not.
func RehydrateJudgeJob(in RehydrateJudgeJobInput) *JudgeJob {
	return &JudgeJob{
		id:            in.ID,
		tenantID:      in.TenantID,
		applicationID: in.ApplicationID,
		intentID:      in.IntentID,
		coarseScore:   in.CoarseScore,
		status:        in.Status,
		attemptCount:  in.AttemptCount,
		lastError:     in.LastError,
		nextAttemptAt: in.NextAttemptAt,
		enqueuedAt:    in.EnqueuedAt,
		completedAt:   in.CompletedAt,
	}
}
