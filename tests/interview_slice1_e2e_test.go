//go:build integration

package tests

import (
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

	interviewcommands "github.com/hustle/hireflow/internal/interview/application/commands"
	interviewqueries "github.com/hustle/hireflow/internal/interview/application/queries"
	interviewhttp "github.com/hustle/hireflow/internal/interview/delivery/http/v1"
	interviewservices "github.com/hustle/hireflow/internal/interview/domain/services"
	interviewvo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	interviewclients "github.com/hustle/hireflow/internal/interview/infrastructure/clients"
	interviewmsg "github.com/hustle/hireflow/internal/interview/infrastructure/messaging"
	interviewpersist "github.com/hustle/hireflow/internal/interview/infrastructure/persistence"
	interviewsubs "github.com/hustle/hireflow/internal/interview/infrastructure/subscribers"
	interviewworker "github.com/hustle/hireflow/internal/interview/infrastructure/worker"

	sourcingcommands "github.com/hustle/hireflow/internal/sourcing/application/commands"
	sourcingqueries "github.com/hustle/hireflow/internal/sourcing/application/queries"
	sourcinghttp "github.com/hustle/hireflow/internal/sourcing/delivery/http/v1"
	sourcingvo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	sourcingclients "github.com/hustle/hireflow/internal/sourcing/infrastructure/clients"
	sourcingembed "github.com/hustle/hireflow/internal/sourcing/infrastructure/embedding"
	sourcingenc "github.com/hustle/hireflow/internal/sourcing/infrastructure/encryption"
	sourcingmsg "github.com/hustle/hireflow/internal/sourcing/infrastructure/messaging"
	sourcingpersist "github.com/hustle/hireflow/internal/sourcing/infrastructure/persistence"
	sourcingscan "github.com/hustle/hireflow/internal/sourcing/infrastructure/scanning"
	sourcingscoring "github.com/hustle/hireflow/internal/sourcing/infrastructure/scoring"
	sourcingsubs "github.com/hustle/hireflow/internal/sourcing/infrastructure/subscribers"
	sourcingstorage "github.com/hustle/hireflow/internal/sourcing/infrastructure/storage"
	sourcingtext "github.com/hustle/hireflow/internal/sourcing/infrastructure/text"
	sourcingworker "github.com/hustle/hireflow/internal/sourcing/infrastructure/worker"
)

// stubCandidateReader is a test double for the interview CandidateReader.
// It returns a fixed profile, bypassing the cross-context DB read. This
// keeps the e2e test deterministic and avoids dependency on the exact JSONB
// schema stored by the sourcing parser.
type stubCandidateReader struct{}

func (stubCandidateReader) GetProfileForQuestions(
	_ context.Context, _ shared.TenantID, candidateID uuid.UUID,
) (interviewservices.CandidateProfile, error) {
	return interviewservices.CandidateProfile{
		ID:       candidateID,
		Headline: "Senior Backend Engineer (test)",
		Location: "Bangalore",
		Skills:   []string{"Go", "Distributed Systems", "Postgres"},
		Experiences: []interviewservices.Experience{
			{Title: "Senior Backend", Company: "Razorpay", Duration: "5y", Summary: "Led payments infra"},
		},
		Education: []interviewservices.EducationEntry{
			{Degree: "B.Tech", Field: "CS", Institution: "IIT Bombay", Year: "2018"},
		},
		Certifications:  []string{},
		SchemaVersion: 1,
	}, nil
}

// stubGenerator is a test double for the interview QuestionGenerator. It
// returns a fixed canned question list (or an error if err is set).
type stubGenerator struct {
	questions []interviewvo.Question
	err       error
}

func (s stubGenerator) Generate(_ context.Context, _ interviewservices.GenerationInput) ([]interviewvo.Question, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.questions, nil
}

