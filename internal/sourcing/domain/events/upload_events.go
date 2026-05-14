// Package events defines the domain events emitted by the sourcing context.
// Each event implements the shared.Event-style interface used by the outbox.
package events

import (
	"time"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// Event is the minimum interface every sourcing event satisfies, matching
// the shape consumed by the outbox dispatcher.
type Event interface {
	EventName() string
	AggregateID() uuid.UUID
	Tenant() shared.TenantID
	At() time.Time
}

// ResumeUploadAccepted is emitted after a file is byte-written and a
// resume_uploads row is persisted in status=Pending.
type ResumeUploadAccepted struct {
	UploadID    uuid.UUID       `json:"upload_id"`
	TenantID    shared.TenantID `json:"tenant_id"`
	IntentID    uuid.UUID       `json:"intent_id"`
	BatchID     uuid.UUID       `json:"batch_id"`
	ContentHash string          `json:"content_hash"`
	OccurredAt  time.Time       `json:"occurred_at"`
}

func (e ResumeUploadAccepted) EventName() string       { return "sourcing.ResumeUploadAccepted" }
func (e ResumeUploadAccepted) AggregateID() uuid.UUID  { return e.UploadID }
func (e ResumeUploadAccepted) Tenant() shared.TenantID { return e.TenantID }
func (e ResumeUploadAccepted) At() time.Time           { return e.OccurredAt }

// ResumeUploadFailed is emitted on any fatal pipeline failure.
type ResumeUploadFailed struct {
	UploadID   uuid.UUID       `json:"upload_id"`
	TenantID   shared.TenantID `json:"tenant_id"`
	BatchID    uuid.UUID       `json:"batch_id"`
	Reason     string          `json:"reason"`
	Detail     string          `json:"detail"`
	OccurredAt time.Time       `json:"occurred_at"`
}

func (e ResumeUploadFailed) EventName() string       { return "sourcing.ResumeUploadFailed" }
func (e ResumeUploadFailed) AggregateID() uuid.UUID  { return e.UploadID }
func (e ResumeUploadFailed) Tenant() shared.TenantID { return e.TenantID }
func (e ResumeUploadFailed) At() time.Time           { return e.OccurredAt }

// ResumeExtracted is emitted when text extraction succeeds (slice 1 terminal state).
// Slice 2's parser will consume this to advance the pipeline.
type ResumeExtracted struct {
	UploadID   uuid.UUID       `json:"upload_id"`
	TenantID   shared.TenantID `json:"tenant_id"`
	BatchID    uuid.UUID       `json:"batch_id"`
	PageCount  int             `json:"page_count"`
	OccurredAt time.Time       `json:"occurred_at"`
}

func (e ResumeExtracted) EventName() string       { return "sourcing.ResumeExtracted" }
func (e ResumeExtracted) AggregateID() uuid.UUID  { return e.UploadID }
func (e ResumeExtracted) Tenant() shared.TenantID { return e.TenantID }
func (e ResumeExtracted) At() time.Time           { return e.OccurredAt }

// ResumeParsed is emitted when parsing succeeds and a candidate has been linked.
// This is the slice-2 terminal event for the ResumeUpload aggregate; slice 3's
// scoring consumer subscribes to it.
type ResumeParsed struct {
	UploadID    uuid.UUID       `json:"upload_id"`
	TenantID    shared.TenantID `json:"tenant_id"`
	BatchID     uuid.UUID       `json:"batch_id"`
	CandidateID uuid.UUID       `json:"candidate_id"`
	OccurredAt  time.Time       `json:"occurred_at"`
}

func (e ResumeParsed) EventName() string       { return "sourcing.ResumeParsed" }
func (e ResumeParsed) AggregateID() uuid.UUID  { return e.UploadID }
func (e ResumeParsed) Tenant() shared.TenantID { return e.TenantID }
func (e ResumeParsed) At() time.Time           { return e.OccurredAt }
