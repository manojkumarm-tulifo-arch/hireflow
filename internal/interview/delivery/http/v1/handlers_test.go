package v1_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	auditdomain "github.com/hustle/hireflow/internal/shared/audit/domain"
	auditinfra "github.com/hustle/hireflow/internal/shared/audit/infrastructure"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/shared/infrastructure/auth"

	"github.com/hustle/hireflow/internal/interview/application/commands"
	"github.com/hustle/hireflow/internal/interview/application/queries"
	v1 "github.com/hustle/hireflow/internal/interview/delivery/http/v1"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/events"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

// ---------------------------------------------------------------------------
// In-memory fakes (self-contained — test packages don't share)
// ---------------------------------------------------------------------------

type fakeProcessRepo struct {
	mu        sync.Mutex
	byID      map[uuid.UUID]*entities.InterviewProcess
	byAppID   map[uuid.UUID]*entities.InterviewProcess
	byRoundID map[uuid.UUID]*entities.InterviewProcess
	saveErr   error
}

func newFakeProcessRepo() *fakeProcessRepo {
	return &fakeProcessRepo{
		byID:      make(map[uuid.UUID]*entities.InterviewProcess),
		byAppID:   make(map[uuid.UUID]*entities.InterviewProcess),
		byRoundID: make(map[uuid.UUID]*entities.InterviewProcess),
	}
}

func (r *fakeProcessRepo) Save(_ context.Context, p *entities.InterviewProcess) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saveErr != nil {
		return r.saveErr
	}
	if existing, ok := r.byAppID[p.ApplicationID()]; ok && existing.ID() != p.ID() {
		return repositories.ErrProcessDuplicate
	}
	r.byID[p.ID()] = p
	r.byAppID[p.ApplicationID()] = p
	for _, round := range p.Rounds() {
		r.byRoundID[round.ID()] = p
	}
	return nil
}

func (r *fakeProcessRepo) FindByID(_ context.Context, _ shared.TenantID, id uuid.UUID) (*entities.InterviewProcess, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byID[id]
	if !ok {
		return nil, repositories.ErrProcessNotFound
	}
	return p, nil
}

func (r *fakeProcessRepo) FindByApplicationID(_ context.Context, tenant shared.TenantID, appID uuid.UUID) (*entities.InterviewProcess, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byAppID[appID]
	if !ok {
		return nil, repositories.ErrProcessNotFound
	}
	if p.TenantID() != tenant {
		return nil, repositories.ErrProcessNotFound
	}
	return p, nil
}

func (r *fakeProcessRepo) FindByRoundID(_ context.Context, _ shared.TenantID, roundID uuid.UUID) (*entities.InterviewProcess, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byRoundID[roundID]
	if !ok {
		return nil, repositories.ErrProcessNotFound
	}
	return p, nil
}

func (r *fakeProcessRepo) ListByTenant(_ context.Context, _ shared.TenantID, _ repositories.ProcessListFilter) ([]*entities.InterviewProcess, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*entities.InterviewProcess, 0, len(r.byID))
	for _, p := range r.byID {
		out = append(out, p)
	}
	return out, nil
}

func (r *fakeProcessRepo) ClaimNextPendingRound(_ context.Context) (*entities.InterviewProcess, uuid.UUID, error) {
	return nil, uuid.Nil, repositories.ErrProcessNotFound
}

// fakeTemplateRepo is an in-memory LoopTemplateRepository.
type fakeTemplateRepo struct {
	mu       sync.Mutex
	byIntent map[uuid.UUID]*entities.LoopTemplate
	findErr  error
}

func newFakeTemplateRepo() *fakeTemplateRepo {
	return &fakeTemplateRepo{byIntent: make(map[uuid.UUID]*entities.LoopTemplate)}
}

func (r *fakeTemplateRepo) Save(_ context.Context, t *entities.LoopTemplate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byIntent[t.IntentID()] = t
	return nil
}

func (r *fakeTemplateRepo) FindByIntent(_ context.Context, _ shared.TenantID, intentID uuid.UUID) (*entities.LoopTemplate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.findErr != nil {
		return nil, r.findErr
	}
	t, ok := r.byIntent[intentID]
	if !ok {
		return nil, repositories.ErrLoopTemplateNotFound
	}
	return t, nil
}