// cannedQuestions returns 3 well-formed Question values for the stubGenerator.
func cannedQuestions() []interviewvo.Question {
	return []interviewvo.Question{
		{
			Prompt:          "Describe your experience with Go concurrency primitives.",
			SkillProbed:     "Go",
			Why:             "Core language skill for backend role.",
			ExpectedSignals: []string{"goroutines", "channels", "select", "sync"},
			ModelAnswer:     "Goroutines + channels for CSP; mutex for shared state; select for fan-in.",
			RedFlags:        []string{"never used Go", "confuses goroutines with threads"},
			FollowUps:       []string{"How would you handle a goroutine leak?"},
		},
		{
			Prompt:          "Walk us through designing a rate limiter.",
			SkillProbed:     "System Design",
			Why:             "Tests system-level thinking for backend engineer.",
			ExpectedSignals: []string{"token bucket", "sliding window", "redis", "distributed"},
			ModelAnswer:     "Token bucket with Redis for distributed rate limiting.",
			RedFlags:        []string{"no awareness of distributed systems", "can't discuss tradeoffs"},
			FollowUps:       []string{"How does it behave under burst traffic?"},
		},
		{
			Prompt:          "Tell me about a time you resolved a production outage.",
			SkillProbed:     "Incident Management",
			Why:             "Assesses on-call reliability and communication.",
			ExpectedSignals: []string{"root cause", "mitigation", "post-mortem", "monitoring"},
			ModelAnswer:     "Identified root cause quickly, mitigated, wrote post-mortem, added alerting.",
			RedFlags:        []string{"no structured approach", "blames teammates"},
			FollowUps:       []string{"What monitoring did you add afterwards?"},
		},
	}
}

