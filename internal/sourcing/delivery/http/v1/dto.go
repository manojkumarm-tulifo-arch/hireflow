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
	Filename       string          `json:"filename"`
	UploadID       string          `json:"upload_id,omitempty"`
	Status         string          `json:"status,omitempty"` // queued | deduplicated | duplicate_in_intent | extracted_from_zip | "" (rejected)
	CandidateID    string          `json:"candidate_id,omitempty"`
	ParentFilename string          `json:"parent_filename,omitempty"`
	ParentItemID   string          `json:"parent_item_id,omitempty"`
	Error          *BatchItemError `json:"error,omitempty"`
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
	ID          string            `json:"id"`
	ContentHash string            `json:"content_hash"`
	Personal    CandidatePersonal `json:"personal"`
	Location    string            `json:"location,omitempty"`
	Headline    string            `json:"headline,omitempty"`
	Profile     json.RawMessage   `json:"profile"`
	Source      string            `json:"source"`
	CreatedAt   string            `json:"created_at"`
}

// CandidatePersonal is the decrypted PII surface in the candidate detail response.
type CandidatePersonal struct {
	FullName string `json:"full_name,omitempty"`
	Email    string `json:"email,omitempty"`
	Phone    string `json:"phone,omitempty"`
}

// ApplicationListResponse is the response body for GET /intents/{id}/applications.
type ApplicationListResponse struct {
	Items  []ApplicationListItem `json:"items"`
	Total  int                   `json:"total"`
	Facets ApplicationListFacets `json:"facets"`
}

// ApplicationListItem is one row in the ranked Applications list.
type ApplicationListItem struct {
	ApplicationID string               `json:"application_id"`
	Candidate     ApplicationCandidate `json:"candidate"`
	Score         ApplicationScore     `json:"score"`
	Status        string               `json:"status"`
	ScoredAt      string               `json:"scored_at,omitempty"`
}

// ApplicationCandidateSkill is a compact skill entry in the list response.
type ApplicationCandidateSkill struct {
	Name  string  `json:"name"`
	Years float64 `json:"years,omitempty"`
}

// ApplicationCandidate is the masked candidate projection in the list response.
type ApplicationCandidate struct {
	ID             string                      `json:"id"`
	FullNameMasked string                      `json:"full_name_masked,omitempty"`
	Headline       string                      `json:"headline,omitempty"`
	Location       string                      `json:"location,omitempty"`
	TopSkills      []ApplicationCandidateSkill `json:"top_skills"`    // top 3 skills by years desc
	JudgeSummary   string                      `json:"judge_summary"` // first sentence of llm_judgment.summary
}

// ApplicationScore holds the scoring detail for one application row.
type ApplicationScore struct {
	Overall        *float64        `json:"overall,omitempty"`
	Band           *string         `json:"band,omitempty"`
	EmbeddingScore *float64        `json:"embedding_score,omitempty"`
	RuleMatch      json.RawMessage `json:"rule_match"`
	LLM            json.RawMessage `json:"llm,omitempty"`
}

// ApplicationListFacets holds per-score-band counts.
type ApplicationListFacets struct {
	Strong   int `json:"strong"`
	Moderate int `json:"moderate"`
	Weak     int `json:"weak"`
}

// ApplicationRejectRequest is the request body for POST /applications/{id}:reject.
type ApplicationRejectRequest struct {
	Reason string `json:"reason"`
}
