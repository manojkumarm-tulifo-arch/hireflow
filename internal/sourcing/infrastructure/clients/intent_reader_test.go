//go:build integration

package clients_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/clients"
)

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	// Per-test isolation: drop all sourcing+hiringintent rows so tests don't
	// see each other's data.
	_, err = pool.Exec(context.Background(), `
		TRUNCATE applications, hiring_intent_embeddings, judge_jobs,
		         resume_uploads, resume_uploads_dedup, candidates,
		         sourcing_outbox, hiring_intents, audit_log CASCADE`)
	require.NoError(t, err)
	return pool
}

// insertHiringIntent inserts a minimal hiring_intents row for testing.
// The roleJSON string must be valid JSONB matching the hiringintent serializer layout.
func insertHiringIntent(t *testing.T, pool *pgxpool.Pool, id uuid.UUID, tenant uuid.UUID, status string, roleJSON string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO hiring_intents (
			id, tenant_id, recruiter_id, role, priority,
			intent_signals, trust_signals, budget,
			reason, team, reports_to,
			status, created_at, updated_at, cancel_reason
		) VALUES ($1, $2, $3, $4::jsonb, 'MEDIUM',
		          '[]'::jsonb, '[]'::jsonb, NULL,
		          '', '', '',
		          $5, now(), now(), '')
	`, id, tenant, uuid.New(), roleJSON, status)
	require.NoError(t, err)
}

// sampleRoleJSON returns a representative role JSONB payload.
func sampleRoleJSON() string {
	return `{
		"title": "Senior Backend Engineer",
		"skills": [
			{"name": "Go", "required": true},
			{"name": "Postgres", "required": true},
			{"name": "GraphQL", "required": false}
		],
		"experience": {"min": 5, "max": 10},
		"headcount": 2,
		"locations": ["Bangalore", "Remote"],
		"work_mode": "hybrid"
	}`
}

// TestIntentReaderFindByID_HappyPath inserts a CONFIRMED intent and verifies
// FindByID returns the correctly projected snapshot.
func TestIntentReaderFindByID_HappyPath(t *testing.T) {
	pool := newPool(t)
	reader := clients.NewPostgresIntentReader(pool)

	tenantID := uuid.New()
	intentID := uuid.New()
	tenant, err := shared.ParseTenantID(tenantID.String())
	require.NoError(t, err)

	insertHiringIntent(t, pool, intentID, tenantID, "CONFIRMED", sampleRoleJSON())

	snap, err := reader.FindByID(context.Background(), tenant, intentID)
	require.NoError(t, err)

	assert.Equal(t, intentID, snap.ID)
	assert.Equal(t, tenant, snap.TenantID)
	assert.Equal(t, "CONFIRMED", snap.Status)
	assert.Equal(t, 1, snap.SpecVersion, "SpecVersion must be hardcoded to 1 (slice-3 limitation)")

	assert.Equal(t, "Senior Backend Engineer", snap.Role.Title)
	assert.Equal(t, "hybrid", snap.Role.WorkMode)
	assert.Equal(t, []string{"Bangalore", "Remote"}, snap.Role.Locations)
	assert.Equal(t, 5, snap.Role.MinYears)
	assert.Equal(t, 10, snap.Role.MaxYears)

	require.Len(t, snap.Role.RequiredSkills, 2, "Go and Postgres are required")
	assert.Equal(t, "Go", snap.Role.RequiredSkills[0].Name)
	assert.Equal(t, "Postgres", snap.Role.RequiredSkills[1].Name)

	require.Len(t, snap.Role.OptionalSkills, 1, "GraphQL is optional")
	assert.Equal(t, "GraphQL", snap.Role.OptionalSkills[0].Name)
}

// TestIntentReaderFindByID_NotFound verifies that a random UUID returns a
// wrapped error (not a panic or zero-value snapshot).
func TestIntentReaderFindByID_NotFound(t *testing.T) {
	pool := newPool(t)
	reader := clients.NewPostgresIntentReader(pool)

	tenantID := uuid.New()
	tenant, err := shared.ParseTenantID(tenantID.String())
	require.NoError(t, err)

	_, err = reader.FindByID(context.Background(), tenant, uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "intent not found")
}

// TestIntentReaderListConfirmedIntents_FiltersByStatusAndTenant inserts:
//   - one CONFIRMED intent for tenant A
//   - one DRAFTED intent for tenant A
//   - one CONFIRMED intent for tenant B
//
// and asserts that ListConfirmedIntents for tenant A returns only the single
// CONFIRMED tenant-A intent.
func TestIntentReaderListConfirmedIntents_FiltersByStatusAndTenant(t *testing.T) {
	pool := newPool(t)
	reader := clients.NewPostgresIntentReader(pool)

	tenantAID := uuid.New()
	tenantBID := uuid.New()
	tenantA, err := shared.ParseTenantID(tenantAID.String())
	require.NoError(t, err)

	confirmedID := uuid.New()
	draftedID := uuid.New()
	tenantBConfirmedID := uuid.New()

	insertHiringIntent(t, pool, confirmedID, tenantAID, "CONFIRMED", sampleRoleJSON())
	insertHiringIntent(t, pool, draftedID, tenantAID, "DRAFTED", sampleRoleJSON())
	insertHiringIntent(t, pool, tenantBConfirmedID, tenantBID, "CONFIRMED", sampleRoleJSON())

	results, err := reader.ListConfirmedIntents(context.Background(), tenantA)
	require.NoError(t, err)

	// Filter to only the IDs we inserted (other tests may have left rows in DB).
	var matched []uuid.UUID
	for _, s := range results {
		if s.ID == confirmedID || s.ID == draftedID || s.ID == tenantBConfirmedID {
			matched = append(matched, s.ID)
		}
	}

	require.Len(t, matched, 1, "only the tenant-A CONFIRMED intent must appear")
	assert.Equal(t, confirmedID, matched[0])
}