// TestInterviewSlice1_E2E exercises the full interview lifecycle:
//
//  1. Upload resume → Scored.
//  2. Shortlist application → ApplicationShortlisted event.
//  3. ApplicationShortlistedConsumer → StartInterviewProcess → 3-round DefaultLoop (Pending).
//  4. QuestionGenerationPool picks up each Pending round → QuestionsReady (stub questions).
//  5. Record feedback on round 1 → 201.
//  6. Mark round 1 done → 204; reload → Completed.
//  7. Skip round 2 → 204.
//  8. Regenerate round 3 → 202; poll → QuestionsReady again.
//  9. Mark round 3 done → 204.
//  10. Complete process → 204; reload → Completed.
//  11. Verify audit_log + interview_outbox counts via raw SQL.
func TestInterviewSlice1_E2E(t *testing.T) {
	pool := newPgvectorPool(t) // skips if DATABASE_URL not set
	logger := zerolog.New(io.Discard)

	// ── Outer timeout ─────────────────────────────────────────────────────────
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// ── Identity ──────────────────────────────────────────────────────────────
	tenant := shared.NewTenantID()
	tenantUUID, err := uuid.Parse(tenant.String())
	require.NoError(t, err)
	recruiterID := shared.NewRecruiterID()
	identity := auth.Identity{TenantID: tenant, RecruiterID: recruiterID}

	// ── Intent ────────────────────────────────────────────────────────────────
	intentID := uuid.New()
	insertHiringIntentForSlice3(t, pool, intentID, tenantUUID)

	// ── Sourcing infra ────────────────────────────────────────────────────────
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
	auditWriter := auditinfra.NewPostgresAuditWriter(pool)

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
	transitionAppH := sourcingcommands.NewTransitionApplicationHandler(appRepo, auditWriter)
	listAppH := sourcingqueries.NewListApplicationsHandler(appRepo, candRepo, piiEnc)

	// ── Interview infra ───────────────────────────────────────────────────────
	processRepo := interviewpersist.NewPostgresProcessRepository(pool)
	templateRepo := interviewpersist.NewPostgresLoopTemplateRepository(pool)
	feedbackRepo := interviewpersist.NewPostgresFeedbackRepository(pool)

	interviewIntentReader := interviewclients.NewPostgresIntentReader(pool)
	// Use a stub CandidateReader to avoid schema mismatch between sourcing's
	// ParsedSkill objects and the interview reader's expected []string shape.
	// The real PostgresCandidateReader is exercised in its own integration test.
	interviewCandidateReader := stubCandidateReader{}

	gen := stubGenerator{questions: cannedQuestions()}
	outboxAppender := interviewmsg.NewPostgresOutboxAppender(pool)

	startProcessH := interviewcommands.NewStartInterviewProcessHandler(processRepo, templateRepo)
	generateQH := interviewcommands.NewGenerateRoundQuestionsHandler(
		processRepo, interviewIntentReader, interviewCandidateReader, gen,
	)
	regenerateQH := interviewcommands.NewRegenerateRoundQuestionsHandler(processRepo)
	recordFeedbackH := interviewcommands.NewRecordFeedbackHandler(feedbackRepo, processRepo, auditWriter, outboxAppender)
	markDoneH := interviewcommands.NewMarkRoundCompletedHandler(processRepo, auditWriter)
	markSkipH := interviewcommands.NewMarkRoundSkippedHandler(processRepo, auditWriter)
	completeProcessH := interviewcommands.NewCompleteProcessHandler(processRepo, auditWriter)
	cancelProcessH := interviewcommands.NewCancelProcessHandler(processRepo, auditWriter)

	getProcessH := interviewqueries.NewGetInterviewProcessHandler(processRepo, feedbackRepo, auditWriter)
	listProcessesH := interviewqueries.NewListInterviewProcessesHandler(processRepo)
	getTemplateH := interviewqueries.NewGetLoopTemplateHandler(templateRepo)

	// ── Event bus ─────────────────────────────────────────────────────────────
	bus := eventbus.NewInMemory(logger)

	// ── Sourcing bus subscriptions ────────────────────────────────────────────
	bus.Subscribe("hiringintent.IntentConfirmed",
		sourcingsubs.NewIntentConfirmedConsumer(scoreIntentH, logger).Handle,
	)
	bus.Subscribe("sourcing.CandidateParsed",
		sourcingsubs.NewCandidateParsedConsumer(scoreCandidateH, logger).Handle,
	)

	// ── Interview bus subscription ────────────────────────────────────────────
	shortlistedConsumer := interviewsubs.NewApplicationShortlistedConsumer(startProcessH, logger)
	bus.Subscribe("sourcing.ApplicationShortlisted", shortlistedConsumer.Handle)

	// ── HTTP routers ──────────────────────────────────────────────────────────
	sourcingHandler := sourcinghttp.NewSourcingHandler(sourcinghttp.SourcingHandlerDeps{
		Upload:         uploadH,
		Status:         statusH,
		ListApplications: listAppH,
		Transition:     transitionAppH,
		Logger:         logger,
	})
	interviewHandler := interviewhttp.NewInterviewHandler(interviewhttp.InterviewHandlerDeps{
		UpsertTemplate:           interviewcommands.NewUpsertLoopTemplateHandler(templateRepo, auditWriter),
		RecordFeedback:           recordFeedbackH,
		MarkRoundCompleted:       markDoneH,
		MarkRoundSkipped:         markSkipH,
		CompleteProcess:          completeProcessH,
		CancelProcess:            cancelProcessH,
		RegenerateRoundQuestions: regenerateQH,
		GetInterviewProcess:      getProcessH,
		ListInterviewProcesses:   listProcessesH,
		GetLoopTemplate:          getTemplateH,
		Logger:                   logger,
	})

	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), identity)))
		})
	})
	sourcinghttp.Mount(router, sourcingHandler)
	interviewhttp.Mount(router, interviewHandler)

	// ── Outbox dispatchers + worker pools ─────────────────────────────────────
	sourcingPub := sourcingmsg.NewBusPublisher(bus)
	sourcingDispatcher := sourcingmsg.NewOutboxDispatcher(pool, sourcingPub, logger,
		sourcingmsg.DispatcherConfig{PollInterval: 100 * time.Millisecond},
	)
	interviewPub := interviewmsg.NewBusPublisher(bus)
	interviewDispatcher := interviewmsg.NewOutboxDispatcher(pool, interviewPub, logger,
		interviewmsg.DispatcherConfig{PollInterval: 100 * time.Millisecond},
	)

	fastCfg := sourcingworker.Config{Size: 1, PollInterval: 100 * time.Millisecond}
	uploadPool := sourcingworker.NewPool(uploadRepo, processH, fastCfg, logger)
	matchPool := sourcingworker.NewMatchPool(appRepo, scoreAppH, fastCfg, logger)
	judgePool := sourcingworker.NewJudgePool(judgeJobRepo, judgeAppH, fastCfg, logger)

	qgenPool := interviewworker.NewQuestionGenerationPool(
		processRepo, generateQH,
		interviewworker.Config{Size: 1, PollInterval: 200 * time.Millisecond},
		logger,
	)

	go sourcingDispatcher.Run(ctx)
	go interviewDispatcher.Run(ctx)
	go uploadPool.Run(ctx)
	go matchPool.Run(ctx)
	go judgePool.Run(ctx)
	go qgenPool.Run(ctx)

	// ── Step 1: Upload a resume ───────────────────────────────────────────────
	body, ct := writeMultipart(t, map[string][]byte{"alice.pdf": helloPDFBytes(t)})
	req := httptest.NewRequest(http.MethodPost,
		"/intents/"+intentID.String()+"/resumes:batch", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var upResp sourcinghttp.BatchUploadResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&upResp))
	require.Len(t, upResp.Items, 1)

	// ── Step 2: Wait for Parsed ────────────────────────────────────────────────
	pollUntil(t, 30*time.Second, func() bool {
		statusReq := httptest.NewRequest(http.MethodGet, "/resumes/batches/"+upResp.BatchID, nil)
		statusRec := httptest.NewRecorder()
		router.ServeHTTP(statusRec, statusReq)
		if statusRec.Code != http.StatusOK {
			return false
		}
		var s sourcinghttp.BatchStatusResponse
		if err := json.NewDecoder(statusRec.Body).Decode(&s); err != nil {
			return false
		}
		return s.Summary.Total > 0 && s.Items[0].Status == string(sourcingvo.StatusParsed)
	}, "timed out waiting for Parsed status")

	// ── Step 3: Wait for Scored ────────────────────────────────────────────────
	pollUntil(t, 30*time.Second, func() bool {
		appResp := getApplications(t, router, tenant, intentID)
		for _, item := range appResp.Items {
			if item.Status == "Scored" && item.Score.EmbeddingScore != nil {
				return true
			}
		}
		return false
	}, "timed out waiting for Application to reach Scored")

	// Fire IntentConfirmed to enqueue JudgeJobs.
	intentIDVO, err := intentvo.ParseIntentID(intentID.String())
	require.NoError(t, err)
	confirmedEvent := intentevents.NewIntentConfirmed(
		intentIDVO, tenant, shared.NewRecruiterID(),
		intentvo.PriorityMedium, time.Now().UTC(),
	)
	require.NoError(t, bus.Publish(ctx, "hiringintent.IntentConfirmed", confirmedEvent))

	// Wait for judge worker to populate overall_score=87.
	var judgedApp *sourcinghttp.ApplicationListItem
	pollUntil(t, 45*time.Second, func() bool {
		appResp := getApplications(t, router, tenant, intentID)
		for i := range appResp.Items {
			it := &appResp.Items[i]
			if it.Status == "Scored" && it.Score.Overall != nil && *it.Score.Overall == 87 {
				judgedApp = it
				return true
			}
		}
		return false
	}, "timed out waiting for judged application (overall_score=87)")
	require.NotNil(t, judgedApp)

	applicationID, err := uuid.Parse(judgedApp.ApplicationID)
	require.NoError(t, err)

	// ── Step 4: Shortlist the application ─────────────────────────────────────
	shortlistReq := httptest.NewRequest(http.MethodPost,
		"/applications/"+applicationID.String()+":shortlist", nil)
	shortlistRec := httptest.NewRecorder()
	router.ServeHTTP(shortlistRec, shortlistReq)
	require.Equal(t, http.StatusNoContent, shortlistRec.Code,
		"shortlist must return 204: %s", shortlistRec.Body.String())

	// ── Step 5: Poll until InterviewProcess appears (3 rounds, all Pending) ───
	var processID uuid.UUID
	pollUntil(t, 15*time.Second, func() bool {
		listResp := listProcesses(t, router, intentID)
		if len(listResp.Processes) == 0 {
			return false
		}
		p := listResp.Processes[0]
		pidParsed, err := uuid.Parse(p.ID)
		if err != nil {
			return false
		}
		processID = pidParsed
		return true
	}, "timed out waiting for InterviewProcess to be created")

	require.NotEqual(t, uuid.Nil, processID, "processID must be set")

	// Verify process has 3 rounds (DefaultLoop: screen, technical, bar_raiser).
	// We don't assert Pending here because the worker may have already advanced
	// one or more rounds by the time we read the process; the important assertion
	// is count and eventual QuestionsReady below.
	proc := getProcessByID(t, router, processID)
	require.Len(t, proc.Rounds, 3, "DefaultLoop must create exactly 3 rounds")

	// ── Step 6: Wait for all 3 rounds to reach QuestionsReady ─────────────────
	pollUntil(t, 15*time.Second, func() bool {
		p := getProcessByID(t, router, processID)
		for _, r := range p.Rounds {
			if r.Status != "QuestionsReady" {
				return false
			}
		}
		return true
	}, "timed out waiting for all rounds to reach QuestionsReady")

	proc = getProcessByID(t, router, processID)
	require.Len(t, proc.Rounds, 3)
	for _, r := range proc.Rounds {
		assert.Equal(t, "QuestionsReady", r.Status)
		assert.Len(t, r.Questions, 3, "each round must have 3 stub questions")
	}

	round1ID, err := uuid.Parse(proc.Rounds[0].ID)
	require.NoError(t, err)
	round2ID, err := uuid.Parse(proc.Rounds[1].ID)
	require.NoError(t, err)
	round3ID, err := uuid.Parse(proc.Rounds[2].ID)
	require.NoError(t, err)

	// ── Step 7: POST feedback on round 1 ─────────────────────────────────────
	fbBody, _ := json.Marshal(interviewhttp.RecordFeedbackRequest{
		InterviewerName: "Alice Interviewer",
		Decision:        "yes",
		Notes:           "Strong technical skills.",
	})
	fbReq := httptest.NewRequest(http.MethodPost,
		"/interview/rounds/"+round1ID.String()+"/feedback",
		bytes.NewReader(fbBody))
	fbReq.Header.Set("Content-Type", "application/json")
	fbRec := httptest.NewRecorder()
	router.ServeHTTP(fbRec, fbReq)
	require.Equal(t, http.StatusCreated, fbRec.Code,
		"feedback must return 201: %s", fbRec.Body.String())

	// ── Step 8: Mark round 1 done ─────────────────────────────────────────────
	doneReq := httptest.NewRequest(http.MethodPost,
		"/interview/rounds/"+round1ID.String()+":mark-done", nil)
	doneRec := httptest.NewRecorder()
	router.ServeHTTP(doneRec, doneReq)
	require.Equal(t, http.StatusNoContent, doneRec.Code,
		"mark-done must return 204: %s", doneRec.Body.String())

	proc = getProcessByID(t, router, processID)
	require.Equal(t, "Completed", proc.Rounds[0].Status, "round 1 must be Completed")

	// ── Step 9: Skip round 2 ──────────────────────────────────────────────────
	skipReq := httptest.NewRequest(http.MethodPost,
		"/interview/rounds/"+round2ID.String()+":skip", nil)
	skipRec := httptest.NewRecorder()
	router.ServeHTTP(skipRec, skipReq)
	require.Equal(t, http.StatusNoContent, skipRec.Code,
		"skip must return 204: %s", skipRec.Body.String())

	// ── Step 10: Regenerate round 3 ───────────────────────────────────────────
	regenReq := httptest.NewRequest(http.MethodPost,
		"/interview/rounds/"+round3ID.String()+":regenerate", nil)
	regenRec := httptest.NewRecorder()
	router.ServeHTTP(regenRec, regenReq)
	require.Equal(t, http.StatusAccepted, regenRec.Code,
		"regenerate must return 202: %s", regenRec.Body.String())

	// Poll until round 3 is back at QuestionsReady (worker re-fires generation).
	pollUntil(t, 15*time.Second, func() bool {
		p := getProcessByID(t, router, processID)
		for _, r := range p.Rounds {
			if r.ID == round3ID.String() {
				return r.Status == "QuestionsReady"
			}
		}
		return false
	}, "timed out waiting for round 3 to reach QuestionsReady after regenerate")

	// ── Step 11: Mark round 3 done ────────────────────────────────────────────
	done3Req := httptest.NewRequest(http.MethodPost,
		"/interview/rounds/"+round3ID.String()+":mark-done", nil)
	done3Rec := httptest.NewRecorder()
	router.ServeHTTP(done3Rec, done3Req)
	require.Equal(t, http.StatusNoContent, done3Rec.Code,
		"mark-done round 3 must return 204: %s", done3Rec.Body.String())

	// ── Step 12: Complete the process ────────────────────────────────────────
	completeReq := httptest.NewRequest(http.MethodPost,
		"/interview/processes/"+processID.String()+":complete", nil)
	completeRec := httptest.NewRecorder()
	router.ServeHTTP(completeRec, completeReq)
	require.Equal(t, http.StatusNoContent, completeRec.Code,
		"complete must return 204: %s", completeRec.Body.String())

	proc = getProcessByID(t, router, processID)
	assert.Equal(t, "Completed", proc.Status, "process must be Completed")

	// ── Step 13: Verify audit_log rows ────────────────────────────────────────
	assertAuditRowCount(t, pool, tenant, "interview_process_read", 1)
	assertAuditRowCount(t, pool, tenant, "interview_round_feedback_recorded", 1)
	assertAuditRowCount(t, pool, tenant, "interview_round_completed", 1)
	assertAuditRowCount(t, pool, tenant, "interview_round_skipped", 1)
	assertAuditRowCount(t, pool, tenant, "interview_process_completed", 1)

	// ── Step 14: Verify interview_outbox event counts ─────────────────────────
	assertOutboxCount(t, pool, tenant, "interview.InterviewProcessCreated", 1)
	// 3 initial generations + 1 regenerate = 4
	assertOutboxCount(t, pool, tenant, "interview.InterviewQuestionsGenerated", 4)
	assertOutboxCount(t, pool, tenant, "interview.InterviewFeedbackRecorded", 1)
}

