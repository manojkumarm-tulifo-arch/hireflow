// Package entities holds the sourcing context's aggregate roots.
package entities

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/sourcing/domain/events"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ErrInvalidTransition is returned when a state transition is not permitted.
var ErrInvalidTransition = errors.New("invalid status transition")

// UploadInput is the constructor input for NewResumeUpload.
type UploadInput struct {
	TenantID     shared.TenantID
	IntentID     uuid.UUID
	BatchID      uuid.UUID
	StorageKey   string
	OriginalName string
	MimeType     vo.MimeType
	SizeBytes    int64
	ContentHash  vo.ContentHash
	// Optional override of now/id for deterministic tests; nil → real values.
	Now func() time.Time
	ID  uuid.UUID
}

// ResumeUpload is the per-file aggregate root of the sourcing pipeline.
type ResumeUpload struct {
	id            uuid.UUID
	tenantID      shared.TenantID
	intentID      uuid.UUID
	batchID       uuid.UUID
	candidateID   uuid.UUID // zero until slice 2
	storageKey    string
	originalName  string
	mimeType      vo.MimeType
	sizeBytes     int64
	contentHash   vo.ContentHash
	status        vo.UploadStatus
	artifacts     vo.StageArtifacts
	attemptCount  int
	lastError     string
	nextAttemptAt time.Time
	createdAt     time.Time
	updatedAt     time.Time
	pendingEvents []events.Event
}

// NewResumeUpload constructs a fresh upload row in status=Pending.
// Emits ResumeUploadAccepted on success.
func NewResumeUpload(in UploadInput) (*ResumeUpload, error) {
	if in.StorageKey == "" || in.OriginalName == "" || in.SizeBytes <= 0 {
		return nil, errors.New("storage_key, original_name, and positive size required")
	}
	now := time.Now().UTC
	if in.Now != nil {
		now = in.Now
	}
	id := in.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	t := now().UTC()
	u := &ResumeUpload{
		id:            id,
		tenantID:      in.TenantID,
		intentID:      in.IntentID,
		batchID:       in.BatchID,
		storageKey:    in.StorageKey,
		originalName:  in.OriginalName,
		mimeType:      in.MimeType,
		sizeBytes:     in.SizeBytes,
		contentHash:   in.ContentHash,
		status:        vo.StatusPending,
		artifacts:     vo.NewStageArtifacts(),
		nextAttemptAt: t,
		createdAt:     t,
		updatedAt:     t,
	}
	u.emit(events.ResumeUploadAccepted{
		UploadID: id, TenantID: in.TenantID, IntentID: in.IntentID,
		BatchID: in.BatchID, ContentHash: in.ContentHash.String(), OccurredAt: t,
	})
	return u, nil
}

// Accessors used by repositories, queries, and tests.
func (u *ResumeUpload) ID() uuid.UUID               { return u.id }
func (u *ResumeUpload) TenantID() shared.TenantID   { return u.tenantID }
func (u *ResumeUpload) IntentID() uuid.UUID          { return u.intentID }
func (u *ResumeUpload) BatchID() uuid.UUID           { return u.batchID }
func (u *ResumeUpload) CandidateID() uuid.UUID       { return u.candidateID }
func (u *ResumeUpload) StorageKey() string           { return u.storageKey }
func (u *ResumeUpload) OriginalName() string         { return u.originalName }
func (u *ResumeUpload) MimeType() vo.MimeType        { return u.mimeType }
func (u *ResumeUpload) SizeBytes() int64             { return u.sizeBytes }
func (u *ResumeUpload) ContentHash() vo.ContentHash  { return u.contentHash }
func (u *ResumeUpload) Status() vo.UploadStatus      { return u.status }
func (u *ResumeUpload) Artifacts() vo.StageArtifacts { return u.artifacts }
func (u *ResumeUpload) AttemptCount() int            { return u.attemptCount }
func (u *ResumeUpload) LastError() string            { return u.lastError }
func (u *ResumeUpload) NextAttemptAt() time.Time     { return u.nextAttemptAt }
func (u *ResumeUpload) CreatedAt() time.Time         { return u.createdAt }
func (u *ResumeUpload) UpdatedAt() time.Time         { return u.updatedAt }

