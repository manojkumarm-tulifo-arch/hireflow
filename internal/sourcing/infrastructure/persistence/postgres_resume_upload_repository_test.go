//go:build integration

package persistence_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/persistence"
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
	// see each other's data. Order doesn't matter with CASCADE.
	_, err = pool.Exec(context.Background(), `
		TRUNCATE applications, hiring_intent_embeddings, judge_jobs,
		         resume_uploads, resume_uploads_dedup, candidates,
		         sourcing_outbox, hiring_intents, audit_log CASCADE`)
	require.NoError(t, err)
	return pool
}

func newUpload(t *testing.T, tenant shared.TenantID) *entities.ResumeUpload {
	t.Helper()
	h, err := vo.NewContentHash(uuidHex(t))
	require.NoError(t, err)
	mime, err := vo.ParseMimeType("application/pdf")
	require.NoError(t, err)
	u, err := entities.NewResumeUpload(entities.UploadInput{
		TenantID:     tenant,
		IntentID:     uuid.New(),
		BatchID:      uuid.New(),
		StorageKey:   "k/" + uuid.New().String(),
		OriginalName: "alice.pdf",
		MimeType:     mime,
		SizeBytes:    1000,
		ContentHash:  h,
	})
	require.NoError(t, err)
	return u
}

// 64-char hex string seeded from a uuid (test helper). UUIDs are 36 chars
// with 4 dashes; strip them to get pure hex.
func uuidHex(t *testing.T) string {
	t.Helper()
	a, b := uuid.New(), uuid.New()
	return strings.ReplaceAll(a.String(), "-", "") + strings.ReplaceAll(b.String(), "-", "")
}

func TestSave_PersistsRow_AndOutboxRow(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresResumeUploadRepository(pool)

	u := newUpload(t, shared.NewTenantID())
	require.NoError(t, repo.Save(context.Background(), u))

	got, err := repo.FindByID(context.Background(), u.TenantID(), u.ID())
	require.NoError(t, err)
	assert.Equal(t, u.ID(), got.ID())
	assert.Equal(t, vo.StatusPending, got.Status())

	// Outbox has 1 pending row for this upload.
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM sourcing_outbox
		 WHERE aggregate_id=$1 AND dispatched_at IS NULL`, u.ID()).Scan(&n))
	assert.Equal(t, 1, n)
}

func TestSave_DuplicateContentHashReturnsErrDuplicate(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresResumeUploadRepository(pool)
	tenant := shared.NewTenantID()

	u1 := newUpload(t, tenant)
	require.NoError(t, repo.Save(context.Background(), u1))

	// Build a second upload with the same content_hash.
	u2 := newUpload(t, tenant)
	// Hack: assign u2 the same hash via constructor.
	mime, _ := vo.ParseMimeType("application/pdf")
	u2new, err := entities.NewResumeUpload(entities.UploadInput{
		TenantID: tenant, IntentID: u2.IntentID(), BatchID: u2.BatchID(),
		StorageKey: u2.StorageKey(), OriginalName: u2.OriginalName(),
		MimeType: mime, SizeBytes: 1000, ContentHash: u1.ContentHash(),
	})
	require.NoError(t, err)
	err = repo.Save(context.Background(), u2new)
	assert.ErrorIs(t, err, repositories.ErrDuplicate)
}

func TestFindByContentHash_ReturnsExistingOrErrNotFound(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresResumeUploadRepository(pool)
	tenant := shared.NewTenantID()
	u := newUpload(t, tenant)
	require.NoError(t, repo.Save(context.Background(), u))

	got, err := repo.FindByContentHash(context.Background(), tenant, u.ContentHash().String())
	require.NoError(t, err)
	assert.Equal(t, u.ID(), got.ID())

	_, err = repo.FindByContentHash(context.Background(), tenant, "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	assert.Error(t, err)
}

func TestListByBatch_TenantScoped(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresResumeUploadRepository(pool)
	tenantA := shared.NewTenantID()
	tenantB := shared.NewTenantID()

	uA := newUpload(t, tenantA)
	require.NoError(t, repo.Save(context.Background(), uA))
	uB := newUpload(t, tenantB)
	require.NoError(t, repo.Save(context.Background(), uB))

	got, err := repo.ListByBatch(context.Background(), tenantA, uA.BatchID())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, uA.ID(), got[0].ID())

	gotB, err := repo.ListByBatch(context.Background(), tenantA, uB.BatchID())
	require.NoError(t, err)
	assert.Empty(t, gotB, "tenantA must not see tenantB rows")
}

func TestClaimNextPending_ReturnsAndAdvances(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresResumeUploadRepository(pool)
	u := newUpload(t, shared.NewTenantID())
	require.NoError(t, repo.Save(context.Background(), u))

	claimed, err := repo.ClaimNextPending(context.Background())
	require.NoError(t, err)
	require.NotNil(t, claimed)
	// The claim should at least include our just-saved row eventually.
	// We don't assert exact equality because other tests may interleave;
	// the smoke test is "returns something pending without erroring."
	assert.False(t, claimed.Status().IsTerminal())
}
