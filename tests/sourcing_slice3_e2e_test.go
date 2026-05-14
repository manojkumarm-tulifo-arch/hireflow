//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvectorpgx "github.com/pgvector/pgvector-go/pgx"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	intentevents "github.com/hustle/hireflow/internal/hiringintent/domain/events"
	intentvo "github.com/hustle/hireflow/internal/hiringintent/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/shared/infrastructure/auth"
	"github.com/hustle/hireflow/internal/shared/infrastructure/eventbus"
	sourcingcommands "github.com/hustle/hireflow/internal/sourcing/application/commands"
	sourcingqueries "github.com/hustle/hireflow/internal/sourcing/application/queries"
	v1 "github.com/hustle/hireflow/internal/sourcing/delivery/http/v1"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	sourcingclients "github.com/hustle/hireflow/internal/sourcing/infrastructure/clients"
	sourcingenc "github.com/hustle/hireflow/internal/sourcing/infrastructure/encryption"
	sourcingembed "github.com/hustle/hireflow/internal/sourcing/infrastructure/embedding"
	sourcingmsg "github.com/hustle/hireflow/internal/sourcing/infrastructure/messaging"
	sourcingpersist "github.com/hustle/hireflow/internal/sourcing/infrastructure/persistence"
	sourcingscan "github.com/hustle/hireflow/internal/sourcing/infrastructure/scanning"
	sourcingscoring "github.com/hustle/hireflow/internal/sourcing/infrastructure/scoring"
	sourcingstorage "github.com/hustle/hireflow/internal/sourcing/infrastructure/storage"
	sourcingsubs "github.com/hustle/hireflow/internal/sourcing/infrastructure/subscribers"
	sourcingtext "github.com/hustle/hireflow/internal/sourcing/infrastructure/text"
	sourcingworker "github.com/hustle/hireflow/internal/sourcing/infrastructure/worker"
)

// stubJudge always returns a canned LLMJudgment with score=87.
type stubJudge struct{}

func (stubJudge) Judge(_ context.Context, _ vo.ParsedProfile, _ services.RoleSpec, _ vo.RuleMatchReport) (vo.LLMJudgment, error) {
	return vo.LLMJudgment{
		Score: 87,
		Evidence: []vo.JudgmentEvidence{
			{Kind: "skill", Skill: "Go", Claim: "5y", Support: "Senior Backend at Razorpay 2020-2025"},
		},
		Summary:       "Strong match (e2e stub)",
		Concerns:      []string{},
		PromptVersion: "v1-e2e-stub",
	}, nil
}

// newPgvectorPool creates a pgxpool with pgvector type codecs registered via
// AfterConnect. This mirrors the setup in cmd/api/main.go and is required for
// reading vector(1024) columns back via pgx's binary protocol. Skips if
// DATABASE_URL is not set.
func newPgvectorPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	cfg, err := pgxpool.ParseConfig(dbURL)
	require.NoError(t, err)
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgvectorpgx.RegisterTypes(ctx, conn)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	// Per-test isolation: drop all sourcing+hiringintent rows so e2e tests
	// don't see each other's data.
	_, err = pool.Exec(context.Background(), `
		TRUNCATE applications, hiring_intent_embeddings, judge_jobs,
		         resume_uploads, resume_uploads_dedup, candidates,
		         sourcing_outbox, hiring_intents, audit_log CASCADE`)
	require.NoError(t, err)
	return pool
}

// insertHiringIntentForSlice3 inserts a confirmed hiring_intents row with a
// RoleSpec containing a required "Go" skill that matches the stubParser profile.
// The JSONB layout mirrors what the IntentReader (clients/intent_reader.go) expects.
func insertHiringIntentForSlice3(t *testing.T, pool *pgxpool.Pool, intentID, tenantID uuid.UUID) {
	t.Helper()
	const roleJSON = `{
		"title": "Senior Backend Engineer",
		"skills": [
			{"name": "Go", "required": true}
		],
		"experience": {"min": 0, "max": 10},
		"headcount": 1,
		"locations": ["Bangalore"],
		"work_mode": "hybrid"
	}`
	_, err := pool.Exec(context.Background(), `
		INSERT INTO hiring_intents (
			id, tenant_id, recruiter_id, role, priority,
			intent_signals, trust_signals, budget,
			reason, team, reports_to,
			status, created_at, updated_at, cancel_reason
		) VALUES ($1, $2, $3, $4::jsonb, 'MEDIUM',
		          '[]'::jsonb, '[]'::jsonb, NULL,
		          '', '', '',
		          'CONFIRMED', now(), now(), '')
	`, intentID, tenantID, uuid.New(), roleJSON)
	require.NoError(t, err)
}

