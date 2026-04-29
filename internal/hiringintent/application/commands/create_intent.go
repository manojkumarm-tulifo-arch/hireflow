// Package commands holds command handlers for the hiringintent context.
// Each command represents a state-changing use case; queries live next door.
package commands

import (
	"context"
	"fmt"

	"github.com/hustle/hireflow/internal/hiringintent/application/dto"
	"github.com/hustle/hireflow/internal/hiringintent/domain/entities"
	"github.com/hustle/hireflow/internal/hiringintent/domain/repositories"
	"github.com/hustle/hireflow/internal/hiringintent/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// CreateIntentInput describes the inputs required to draft a hiring intent.
// All identifiers are raw strings — the handler validates and converts them
// into value objects, so the delivery layer doesn't need to know about VOs.
type CreateIntentInput struct {
	TenantID    string
	RecruiterID string
	RoleTitle   string
	Skills      []SkillInput
	MinYears    int
	MaxYears    int
	Headcount   int
	Locations   []string
	WorkMode    string
	Priority    string
	Budget      *BudgetInput
	Reason      string
	Team        string
	ReportsTo   string
}

// SkillInput is the input shape for a skill.
type SkillInput struct {
	Name     string
	Required bool
}

// BudgetInput is the input shape for a budget range (minor units).
type BudgetInput struct {
	MinMinor int64
	MaxMinor int64
	Currency string
}

// CreateIntentHandler drafts a new HiringIntent.
type CreateIntentHandler struct {
	repo repositories.IntentRepository
}

// NewCreateIntentHandler wires the handler.
func NewCreateIntentHandler(repo repositories.IntentRepository) *CreateIntentHandler {
	return &CreateIntentHandler{repo: repo}
}

// Handle executes the use case.
func (h *CreateIntentHandler) Handle(ctx context.Context, in CreateIntentInput) (dto.IntentDTO, error) {
	tenantID, err := shared.ParseTenantID(in.TenantID)
	if err != nil {
		return dto.IntentDTO{}, fmt.Errorf("create intent: %w", err)
	}
	recruiterID, err := shared.ParseRecruiterID(in.RecruiterID)
	if err != nil {
		return dto.IntentDTO{}, fmt.Errorf("create intent: %w", err)
	}

	skills := make([]valueobjects.Skill, 0, len(in.Skills))
	for _, s := range in.Skills {
		sv, err := valueobjects.NewSkill(s.Name, s.Required)
		if err != nil {
			return dto.IntentDTO{}, fmt.Errorf("create intent: skill %q: %w", s.Name, err)
		}
		skills = append(skills, sv)
	}

	expRange, err := valueobjects.NewExperienceRange(in.MinYears, in.MaxYears)
	if err != nil {
		return dto.IntentDTO{}, fmt.Errorf("create intent: %w", err)
	}
	headcount, err := valueobjects.NewHeadcount(in.Headcount)
	if err != nil {
		return dto.IntentDTO{}, fmt.Errorf("create intent: %w", err)
	}
	workMode, err := valueobjects.ParseWorkMode(in.WorkMode)
	if err != nil {
		return dto.IntentDTO{}, fmt.Errorf("create intent: %w", err)
	}
	role, err := valueobjects.NewRoleSpec(in.RoleTitle, skills, expRange, headcount, in.Locations, workMode)
	if err != nil {
		return dto.IntentDTO{}, fmt.Errorf("create intent: %w", err)
	}
	priority, err := valueobjects.ParsePriority(in.Priority)
	if err != nil {
		return dto.IntentDTO{}, fmt.Errorf("create intent: %w", err)
	}

	intent, err := entities.NewHiringIntent(tenantID, recruiterID, role, priority)
	if err != nil {
		return dto.IntentDTO{}, fmt.Errorf("create intent: %w", err)
	}

	if in.Budget != nil {
		budget, err := valueobjects.NewBudgetRange(in.Budget.MinMinor, in.Budget.MaxMinor, in.Budget.Currency)
		if err != nil {
			return dto.IntentDTO{}, fmt.Errorf("create intent: %w", err)
		}
		if err := intent.SetBudget(budget); err != nil {
			return dto.IntentDTO{}, fmt.Errorf("create intent: %w", err)
		}
	}
	if err := intent.SetReason(in.Reason); err != nil {
		return dto.IntentDTO{}, fmt.Errorf("create intent: %w", err)
	}
	if err := intent.SetTeam(in.Team); err != nil {
		return dto.IntentDTO{}, fmt.Errorf("create intent: %w", err)
	}
	if err := intent.SetReportsTo(in.ReportsTo); err != nil {
		return dto.IntentDTO{}, fmt.Errorf("create intent: %w", err)
	}

	if err := h.repo.Save(ctx, intent); err != nil {
		return dto.IntentDTO{}, fmt.Errorf("create intent: save: %w", err)
	}

	return dto.FromEntity(intent), nil
}