// fakeFeedbackRepo is an in-memory FeedbackRepository.
type fakeFeedbackRepo struct {
	mu        sync.Mutex
	rows      []repositories.FeedbackRow
	appendErr error
}

func (r *fakeFeedbackRepo) Append(_ context.Context, row repositories.FeedbackRow) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.appendErr != nil {
		return r.appendErr
	}
	r.rows = append(r.rows, row)
	return nil
}

func (r *fakeFeedbackRepo) ListByRound(_ context.Context, _ shared.TenantID, _ uuid.UUID) ([]repositories.FeedbackRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]repositories.FeedbackRow(nil), r.rows...), nil
}

// fakeOutboxAppender captures emitted events (satisfies commands.OutboxAppender).
type fakeOutboxAppender struct {
	mu     sync.Mutex
	events []events.Event
}

func (a *fakeOutboxAppender) Append(_ context.Context, ev events.Event) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, ev)
	return nil
}

// captureAuditWriter records audit events.
type captureAuditWriter struct {
	mu     sync.Mutex
	events []auditdomain.AuditEvent
}

func (w *captureAuditWriter) Write(_ context.Context, e auditdomain.AuditEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = append(w.events, e)
	return nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// withIdentity injects an auth.Identity into the request context.
func withIdentity(r *http.Request, tenant shared.TenantID) *http.Request {
	return r.WithContext(auth.WithIdentity(r.Context(), auth.Identity{
		TenantID:    tenant,
		RecruiterID: shared.NewRecruiterID(),
	}))
}

func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	require.NoError(t, json.NewEncoder(buf).Encode(v))
	return buf
}

// seedProcess creates an InterviewProcess with one Pending round and saves it.
func seedProcess(t *testing.T, repo *fakeProcessRepo, tenantID shared.TenantID) (*entities.InterviewProcess, uuid.UUID) {
	t.Helper()
	p, err := entities.NewInterviewProcess(entities.NewInterviewProcessInput{
		TenantID:      tenantID,
		ApplicationID: uuid.New(),
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
		Rounds: []entities.TemplateRound{
			{Kind: vo.RoundKindTechnical, Sequence: 1},
		},
	})
	require.NoError(t, err)
	require.NoError(t, repo.Save(context.Background(), p))
	return p, p.Rounds()[0].ID()
}

// advanceRoundToQuestionsReady marks the round QuestionsReady and re-saves.
func advanceRoundToQuestionsReady(t *testing.T, repo *fakeProcessRepo, p *entities.InterviewProcess, roundID uuid.UUID) {
	t.Helper()
	questions := []vo.Question{{
		Prompt:          "Describe a complex system.",
		SkillProbed:     "system design",
		Why:             "Tests architecture thinking.",
		ExpectedSignals: []string{"scalability", "trade-offs", "communication"},
		ModelAnswer:     "Good answer covers requirements and trade-offs.",
		RedFlags:        []string{"no trade-offs", "ignores failure modes"},
		FollowUps:       []string{"How would you handle 10x traffic?"},
	}}
	require.NoError(t, p.MarkRoundQuestionsReady(roundID, questions))
	require.NoError(t, repo.Save(context.Background(), p))
}

// advanceRoundToCompleted marks the round QuestionsReady then Completed.
func advanceRoundToCompleted(t *testing.T, repo *fakeProcessRepo, p *entities.InterviewProcess, roundID uuid.UUID) {
	t.Helper()
	advanceRoundToQuestionsReady(t, repo, p, roundID)
	require.NoError(t, p.MarkRoundCompleted(roundID))
	require.NoError(t, repo.Save(context.Background(), p))
}

