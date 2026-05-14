//go:build integration

package tests

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	intentevents "github.com/hustle/hireflow/internal/hiringintent/domain/events"
	intentvo "github.com/hustle/hireflow/internal/hiringintent/domain/valueobjects"
	auditinfra "github.com/hustle/hireflow/internal/shared/audit/infrastructure"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/shared/infrastructure/auth"
	"github.com/hustle/hireflow/internal/shared/infrastructure/eventbus"
	sourcingcommands "github.com/hustle/hireflow/internal/sourcing/application/commands"
	sourcingqueries "github.com/hustle/hireflow/internal/sourcing/application/queries"
	v1 "github.com/hustle/hireflow/internal/sourcing/delivery/http/v1"
	sourcingevents "github.com/hustle/hireflow/internal/sourcing/domain/events"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	sourcingclients "github.com/hustle/hireflow/internal/sourcing/infrastructure/clients"
	sourcingembed "github.com/hustle/hireflow/internal/sourcing/infrastructure/embedding"
	sourcingenc "github.com/hustle/hireflow/internal/sourcing/infrastructure/encryption"
	sourcingmsg "github.com/hustle/hireflow/internal/sourcing/infrastructure/messaging"
	sourcingpersist "github.com/hustle/hireflow/internal/sourcing/infrastructure/persistence"
	sourcingscan "github.com/hustle/hireflow/internal/sourcing/infrastructure/scanning"
	sourcingscoring "github.com/hustle/hireflow/internal/sourcing/infrastructure/scoring"
	sourcingsse "github.com/hustle/hireflow/internal/sourcing/infrastructure/sse"
	sourcingstorage "github.com/hustle/hireflow/internal/sourcing/infrastructure/storage"
	sourcingsubs "github.com/hustle/hireflow/internal/sourcing/infrastructure/subscribers"
	sourcingtext "github.com/hustle/hireflow/internal/sourcing/infrastructure/text"
	sourcingworker "github.com/hustle/hireflow/internal/sourcing/infrastructure/worker"
)

