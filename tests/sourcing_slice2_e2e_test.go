//go:build integration

package tests

import (
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
	"github.com/hustle/hireflow/internal/shared/infrastructure/eventbus"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/application/queries"
	v1 "github.com/hustle/hireflow/internal/sourcing/delivery/http/v1"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/encryption"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/messaging"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/persistence"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/scanning"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/storage"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/text"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/worker"
)

// stubParser produces a fixed ParsedProfile regardless of input. The slice-1
// extracted text from testdata/hello.pdf says "hello world" — that's not a
// real resume, so we don't want to send it to Claude. The stub returns a
// canned profile so the test verifies the pipeline glue, not the LLM.
type stubParser struct{}

func (stubParser) Parse(_ context.Context, _ string) (vo.ParsedProfile, error) {
	p := vo.NewParsedProfile()
	p.Personal.FullName = "Alice (test)"
	p.Personal.Email = "alice@test.example"
	p.Personal.Phone = "+91-000-0000"
	p.Personal.Location = "Bangalore"
	p.Headline = "Senior Backend Engineer (test)"
	p.Skills = []vo.ParsedSkill{{Name: "Go", Years: 5}}
	return p, nil
}

// stubOCR is wired into the pipeline for completeness but is not exercised
// because hello.pdf has enough extractable text to exceed the OCR threshold.
type stubOCR struct{}

func (stubOCR) ExtractFromBytes(_ context.Context, _ []byte, _ string) (services.RawText, error) {
	return services.RawText{Text: "fallback text", PageCount: 1}, nil
}

// TestSourcingSlice2_E2E exercises the full slice-1+2 pipeline:
//
//	upload → scan → extract → parse → Candidate created
//	→ GET /candidates/{id} returns the decrypted profile.
func TestSourcingSlice2_E2E(t *testing.T) {
	pool := newPool(t) // skips if DATABASE_URL unset
	logger := zerolog.New(io.Discard)

	storageDir := t.TempDir()
	store, err := storage.NewLocalFS(storageDir)
	require.NoError(t, err)

	uploadRepo := persistence.NewPostgresResumeUploadRepository(pool)
	candRepo := persistence.NewPostgresCandidateRepository(pool)

	piiEnc, err := encryption.NewLocalDevDEK("0000000000000000000000000000000000000000000000000000000000000000")
	require.NoError(t, err)

	uploadH := commands.NewUploadResumeBatchHandler(uploadRepo, store, commands.UploadConfig{MaxFileBytes: 10 * 1024 * 1024})
	processH := commands.NewProcessUploadHandler(commands.ProcessConfig{
		Repo:          uploadRepo,
		Storage:       store,
		Scanner:       scanning.NewNoop(),
		Extractor:     text.NewSimple(),
		Parser:        stubParser{},
		OCR:           stubOCR{},
		Encryptor:     piiEnc,
		CandidateRepo: candRepo,
		// hello.pdf extracts to ~"hello world\n" (~12 chars). Set the OCR
		// threshold below that so the test exercises the text path, not OCR
		// fallback. Production default is 50.
		OCRThreshold:  5,
		RetryBackoff:  []time.Duration{time.Second, 5 * time.Second},
	})
	statusH := queries.NewGetBatchStatusHandler(uploadRepo)
	candH := queries.NewGetCandidateHandler(candRepo, piiEnc)
	handler := v1.NewSourcingHandler(uploadH, statusH, candH, nil, nil, logger)

	router := chi.NewRouter()
	v1.Mount(router, handler)

	bus := eventbus.NewInMemory(logger)
	pub := messaging.NewBusPublisher(bus)
	dispatcher := messaging.NewOutboxDispatcher(pool, pub, logger, messaging.DispatcherConfig{PollInterval: 100 * time.Millisecond})
	workerPool := worker.NewPool(uploadRepo, processH, worker.Config{Size: 1, PollInterval: 100 * time.Millisecond}, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Run(ctx)
	go workerPool.Run(ctx)

	// Upload one PDF via the HTTP layer.
	body, ct := writeMultipart(t, map[string][]byte{"alice.pdf": helloPDFBytes(t)})
	tenant := shared.NewTenantID()
	req := httptest.NewRequest(http.MethodPost, "/intents/"+uuid.New().String()+"/resumes:batch", body)
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

	// Poll the status endpoint until the row reaches Parsed (slice-2 terminal).
	deadline := time.Now().Add(30 * time.Second)
	for {
		statusReq := httptest.NewRequest(http.MethodGet, "/resumes/batches/"+upResp.BatchID, nil)
		statusReq = statusReq.WithContext(auth.WithIdentity(statusReq.Context(), auth.Identity{
			TenantID:    tenant,
			RecruiterID: shared.NewRecruiterID(),
		}))
		statusRec := httptest.NewRecorder()
		router.ServeHTTP(statusRec, statusReq)
		require.Equal(t, http.StatusOK, statusRec.Code)

		var s v1.BatchStatusResponse
		require.NoError(t, json.NewDecoder(statusRec.Body).Decode(&s))

		if s.Summary.Total > 0 && s.Items[0].Status == string(vo.StatusParsed) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for Parsed status; got %+v", s)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Look up the Candidate by content hash — avoids requiring the API to expose
	// candidate_id on the batch status surface.
	hash := vo.ComputeContentHash(helloPDFBytes(t))
	cand, err := candRepo.FindByContentHash(context.Background(), tenant, hash.String())
	require.NoError(t, err, "candidate must exist after Parsed status")
	candidateID := cand.ID().String()

	// Hit GET /candidates/{id} and verify the decrypted PII round-trips correctly.
	candReq := httptest.NewRequest(http.MethodGet, "/candidates/"+candidateID, nil)
	candReq = candReq.WithContext(auth.WithIdentity(candReq.Context(), auth.Identity{
		TenantID:    tenant,
		RecruiterID: shared.NewRecruiterID(),
	}))
	candRec := httptest.NewRecorder()
	router.ServeHTTP(candRec, candReq)
	require.Equal(t, http.StatusOK, candRec.Code, candRec.Body.String())

	var candResp v1.CandidateDetailResponse
	require.NoError(t, json.NewDecoder(candRec.Body).Decode(&candResp))
	assert.Equal(t, "Alice (test)", candResp.Personal.FullName, "PII must be decrypted in response")
	assert.Equal(t, "alice@test.example", candResp.Personal.Email)
	assert.Equal(t, "Bangalore", candResp.Location)
}
