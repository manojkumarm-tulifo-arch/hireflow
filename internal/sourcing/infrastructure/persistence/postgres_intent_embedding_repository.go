package persistence

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
)

// PostgresIntentEmbeddingRepository caches dense vector representations of
// hiring intent RoleSpecs in hiring_intent_embeddings.
type PostgresIntentEmbeddingRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresIntentEmbeddingRepository wires the repository.
func NewPostgresIntentEmbeddingRepository(pool *pgxpool.Pool) *PostgresIntentEmbeddingRepository {
	return &PostgresIntentEmbeddingRepository{pool: pool}
}

const intentEmbeddingUpsertSQL = `
INSERT INTO hiring_intent_embeddings (intent_id, tenant_id, spec_version, role_embedding, created_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (intent_id, spec_version) DO UPDATE SET
    role_embedding = EXCLUDED.role_embedding,
    tenant_id      = EXCLUDED.tenant_id`

// Save upserts the embedding for (intentID, specVersion).
func (r *PostgresIntentEmbeddingRepository) Save(ctx context.Context, intentID uuid.UUID, tenant shared.TenantID, specVersion int, vector []float32) error {
	vec := pgvector.NewVector(vector)
	_, err := r.pool.Exec(ctx, intentEmbeddingUpsertSQL,
		intentID, tenant.String(), specVersion, vec,
	)
	if err != nil {
		return fmt.Errorf("upsert intent embedding: %w", err)
	}
	return nil
}

// Find returns the cached embedding for (intentID, specVersion).
// Returns ErrIntentEmbeddingNotFound when no row exists.
func (r *PostgresIntentEmbeddingRepository) Find(ctx context.Context, intentID uuid.UUID, specVersion int) ([]float32, error) {
	var vec pgvector.Vector
	err := r.pool.QueryRow(ctx,
		`SELECT role_embedding FROM hiring_intent_embeddings WHERE intent_id=$1 AND spec_version=$2`,
		intentID, specVersion,
	).Scan(&vec)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repositories.ErrIntentEmbeddingNotFound
		}
		return nil, fmt.Errorf("find intent embedding: %w", err)
	}
	return vec.Slice(), nil
}
