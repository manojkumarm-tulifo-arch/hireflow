package services

import (
	"context"

	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

type GenerationInput struct {
	RoundKind        vo.RoundKind
	RoleSpec         RoleSpec
	CandidateProfile CandidateProfile
	Steering         string // optional recruiter steering for regenerations
}

type QuestionGenerator interface {
	Generate(ctx context.Context, in GenerationInput) ([]vo.Question, error)
}
