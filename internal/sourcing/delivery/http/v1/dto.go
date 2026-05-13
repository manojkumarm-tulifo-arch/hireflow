// Package v1 holds the sourcing context's HTTP wire shapes and handlers.
package v1

import "encoding/json"

// BatchUploadResponse is the response body for POST /intents/{id}/resumes:batch.
type BatchUploadResponse struct {
	BatchID string              `json:"batch_id"`
	Items   []BatchItemResponse `json:"items"`
}

// BatchItemResponse is one per-file outcome row.
type BatchItemResponse struct {
	Filename    string          `json:"filename"`
	UploadID    string          `json:"upload_id,omitempty"`
	Status      string          `json:"status,omitempty"` // "queued" | "deduplicated"
	CandidateID string          `json:"candidate_id,omitempty"`
	Error       *BatchItemError `json:"error,omitempty"`
}

// BatchItemError is the structured rejection payload for a single file.
type BatchItemError struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Detail  map[string]interface{} `json:"detail,omitempty"`
}

// BatchStatusResponse is the response for GET /resumes/batches/{id}.
type BatchStatusResponse struct {
	BatchID  string             `json:"batch_id"`
	IntentID string             `json:"intent_id"`
	Summary  BatchStatusSummary `json:"summary"`
	Items    []BatchStatusItem  `json:"items"`
}

// BatchStatusSummary aggregates status counts.
type BatchStatusSummary struct {
	Total       int `json:"total"`
	InFlight    int `json:"in_flight"`
	Extracted   int `json:"extracted"`
	Failed      int `json:"failed"`
	Quarantined int `json:"quarantined"`
}

// BatchStatusItem is one row.
type BatchStatusItem struct {
	UploadID  string `json:"upload_id"`
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Attempt   int    `json:"attempt"`
	LastError string `json:"last_error,omitempty"`
}

// errorBody is the standard error response shape used by writeError.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// CandidateDetailResponse is the response body for GET /candidates/{candidate_id}.
type CandidateDetailResponse struct {
	ID          string              `json:"id"`
	ContentHash string              `json:"content_hash"`
	Personal    CandidatePersonal   `json:"personal"`
	Location    string              `json:"location,omitempty"`
	Headline    string              `json:"headline,omitempty"`
	Profile     json.RawMessage     `json:"profile"`
	Source      string              `json:"source"`
	CreatedAt   string              `json:"created_at"`
}

// CandidatePersonal is the decrypted PII surface in the candidate detail response.
type CandidatePersonal struct {
	FullName string `json:"full_name,omitempty"`
	Email    string `json:"email,omitempty"`
	Phone    string `json:"phone,omitempty"`
}