// ── helpers ──────────────────────────────────────────────────────────────────

// pollUntil retries fn() every 200ms until it returns true or the deadline
// elapses. Fails the test with msg if the deadline is exceeded.
func pollUntil(t *testing.T, timeout time.Duration, fn func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if fn() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// listProcesses calls GET /intents/{id}/interview-processes and decodes the
// response.
func listProcesses(t *testing.T, router chi.Router, intentID uuid.UUID) interviewhttp.ListProcessesResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet,
		"/intents/"+intentID.String()+"/interview-processes", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp interviewhttp.ListProcessesResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp
}

// getProcessByID calls GET /interview/processes/{id} and decodes the response.
func getProcessByID(t *testing.T, router chi.Router, processID uuid.UUID) interviewhttp.InterviewProcessResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet,
		"/interview/processes/"+processID.String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp interviewhttp.InterviewProcessResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp
}

// assertAuditRowCount asserts that the audit_log has at least minCount rows
// for the given (tenant, action).
func assertAuditRowCount(t *testing.T, pool *pgxpool.Pool, tenant shared.TenantID, action string, minCount int) {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND action=$2`,
		tenant.String(), action,
	).Scan(&count)
	require.NoError(t, err, "assertAuditRowCount: query failed for action=%s", action)
	assert.GreaterOrEqual(t, count, minCount,
		"audit_log must have at least %d row(s) for action=%s (got %d)", minCount, action, count)
}

// assertOutboxCount asserts that interview_outbox has exactly wantCount
// dispatched or pending rows for the given (tenant, event_name).
func assertOutboxCount(t *testing.T, pool *pgxpool.Pool, tenant shared.TenantID, eventName string, wantCount int) {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM interview_outbox WHERE tenant_id=$1 AND event_name=$2`,
		tenant.String(), eventName,
	).Scan(&count)
	require.NoError(t, err, "assertOutboxCount: query failed for event=%s", eventName)
	assert.Equal(t, wantCount, count,
		"interview_outbox must have exactly %d row(s) for event=%s (got %d)", wantCount, eventName, count)
}
