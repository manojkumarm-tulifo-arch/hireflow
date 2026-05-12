// Package dto holds the application-layer DTOs of the sourcing context.
package dto

import (
	"io"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// BatchItemSource yields one resume part to the command. The HTTP delivery
// adapts multipart.Reader into this; tests use an in-memory implementation.
type BatchItemSource interface {
	// Next returns the next item or io.EOF when done.
	Next() (BatchItem, error)
}

// BatchItem is one uploaded file's input.
type BatchItem struct {
	Filename string
	Body     io.Reader // single read; the command copies to storage as it reads
}

// BatchUploadInput is the command's input.
type BatchUploadInput struct {
	TenantID shared.TenantID
	IntentID uuid.UUID
	Source   BatchItemSource
}

// ItemOutcome is the per-file result of a batch upload.
type ItemOutcome struct {
	Filename    string
	UploadID    *uuid.UUID // populated on queued or deduplicated
	Status      string     // "queued" | "deduplicated"
	CandidateID *uuid.UUID // populated on deduplicated (slice 1: always nil, slice 2+ sets it)
	Error       *ItemError // populated on rejection
}

// ItemError carries a structured rejection reason.
type ItemError struct {
	Code    string         // "mime_unsupported" | "size_exceeded" | "empty_file" | "storage_write_failed"
	Message string
	Detail  map[string]any // optional structured detail
}

// BatchUploadOutput is the command's result.
type BatchUploadOutput struct {
	BatchID uuid.UUID
	Items   []ItemOutcome
}

// BatchStatusDTO is the result of GetBatchStatus.
type BatchStatusDTO struct {
	BatchID  uuid.UUID          `json:"batch_id"`
	IntentID uuid.UUID          `json:"intent_id"`
	Summary  BatchStatusSummary `json:"summary"`
	Items    []BatchStatusItemDTO `json:"items"`
}

// BatchStatusSummary aggregates status counts.
type BatchStatusSummary struct {
	Total       int `json:"total"`
	InFlight    int `json:"in_flight"`
	Extracted   int `json:"extracted"`
	Failed      int `json:"failed"`
	Quarantined int `json:"quarantined"`
}

// BatchStatusItemDTO is one row in the status response.
type BatchStatusItemDTO struct {
	UploadID  uuid.UUID `json:"upload_id"`
	Filename  string    `json:"filename"`
	Status    string    `json:"status"`
	Attempt   int       `json:"attempt"`
	LastError string    `json:"last_error,omitempty"`
}
