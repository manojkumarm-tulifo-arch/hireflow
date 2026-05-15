// Package clients holds interview-context Postgres adapters for cross-context
// reads. These read tables owned by other contexts (hiringintent.hiring_intents,
// sourcing.candidates) via the shared pool — there is no Go-level import
// from the interview package into sourcing or hiringintent.
package clients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hustle/hireflow/internal/interview/domain/services"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

type PostgresIntentReader struct {
	pool *pgxpool.Pool
}

var _ services.IntentReader = (*PostgresIntentReader)(nil)

func NewPostgresIntentReader(pool *pgxpool.Pool) *PostgresIntentReader {
	return &PostgresIntentReader{pool: pool}
}

// roleJSON mirrors the shape stored in hiring_intents.role. The hiringintent
// context owns the canonical struct; this is a duplicated narrow shape that
// pulls only the fields the question generator needs.
type roleJSON struct {
	Title     string      `json:"title"`
	Skills    []skillJSON `json:"skills"`
	YearsMin  int         `json:"years_min"`
	YearsMax  int         `json:"years_max"`
	Seniority string      `json:"seniority"`
}

type skillJSON struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
}

func (r *PostgresIntentReader) GetRoleSpec(ctx context.Context, tenant shared.TenantID, intentID uuid.UUID) (services.RoleSpec, error) {
	var (
		payload []byte
		reports string
		team    string
	)
	err := r.pool.QueryRow(ctx, `
		SELECT role, reports_to, team
		FROM hiring_intents
		WHERE tenant_id=$1 AND id=$2`,
		tenant.String(), intentID,
	).Scan(&payload, &reports, &team)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return services.RoleSpec{}, services.ErrIntentNotFound
		}
		return services.RoleSpec{}, fmt.Errorf("scan intent: %w", err)
	}
	var role roleJSON
	if err := json.Unmarshal(payload, &role); err != nil {
		return services.RoleSpec{}, fmt.Errorf("unmarshal role: %w", err)
	}
	skills := make([]services.SkillRequirement, 0, len(role.Skills))
	for _, s := range role.Skills {
		skills = append(skills, services.SkillRequirement{Name: s.Name, Required: s.Required})
	}
	return services.RoleSpec{
		Title:     role.Title,
		Skills:    skills,
		YearsMin:  role.YearsMin,
		YearsMax:  role.YearsMax,
		Seniority: role.Seniority,
		Reports:   reports,
		Team:      team,
	}, nil
}
