package services

import (
	"context"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// RoleSpec is the anti-corruption type for a hiring intent's role requirements.
// It is sourced from the hiringintent context but deliberately does NOT import
// that aggregate — the IntentReader adapter translates the upstream shape into
// this primitive struct so the sourcing context remains independent.
type RoleSpec struct {
	Title          string
	RequiredSkills []SkillSpec
	OptionalSkills []SkillSpec
	MinYears       int
	MaxYears       int
	Locations      []string
	WorkMode       string // "remote" | "hybrid" | "onsite"
	Degree         string
	Languages      []string
}

// SkillSpec describes a single skill requirement within a RoleSpec.
type SkillSpec struct {
	Name     string
	MinYears float64
}

// IntentSnapshot is a point-in-time view of a hiring intent as seen by the
// sourcing context. Status is kept as a plain string to avoid importing the
// hiringintent domain — callers MUST only act on Confirmed intents.
type IntentSnapshot struct {
	ID          uuid.UUID
	TenantID    shared.TenantID
	Status      string // we only score "Confirmed" intents
	SpecVersion int
	Role        RoleSpec
}

// IntentReader is the anti-corruption port for reading hiring intents from the
// hiringintent context. The adapter (internal/sourcing/infrastructure/clients)
// queries the hiring_intents table directly via pgx and projects the persisted
// shape into IntentSnapshot without importing the hiringintent domain package.
type IntentReader interface {
	// FindByID returns the intent snapshot for the given tenant and intent ID.
	// Returns an error wrapping ErrIntentNotFound when no matching row exists.
	FindByID(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (IntentSnapshot, error)

	// ListConfirmedIntents returns all currently-Confirmed intents for the
	// tenant. Used by ScoreCandidate to fan out over open roles.
	ListConfirmedIntents(ctx context.Context, tenant shared.TenantID) ([]IntentSnapshot, error)
}
