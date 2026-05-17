package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// Compile-time interface assertion.
var _ repositories.LoopTemplateRepository = (*PostgresLoopTemplateRepository)(nil)

// PostgresLoopTemplateRepository persists LoopTemplate aggregates.
type PostgresLoopTemplateRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresLoopTemplateRepository wires the repository.
func NewPostgresLoopTemplateRepository(pool *pgxpool.Pool) *PostgresLoopTemplateRepository {
	return &PostgresLoopTemplateRepository{pool: pool}
}

// templateRoundJSON is the JSONB shape stored per round in intent_loops.rounds.
type templateRoundJSON struct {
	Kind     string `json:"kind"`
	Sequence int    `json:"sequence"`
}

// Save upserts a LoopTemplate on (tenant_id, intent_id).
func (r *PostgresLoopTemplateRepository) Save(ctx context.Context, t *entities.LoopTemplate) error {
	rounds := t.Rounds()
	jsonRounds := make([]templateRoundJSON, 0, len(rounds))
	for _, tr := range rounds {
		jsonRounds = append(jsonRounds, templateRoundJSON{
			Kind:     string(tr.Kind),
			Sequence: tr.Sequence,
		})
	}

	roundsBytes, err := json.Marshal(jsonRounds)
	if err != nil {
		return fmt.Errorf("marshal rounds: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO intent_loops (id, tenant_id, intent_id, rounds, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id, intent_id) DO UPDATE SET
		    rounds     = EXCLUDED.rounds,
		    updated_at = EXCLUDED.updated_at`,
		t.ID(), t.TenantID().String(), t.IntentID(), roundsBytes, t.CreatedAt(), t.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("upsert loop template: %w", err)
	}
	return nil
}

// FindByIntent returns the LoopTemplate for the given (tenant, intent) pair.
func (r *PostgresLoopTemplateRepository) FindByIntent(ctx context.Context, tenant shared.TenantID, intentID uuid.UUID) (*entities.LoopTemplate, error) {
	var id uuid.UUID
	var tenantIDStr string
	var intentIDVal uuid.UUID
	var roundsBytes []byte
	var createdAt, updatedAt time.Time

	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, intent_id, rounds, created_at, updated_at
		FROM intent_loops
		WHERE tenant_id=$1 AND intent_id=$2`,
		tenant.String(), intentID,
	).Scan(&id, &tenantIDStr, &intentIDVal, &roundsBytes, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repositories.ErrLoopTemplateNotFound
		}
		return nil, fmt.Errorf("scan loop template: %w", err)
	}

	parsedTenant, err := shared.ParseTenantID(tenantIDStr)
	if err != nil {
		return nil, fmt.Errorf("parse tenant: %w", err)
	}

	var jsonRounds []templateRoundJSON
	if err := json.Unmarshal(roundsBytes, &jsonRounds); err != nil {
		return nil, fmt.Errorf("unmarshal rounds: %w", err)
	}

	templateRounds := make([]entities.TemplateRound, 0, len(jsonRounds))
	for _, jr := range jsonRounds {
		kind, err := vo.ParseRoundKind(jr.Kind)
		if err != nil {
			return nil, fmt.Errorf("parse round kind: %w", err)
		}
		templateRounds = append(templateRounds, entities.TemplateRound{
			Kind:     kind,
			Sequence: jr.Sequence,
		})
	}

	return entities.RehydrateLoopTemplate(entities.RehydrateLoopTemplateInput{
		ID:        id,
		TenantID:  parsedTenant,
		IntentID:  intentIDVal,
		Rounds:    templateRounds,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}), nil
}
