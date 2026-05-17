//go:build integration

package tests

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/shared/infrastructure/auth"
	sourcingcommands "github.com/hustle/hireflow/internal/sourcing/application/commands"
	sourcingqueries "github.com/hustle/hireflow/internal/sourcing/application/queries"
	v1 "github.com/hustle/hireflow/internal/sourcing/delivery/http/v1"
	sourcingenc "github.com/hustle/hireflow/internal/sourcing/infrastructure/encryption"
	sourcingpersist "github.com/hustle/hireflow/internal/sourcing/infrastructure/persistence"
	sourcingstorage "github.com/hustle/hireflow/internal/sourcing/infrastructure/storage"
)

// zipEntry describes one file to include in a test ZIP.
type zipEntry struct {
	name    string
	content []byte
}

// buildE2EZip constructs a valid ZIP archive in memory from the given entries.
func buildE2EZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		f, err := w.Create(e.name)
		require.NoError(t, err)
		_, err = f.Write(e.content)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// postZip sends a multipart POST containing a single zip file to
// POST /intents/{intentID}/resumes:batch. It injects the given tenant identity
// and returns the decoded BatchUploadResponse.
func postZip(
	t *testing.T,
	router chi.Router,
	tenant shared.TenantID,
	intentID uuid.UUID,
	filename string,
	zipBytes []byte,
) v1.BatchUploadResponse {
	t.Helper()
	body, ct := writeMultipart(t, map[string][]byte{filename: zipBytes})
	req := httptest.NewRequest(http.MethodPost,
		"/intents/"+intentID.String()+"/resumes:batch", body)
	req.Header.Set("Content-Type", ct)
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{
		TenantID:    tenant,
		RecruiterID: shared.NewRecruiterID(),
	}))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "POST zip: %s", rec.Body.String())

	var resp v1.BatchUploadResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp
}

// minimalPDFContent returns a small but MIME-detectable PDF byte slice whose
// content is unique to the given seed string, ensuring distinct content hashes.
func minimalPDFContent(seed string) []byte {
	return []byte("%PDF-1.4\n%" + seed + "\n%%EOF\n")
}

