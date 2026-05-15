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

	"github.com/hustle/hireflow/internal/interview/domain/services"
	"github.com/hustle/hireflow/internal/interview/infrastructure/clients"
	shared "github.com/hustle/hireflow/internal/shared/domain"
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
	_, err = pool.Exec(context.Background(), `
		TRUNCATE applications, hiring_intent_embeddings, judge_jobs,
		         resume_uploads, resume_uploads_dedup, candidates,
		         sourcing_outbox, hiring_intents, audit_log,
		         interview_processes, interview_rounds, interview_feedback,
		         intent_loops, interview_outbox CASCADE`)
	require.NoError(t, err)
	return pool
}

// uuidHex returns a 64-char hex string suitable for use as a content_hash.
func uuidHex(t *testing.T) string {
	t.Helper()
	return uuid.New().String() + uuid.New().String()
}

func TestIntentReader_ReadsRoleSpec(t *testing.T) {
	pool := newPool(t)
	reader := clients.NewPostgresIntentReader(pool)

	intentID := uuid.New()
	tenantID := uuid.New()
	tenant, err := shared.ParseTenantID(tenantID.String())
	require.NoError(t, err)

	_, err = pool.Exec(context.Background(), `
		INSERT INTO hiring_intents (id, tenant_id, recruiter_id, role, priority, status, created_at, updated_at, reports_to, team)
		VALUES ($1, $2, $3, $4::jsonb, 'MEDIUM', 'CONFIRMED', now(), now(), $5, $6)
	`, intentID, tenant.String(), uuid.New(),
		`{"title":"Senior Backend","skills":[{"name":"Go","required":true},{"name":"Kafka","required":false}],"years_min":4,"years_max":8,"seniority":"senior"}`,
		"VP Eng", "Payments",
	)
	require.NoError(t, err)

	spec, err := reader.GetRoleSpec(context.Background(), tenant, intentID)
	require.NoError(t, err)

	assert.Equal(t, "Senior Backend", spec.Title)
	assert.Equal(t, 4, spec.YearsMin)
	assert.Equal(t, 8, spec.YearsMax)
	assert.Equal(t, "senior", spec.Seniority)
	assert.Equal(t, "VP Eng", spec.Reports)
	assert.Equal(t, "Payments", spec.Team)

	require.Len(t, spec.Skills, 2)
	assert.Equal(t, "Go", spec.Skills[0].Name)
	assert.True(t, spec.Skills[0].Required)
	assert.Equal(t, "Kafka", spec.Skills[1].Name)
	assert.False(t, spec.Skills[1].Required)
}

func TestIntentReader_TenantScoped(t *testing.T) {
	pool := newPool(t)
	reader := clients.NewPostgresIntentReader(pool)

	intentID := uuid.New()
	tenantAID := uuid.New()
	tenantBID := uuid.New()
	tenantA, err := shared.ParseTenantID(tenantAID.String())
	require.NoError(t, err)
	tenantB, err := shared.ParseTenantID(tenantBID.String())
	require.NoError(t, err)

	// Insert intent under tenantA.
	_, err = pool.Exec(context.Background(), `
		INSERT INTO hiring_intents (id, tenant_id, recruiter_id, role, priority, status, created_at, updated_at, reports_to, team)
		VALUES ($1, $2, $3, $4::jsonb, 'MEDIUM', 'CONFIRMED', now(), now(), '', '')
	`, intentID, tenantA.String(), uuid.New(),
		`{"title":"Backend Dev","skills":[],"years_min":2,"years_max":5,"seniority":"mid"}`)
	require.NoError(t, err)

	// tenantB cannot see tenantA's intent.
	_, err = reader.GetRoleSpec(context.Background(), tenantB, intentID)
	assert.ErrorIs(t, err, services.ErrIntentNotFound)
}

func TestIntentReader_NotFound(t *testing.T) {
	pool := newPool(t)
	reader := clients.NewPostgresIntentReader(pool)

	tenantID := uuid.New()
	tenant, err := shared.ParseTenantID(tenantID.String())
	require.NoError(t, err)

	_, err = reader.GetRoleSpec(context.Background(), tenant, uuid.New())
	assert.ErrorIs(t, err, services.ErrIntentNotFound)
}
