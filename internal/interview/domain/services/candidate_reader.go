package services

import (
	"context"
	"errors"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

var ErrCandidateNotFound = errors.New("interview: candidate not found")

// CandidateProfile is the interview-context-local DTO. Excludes encrypted PII
// (name/email/phone) — questions don't need them.
type CandidateProfile struct {
	ID             uuid.UUID
	Headline       string
	Location       string
	Skills         []string
	Experiences    []Experience
	Education      []EducationEntry
	Certifications []string
	SchemaVersion  int
}

type Experience struct {
	Title    string
	Company  string
	Duration string
	Summary  string
}

type EducationEntry struct {
	Degree      string
	Field       string
	Institution string
	Year        string
}

type CandidateReader interface {
	GetProfileForQuestions(ctx context.Context, tenant shared.TenantID, candidateID uuid.UUID) (CandidateProfile, error)
}
