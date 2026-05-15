package clients

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hustle/hireflow/internal/interview/domain/services"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

type PostgresCandidateReader struct {
	pool *pgxpool.Pool
}

var _ services.CandidateReader = (*PostgresCandidateReader)(nil)

func NewPostgresCandidateReader(pool *pgxpool.Pool) *PostgresCandidateReader {
	return &PostgresCandidateReader{pool: pool}
}

type profileJSON struct {
	Skills         []candidateSkillJSON `json:"skills"`
	Experiences    []experienceJSON     `json:"experiences"`
	Education      []educationJSON      `json:"education"`
	Certifications []certificationJSON  `json:"certifications"`
}

type candidateSkillJSON struct {
	Name  string  `json:"name"`
	Years float64 `json:"years,omitempty"`
}

type experienceJSON struct {
	Title       string `json:"title"`
	Company     string `json:"company"`
	Start       string `json:"start,omitempty"`
	End         string `json:"end,omitempty"`
	Description string `json:"description,omitempty"`
}

type educationJSON struct {
	Degree      string `json:"degree,omitempty"`
	Field       string `json:"field,omitempty"`
	Institution string `json:"institution"`
	Start       string `json:"start,omitempty"`
	End         string `json:"end,omitempty"`
}

type certificationJSON struct {
	Name   string `json:"name"`
	Issuer string `json:"issuer,omitempty"`
}

func (r *PostgresCandidateReader) GetProfileForQuestions(ctx context.Context, tenant shared.TenantID, candidateID uuid.UUID) (services.CandidateProfile, error) {
	var (
		headline sql.NullString
		location sql.NullString
		schema   int
		payload  []byte
	)
	err := r.pool.QueryRow(ctx, `
		SELECT headline, location, profile_schema, parsed_profile
		FROM candidates
		WHERE tenant_id=$1 AND id=$2`,
		tenant.String(), candidateID,
	).Scan(&headline, &location, &schema, &payload)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return services.CandidateProfile{}, services.ErrCandidateNotFound
		}
		return services.CandidateProfile{}, fmt.Errorf("scan candidate: %w", err)
	}
	var prof profileJSON
	if err := json.Unmarshal(payload, &prof); err != nil {
		return services.CandidateProfile{}, fmt.Errorf("unmarshal profile: %w", err)
	}
	skillNames := make([]string, 0, len(prof.Skills))
	for _, s := range prof.Skills {
		skillNames = append(skillNames, s.Name)
	}
	exps := make([]services.Experience, 0, len(prof.Experiences))
	for _, e := range prof.Experiences {
		duration := e.Start
		if e.End != "" {
			duration += " - " + e.End
		} else if e.Start != "" {
			duration += " - present"
		}
		exps = append(exps, services.Experience{
			Title:    e.Title,
			Company:  e.Company,
			Duration: duration,
			Summary:  e.Description,
		})
	}
	edus := make([]services.EducationEntry, 0, len(prof.Education))
	for _, e := range prof.Education {
		year := e.End
		if year == "" {
			year = e.Start
		}
		edus = append(edus, services.EducationEntry{
			Degree:      e.Degree,
			Field:       e.Field,
			Institution: e.Institution,
			Year:        year,
		})
	}
	certs := make([]string, 0, len(prof.Certifications))
	for _, c := range prof.Certifications {
		certs = append(certs, c.Name)
	}
	return services.CandidateProfile{
		ID:             candidateID,
		Headline:       headline.String,
		Location:       location.String,
		Skills:         skillNames,
		Experiences:    exps,
		Education:      edus,
		Certifications: certs,
		SchemaVersion:  schema,
	}, nil
}
