// Package dto holds the application-layer DTOs of the sourcing context.
package dto

import (
	"encoding/json"
	"io"
	"time"

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
//
// Status values:
//   - "queued"              — accepted; the pipeline worker will process this file.
//   - "deduplicated"        — deprecated alias kept for backward compatibility; prefer duplicate_in_intent.
//   - "duplicate_in_intent" — the exact same content was already uploaded to THIS intent; skipped.
//   - "extracted_from_zip"  — parent marker for a ZIP file; child entries follow in the Items slice.
//   - ""                    — rejection; see Error for the structured reason.
type ItemOutcome struct {
	Filename       string
	UploadID       *uuid.UUID // populated on queued or deduplicated
	Status         string     // "queued" | "duplicate_in_intent" | "extracted_from_zip"
	CandidateID    *uuid.UUID // populated on deduplicated (slice 1: always nil, slice 2+ sets it)
	Error          *ItemError // populated on rejection
	ParentFilename *string    `json:"parent_filename,omitempty"` // set on child entries extracted from a ZIP
	ParentItemID   *string    `json:"parent_item_id,omitempty"`  // UUID string of the parent ZIP outcome
}

// ItemError carries a structured rejection reason.
type ItemError struct {
	Code    string // "mime_unsupported" | "size_exceeded" | "empty_file" | "storage_write_failed"
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
	BatchID  uuid.UUID            `json:"batch_id"`
	IntentID uuid.UUID            `json:"intent_id"`
	Summary  BatchStatusSummary   `json:"summary"`
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

// CandidateDetailDTO is the result of GetCandidate.
type CandidateDetailDTO struct {
	ID          uuid.UUID         `json:"id"`
	ContentHash string            `json:"content_hash"`
	Personal    CandidatePersonal `json:"personal"`
	Location    string            `json:"location,omitempty"`
	Headline    string            `json:"headline,omitempty"`
	Profile     json.RawMessage   `json:"profile"` // the full parsed profile (PII still in cleartext after server-side decrypt)
	Source      string            `json:"source"`
	CreatedAt   time.Time         `json:"created_at"`
}

// CandidatePersonal is the decrypted PII surface returned only on the
// detail endpoint. List endpoints (slice 4) return a masked variant.
type CandidatePersonal struct {
	FullName string `json:"full_name,omitempty"`
	Email    string `json:"email,omitempty"`
	Phone    string `json:"phone,omitempty"`
}

// SkillSummary is a compact skill projection used in list responses.
type SkillSummary struct {
	Name  string  `json:"name"`
	Years float64 `json:"years,omitempty"`
}

// ApplicationListItemDTO is one row in the GET /intents/{id}/applications response.
// CandidateName is masked (e.g. "A***") — the raw decrypted name is never exposed here.
type ApplicationListItemDTO struct {
	ApplicationID  uuid.UUID       `json:"application_id"`
	CandidateID    uuid.UUID       `json:"candidate_id"`
	CandidateName  string          `json:"candidate_name"` // masked: first char + "***"
	Headline       string          `json:"headline,omitempty"`
	Location       string          `json:"location,omitempty"`
	Status         string          `json:"status"`
	OverallScore   *float64        `json:"overall_score,omitempty"`
	ScoreBand      *string         `json:"score_band,omitempty"`
	EmbeddingScore *float64        `json:"embedding_score,omitempty"`
	RuleMatch      json.RawMessage `json:"rule_match,omitempty"`
	LLMJudgment    json.RawMessage `json:"llm_judgment,omitempty"` // populated only for judged rows
	ScoredAt       *time.Time      `json:"scored_at,omitempty"`
	UpdatedAt      time.Time       `json:"updated_at"`
	TopSkills      []SkillSummary  `json:"top_skills"`    // top 3 skills by years desc from parsed_profile
	JudgeSummary   string          `json:"judge_summary"` // first sentence of llm_judgment.summary
}

// ApplicationListResponse is the full GET /intents/{id}/applications response.
type ApplicationListResponse struct {
	Items  []ApplicationListItemDTO `json:"items"`
	Total  int                      `json:"total"`
	Facets ApplicationListFacets   `json:"facets"`
}

// ApplicationListFacets holds per-score-band counts for Applications that have
// been LLM-judged (i.e. ScoreBand is non-nil). Applications that are Scored
// but not yet judged are not counted in any band.
type ApplicationListFacets struct {
	Strong   int `json:"strong"`
	Moderate int `json:"moderate"`
	Weak     int `json:"weak"`
}