// newHandler builds a full InterviewHandler wired with in-memory fakes.
func newHandler(t *testing.T) (*v1.InterviewHandler, *fakeProcessRepo, *fakeTemplateRepo, *fakeFeedbackRepo) {
	t.Helper()
	processes := newFakeProcessRepo()
	templates := newFakeTemplateRepo()
	feedback := &fakeFeedbackRepo{}
	outbox := &fakeOutboxAppender{}
	audit := auditinfra.NewNoopAuditWriter()
	logger := zerolog.Nop()

	upsertTemplate := commands.NewUpsertLoopTemplateHandler(templates, audit)
	recordFeedback := commands.NewRecordFeedbackHandler(feedback, processes, audit, outbox)
	markDone := commands.NewMarkRoundCompletedHandler(processes, audit)
	markSkipped := commands.NewMarkRoundSkippedHandler(processes, audit)
	completeProcess := commands.NewCompleteProcessHandler(processes, audit)
	cancelProcess := commands.NewCancelProcessHandler(processes, audit)
	regenerate := commands.NewRegenerateRoundQuestionsHandler(processes)

	getProcess := queries.NewGetInterviewProcessHandler(processes, feedback, audit)
	listProcesses := queries.NewListInterviewProcessesHandler(processes)
	getTemplate := queries.NewGetLoopTemplateHandler(templates)

	h := v1.NewInterviewHandler(v1.InterviewHandlerDeps{
		UpsertTemplate:           upsertTemplate,
		RecordFeedback:           recordFeedback,
		MarkRoundCompleted:       markDone,
		MarkRoundSkipped:         markSkipped,
		CompleteProcess:          completeProcess,
		CancelProcess:            cancelProcess,
		RegenerateRoundQuestions: regenerate,
		GetInterviewProcess:      getProcess,
		ListInterviewProcesses:   listProcesses,
		GetLoopTemplate:          getTemplate,
		Logger:                   logger,
	})
	return h, processes, templates, feedback
}

func newRouter(h *v1.InterviewHandler) chi.Router {
	r := chi.NewRouter()
	v1.Mount(r, h)
	return r
}

// ---------------------------------------------------------------------------
// UpsertLoopTemplate
// ---------------------------------------------------------------------------

