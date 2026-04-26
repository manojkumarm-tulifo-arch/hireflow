package persistence

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hustle/hireflow/internal/hiringintent/domain/entities"
	"github.com/hustle/hireflow/internal/hiringintent/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// intentRow mirrors the columns of the hiring_intents table.
// JSON columns are stored as []byte and (de)serialized via json.Marshal.
type intentRow struct {
	id            string
	tenantID      string
	recruiterID   string
	role          []byte
	priority      string
	intentSignals []byte
	trustSignals  []byte
	budget        []byte // nullable
	status        string
	createdAt     time.Time
	updatedAt     time.Time
	confirmedAt   *time.Time
	cancelledAt   *time.Time
	cancelReason  string
}

type rolePayload struct {
	Title      string                  `json:"title"`
	Skills     []skillPayload          `json:"skills"`
	Experience experiencePayload       `json:"experience"`
	Headcount  int                     `json:"headcount"`
	Locations  []string                `json:"locations"`
	WorkMode   valueobjects.WorkMode   `json:"work_mode"`
}

type skillPayload struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
}

type experiencePayload struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

type intentSignalPayload struct {
	Label string                    `json:"label"`
	Value string                    `json:"value"`
	Level valueobjects.SignalLevel  `json:"level"`
}

type trustSignalPayload struct {
	Label    string `json:"label"`
	Value    string `json:"value"`
	Required bool   `json:"required"`
}

type budgetPayload struct {
	Min      int64  `json:"min"`
	Max      int64  `json:"max"`
	Currency string `json:"currency"`
}

func serialize(h *entities.HiringIntent) (intentRow, error) {
	role := h.Role()
	skills := role.Skills()
	skillP := make([]skillPayload, len(skills))
	for i, s := range skills {
		skillP[i] = skillPayload{Name: s.Name, Required: s.Required}
	}
	roleBytes, err := json.Marshal(rolePayload{
		Title:      role.Title(),
		Skills:     skillP,
		Experience: experiencePayload{Min: role.Experience().Min(), Max: role.Experience().Max()},
		Headcount:  role.Headcount().Value(),
		Locations:  role.Locations(),
		WorkMode:   role.WorkMode(),
	})
	if err != nil {
		return intentRow{}, fmt.Errorf("marshal role: %w", err)
	}

	intentSigs := h.IntentSignals()
	intentSigP := make([]intentSignalPayload, len(intentSigs))
	for i, s := range intentSigs {
		intentSigP[i] = intentSignalPayload{Label: s.Label, Value: s.Value, Level: s.Level}
	}
	intentSigBytes, err := json.Marshal(intentSigP)
	if err != nil {
		return intentRow{}, fmt.Errorf("marshal intent signals: %w", err)
	}

	trustSigs := h.TrustSignals()
	trustSigP := make([]trustSignalPayload, len(trustSigs))
	for i, s := range trustSigs {
		trustSigP[i] = trustSignalPayload{Label: s.Label, Value: s.Value, Required: s.Required}
	}
	trustSigBytes, err := json.Marshal(trustSigP)
	if err != nil {
		return intentRow{}, fmt.Errorf("marshal trust signals: %w", err)
	}

	var budgetBytes []byte
	if b := h.Budget(); b != nil {
		budgetBytes, err = json.Marshal(budgetPayload{Min: b.MinMinor(), Max: b.MaxMinor(), Currency: b.Currency()})
		if err != nil {
			return intentRow{}, fmt.Errorf("marshal budget: %w", err)
		}
	}

	return intentRow{
		id:            h.ID().String(),
		tenantID:      h.TenantID().String(),
		recruiterID:   h.RecruiterID().String(),
		role:          roleBytes,
		priority:      string(h.Priority()),
		intentSignals: intentSigBytes,
		trustSignals:  trustSigBytes,
		budget:        budgetBytes,
		status:        string(h.Status()),
		createdAt:     h.CreatedAt(),
		updatedAt:     h.UpdatedAt(),
		confirmedAt:   h.ConfirmedAt(),
		cancelledAt:   h.CancelledAt(),
		cancelReason:  h.CancelReason(),
	}, nil
}

func deserialize(row intentRow) (*entities.HiringIntent, error) {
	id, err := valueobjects.ParseIntentID(row.id)
	if err != nil {
		return nil, err
	}
	tenantID, err := shared.ParseTenantID(row.tenantID)
	if err != nil {
		return nil, err
	}
	recruiterID, err := shared.ParseRecruiterID(row.recruiterID)
	if err != nil {
		return nil, err
	}

	var rp rolePayload
	if err := json.Unmarshal(row.role, &rp); err != nil {
		return nil, fmt.Errorf("unmarshal role: %w", err)
	}
	skills := make([]valueobjects.Skill, 0, len(rp.Skills))
	for _, s := range rp.Skills {
		sv, err := valueobjects.NewSkill(s.Name, s.Required)
		if err != nil {
			return nil, err
		}
		skills = append(skills, sv)
	}
	expRange, err := valueobjects.NewExperienceRange(rp.Experience.Min, rp.Experience.Max)
	if err != nil {
		return nil, err
	}
	headcount, err := valueobjects.NewHeadcount(rp.Headcount)
	if err != nil {
		return nil, err
	}
	role, err := valueobjects.NewRoleSpec(rp.Title, skills, expRange, headcount, rp.Locations, rp.WorkMode)
	if err != nil {
		return nil, err
	}

	priority, err := valueobjects.ParsePriority(row.priority)
	if err != nil {
		return nil, err
	}
	status, err := valueobjects.ParseIntentStatus(row.status)
	if err != nil {
		return nil, err
	}

	var intentSigs []valueobjects.IntentSignal
	if len(row.intentSignals) > 0 {
		var sigs []intentSignalPayload
		if err := json.Unmarshal(row.intentSignals, &sigs); err != nil {
			return nil, fmt.Errorf("unmarshal intent signals: %w", err)
		}
		for _, s := range sigs {
			sv, err := valueobjects.NewIntentSignal(s.Label, s.Value, s.Level)
			if err != nil {
				return nil, err
			}
			intentSigs = append(intentSigs, sv)
		}
	}

	var trustSigs []valueobjects.TrustSignal
	if len(row.trustSignals) > 0 {
		var sigs []trustSignalPayload
		if err := json.Unmarshal(row.trustSignals, &sigs); err != nil {
			return nil, fmt.Errorf("unmarshal trust signals: %w", err)
		}
		for _, s := range sigs {
			sv, err := valueobjects.NewTrustSignal(s.Label, s.Value, s.Required)
			if err != nil {
				return nil, err
			}
			trustSigs = append(trustSigs, sv)
		}
	}

	var budget *valueobjects.BudgetRange
	if len(row.budget) > 0 {
		var bp budgetPayload
		if err := json.Unmarshal(row.budget, &bp); err != nil {
			return nil, fmt.Errorf("unmarshal budget: %w", err)
		}
		bv, err := valueobjects.NewBudgetRange(bp.Min, bp.Max, bp.Currency)
		if err != nil {
			return nil, err
		}
		budget = &bv
	}

	return entities.HydrateHiringIntent(
		id, tenantID, recruiterID,
		role, priority, intentSigs, trustSigs, budget,
		status, row.createdAt, row.updatedAt, row.confirmedAt, row.cancelledAt, row.cancelReason,
	), nil
}
