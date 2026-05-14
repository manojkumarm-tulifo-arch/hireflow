//go:build integration

package persistence_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/persistence"
)

// TestEraseCascade_DeletesAllRelatedRows seeds a candidate with 2 applications,
// 1 judge_job (linked to one application), 2 resume_uploads, and 1 dedup row.
// It calls EraseCascade and verifies that all rows are deleted and the correct
// storage keys are returned.
func TestEraseCascade_DeletesAllRelatedRows(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresCandidateRepository(pool)
	ctx := context.Background()

	tenant := shared.NewTenantID()
	candidateID := uuid.New()

	// --- seed candidate ---
	_, err := pool.Exec(ctx, `
		INSERT INTO candidates (id, tenant_id, content_hash, full_name_enc, email_enc, phone_enc,
			location, headline, parsed_profile, profile_schema, source, created_at, updated_at)
		VALUES ($1, $2, $3, 'enc:full', 'enc:email', 'enc:phone',
			'Bangalore', 'Engineer', '{}', 1, 'manual_upload', $4, $4)
	`, candidateID, tenant.String(), uuidHex(t), time.Now())
	require.NoError(t, err)

	// --- seed intent (required for FK on applications if present) ---
	intentID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO hiring_intents (id, tenant_id, status, title, created_at, updated_at)
		VALUES ($1, $2, 'Confirmed', 'SWE', $3, $3)
	`, intentID, tenant.String(), time.Now())
	require.NoError(t, err)

	// --- seed 2 applications ---
	appID1, appID2 := uuid.New(), uuid.New()
	for _, appID := range []uuid.UUID{appID1, appID2} {
		_, err = pool.Exec(ctx, `
			INSERT INTO applications (id, tenant_id, candidate_id, intent_id,
				status, intent_spec_version, profile_schema_version, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'New', 1, 1, $5, $5)
		`, appID, tenant.String(), candidateID, intentID, time.Now())
		require.NoError(t, err)
	}

	// --- seed 1 judge_job linked to appID1 ---
	_, err = pool.Exec(ctx, `
		INSERT INTO judge_jobs (id, tenant_id, application_id, status, attempt_count, created_at, updated_at)
		VALUES ($1, $2, $3, 'Pending', 0, $4, $4)
	`, uuid.New(), tenant.String(), appID1, time.Now())
	require.NoError(t, err)

	// --- seed 2 resume_uploads ---
	storageKey1 := "resumes/tenant1/file1.pdf"
	storageKey2 := "resumes/tenant1/file2.pdf"
	contentHash1 := uuidHex(t)
	contentHash2 := uuidHex(t)

	for _, row := range []struct {
		key  string
		hash string
	}{
		{storageKey1, contentHash1},
		{storageKey2, contentHash2},
	} {
		_, err = pool.Exec(ctx, `
			INSERT INTO resume_uploads (id, tenant_id, candidate_id, intent_id, batch_id,
				storage_key, original_name, mime_type, size_bytes, content_hash,
				status, attempt_count, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, 'resume.pdf', 'application/pdf', 1000, $7,
				'Extracted', 0, $8, $8)
		`, uuid.New(), tenant.String(), candidateID, intentID, uuid.New(),
			row.key, row.hash, time.Now())
		require.NoError(t, err)
	}

	// --- seed 1 resume_uploads_dedup row (for contentHash1 only) ---
	_, err = pool.Exec(ctx, `
		INSERT INTO resume_uploads_dedup (tenant_id, content_hash, upload_id, created_at)
		VALUES ($1, $2, $3, $4)
	`, tenant.String(), contentHash1, uuid.New(), time.Now())
	require.NoError(t, err)

	// --- call EraseCascade ---
	keys, err := repo.EraseCascade(ctx, tenant, candidateID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{storageKey1, storageKey2}, keys,
		"returned storage keys must match the seeded resume_uploads")

	// --- verify all rows gone ---
	var count int

	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM candidates WHERE id=$1`, candidateID).Scan(&count))
	assert.Equal(t, 0, count, "candidate row must be deleted")

	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM applications WHERE candidate_id=$1`, candidateID).Scan(&count))
	assert.Equal(t, 0, count, "application rows must be deleted")

	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM judge_jobs WHERE application_id=$1 OR application_id=$2`, appID1, appID2).Scan(&count))
	assert.Equal(t, 0, count, "judge_job rows must be deleted")

	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM resume_uploads WHERE candidate_id=$1`, candidateID).Scan(&count))
	assert.Equal(t, 0, count, "resume_upload rows must be deleted")

	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM resume_uploads_dedup WHERE tenant_id=$1 AND content_hash=$2`,
		tenant.String(), contentHash1).Scan(&count))
	assert.Equal(t, 0, count, "resume_uploads_dedup row must be deleted")
}