// TestSourcingSlice4_E2E exercises the full recruiter lifecycle flow end-to-end:
//
//  1. Setup: insert intent, upload, score (reuse slice-3 wiring with stubs).
//  2. Wait for Application to be Scored (embedding + LLM judge).
//  3. POST /applications/{id}:shortlist → 204; verify status=Shortlisted; verify audit row.
//  4. Subscribe to GET /resumes/batches/{batch_id}/events SSE.
//  5. Publish a fake ResumeUploadAccepted event on the bus → assert SSE delivers item_accepted.
//  6. POST /intents/{id}/applications:rescore → 202; verify llm_judgment NULL'd then re-populated.
//  7. POST /resumes/{upload_id}:retry on a fabricated Failed upload → verify status=Pending.
//  8. DELETE /candidates/{id} → 204; verify cascade; verify CandidateErased event; verify audit.
//  9. GET /candidates/{id} afterwards → 404.
func TestSourcingSlice4_E2E(t *testing.T) {
	pool := newPgvectorPool(t) // skips if DATABASE_URL not set
	logger := zerolog.New(io.Discard)

	// ── Identity ─────────────────────────────────────────────────────────────
	tenant := shared.NewTenantID()
	tenantUUID, err := uuid.Parse(tenant.String())
	require.NoError(t, err)
	recruiterID := shared.NewRecruiterID()
	identity := auth.Identity{TenantID: tenant, RecruiterID: recruiterID}

	// ── Intent ───────────────────────────────────────────────────────────────
	intentID := uuid.New()
	insertHiringIntentForSlice3(t, pool, intentID, tenantUUID)

	// ── Infrastructure ───────────────────────────────────────────────────────
	storageDir := t.TempDir()
	store, err := sourcingstorage.NewLocalFS(storageDir)
	require.NoError(t, err)

	piiEnc, err := sourcingenc.NewLocalDevDEK("0000000000000000000000000000000000000000000000000000000000000000")
	require.NoError(t, err)

	uploadRepo := sourcingpersist.NewPostgresResumeUploadRepository(pool)
	candRepo := sourcingpersist.NewPostgresCandidateRepository(pool)
	appRepo := sourcingpersist.NewPostgresApplicationRepository(pool)
	intentEmbeddingRepo := sourcingpersist.NewPostgresIntentEmbeddingRepository(pool)
	judgeJobRepo := sourcingpersist.NewPostgresJudgeJobRepository(pool)
	intentReader := sourcingclients.NewPostgresIntentReader(pool)

	embedder := sourcingembed.NewStub()
	matchScorer := sourcingscoring.NewInProcMatchScorer()
	judge := stubJudge{}

	// Audit writer (Postgres-backed, load-bearing).
	auditWriter := auditinfra.NewPostgresAuditWriter(pool)

	// ── Command handlers ─────────────────────────────────────────────────────
	uploadH := sourcingcommands.NewUploadResumeBatchHandler(
		uploadRepo, store,
		sourcingcommands.UploadConfig{MaxFileBytes: 10 * 1024 * 1024},
	)
	processH := sourcingcommands.NewProcessUploadHandler(sourcingcommands.ProcessConfig{
		Repo:          uploadRepo,
		Storage:       store,
		Scanner:       sourcingscan.NewNoop(),
		Extractor:     sourcingtext.NewSimple(),
		Parser:        stubParser{},
		OCR:           stubOCR{},
		Encryptor:     piiEnc,
		CandidateRepo: candRepo,
		OCRThreshold:  5,
		RetryBackoff:  []time.Duration{time.Second, 5 * time.Second},
	})

	scoreCandidateH := sourcingcommands.NewScoreCandidateHandler(candRepo, intentReader, appRepo)
	scoreIntentH := sourcingcommands.NewScoreIntentHandler(
		intentReader, appRepo, candRepo, judgeJobRepo,
		sourcingcommands.ScoreIntentConfig{JudgeTopK: 20},
	)
	scoreAppH := sourcingcommands.NewScoreApplicationHandler(
		appRepo, candRepo, intentReader,
		embedder, matchScorer, intentEmbeddingRepo,
		sourcingcommands.ScoreApplicationConfig{
			RetryBackoff: []time.Duration{time.Second, 5 * time.Second},
		},
	)
	judgeAppH := sourcingcommands.NewJudgeApplicationHandler(
		appRepo, candRepo, intentReader,
		judge, judgeJobRepo,
		sourcingcommands.JudgeApplicationConfig{
			RetryBackoff: []time.Duration{time.Second, 5 * time.Second},
		},
	)

	transitionAppH := sourcingcommands.NewTransitionApplicationHandler(appRepo, auditWriter)
	retryH := sourcingcommands.NewRetryResumeUploadHandler(uploadRepo)
	rescoreH := sourcingcommands.NewRescoreIntentHandler(appRepo, scoreIntentH, auditWriter)

	// ── Event bus ────────────────────────────────────────────────────────────
	bus := eventbus.NewInMemory(logger)

	eraseH := sourcingcommands.NewEraseCandidateHandler(candRepo, store, auditWriter, bus, logger)

	// ── Query handlers ───────────────────────────────────────────────────────
	statusH := sourcingqueries.NewGetBatchStatusHandler(uploadRepo)
	listAppH := sourcingqueries.NewListApplicationsHandler(appRepo, candRepo, piiEnc)
	candidateH := sourcingqueries.NewGetCandidateHandler(candRepo, piiEnc, auditWriter)

	// ── SSE fanout ───────────────────────────────────────────────────────────
	batchFanout := sourcingsse.NewBatchEventFanout(logger)
	bus.Subscribe("sourcing.ResumeUploadAccepted", batchFanout.OnEvent)
	bus.Subscribe("sourcing.ResumeUploadFailed", batchFanout.OnEvent)
	bus.Subscribe("sourcing.ResumeExtracted", batchFanout.OnEvent)
	bus.Subscribe("sourcing.ResumeParsed", batchFanout.OnEvent)

	// ── Event bus consumers ──────────────────────────────────────────────────
	bus.Subscribe("hiringintent.IntentConfirmed",
		sourcingsubs.NewIntentConfirmedConsumer(scoreIntentH, logger).Handle,
	)
	bus.Subscribe("sourcing.CandidateParsed",
		sourcingsubs.NewCandidateParsedConsumer(scoreCandidateH, logger).Handle,
	)

	// ── HTTP router ──────────────────────────────────────────────────────────
	sourcingH := v1.NewSourcingHandler(
		uploadH, statusH, candidateH, listAppH,
		transitionAppH, retryH, rescoreH, eraseH,
		batchFanout, 50*time.Millisecond, // short heartbeat for fast SSE test
		logger,
	)
	router := chi.NewRouter()
	// Inject identity for every request so SSE stream and all handlers see it.
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), identity)))
		})
	})
	v1.Mount(router, sourcingH)

	// ── Outbox dispatcher + worker pools ─────────────────────────────────────
	pub := sourcingmsg.NewBusPublisher(bus)
	dispatcher := sourcingmsg.NewOutboxDispatcher(pool, pub, logger,
		sourcingmsg.DispatcherConfig{PollInterval: 100 * time.Millisecond},
	)
	fastCfg := sourcingworker.Config{Size: 1, PollInterval: 100 * time.Millisecond}
	uploadPool := sourcingworker.NewPool(uploadRepo, processH, fastCfg, logger)
	matchPool := sourcingworker.NewMatchPool(appRepo, scoreAppH, fastCfg, logger)
	judgePool := sourcingworker.NewJudgePool(judgeJobRepo, judgeAppH, fastCfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Run(ctx)
	go uploadPool.Run(ctx)
	go matchPool.Run(ctx)
	go judgePool.Run(ctx)

	// ── Step 1: Upload a resume via HTTP ─────────────────────────────────────
	body, ct := writeMultipart(t, map[string][]byte{"alice.pdf": helloPDFBytes(t)})
	req := httptest.NewRequest(http.MethodPost,
		"/intents/"+intentID.String()+"/resumes:batch", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var upResp v1.BatchUploadResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&upResp))
	require.Len(t, upResp.Items, 1)
	require.Equal(t, "queued", upResp.Items[0].Status)

	batchID, err := uuid.Parse(upResp.BatchID)
	require.NoError(t, err)

	// ── Step 2: Wait for Application to reach Scored (embedding + judge) ─────
	// First, wait for Parsed status so we know the candidate exists.
	deadline := time.Now().Add(30 * time.Second)
	for {
		statusReq := httptest.NewRequest(http.MethodGet, "/resumes/batches/"+upResp.BatchID, nil)
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

	// Wait for Application to reach Scored (embedding_score populated).
	deadline = time.Now().Add(30 * time.Second)
	for {
		appResp := getApplications(t, router, tenant, intentID)
		for _, item := range appResp.Items {
			if item.Status == "Scored" && item.Score.EmbeddingScore != nil {
				goto scoredFound4
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for Application to reach Scored; applications: %+v",
				getApplications(t, router, tenant, intentID))
		}
		time.Sleep(200 * time.Millisecond)
	}
scoredFound4:

	// Fire IntentConfirmed so ScoreIntent enqueues a JudgeJob.
	intentIDVO, err := intentvo.ParseIntentID(intentID.String())
	require.NoError(t, err)
	confirmedEvent := intentevents.NewIntentConfirmed(
		intentIDVO, tenant, shared.NewRecruiterID(),
		intentvo.PriorityMedium, time.Now().UTC(),
	)
	require.NoError(t, bus.Publish(ctx, "hiringintent.IntentConfirmed", confirmedEvent))

	// Wait for judge worker to populate overall_score=87.
	deadline = time.Now().Add(45 * time.Second)
	var judgedApp *v1.ApplicationListItem
	for {
		appResp := getApplications(t, router, tenant, intentID)
		for i := range appResp.Items {
			it := &appResp.Items[i]
			if it.Status == "Scored" && it.Score.Overall != nil && *it.Score.Overall == 87 {
				judgedApp = it
				goto judgedFound4
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for judged application (overall_score=87); apps: %+v",
				getApplications(t, router, tenant, intentID))
		}
		time.Sleep(200 * time.Millisecond)
	}
judgedFound4:
	require.NotNil(t, judgedApp, "must have a judged application")
	applicationID, err := uuid.Parse(judgedApp.ApplicationID)
	require.NoError(t, err)
	candidateID, err := uuid.Parse(judgedApp.Candidate.ID)
	require.NoError(t, err)

	// ── Step 3: Shortlist the application ────────────────────────────────────
	shortlistReq := httptest.NewRequest(http.MethodPost,
		"/applications/"+applicationID.String()+":shortlist", nil)
	shortlistRec := httptest.NewRecorder()
	router.ServeHTTP(shortlistRec, shortlistReq)
	require.Equal(t, http.StatusNoContent, shortlistRec.Code,
		"shortlist must return 204: %s", shortlistRec.Body.String())

	// Verify status=Shortlisted via list endpoint.
	deadline = time.Now().Add(10 * time.Second)
	for {
		appResp := getApplications(t, router, tenant, intentID)
		for _, item := range appResp.Items {
			if item.ApplicationID == applicationID.String() && item.Status == "Shortlisted" {
				goto shortlistedVerified
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for Shortlisted status in list endpoint")
		}
		time.Sleep(100 * time.Millisecond)
	}
shortlistedVerified:

	// Verify audit row for shortlist.
	assertAuditRow(t, pool, tenant, "application_shortlist", applicationID)

	// ── Step 4+5: SSE — subscribe and publish a fake event ───────────────────
	// Use httptest.NewServer so we get a real HTTP connection that supports
	// streaming (httptest.NewRecorder does not flush SSE properly).
	srv := httptest.NewServer(router)
	defer srv.Close()

	sseCtx, sseCancel := context.WithTimeout(ctx, 10*time.Second)
	defer sseCancel()

	sseDone := make(chan string, 1) // will receive the first event: line seen
	go func() {
		sseURL := srv.URL + "/resumes/batches/" + batchID.String() + "/events"
		sseReq, err2 := http.NewRequestWithContext(sseCtx, http.MethodGet, sseURL, nil)
		if err2 != nil {
			sseDone <- ""
			return
		}
		resp, err2 := http.DefaultClient.Do(sseReq)
		if err2 != nil {
			sseDone <- ""
			return
		}
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event:") {
				sseDone <- strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				return
			}
		}
		sseDone <- ""
	}()

	// Give the SSE goroutine time to connect and register with the fanout.
	time.Sleep(150 * time.Millisecond)

	// Publish a fake ResumeUploadAccepted event directly on the bus to trigger
	// the fanout (bypasses the full upload pipeline, keeping the test deterministic).
	fakeUploadID := uuid.New()
	fakeAccepted := sourcingevents.ResumeUploadAccepted{
		UploadID:    fakeUploadID,
		TenantID:    tenant,
		IntentID:    intentID,
		BatchID:     batchID,
		ContentHash: "fakehash",
		OccurredAt:  time.Now().UTC(),
	}
	require.NoError(t, bus.Publish(ctx, "sourcing.ResumeUploadAccepted", fakeAccepted))

	// Assert SSE event arrives within 5 s.
	select {
	case eventType := <-sseDone:
		assert.Equal(t, "item_accepted", eventType,
			"SSE stream must deliver item_accepted for ResumeUploadAccepted")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for SSE item_accepted event")
	}
	sseCancel()

	// ── Step 6: Rescore intent ────────────────────────────────────────────────
	rescoreReq := httptest.NewRequest(http.MethodPost,
		"/intents/"+intentID.String()+"/applications:rescore", nil)
	rescoreRec := httptest.NewRecorder()
	router.ServeHTTP(rescoreRec, rescoreReq)
	require.Equal(t, http.StatusAccepted, rescoreRec.Code,
		"rescore must return 202: %s", rescoreRec.Body.String())

	// Immediately after rescore: verify llm_judgment is NULL in DB.
	var nullCount int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM applications WHERE tenant_id=$1 AND intent_id=$2 AND llm_judgment IS NULL`,
		tenant.String(), intentID,
	).Scan(&nullCount)
	require.NoError(t, err)
	assert.Greater(t, nullCount, 0, "llm_judgment must be NULL'd immediately after rescore")

	// Wait for judge worker to re-populate overall_score.
	deadline = time.Now().Add(45 * time.Second)
	for {
		appResp := getApplications(t, router, tenant, intentID)
		for _, item := range appResp.Items {
			if item.ApplicationID == applicationID.String() &&
				item.Score.Overall != nil && *item.Score.Overall == 87 {
				goto rescoreJudged
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for overall_score to be re-populated after rescore")
		}
		time.Sleep(200 * time.Millisecond)
	}
rescoreJudged:

	// Verify audit row for rescore.
	assertAuditRow(t, pool, tenant, "intent_rescored", intentID)

	// ── Step 7: Retry a fabricated Failed upload ──────────────────────────────
	// Insert a new upload row in Failed status directly via SQL. The domain
	// entity disallows MarkFailed from a terminal state (Parsed), so we insert
	// fresh with status=Failed to satisfy the retry pre-condition.
	failedUploadID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO resume_uploads (
			id, tenant_id, intent_id, batch_id, candidate_id,
			storage_key, original_name, mime_type, size_bytes, content_hash,
			status, attempt_count, last_error, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, NULL,
			'test/failed.pdf', 'failed.pdf', 'application/pdf', 1024, $5,
			'Failed', 1, '{"reason":"e2e_force_fail","detail":"injected by e2e test"}',
			now(), now()
		)
	`, failedUploadID, tenant.String(), intentID, batchID, func() string {
		sum := sha256.Sum256([]byte("failed-upload:" + failedUploadID.String()))
		return hex.EncodeToString(sum[:])
	}())
	require.NoError(t, err, "failed to insert fabricated Failed upload")

	// POST :retry
	retryReq := httptest.NewRequest(http.MethodPost,
		"/resumes/"+failedUploadID.String()+":retry", nil)
	retryRec := httptest.NewRecorder()
	router.ServeHTTP(retryRec, retryReq)
	require.Equal(t, http.StatusNoContent, retryRec.Code,
		"retry must return 204: %s", retryRec.Body.String())

	// Verify the upload is now Pending (quickly, before worker re-claims it).
	retried, err := uploadRepo.FindByID(ctx, tenant, failedUploadID)
	require.NoError(t, err)
	assert.Equal(t, string(vo.StatusPending), string(retried.Status()),
		"upload must be Pending after retry")

	// ── Step 8: Erase the candidate ──────────────────────────────────────────
	// Register a subscriber to capture the CandidateErased event before issuing
	// the DELETE so we don't miss it.
	erasedCh := make(chan sourcingevents.CandidateErased, 1)
	bus.Subscribe("sourcing.CandidateErased", func(_ context.Context, ev any) error {
		if e, ok := ev.(sourcingevents.CandidateErased); ok {
			select {
			case erasedCh <- e:
			default:
			}
		}
		return nil
	})

	// Verify candidate + application rows exist before erase.
	var preEraseCandCount int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM candidates WHERE tenant_id=$1 AND id=$2`,
		tenant.String(), candidateID,
	).Scan(&preEraseCandCount)
	require.NoError(t, err)
	require.Equal(t, 1, preEraseCandCount, "candidate must exist before erase")

	var preEraseAppCount int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM applications WHERE tenant_id=$1 AND candidate_id=$2`,
		tenant.String(), candidateID,
	).Scan(&preEraseAppCount)
	require.NoError(t, err)
	require.Greater(t, preEraseAppCount, 0, "application(s) must exist before erase")

	// Issue DELETE /candidates/{id}.
	eraseReq := httptest.NewRequest(http.MethodDelete, "/candidates/"+candidateID.String(), nil)
	eraseRec := httptest.NewRecorder()
	router.ServeHTTP(eraseRec, eraseReq)
	require.Equal(t, http.StatusNoContent, eraseRec.Code,
		"erase must return 204: %s", eraseRec.Body.String())

	// Verify cascade: candidate gone.
	var postEraseCandCount int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM candidates WHERE tenant_id=$1 AND id=$2`,
		tenant.String(), candidateID,
	).Scan(&postEraseCandCount)
	require.NoError(t, err)
	assert.Equal(t, 0, postEraseCandCount, "candidate must be gone after erase")

	// Verify cascade: applications gone.
	var postEraseAppCount int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM applications WHERE tenant_id=$1 AND candidate_id=$2`,
		tenant.String(), candidateID,
	).Scan(&postEraseAppCount)
	require.NoError(t, err)
	assert.Equal(t, 0, postEraseAppCount, "applications must be cascade-deleted after erase")

	// Verify CandidateErased event was published on the bus.
	select {
	case erasedEv := <-erasedCh:
		assert.Equal(t, candidateID, erasedEv.CandidateID,
			"CandidateErased event must carry the correct candidate_id")
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for CandidateErased event on bus")
	}

	// Verify audit row for erase.
	assertAuditRow(t, pool, tenant, "candidate_erased", candidateID)

	// ── Step 9: GET /candidates/{id} returns 404 after erasure ───────────────
	getAfterEraseReq := httptest.NewRequest(http.MethodGet, "/candidates/"+candidateID.String(), nil)
	getAfterEraseRec := httptest.NewRecorder()
	router.ServeHTTP(getAfterEraseRec, getAfterEraseReq)
	assert.Equal(t, http.StatusNotFound, getAfterEraseRec.Code,
		"GET /candidates/{id} must return 404 after erasure; body: %s",
		getAfterEraseRec.Body.String())

	// No audit row should exist for this 404 read (spec: only audit successful reads).
	var readAuditCount int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND action='candidate_read' AND resource_id=$2`,
		tenant.String(), candidateID,
	).Scan(&readAuditCount)
	require.NoError(t, err)
	assert.Equal(t, 0, readAuditCount,
		"a 404 candidate read must NOT produce an audit row")
}

// assertAuditRow is a test helper that asserts at least one audit_log row exists
// for the given (tenant, action, resource_id) triple.
func assertAuditRow(t *testing.T, pool *pgxpool.Pool, tenant shared.TenantID, action string, resourceID uuid.UUID) {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND action=$2 AND resource_id=$3`,
		tenant.String(), action, resourceID,
	).Scan(&count)
	require.NoError(t, err, "assertAuditRow: query failed for action=%s resource=%s", action, resourceID)
	assert.GreaterOrEqual(t, count, 1,
		"must have at least one audit_log row for action=%s resource=%s", action, resourceID)
}
