//go:build integration

package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/shared/infrastructure/auth"
	"github.com/hustle/hireflow/internal/shared/infrastructure/eventbus"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/application/queries"
	v1 "github.com/hustle/hireflow/internal/sourcing/delivery/http/v1"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/messaging"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/persistence"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/scanning"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/storage"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/text"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/worker"
)

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	p, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(p.Close)
	// Per-test isolation: drop all sourcing+hiringintent rows so e2e tests
	// don't see each other's data.
	_, err = p.Exec(context.Background(), `
		TRUNCATE applications, hiring_intent_embeddings, judge_jobs,
		         resume_uploads, resume_uploads_dedup, candidates,
		         sourcing_outbox, hiring_intents, audit_log CASCADE`)
	require.NoError(t, err)
	return p
}

func helloPDFBytes(t *testing.T) []byte {
	t.Helper()
	// Locate the fixture relative to repo root.
	wd, _ := os.Getwd()
	root := wd
	for {
		_, err := os.Stat(filepath.Join(root, "go.mod"))
		if err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("go.mod not found")
		}
		root = parent
	}
	path := filepath.Join(root, "internal", "sourcing", "infrastructure", "text", "testdata", "hello.pdf")
	b, err := os.ReadFile(path)
	require.NoError(t, err, "fixture: %s", path)
	return b
}

func writeMultipart(t *testing.T, files map[string][]byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	for name, data := range files {
		w, err := mw.CreateFormFile("resume", name)
		require.NoError(t, err)
		_, err = w.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, mw.Close())
	return body, mw.FormDataContentType()
}

// TestSourcingSlice1_E2E_UploadScansExtracts is the slice-1 happy path:
//   - HR uploads a hello.pdf via POST :batch
//   - Worker scans (noop=clean) + extracts text
//   - Status endpoint reports Extracted=1
func TestSourcingSlice1_E2E_UploadScansExtracts(t *testing.T) {
	pool := newPool(t)
	logger := zerolog.New(io.Discard)

	storageDir := t.TempDir()
	store, err := storage.NewLocalFS(storageDir)
	require.NoError(t, err)

	repo := persistence.NewPostgresResumeUploadRepository(pool)
	uploadH := commands.NewUploadResumeBatchHandler(repo, store, commands.UploadConfig{MaxFileBytes: 10 * 1024 * 1024})
	processH := commands.NewProcessUploadHandler(commands.ProcessConfig{
		Repo:         repo,
		Storage:      store,
		Scanner:      scanning.NewNoop(),
		Extractor:    text.NewSimple(),
		RetryBackoff: []time.Duration{time.Second, 5 * time.Second},
	})
	statusH := queries.NewGetBatchStatusHandler(repo)
	handler := v1.NewSourcingHandler(v1.SourcingHandlerDeps{Upload: uploadH, Status: statusH, Logger: logger})

	router := chi.NewRouter()
	v1.Mount(router, handler)

	bus := eventbus.NewInMemory(logger)
	pub := messaging.NewBusPublisher(bus)
	dispatcher := messaging.NewOutboxDispatcher(pool, pub, logger, messaging.DispatcherConfig{PollInterval: 100 * time.Millisecond})
	pool2 := worker.NewPool(repo, processH, worker.Config{Size: 1, PollInterval: 100 * time.Millisecond}, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Run(ctx)
	go pool2.Run(ctx)

	// Upload one PDF via the HTTP layer.
	body, ct := writeMultipart(t, map[string][]byte{"alice.pdf": helloPDFBytes(t)})
	tenant := shared.NewTenantID()
	req := httptest.NewRequest(http.MethodPost,
		"/intents/"+uuid.New().String()+"/resumes:batch", body)
	req.Header.Set("Content-Type", ct)
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{
		TenantID:    tenant,
		RecruiterID: shared.NewRecruiterID(),
	}))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var upResp v1.BatchUploadResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&upResp))
	require.Len(t, upResp.Items, 1)
	require.Equal(t, "queued", upResp.Items[0].Status)

	// Poll the status endpoint until the row reaches Extracted (or timeout).
	deadline := time.Now().Add(30 * time.Second)
	for {
		statusReq := httptest.NewRequest(http.MethodGet,
			"/resumes/batches/"+upResp.BatchID, nil)
		statusReq = statusReq.WithContext(auth.WithIdentity(statusReq.Context(), auth.Identity{
			TenantID:    tenant,
			RecruiterID: shared.NewRecruiterID(),
		}))
		statusRec := httptest.NewRecorder()
		router.ServeHTTP(statusRec, statusReq)
		require.Equal(t, http.StatusOK, statusRec.Code)
		var s v1.BatchStatusResponse
		require.NoError(t, json.NewDecoder(statusRec.Body).Decode(&s))

		if s.Summary.Extracted == 1 {
			// Final assertions.
			assert.Equal(t, 1, s.Summary.Total)
			assert.Equal(t, 0, s.Summary.Failed)
			assert.Equal(t, 0, s.Summary.InFlight)
			// Verify the storage adapter actually wrote the file.
			var found bool
			_ = filepath.WalkDir(storageDir, func(_ string, d fs.DirEntry, _ error) error {
				if d != nil && !d.IsDir() {
					found = true
				}
				return nil
			})
			assert.True(t, found, "storage dir must contain the uploaded file")
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for extraction; status: %+v", s)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