func TestUpsertLoopTemplate_Returns204(t *testing.T) {
	h, _, _, _ := newHandler(t)
	router := newRouter(h)

	body := jsonBody(t, v1.UpsertLoopTemplateRequest{
		Rounds: []v1.TemplateRoundRequest{
			{Kind: "screen", Sequence: 1},
			{Kind: "technical", Sequence: 2},
		},
	})
	req := httptest.NewRequest(http.MethodPut, "/intents/"+uuid.New().String()+"/loop-template", body)
	req.Header.Set("Content-Type", "application/json")
	req = withIdentity(req, shared.NewTenantID())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

func TestUpsertLoopTemplate_NoAuth_401(t *testing.T) {
	h, _, _, _ := newHandler(t)
	router := newRouter(h)

	body := jsonBody(t, v1.UpsertLoopTemplateRequest{
		Rounds: []v1.TemplateRoundRequest{{Kind: "screen", Sequence: 1}},
	})
	req := httptest.NewRequest(http.MethodPut, "/intents/"+uuid.New().String()+"/loop-template", body)
	req.Header.Set("Content-Type", "application/json")
	// No withIdentity.

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestUpsertLoopTemplate_BadIntentID_400(t *testing.T) {
	h, _, _, _ := newHandler(t)
	router := newRouter(h)

	body := jsonBody(t, v1.UpsertLoopTemplateRequest{
		Rounds: []v1.TemplateRoundRequest{{Kind: "screen", Sequence: 1}},
	})
	req := httptest.NewRequest(http.MethodPut, "/intents/not-a-uuid/loop-template", body)
	req.Header.Set("Content-Type", "application/json")
	req = withIdentity(req, shared.NewTenantID())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assertErrorCode(t, rec, "invalid_intent_id")
}

func TestUpsertLoopTemplate_InvalidRoundKind_400(t *testing.T) {
	h, _, _, _ := newHandler(t)
	router := newRouter(h)

	body := jsonBody(t, v1.UpsertLoopTemplateRequest{
		Rounds: []v1.TemplateRoundRequest{{Kind: "BOGUS_KIND", Sequence: 1}},
	})
	req := httptest.NewRequest(http.MethodPut, "/intents/"+uuid.New().String()+"/loop-template", body)
	req.Header.Set("Content-Type", "application/json")
	req = withIdentity(req, shared.NewTenantID())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assertErrorCode(t, rec, "invalid_round_kind")
}

// ---------------------------------------------------------------------------
// GetLoopTemplate
// ---------------------------------------------------------------------------

func TestGetLoopTemplate_ReturnsTemplate(t *testing.T) {
	h, _, templates, _ := newHandler(t)
	router := newRouter(h)

	tenantID := shared.NewTenantID()
	intentID := uuid.New()

	// Seed a template.
	tmpl, err := entities.NewLoopTemplate(entities.NewLoopTemplateInput{
		TenantID: tenantID,
		IntentID: intentID,
		Rounds: []entities.TemplateRound{
			{Kind: vo.RoundKindScreen, Sequence: 1},
			{Kind: vo.RoundKindTechnical, Sequence: 2},
		},
	})
	require.NoError(t, err)
	require.NoError(t, templates.Save(context.Background(), tmpl))

	req := httptest.NewRequest(http.MethodGet, "/intents/"+intentID.String()+"/loop-template", nil)
	req = withIdentity(req, tenantID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp v1.LoopTemplateResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, intentID.String(), resp.IntentID)
	assert.False(t, resp.IsDefault)
	assert.Len(t, resp.Rounds, 2)
}

func TestGetLoopTemplate_NoTemplate_ReturnsDefault(t *testing.T) {
	h, _, _, _ := newHandler(t)
	router := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/intents/"+uuid.New().String()+"/loop-template", nil)
	req = withIdentity(req, shared.NewTenantID())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp v1.LoopTemplateResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.IsDefault)
	assert.Len(t, resp.Rounds, 3) // DefaultLoop has 3 rounds.
}

func TestGetLoopTemplate_NoAuth_401(t *testing.T) {
	h, _, _, _ := newHandler(t)
	router := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/intents/"+uuid.New().String()+"/loop-template", nil)
	// No withIdentity.

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetLoopTemplate_BadIntentID_400(t *testing.T) {
	h, _, _, _ := newHandler(t)
	router := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/intents/not-a-uuid/loop-template", nil)
	req = withIdentity(req, shared.NewTenantID())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assertErrorCode(t, rec, "invalid_intent_id")
}

// ---------------------------------------------------------------------------
// ListInterviewProcesses
// ---------------------------------------------------------------------------

func TestListInterviewProcesses_Returns200WithList(t *testing.T) {
	h, processes, _, _ := newHandler(t)
	router := newRouter(h)

	tenantID := shared.NewTenantID()
	intentID := uuid.New()

	// Seed a process for this intent.
	p, err := entities.NewInterviewProcess(entities.NewInterviewProcessInput{
		TenantID:      tenantID,
		ApplicationID: uuid.New(),
		CandidateID:   uuid.New(),
		IntentID:      intentID,
		Rounds:        []entities.TemplateRound{{Kind: vo.RoundKindScreen, Sequence: 1}},
	})
	require.NoError(t, err)
	require.NoError(t, processes.Save(context.Background(), p))

	req := httptest.NewRequest(http.MethodGet, "/intents/"+intentID.String()+"/interview-processes", nil)
	req = withIdentity(req, tenantID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp v1.ListProcessesResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Len(t, resp.Processes, 1)
	assert.Equal(t, p.ID().String(), resp.Processes[0].ID)
}

func TestListInterviewProcesses_FilterByStatus(t *testing.T) {
	h, _, _, _ := newHandler(t)
	router := newRouter(h)

	// Empty repo, filter by status — should return empty list without error.
	req := httptest.NewRequest(http.MethodGet,
		"/intents/"+uuid.New().String()+"/interview-processes?status=New", nil)
	req = withIdentity(req, shared.NewTenantID())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// ---------------------------------------------------------------------------
// GetInterviewProcess
// ---------------------------------------------------------------------------

func TestGetInterviewProcess_Returns200(t *testing.T) {
	h, processes, _, _ := newHandler(t)
	router := newRouter(h)

	tenantID := shared.NewTenantID()
	p, _ := seedProcess(t, processes, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/interview/processes/"+p.ID().String(), nil)
	req = withIdentity(req, tenantID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp v1.InterviewProcessResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, p.ID().String(), resp.ID)
	assert.Len(t, resp.Rounds, 1)
}

func TestGetInterviewProcess_NotFound_404(t *testing.T) {
	h, _, _, _ := newHandler(t)
	router := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/interview/processes/"+uuid.New().String(), nil)
	req = withIdentity(req, shared.NewTenantID())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assertErrorCode(t, rec, "process_not_found")
}

func TestGetInterviewProcess_BadProcessID_400(t *testing.T) {
	h, _, _, _ := newHandler(t)
	router := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/interview/processes/not-a-uuid", nil)
	req = withIdentity(req, shared.NewTenantID())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assertErrorCode(t, rec, "invalid_process_id")
}

// ---------------------------------------------------------------------------
// CompleteProcess
// ---------------------------------------------------------------------------

func TestCompleteProcess_Returns204(t *testing.T) {
	h, processes, _, _ := newHandler(t)
	router := newRouter(h)

	tenantID := shared.NewTenantID()
	p, roundID := seedProcess(t, processes, tenantID)
	advanceRoundToCompleted(t, processes, p, roundID)

	req := httptest.NewRequest(http.MethodPost, "/interview/processes/"+p.ID().String()+":complete", nil)
	req = withIdentity(req, tenantID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

func TestCompleteProcess_InvalidTransition_409(t *testing.T) {
	h, processes, _, _ := newHandler(t)
	router := newRouter(h)

	tenantID := shared.NewTenantID()
	p, roundID := seedProcess(t, processes, tenantID)
	// Complete once.
	advanceRoundToCompleted(t, processes, p, roundID)
	_ = p.Complete() // first completion
	require.NoError(t, processes.Save(context.Background(), p))

	// Second complete attempt — process is already terminal.
	req := httptest.NewRequest(http.MethodPost, "/interview/processes/"+p.ID().String()+":complete", nil)
	req = withIdentity(req, tenantID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assertErrorCode(t, rec, "invalid_transition")
}

func TestCompleteProcess_NotFound_404(t *testing.T) {
	h, _, _, _ := newHandler(t)
	router := newRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/interview/processes/"+uuid.New().String()+":complete", nil)
	req = withIdentity(req, shared.NewTenantID())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assertErrorCode(t, rec, "process_not_found")
}

// ---------------------------------------------------------------------------
// CancelProcess
// ---------------------------------------------------------------------------

func TestCancelProcess_Returns204(t *testing.T) {
	h, processes, _, _ := newHandler(t)
	router := newRouter(h)

	tenantID := shared.NewTenantID()
	p, _ := seedProcess(t, processes, tenantID)

	req := httptest.NewRequest(http.MethodPost, "/interview/processes/"+p.ID().String()+":cancel", nil)
	req = withIdentity(req, tenantID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

// ---------------------------------------------------------------------------
// RecordFeedback
// ---------------------------------------------------------------------------

func TestRecordFeedback_Returns201(t *testing.T) {
	h, processes, _, _ := newHandler(t)
	router := newRouter(h)

	tenantID := shared.NewTenantID()
	p, roundID := seedProcess(t, processes, tenantID)
	advanceRoundToQuestionsReady(t, processes, p, roundID)

	body := jsonBody(t, v1.RecordFeedbackRequest{
		InterviewerName: "Jane Doe",
		Decision:        "yes",
		Notes:           "Great candidate.",
	})
	req := httptest.NewRequest(http.MethodPost, "/interview/rounds/"+roundID.String()+"/feedback", body)
	req.Header.Set("Content-Type", "application/json")
	req = withIdentity(req, tenantID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}

func TestRecordFeedback_BadDecision_400(t *testing.T) {
	h, processes, _, _ := newHandler(t)
	router := newRouter(h)

	tenantID := shared.NewTenantID()
	p, roundID := seedProcess(t, processes, tenantID)
	advanceRoundToQuestionsReady(t, processes, p, roundID)

	body := jsonBody(t, v1.RecordFeedbackRequest{
		InterviewerName: "Jane Doe",
		Decision:        "INVALID_DECISION",
	})
	req := httptest.NewRequest(http.MethodPost, "/interview/rounds/"+roundID.String()+"/feedback", body)
	req.Header.Set("Content-Type", "application/json")
	req = withIdentity(req, tenantID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assertErrorCode(t, rec, "invalid_decision")
}

func TestRecordFeedback_MissingName_400(t *testing.T) {
	h, processes, _, _ := newHandler(t)
	router := newRouter(h)

	tenantID := shared.NewTenantID()
	p, roundID := seedProcess(t, processes, tenantID)
	advanceRoundToQuestionsReady(t, processes, p, roundID)

	// interviewer_name is empty — Feedback.Validate() should reject it.
	body := jsonBody(t, v1.RecordFeedbackRequest{
		InterviewerName: "",
		Decision:        "yes",
	})
	req := httptest.NewRequest(http.MethodPost, "/interview/rounds/"+roundID.String()+"/feedback", body)
	req.Header.Set("Content-Type", "application/json")
	req = withIdentity(req, tenantID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assertErrorCode(t, rec, "invalid_feedback")
}

// ---------------------------------------------------------------------------
// RegenerateRoundQuestions
// ---------------------------------------------------------------------------

func TestRegenerateRoundQuestions_Returns202(t *testing.T) {
	h, processes, _, _ := newHandler(t)
	router := newRouter(h)

	tenantID := shared.NewTenantID()
	p, roundID := seedProcess(t, processes, tenantID)
	// Must be in QuestionsReady to be regenerable.
	advanceRoundToQuestionsReady(t, processes, p, roundID)

	req := httptest.NewRequest(http.MethodPost, "/interview/rounds/"+roundID.String()+":regenerate", nil)
	req = withIdentity(req, tenantID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
}

func TestRegenerateRoundQuestions_TerminalRound_409(t *testing.T) {
	h, processes, _, _ := newHandler(t)
	router := newRouter(h)

	tenantID := shared.NewTenantID()
	p, roundID := seedProcess(t, processes, tenantID)
	// Skip the round — terminal, not regenerable.
	require.NoError(t, p.MarkRoundSkipped(roundID))
	require.NoError(t, processes.Save(context.Background(), p))

	req := httptest.NewRequest(http.MethodPost, "/interview/rounds/"+roundID.String()+":regenerate", nil)
	req = withIdentity(req, tenantID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assertErrorCode(t, rec, "invalid_transition")
}

func TestRegenerateRoundQuestions_NotFound_404(t *testing.T) {
	h, _, _, _ := newHandler(t)
	router := newRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/interview/rounds/"+uuid.New().String()+":regenerate", nil)
	req = withIdentity(req, shared.NewTenantID())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assertErrorCode(t, rec, "round_not_found")
}

// ---------------------------------------------------------------------------
// MarkRoundCompleted
// ---------------------------------------------------------------------------

func TestMarkRoundCompleted_Returns204(t *testing.T) {
	h, processes, _, _ := newHandler(t)
	router := newRouter(h)

	tenantID := shared.NewTenantID()
	p, roundID := seedProcess(t, processes, tenantID)
	advanceRoundToQuestionsReady(t, processes, p, roundID)

	req := httptest.NewRequest(http.MethodPost, "/interview/rounds/"+roundID.String()+":mark-done", nil)
	req = withIdentity(req, tenantID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

func TestMarkRoundCompleted_InvalidTransition_409(t *testing.T) {
	h, processes, _, _ := newHandler(t)
	router := newRouter(h)

	tenantID := shared.NewTenantID()
	p, roundID := seedProcess(t, processes, tenantID)
	// Round is still Pending — cannot mark done directly.
	_ = p

	req := httptest.NewRequest(http.MethodPost, "/interview/rounds/"+roundID.String()+":mark-done", nil)
	req = withIdentity(req, tenantID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assertErrorCode(t, rec, "invalid_transition")
}

// ---------------------------------------------------------------------------
// MarkRoundSkipped
// ---------------------------------------------------------------------------

func TestMarkRoundSkipped_Returns204(t *testing.T) {
	h, processes, _, _ := newHandler(t)
	router := newRouter(h)

	tenantID := shared.NewTenantID()
	p, roundID := seedProcess(t, processes, tenantID)
	_ = p

	req := httptest.NewRequest(http.MethodPost, "/interview/rounds/"+roundID.String()+":skip", nil)
	req = withIdentity(req, tenantID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

// ---------------------------------------------------------------------------
// assertErrorCode is a test helper that decodes the response body and checks
// the "code" field matches wantCode.
// ---------------------------------------------------------------------------

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	var body v1.ErrorResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body), "body: %s", rec.Body.String())
	assert.Equal(t, wantCode, body.Code)
}

// Ensure time package is used (time.Time fields in responses).
var _ = time.Now
