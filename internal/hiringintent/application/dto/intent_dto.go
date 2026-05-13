// Package dto holds the data transfer objects exchanged between the
// application layer and the outside world. DTOs are returned from
// application services so domain types never leak across the boundary.
package dto

import (
	"time"

	"github.com/hustle/hireflow/internal/hiringintent/domain/entities"
)

// SkillDTO mirrors valueobjects.Skill for API responses.
type SkillDTO struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
}

// ExperienceRangeDTO mirrors valueobjects.ExperienceRange.
type ExperienceRangeDTO struct {
	MinYears int `json:"min_years"`
	MaxYears int `json:"max_years"`
}

// RoleSpecDTO mirrors valueobjects.RoleSpec.
type RoleSpecDTO struct {
	Title      string             `json:"title"`
	Skills     []SkillDTO         `json:"skills"`
	Experience ExperienceRangeDTO `json:"experience"`
	Headcount  int                `json:"headcount"`
	Locations  []string           `json:"locations"`
	WorkMode   string             `json:"work_mode"`
}

// IntentSignalDTO mirrors valueobjects.IntentSignal.
type IntentSignalDTO struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Level string `json:"level"`
}

// TrustSignalDTO mirrors valueobjects.TrustSignal.
type TrustSignalDTO struct {
	Label    string `json:"label"`
	Value    string `json:"value"`
	Required bool   `json:"required"`
}

// BudgetDTO mirrors valueobjects.BudgetRange (amounts in minor units).
type BudgetDTO struct {
	MinMinor int64  `json:"min_minor"`
	MaxMinor int64  `json:"max_minor"`
	Currency string `json:"currency"`
}

// StatusCountsDTO is the per-status histogram returned in IntentSummaryDTO.
// Total is a precomputed sum so the FE doesn't need to add the four fields.
type StatusCountsDTO struct {
	Drafted   int `json:"DRAFTED"`
	Confirmed int `json:"CONFIRMED"`
	Cancelled int `json:"CANCELLED"`
	Closed    int `json:"CLOSED"`
	Total     int `json:"total"`
}

// IntentSummaryDTO is the response shape for GET /intents/summary.
type IntentSummaryDTO struct {
	Counts StatusCountsDTO `json:"counts"`
}

// IntentDTO is the response shape for a single hiring intent.
type IntentDTO struct {
	ID            string            `json:"id"`
	TenantID      string            `json:"tenant_id"`
	RecruiterID   string            `json:"recruiter_id"`
	Role          RoleSpecDTO       `json:"role"`
	Priority      string            `json:"priority"`
	IntentSignals []IntentSignalDTO `json:"intent_signals"`
	TrustSignals  []TrustSignalDTO  `json:"trust_signals"`
	Budget        *BudgetDTO        `json:"budget,omitempty"`
	Reason        string            `json:"reason,omitempty"`
	Team          string            `json:"team,omitempty"`
	ReportsTo     string            `json:"reports_to,omitempty"`
	Status        string            `json:"status"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	ConfirmedAt   *time.Time        `json:"confirmed_at,omitempty"`
	CancelledAt   *time.Time        `json:"cancelled_at,omitempty"`
	CancelReason  string            `json:"cancel_reason,omitempty"`
}

// FromEntity maps a domain aggregate to its API DTO.
func FromEntity(h *entities.HiringIntent) IntentDTO {
	role := h.Role()
	skills := role.Skills()
	skillDTOs := make([]SkillDTO, len(skills))
	for i, s := range skills {
		skillDTOs[i] = SkillDTO{Name: s.Name(), Required: s.Required()}
	}
	intentSigs := h.IntentSignals()
	intentSigDTOs := make([]IntentSignalDTO, len(intentSigs))
	for i, s := range intentSigs {
		intentSigDTOs[i] = IntentSignalDTO{Label: s.Label(), Value: s.Value(), Level: string(s.Level())}
	}
	trustSigs := h.TrustSignals()
	trustSigDTOs := make([]TrustSignalDTO, len(trustSigs))
	for i, s := range trustSigs {
		trustSigDTOs[i] = TrustSignalDTO{Label: s.Label(), Value: s.Value(), Required: s.Required()}
	}

	dto := IntentDTO{
		ID:          h.ID().String(),
		TenantID:    h.TenantID().String(),
		RecruiterID: h.RecruiterID().String(),
		Role: RoleSpecDTO{
			Title:      role.Title(),
			Skills:     skillDTOs,
			Experience: ExperienceRangeDTO{MinYears: role.Experience().Min(), MaxYears: role.Experience().Max()},
			Headcount:  role.Headcount().Value(),
			Locations:  role.Locations(),
			WorkMode:   string(role.WorkMode()),
		},
		Priority:      string(h.Priority()),
		IntentSignals: intentSigDTOs,
		TrustSignals:  trustSigDTOs,
		Reason:        h.Reason(),
		Team:          h.Team(),
		ReportsTo:     h.ReportsTo(),
		Status:        string(h.Status()),
		CreatedAt:     h.CreatedAt(),
		UpdatedAt:     h.UpdatedAt(),
		ConfirmedAt:   h.ConfirmedAt(),
		CancelledAt:   h.CancelledAt(),
		CancelReason:  h.CancelReason(),
	}
	if b := h.Budget(); b != nil {
		dto.Budget = &BudgetDTO{MinMinor: b.MinMinor(), MaxMinor: b.MaxMinor(), Currency: b.Currency()}
	}
	return dto
}
