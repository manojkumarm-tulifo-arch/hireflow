// Package dto holds the input/output shapes for interview commands + queries.
package dto

import (
	"time"

	"github.com/google/uuid"

	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

type InterviewProcessDTO struct {
	ID            uuid.UUID
	TenantID      string
	ApplicationID uuid.UUID
	CandidateID   uuid.UUID
	IntentID      uuid.UUID
	Status        string
	Rounds        []InterviewRoundDTO
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type InterviewRoundDTO struct {
	ID              uuid.UUID
	Kind            string
	Sequence        int
	Status          string
	Questions       []vo.Question
	AttemptCount    int
	LastError       string
	FeedbackSummary FeedbackSummaryDTO
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type FeedbackSummaryDTO struct {
	StrongYes      int
	Yes            int
	Mixed          int
	No             int
	StrongNo       int
	Total          int
	LatestDecision string
}

type LoopTemplateDTO struct {
	IntentID  uuid.UUID
	Rounds    []LoopTemplateRoundDTO
	IsDefault bool
}

type LoopTemplateRoundDTO struct {
	Kind     string
	Sequence int
}
