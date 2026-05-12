package entities_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

func mustHash(t *testing.T) vo.ContentHash {
	h, err := vo.NewContentHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.NoError(t, err)
	return h
}

func mustMime(t *testing.T) vo.MimeType {
	m, err := vo.ParseMimeType("application/pdf")
	require.NoError(t, err)
	return m
}

func newUpload(t *testing.T) *entities.ResumeUpload {
	t.Helper()
	u, err := entities.NewResumeUpload(entities.UploadInput{
		TenantID:     shared.NewTenantID(),
		IntentID:     uuid.New(),
		BatchID:      uuid.New(),
		StorageKey:   "abc/file",
		OriginalName: "alice.pdf",
		MimeType:     mustMime(t),
		SizeBytes:    1234,
		ContentHash:  mustHash(t),
	})
	require.NoError(t, err)
	return u
}

func TestNewResumeUpload_StartsPending_EmitsAccepted(t *testing.T) {
	u := newUpload(t)

	assert.Equal(t, vo.StatusPending, u.Status())
	assert.Equal(t, 0, u.AttemptCount())
	assert.False(t, u.NextAttemptAt().IsZero())

	evs := u.PullEvents()
	require.Len(t, evs, 1)
	assert.Equal(t, "sourcing.ResumeUploadAccepted", evs[0].EventName())
	assert.Empty(t, u.PullEvents(), "PullEvents must drain")
}

func TestBeginScanning_Transitions(t *testing.T) {
	u := newUpload(t)
	_ = u.PullEvents()

	require.NoError(t, u.BeginScanning())
	assert.Equal(t, vo.StatusScanning, u.Status())
}

func TestBeginScanning_FromTerminalRejected(t *testing.T) {
	u := newUpload(t)
	require.NoError(t, u.MarkFailed(vo.Fatal("size_exceeded", "x")))
	err := u.BeginScanning()
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

func TestQuarantine_FromScanning(t *testing.T) {
	u := newUpload(t)
	_ = u.PullEvents()
	require.NoError(t, u.BeginScanning())

	require.NoError(t, u.Quarantine("EICAR-TEST"))
	assert.Equal(t, vo.StatusQuarantined, u.Status())
	assert.Equal(t, "EICAR-TEST", u.LastError())

	evs := u.PullEvents()
	require.Len(t, evs, 1)
	assert.Equal(t, "sourcing.ResumeUploadFailed", evs[0].EventName())
}

func TestExtractingFlow_PersistsArtifactAndCompletes(t *testing.T) {
	u := newUpload(t)
	_ = u.PullEvents()
	require.NoError(t, u.BeginScanning())
	require.NoError(t, u.BeginExtracting())
	assert.Equal(t, vo.StatusExtracting, u.Status())

	require.NoError(t, u.RecordExtractedText("hello", 1))
	require.NoError(t, u.CompleteExtracted())
	assert.Equal(t, vo.StatusExtracted, u.Status())

	evs := u.PullEvents()
	require.Len(t, evs, 1)
	assert.Equal(t, "sourcing.ResumeExtracted", evs[0].EventName())

	text, pages, ok := u.Artifacts().ExtractedText()
	require.True(t, ok)
	assert.Equal(t, "hello", text)
	assert.Equal(t, 1, pages)
}

func TestScheduleRetry_IncrementsAttemptAndBacksOff(t *testing.T) {
	u := newUpload(t)
	_ = u.PullEvents()
	require.NoError(t, u.BeginScanning())

	now := time.Now().UTC()
	u.ScheduleRetry(vo.Retryable("transient", "boom"), now, []time.Duration{30 * time.Second})

	assert.Equal(t, 1, u.AttemptCount())
	assert.Equal(t, "boom", u.LastError())
	assert.True(t, u.NextAttemptAt().After(now), "next_attempt_at must advance")
	// Status reverts to Pending so the worker picks it up again.
	assert.Equal(t, vo.StatusPending, u.Status())
}

func TestScheduleRetry_FailsAfterMaxAttempts(t *testing.T) {
	u := newUpload(t)
	_ = u.PullEvents()
	require.NoError(t, u.BeginScanning())

	now := time.Now().UTC()
	backoff := []time.Duration{1 * time.Second, 2 * time.Second}

	u.ScheduleRetry(vo.Retryable("t", "x"), now, backoff)
	u.ScheduleRetry(vo.Retryable("t", "x"), now, backoff)
	u.ScheduleRetry(vo.Retryable("t", "x"), now, backoff) // exceeds cap

	assert.Equal(t, vo.StatusFailed, u.Status())

	evs := u.PullEvents()
	require.NotEmpty(t, evs)
	assert.Equal(t, "sourcing.ResumeUploadFailed", evs[len(evs)-1].EventName())
}

func TestMarkFailed_RecordsReasonAndEmitsEvent(t *testing.T) {
	u := newUpload(t)
	_ = u.PullEvents()

	require.NoError(t, u.MarkFailed(vo.Fatal("size_exceeded", "12MB")))
	assert.Equal(t, vo.StatusFailed, u.Status())
	assert.Equal(t, "12MB", u.LastError())

	evs := u.PullEvents()
	require.Len(t, evs, 1)
	assert.Equal(t, "sourcing.ResumeUploadFailed", evs[0].EventName())
}
