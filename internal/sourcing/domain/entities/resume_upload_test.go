package entities_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/events"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
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
	failed, ok2 := evs[0].(events.ResumeUploadFailed)
	require.True(t, ok2, "event must be ResumeUploadFailed")
	assert.Equal(t, u.BatchID(), failed.BatchID)
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
	extracted, ok2 := evs[0].(events.ResumeExtracted)
	require.True(t, ok2, "event must be ResumeExtracted")
	assert.Equal(t, u.BatchID(), extracted.BatchID)

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

func mustParsedJSON(t *testing.T) []byte {
	t.Helper()
	return []byte(`{"schema_version":1,"headline":"Senior Backend Engineer"}`)
}

func TestParsingFlow_HappyPath(t *testing.T) {
	u := newUpload(t)
	_ = u.PullEvents()
	require.NoError(t, u.BeginScanning())
	require.NoError(t, u.BeginExtracting())
	require.NoError(t, u.RecordExtractedText("resume text", 2))
	require.NoError(t, u.CompleteExtracted())
	_ = u.PullEvents() // drain ResumeExtracted

	require.NoError(t, u.BeginParsing())
	assert.Equal(t, vo.StatusParsing, u.Status())

	require.NoError(t, u.RecordParsedProfile(mustParsedJSON(t)))

	candID := uuid.New()
	require.NoError(t, u.LinkCandidate(candID))
	assert.Equal(t, candID, u.CandidateID())

	require.NoError(t, u.CompleteParsed())
	assert.Equal(t, vo.StatusParsed, u.Status())
	assert.True(t, u.Status().IsTerminal())

	evs := u.PullEvents()
	require.Len(t, evs, 1)
	assert.Equal(t, "sourcing.ResumeParsed", evs[0].EventName())
	parsed, ok3 := evs[0].(events.ResumeParsed)
	require.True(t, ok3, "event must be ResumeParsed")
	assert.Equal(t, u.BatchID(), parsed.BatchID)
}

func TestParsingFlow_RecordParsedProfile_OnlyDuringParsing(t *testing.T) {
	u := newUpload(t)
	err := u.RecordParsedProfile(mustParsedJSON(t))
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

func TestParsingFlow_LinkCandidate_OnlyDuringParsing(t *testing.T) {
	u := newUpload(t)
	err := u.LinkCandidate(uuid.New())
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

// ---------------------------------------------------------------------------
// ResetForRetry tests
// ---------------------------------------------------------------------------

func uploadInStatus(t *testing.T, targetStatus vo.UploadStatus) *entities.ResumeUpload {
	t.Helper()
	u := newUpload(t)
	_ = u.PullEvents()
	switch targetStatus {
	case vo.StatusFailed:
		require.NoError(t, u.MarkFailed(vo.Fatal("test_reason", "test detail")))
		_ = u.PullEvents()
	case vo.StatusQuarantined:
		require.NoError(t, u.BeginScanning())
		require.NoError(t, u.Quarantine("EICAR-TEST"))
		_ = u.PullEvents()
	}
	return u
}

func TestResetForRetry_FromFailed_ResetsToPending(t *testing.T) {
	u := uploadInStatus(t, vo.StatusFailed)
	require.Equal(t, vo.StatusFailed, u.Status())

	before := time.Now().UTC()
	err := u.ResetForRetry()
	require.NoError(t, err)

	assert.Equal(t, vo.StatusPending, u.Status())
	assert.Equal(t, 0, u.AttemptCount())
	assert.Equal(t, "", u.LastError())
	assert.False(t, u.NextAttemptAt().Before(before), "nextAttemptAt must be >= before")
	assert.Empty(t, u.PullEvents(), "ResetForRetry must not emit events")
}

func TestResetForRetry_FromQuarantined_ResetsToPending(t *testing.T) {
	u := uploadInStatus(t, vo.StatusQuarantined)
	require.Equal(t, vo.StatusQuarantined, u.Status())

	before := time.Now().UTC()
	err := u.ResetForRetry()
	require.NoError(t, err)

	assert.Equal(t, vo.StatusPending, u.Status())
	assert.Equal(t, 0, u.AttemptCount())
	assert.Equal(t, "", u.LastError())
	assert.False(t, u.NextAttemptAt().Before(before), "nextAttemptAt must be >= before")
}

func TestResetForRetry_FromPending_ReturnsErrInvalidTransition(t *testing.T) {
	u := newUpload(t)
	err := u.ResetForRetry()
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

func TestResetForRetry_FromScored_ReturnsErrInvalidTransition(t *testing.T) {
	// Scored is a terminal state that is not retryable.
	u := entities.RehydrateResumeUpload(entities.RehydrateInput{
		ID:           uuid.New(),
		TenantID:     shared.NewTenantID(),
		IntentID:     uuid.New(),
		BatchID:      uuid.New(),
		StorageKey:   "k",
		OriginalName: "f.pdf",
		Status:       vo.StatusScored,
	})
	err := u.ResetForRetry()
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

func TestParsingFlow_CompleteParsed_RequiresProfileAndCandidate(t *testing.T) {
	u := newUpload(t)
	_ = u.PullEvents()
	require.NoError(t, u.BeginScanning())
	require.NoError(t, u.BeginExtracting())
	require.NoError(t, u.RecordExtractedText("x", 1))
	require.NoError(t, u.CompleteExtracted())
	require.NoError(t, u.BeginParsing())

	// Without profile + candidate: CompleteParsed must reject.
	err := u.CompleteParsed()
	assert.Error(t, err)

	// With profile only — still missing candidate.
	require.NoError(t, u.RecordParsedProfile(mustParsedJSON(t)))
	err = u.CompleteParsed()
	assert.Error(t, err)

	// Now link a candidate; complete succeeds.
	require.NoError(t, u.LinkCandidate(uuid.New()))
	require.NoError(t, u.CompleteParsed())
}
