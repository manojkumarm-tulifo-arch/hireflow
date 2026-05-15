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

type PostgresCandidateReader struct {
	pool *pgxpool.Pool
}

var _ services.CandidateReader = (*PostgresCandidateReader)(nil)

func NewPostgresCandidateReader(pool *pgxpool.Pool) *PostgresCandidateReader {
	return &PostgresCandidateReader{pool: pool}
}

type profileJSON struct {
	Skills         []string         `json:"skills"`
	Experiences    []experienceJSON `json:"experiences"`
	Education      []educationJSON  `json:"education"`
	Certifications []string         `json:"certifications"`
}

type experienceJSON struct {
	Title    string `json:"title"`
	Company  string `json:"company"`
	Duration string `json:"duration"`
	Summary  string `json:"summary"`
}

type educationJSON struct {
	Degree      string `json:"degree"`
	Field       string `json:"field"`
	Institution string `json:"institution"`
	Year        string `json:"year"`
}

func (r *PostgresCandidateReader) GetProfileForQuestions(ctx context.Context, tenant shared.TenantID, candidateID uuid.UUID) (services.CandidateProfile, error) {
	var (
		headline string
		location string
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
	exps := make([]services.Experience, 0, len(prof.Experiences))
	for _, e := range prof.Experiences {
		exps = append(exps, services.Experience{
			Title: e.Title, Company: e.Company, Duration: e.Duration, Summary: e.Summary,
		})
	}
	edus := make([]services.EducationEntry, 0, len(prof.Education))
	for _, e := range prof.Education {
		edus = append(edus, services.EducationEntry{
			Degree: e.Degree, Field: e.Field, Institution: e.Institution, Year: e.Year,
		})
	}
	return services.CandidateProfile{
		ID:             candidateID,
		Headline:       headline,
		Location:       location,
		Skills:         append([]string(nil), prof.Skills...),
		Experiences:    exps,
		Education:      edus,
		Certifications: append([]string(nil), prof.Certifications...),
		SchemaVersion:  schema,
	}, nil
}