// TestSourcingZipUploadE2E exercises ZIP fan-out + per-intent dedup end-to-end:
//
//  1. Insert two hiring intents (intentA, intentB) for the same tenant.
//  2. Build a 3-PDF ZIP (alice.pdf, bharat.pdf, chitra.pdf).
//  3. POST the ZIP to intentA → expect 4 items: 1 extracted_from_zip + 3 queued.
//  4. POST the same ZIP again to intentA → expect 4 items: 1 extracted_from_zip + 3 duplicate_in_intent.
//  5. POST the ZIP to intentB → expect 4 items: 1 extracted_from_zip + 3 queued (different intent).
//  6. Assert resume_uploads has 6 rows, resume_uploads_dedup has 6 rows.
func TestSourcingZipUploadE2E(t *testing.T) {
	pool := newPgvectorPool(t) // skips if DATABASE_URL not set
	logger := zerolog.New(io.Discard)

	// ── Identity & Intents ────────────────────────────────────────────────────
	tenant := shared.NewTenantID()
	tenantUUID, err := uuid.Parse(tenant.String())
	require.NoError(t, err)

	intentA := uuid.New()
	intentB := uuid.New()
	insertHiringIntentForSlice3(t, pool, intentA, tenantUUID)
	insertHiringIntentForSlice3(t, pool, intentB, tenantUUID)

	// ── Infrastructure ────────────────────────────────────────────────────────
	storageDir := t.TempDir()
	store, err := sourcingstorage.NewLocalFS(storageDir)
	require.NoError(t, err)

	piiEnc, err := sourcingenc.NewLocalDevDEK("0000000000000000000000000000000000000000000000000000000000000000")
	require.NoError(t, err)
	_ = piiEnc // not needed for this test's assertions but wired for parity

	uploadRepo := sourcingpersist.NewPostgresResumeUploadRepository(pool)

	uploadH := sourcingcommands.NewUploadResumeBatchHandler(
		uploadRepo, store,
		sourcingcommands.UploadConfig{MaxFileBytes: 10 * 1024 * 1024},
	)
	statusH := sourcingqueries.NewGetBatchStatusHandler(uploadRepo)

	sourcingH := v1.NewSourcingHandler(v1.SourcingHandlerDeps{
		Upload: uploadH,
		Status: statusH,
		Logger: logger,
	})
	router := chi.NewRouter()
	v1.Mount(router, sourcingH)

	// ── Build the 3-PDF ZIP ───────────────────────────────────────────────────
	zipBytes := buildE2EZip(t, []zipEntry{
		{name: "alice.pdf", content: minimalPDFContent("alice")},
		{name: "bharat.pdf", content: minimalPDFContent("bharat")},
		{name: "chitra.pdf", content: minimalPDFContent("chitra")},
	})

	// ── POST 1: intentA, first upload ────────────────────────────────────────
	// Expect: 4 items — 1 extracted_from_zip + 3 queued.
	resp1 := postZip(t, router, tenant, intentA, "resumes.zip", zipBytes)
	require.Len(t, resp1.Items, 4,
		"POST 1 (intentA, first): expected 4 items (1 extracted_from_zip + 3 queued); got %d: %+v",
		len(resp1.Items), resp1.Items)

	// First item is the ZIP parent.
	assert.Equal(t, "extracted_from_zip", resp1.Items[0].Status,
		"POST 1: first item must be extracted_from_zip")

	// Remaining 3 must be queued.
	for i, item := range resp1.Items[1:] {
		assert.Equal(t, "queued", item.Status,
			"POST 1: item[%d] (%s) must be queued", i+1, item.Filename)
		assert.NotEmpty(t, item.UploadID,
			"POST 1: item[%d] (%s) must have an upload_id", i+1, item.Filename)
	}

	// Allow a brief moment for any async I/O to settle (there are no background
	// workers wired in this test — the assertion is synchronous).
	time.Sleep(50 * time.Millisecond)

	// ── POST 2: intentA, duplicate upload ─────────────────────────────────────
	// Expect: 4 items — 1 extracted_from_zip + 3 duplicate_in_intent.
	resp2 := postZip(t, router, tenant, intentA, "resumes.zip", zipBytes)
	require.Len(t, resp2.Items, 4,
		"POST 2 (intentA, duplicate): expected 4 items; got %d: %+v",
		len(resp2.Items), resp2.Items)

	assert.Equal(t, "extracted_from_zip", resp2.Items[0].Status,
		"POST 2: first item must be extracted_from_zip")

	for i, item := range resp2.Items[1:] {
		assert.Equal(t, "duplicate_in_intent", item.Status,
			"POST 2: item[%d] (%s) must be duplicate_in_intent (same content, same intent)", i+1, item.Filename)
	}

	// ── POST 3: intentB, first upload ────────────────────────────────────────
	// Same content, but a different intent → should be queued (not deduped).
	resp3 := postZip(t, router, tenant, intentB, "resumes.zip", zipBytes)
	require.Len(t, resp3.Items, 4,
		"POST 3 (intentB): expected 4 items (1 extracted_from_zip + 3 queued); got %d: %+v",
		len(resp3.Items), resp3.Items)

	assert.Equal(t, "extracted_from_zip", resp3.Items[0].Status,
		"POST 3: first item must be extracted_from_zip")

	for i, item := range resp3.Items[1:] {
		assert.Equal(t, "queued", item.Status,
			"POST 3: item[%d] (%s) must be queued (different intent)", i+1, item.Filename)
		assert.NotEmpty(t, item.UploadID,
			"POST 3: item[%d] (%s) must have an upload_id", i+1, item.Filename)
	}

	// ── DB assertions ─────────────────────────────────────────────────────────
	// 3 unique PDFs × 2 intents = 6 resume_uploads rows (the ZIP parent itself
	// does not create a resume_uploads row — only the extracted child PDFs do).
	ctx := context.Background()

	var uploadCount int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM resume_uploads WHERE tenant_id = $1`,
		tenant.String(),
	).Scan(&uploadCount)
	require.NoError(t, err)
	assert.Equal(t, 6, uploadCount,
		"resume_uploads must have exactly 6 rows (3 PDFs × 2 intents)")

	// 3 unique (tenant, intentA, hash) + 3 unique (tenant, intentB, hash) = 6 dedup rows.
	var dedupCount int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM resume_uploads_dedup WHERE tenant_id = $1`,
		tenant.String(),
	).Scan(&dedupCount)
	require.NoError(t, err)
	assert.Equal(t, 6, dedupCount,
		"resume_uploads_dedup must have exactly 6 rows (3 hashes × 2 intents)")
}
