// Package v1 is the v1 HTTP delivery layer of the interview context.
package v1

import (
	"time"

	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

// UpsertLoopTemplateRequest is the body for PUT /intents/{intent_id}/loop-template.
type UpsertLoopTemplateRequest struct {
	Rounds []TemplateRoundRequest `json:"rounds"`
}

// TemplateRoundRequest is one round in the request body.
type TemplateRoundRequest struct {
	Kind     string `json:"kind"`
	Sequence int    `json:"sequence"`
}

// LoopTemplateResponse is the body for GET /intents/{intent_id}/loop-template.
type LoopTemplateResponse struct {
	IntentID  string                  `json:"intent_id"`
	Rounds    []TemplateRoundResponse `json:"rounds"`
	IsDefault bool                    `json:"is_default"`
}

type TemplateRoundResponse struct {
	Kind     string `json:"kind"`
	Sequence int    `json:"sequence"`
}

// InterviewProcessResponse is the body for GET /interview/processes/{id} and
// list responses.
type InterviewProcessResponse struct {
	ID            string                   `json:"id"`
	ApplicationID string                   `json:"application_id"`
	CandidateID   string                   `json:"candidate_id"`
	IntentID      string                   `json:"intent_id"`
	Status        string                   `json:"status"`
	Rounds        []InterviewRoundResponse `json:"rounds,omitempty"`
	CreatedAt     time.Time                `json:"created_at"`
	UpdatedAt     time.Time                `json:"updated_at"`
}

type InterviewRoundResponse struct {
	ID              string                  `json:"id"`
	Kind            string                  `json:"kind"`
	Sequence        int                     `json:"sequence"`
	Status          string                  `json:"status"`
	Questions       []vo.Question           `json:"questions,omitempty"`
	AttemptCount    int                     `json:"attempt_count"`
	LastError       string                  `json:"last_error,omitempty"`
	FeedbackSummary FeedbackSummaryResponse `json:"feedback_summary"`
	CreatedAt       time.Time               `json:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at"`
}

type FeedbackSummaryResponse struct {
	StrongYes      int    `json:"strong_yes"`
	Yes            int    `json:"yes"`
	Mixed          int    `json:"mixed"`
	No             int    `json:"no"`
	StrongNo       int    `json:"strong_no"`
	Total          int    `json:"total"`
	LatestDecision string `json:"latest_decision,omitempty"`
}

// ListProcessesResponse is the body for GET /intents/{id}/interview-processes.
type ListProcessesResponse struct {
	Processes []InterviewProcessResponse `json:"processes"`
}

// RecordFeedbackRequest is the body for POST /interview/rounds/{id}/feedback.
type RecordFeedbackRequest struct {
	InterviewerName  string `json:"interviewer_name"`
	InterviewerEmail string `json:"interviewer_email,omitempty"`
	Decision         string `json:"decision"`
	Notes            string `json:"notes,omitempty"`
}

// RegenerateRoundRequest is the body for POST /interview/rounds/{id}:regenerate.
// Optional steering text; ignored by the slice-1 worker (see spec for
// future-slice steering threading).
type RegenerateRoundRequest struct {
	Steering string `json:"steering,omitempty"`
}

// ErrorResponse is the standard error body.
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