// TestSourcingSlice3_E2E exercises the full Candidate × Intent scoring pipeline:
//
//  1. Insert a confirmed HiringIntent directly into hiring_intents.
//  2. Upload a resume via the HTTP endpoint.
//  3. Upload worker processes scan→extract→parse→Candidate created.
//  4. Outbox dispatcher publishes sourcing.CandidateParsed.
//  5. CandidateParsedConsumer → ScoreCandidate creates Application(New).
//  6. Match worker picks up Application(New), embeds (stub) + rule+cosine →
//     Application(Scored).
//  7. Test fires IntentConfirmed directly on the bus once the Application is
//     Scored so that ScoreIntent's TopByCoarseScoreForIntent finds it and
//     enqueues a JudgeJob.
//  8. Judge worker picks up JudgeJob, calls the stub judge (score=87), records
//     the LLM judgment on the Application.
//  9. GET /api/v1/intents/{id}/applications returns the ranked list.
// 10. Assertions: overall_score=87, status=Scored, masked name, rule_match populated.
func TestSourcingSlice3_E2E(t *testing.T) {
	pool := newPgvectorPool(t) // skips if DATABASE_URL not set
	logger := zerolog.New(io.Discard)

	// Step 1 — insert a confirmed hiring intent into Postgres.
	tenant := shared.NewTenantID()
	tenantUUID, err := uuid.Parse(tenant.String())
	require.NoError(t, err)
	intentID := uuid.New()
	insertHiringIntentForSlice3(t, pool, intentID, tenantUUID)

	// Step 2 — wire infrastructure (same as cmd/api/main.go, but stubs for
	// embedder + judge, fast poll intervals, in-memory storage).
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

	uploadH := sourcingcommands.NewUploadResumeBatchHandler(
		uploadRepo, store,
		sourcingcommands.UploadConfig{MaxFileBytes: 10 * 1024 * 1024},
	)
	processH := sourcingcommands.NewProcessUploadHandler(sourcingcommands.ProcessConfig{
		Repo:          uploadRepo,
		Storage:       store,
		Scanner:       sourcingscan.NewNoop(),
		Extractor:     sourcingtext.NewSimple(),
		Parser:        stubParser{},  // reused from sourcing_slice2_e2e_test.go
		OCR:           stubOCR{},     // reused from sourcing_slice2_e2e_test.go
		Encryptor:     piiEnc,
		CandidateRepo: candRepo,
		OCRThreshold:  5,
		RetryBackoff:  []time.Duration{time.Second, 5 * time.Second},
	})
	statusH := sourcingqueries.NewGetBatchStatusHandler(uploadRepo)

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

	listAppH := sourcingqueries.NewListApplicationsHandler(appRepo, candRepo, piiEnc)
	sourcingH := v1.NewSourcingHandler(uploadH, statusH, nil, listAppH, nil, logger)

	router := chi.NewRouter()
	v1.Mount(router, sourcingH)

	// Wire the event bus, outbox dispatcher, and event consumers.
	bus := eventbus.NewInMemory(logger)
	pub := sourcingmsg.NewBusPublisher(bus)
	dispatcher := sourcingmsg.NewOutboxDispatcher(pool, pub, logger,
		sourcingmsg.DispatcherConfig{PollInterval: 100 * time.Millisecond},
	)

	bus.Subscribe("hiringintent.IntentConfirmed",
		sourcingsubs.NewIntentConfirmedConsumer(scoreIntentH, logger).Handle,
	)
	bus.Subscribe("sourcing.CandidateParsed",
		sourcingsubs.NewCandidateParsedConsumer(scoreCandidateH, logger).Handle,
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

	// Step 3 — upload a resume via the HTTP layer.
	body, ct := writeMultipart(t, map[string][]byte{"alice.pdf": helloPDFBytes(t)})
	req := httptest.NewRequest(http.MethodPost,
		"/intents/"+intentID.String()+"/resumes:batch", body)
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

	// Step 4 — wait for upload to reach Parsed status (slice-2 terminal).
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
		if s.Summary.Total > 0 && s.Items[0].Status == string(vo.StatusParsed) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for Parsed status; got %+v", s)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Step 5 — wait for the match worker to score the Application.
	// Pipeline: CandidateParsed (outbox) → ScoreCandidate → Application(New) →
	// match worker → Application(Scored with embedding_score).
	// We poll the applications endpoint until we see a Scored row.
	deadline = time.Now().Add(30 * time.Second)
	for {
		appResp := getApplications(t, router, tenant, intentID)
		for _, item := range appResp.Items {
			if item.Status == "Scored" && item.Score.EmbeddingScore != nil {
				goto scoredFound
			}
		}
		if time.Now().After(deadline) {
			appResp2 := getApplications(t, router, tenant, intentID)
			t.Fatalf("timed out waiting for Application to reach Scored status; applications: %+v", appResp2)
		}
		time.Sleep(200 * time.Millisecond)
	}
scoredFound:

	// Step 6 — fire IntentConfirmed on the bus now that there is at least one
	// Scored Application. ScoreIntentHandler.Handle calls
	// TopByCoarseScoreForIntent which returns scored apps and enqueues JudgeJobs.
	intentIDVO, err := intentvo.ParseIntentID(intentID.String())
	require.NoError(t, err)
	confirmedEvent := intentevents.NewIntentConfirmed(
		intentIDVO,
		tenant,
		shared.NewRecruiterID(),
		intentvo.PriorityMedium,
		time.Now().UTC(),
	)
	require.NoError(t, bus.Publish(ctx, "hiringintent.IntentConfirmed", confirmedEvent))

	// Step 7 — poll until overall_score is populated by the judge worker.
	// Pipeline: JudgeJob created by ScoreIntent → judge worker → stubJudge returns
	// score=87 → Application.RecordLLMJudgment → overall_score=87.
	deadline = time.Now().Add(45 * time.Second)
	var finalResp v1.ApplicationListResponse
	for {
		finalResp = getApplications(t, router, tenant, intentID)
		for _, item := range finalResp.Items {
			if item.Status == "Scored" && item.Score.Overall != nil && *item.Score.Overall == 87 {
				goto judgedFound
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for judged Application (overall_score=87); applications: %+v", finalResp)
		}
		time.Sleep(200 * time.Millisecond)
	}
judgedFound:

	// Step 8 — final assertions.
	require.GreaterOrEqual(t, len(finalResp.Items), 1, "must have at least one application")

	var judgedItem *v1.ApplicationListItem
	for i := range finalResp.Items {
		it := &finalResp.Items[i]
		if it.Status == "Scored" && it.Score.Overall != nil && *it.Score.Overall == 87 {
			judgedItem = it
			break
		}
	}
	require.NotNil(t, judgedItem, "must find the judged application with overall_score=87")

	// overall_score must be exactly 87 (the stub judge's canned integer value).
	assert.Equal(t, 87.0, *judgedItem.Score.Overall, "overall_score must be 87")

	// score_band must be "strong" (87 >= 80 per DeriveBand thresholds).
	require.NotNil(t, judgedItem.Score.Band, "score_band must be populated")
	assert.Equal(t, "strong", *judgedItem.Score.Band, "score 87 maps to 'strong' band")

	// Candidate name must be masked: first rune + "***".
	name := judgedItem.Candidate.FullNameMasked
	assert.True(t, strings.HasSuffix(name, "***"),
		"candidate name must end with '***' (got %q)", name)
	assert.NotEqual(t, "***", name,
		"masked name must preserve the leading rune (got %q)", name)

	// rule_match must be a populated JSON object with a "results" field.
	require.NotEmpty(t, judgedItem.Score.RuleMatch, "rule_match must be populated")
	var ruleMatchObj map[string]any
	require.NoError(t, json.Unmarshal(judgedItem.Score.RuleMatch, &ruleMatchObj),
		"rule_match must be valid JSON")
	_, hasResults := ruleMatchObj["results"]
	assert.True(t, hasResults, "rule_match must contain a 'results' field (chips)")
}

// getApplications is a helper that sends GET /intents/{id}/applications and
// returns the decoded response.
func getApplications(t *testing.T, router chi.Router, tenant shared.TenantID, intentID uuid.UUID) v1.ApplicationListResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/intents/"+intentID.String()+"/applications", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{
		TenantID:    tenant,
		RecruiterID: shared.NewRecruiterID(),
	}))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp v1.ApplicationListResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp
}
