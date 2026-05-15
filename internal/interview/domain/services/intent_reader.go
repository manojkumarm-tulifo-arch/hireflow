// Package services holds the port interfaces consumed by interview-context
// commands (cross-context readers, question generator).
package services

import (
	"context"
	"errors"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ErrIntentNotFound is returned by IntentReader when the intent doesn't exist
// for the tenant.
var ErrIntentNotFound = errors.New("interview: intent not found")

// RoleSpec is the interview-context-local DTO for the role definition that
// drives question generation. Field set is intentionally narrow — only what
// the generator needs.
type RoleSpec struct {
	Title     string
	Skills    []SkillRequirement
	YearsMin  int
	YearsMax  int
	Seniority string
	Reports   string
	Team      string
}

type SkillRequirement struct {
	Name     string
	Required bool
}

type IntentReader interface {
	GetRoleSpec(ctx context.Context, tenant shared.TenantID, intentID uuid.UUID) (RoleSpec, error)
}