// TestEraseCascade_NotFound_ReturnsError ensures EraseCascade returns
// ErrCandidateNotFound when the target candidate does not exist.
func TestEraseCascade_NotFound_ReturnsError(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresCandidateRepository(pool)
	ctx := context.Background()

	tenant := shared.NewTenantID()
	nonExistent := uuid.New()

	_, err := repo.EraseCascade(ctx, tenant, nonExistent)
	require.ErrorIs(t, err, repositories.ErrCandidateNotFound)
}

// TestEraseCascade_BystanderRowsUntouched ensures that rows belonging to a
// different tenant / different candidate are not deleted.
func TestEraseCascade_BystanderRowsUntouched(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresCandidateRepository(pool)
	ctx := context.Background()

	tenant := shared.NewTenantID()
	otherTenant := shared.NewTenantID()

	// Seed the target candidate (to be erased).
	targetID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO candidates (id, tenant_id, content_hash, full_name_enc, email_enc, phone_enc,
			location, headline, parsed_profile, profile_schema, source, created_at, updated_at)
		VALUES ($1, $2, $3, 'enc:full', 'enc:email', 'enc:phone',
			'City', 'Dev', '{}', 1, 'manual_upload', $4, $4)
	`, targetID, tenant.String(), uuidHex(t), time.Now())
	require.NoError(t, err)

	// Seed a bystander candidate (different tenant — must survive).
	bystanderID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO candidates (id, tenant_id, content_hash, full_name_enc, email_enc, phone_enc,
			location, headline, parsed_profile, profile_schema, source, created_at, updated_at)
		VALUES ($1, $2, $3, 'enc:full2', 'enc:email2', 'enc:phone2',
			'Remote', 'SRE', '{}', 1, 'manual_upload', $4, $4)
	`, bystanderID, otherTenant.String(), uuidHex(t), time.Now())
	require.NoError(t, err)

	// Seed intent + application + upload for the bystander.
	bystanderIntentID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO hiring_intents (id, tenant_id, status, title, created_at, updated_at)
		VALUES ($1, $2, 'Confirmed', 'DevOps', $3, $3)
	`, bystanderIntentID, otherTenant.String(), time.Now())
	require.NoError(t, err)

	bystanderAppID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO applications (id, tenant_id, candidate_id, intent_id,
			status, intent_spec_version, profile_schema_version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'New', 1, 1, $5, $5)
	`, bystanderAppID, otherTenant.String(), bystanderID, bystanderIntentID, time.Now())
	require.NoError(t, err)

	// Erase only the target.
	_, err = repo.EraseCascade(ctx, tenant, targetID)
	require.NoError(t, err)

	// Target is gone.
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM candidates WHERE id=$1`, targetID).Scan(&count))
	assert.Equal(t, 0, count, "target candidate must be deleted")

	// Bystander survives.
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM candidates WHERE id=$1`, bystanderID).Scan(&count))
	assert.Equal(t, 1, count, "bystander candidate must NOT be deleted")

	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM applications WHERE id=$1`, bystanderAppID).Scan(&count))
	assert.Equal(t, 1, count, "bystander application must NOT be deleted")
}