// PullEvents returns and drains the aggregate's pending events. Same pattern
// as HiringIntent.PullEvents.
func (u *ResumeUpload) PullEvents() []events.Event {
	out := u.pendingEvents
	u.pendingEvents = nil
	return out
}

func (u *ResumeUpload) emit(ev events.Event) {
	u.pendingEvents = append(u.pendingEvents, ev)
}

// BeginScanning transitions Pending → Scanning.
func (u *ResumeUpload) BeginScanning() error {
	return u.transition(vo.StatusScanning, "")
}

// BeginExtracting transitions Scanning → Extracting.
func (u *ResumeUpload) BeginExtracting() error {
	return u.transition(vo.StatusExtracting, "")
}

// RecordExtractedText persists the Extracting stage's artifact on the row.
// Idempotent — calling twice overwrites.
func (u *ResumeUpload) RecordExtractedText(text string, pages int) error {
	if u.status != vo.StatusExtracting {
		return ErrInvalidTransition
	}
	u.artifacts.SetExtractedText(text, pages)
	u.touch()
	return nil
}

// CompleteExtracted transitions Extracting → Extracted and emits ResumeExtracted.
func (u *ResumeUpload) CompleteExtracted() error {
	if err := u.transition(vo.StatusExtracted, ""); err != nil {
		return err
	}
	_, pages, _ := u.artifacts.ExtractedText()
	u.emit(events.ResumeExtracted{
		UploadID: u.id, TenantID: u.tenantID, PageCount: pages, OccurredAt: u.updatedAt,
	})
	return nil
}

// Quarantine moves the row to Quarantined (positive virus scan).
func (u *ResumeUpload) Quarantine(signature string) error {
	if !u.status.CanTransitionTo(vo.StatusQuarantined) {
		return ErrInvalidTransition
	}
	u.status = vo.StatusQuarantined
	u.lastError = signature
	u.touch()
	u.emit(events.ResumeUploadFailed{
		UploadID: u.id, TenantID: u.tenantID,
		Reason: "virus_detected", Detail: signature, OccurredAt: u.updatedAt,
	})
	return nil
}

// MarkFailed marks the row as fatally failed with the given decision.
func (u *ResumeUpload) MarkFailed(d vo.RetryDecision) error {
	if !u.status.CanTransitionTo(vo.StatusFailed) {
		return ErrInvalidTransition
	}
	u.status = vo.StatusFailed
	u.lastError = d.Detail
	u.touch()
	u.emit(events.ResumeUploadFailed{
		UploadID: u.id, TenantID: u.tenantID,
		Reason: d.Reason, Detail: d.Detail, OccurredAt: u.updatedAt,
	})
	return nil
}

// ScheduleRetry records a retryable failure: bumps attempt_count, sets
// next_attempt_at via the backoff schedule (capped — beyond schedule length
// the row is marked Failed). Status reverts to Pending so the worker re-claims.
func (u *ResumeUpload) ScheduleRetry(d vo.RetryDecision, now time.Time, schedule []time.Duration) {
	u.attemptCount++
	u.lastError = d.Detail
	if u.attemptCount > len(schedule) {
		_ = u.MarkFailed(vo.Fatal("max_retries_exceeded", d.Detail))
		return
	}
	delay := schedule[u.attemptCount-1]
	if d.BackoffHint > 0 {
		delay = d.BackoffHint
	}
	u.nextAttemptAt = now.Add(delay)
	u.status = vo.StatusPending
	u.touch()
}

func (u *ResumeUpload) transition(next vo.UploadStatus, errDetail string) error {
	if !u.status.CanTransitionTo(next) {
		return ErrInvalidTransition
	}
	u.status = next
	if errDetail != "" {
		u.lastError = errDetail
	}
	u.touch()
	return nil
}

func (u *ResumeUpload) touch() { u.updatedAt = time.Now().UTC() }
