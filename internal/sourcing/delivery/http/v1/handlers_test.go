package v1_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
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
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/application/queries"
	v1 "github.com/hustle/hireflow/internal/sourcing/delivery/http/v1"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	domainevents "github.com/hustle/hireflow/internal/sourcing/domain/events"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/sse"
)

// Reuse the in-memory fakes defined in commands_test by re-declaring here
// (test packages don't share — keep self-contained).
type memRepo struct {
	byHash  map[string]*entities.ResumeUpload
	batches map[string][]*entities.ResumeUpload
}

func newMemRepo() *memRepo {
	return &memRepo{
		byHash:  map[string]*entities.ResumeUpload{},
		batches: map[string][]*entities.ResumeUpload{},
	}
}

func (r *memRepo) Save(_ context.Context, u *entities.ResumeUpload) error {
	r.byHash[u.TenantID().String()+":"+u.ContentHash().String()] = u
	r.batches[u.BatchID().String()] = append(r.batches[u.BatchID().String()], u)
	_ = u.PullEvents()
	return nil
}

func (r *memRepo) FindByID(context.Context, shared.TenantID, uuid.UUID) (*entities.ResumeUpload, error) {
	return nil, repositories.ErrNotFound
}

func (r *memRepo) FindByContentHash(_ context.Context, t shared.TenantID, h string) (*entities.ResumeUpload, error) {
	if u, ok := r.byHash[t.String()+":"+h]; ok {
		return u, nil
	}
	return nil, repositories.ErrNotFound
}
func (r *memRepo) FindByContentHashAndIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID, _ string) (*entities.ResumeUpload, error) {
	return nil, repositories.ErrNotFound
}

func (r *memRepo) ClaimNextPending(context.Context) (*entities.ResumeUpload, error) {
	return nil, repositories.ErrNotFound
}

func (r *memRepo) ListByBatch(_ context.Context, _ shared.TenantID, b uuid.UUID) ([]*entities.ResumeUpload, error) {
	return r.batches[b.String()], nil
}

func (r *memRepo) BatchExistsForTenant(_ context.Context, _ shared.TenantID, b uuid.UUID) (bool, error) {
	return len(r.batches[b.String()]) > 0, nil
}

type memStorage struct{ puts map[string][]byte }

func newMemStorage() *memStorage { return &memStorage{puts: map[string][]byte{}} }

func (s *memStorage) Put(_ context.Context, k string, r io.Reader) error {
	b, _ := io.ReadAll(r)
	s.puts[k] = b
	return nil
}

func (s *memStorage) Open(_ context.Context, k string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.puts[k])), nil
}

func (s *memStorage) MoveToQuarantine(_ context.Context, k string) (string, error) {
	s.puts["quarantine/"+k] = s.puts[k]
	delete(s.puts, k)
	return "quarantine/" + k, nil
}
func (s *memStorage) Delete(_ context.Context, k string) error {
	delete(s.puts, k)
	return nil
}

const pdfMagic = "%PDF-1.4\n%test\n"

// stubCandRepo is an in-memory CandidateRepository for handler tests.
type stubCandRepo struct {
	byID map[string]*entities.Candidate
}

func newStubCandRepo() *stubCandRepo {
	return &stubCandRepo{byID: map[string]*entities.Candidate{}}
}

func (r *stubCandRepo) Save(_ context.Context, c *entities.Candidate) (*entities.Candidate, error) {
	return c, nil
}
func (r *stubCandRepo) FindByID(_ context.Context, _ shared.TenantID, id uuid.UUID) (*entities.Candidate, error) {
	if c, ok := r.byID[id.String()]; ok {
		return c, nil
	}
	return nil, repositories.ErrCandidateNotFound
}
func (r *stubCandRepo) FindByContentHash(_ context.Context, _ shared.TenantID, _ string) (*entities.Candidate, error) {
	return nil, repositories.ErrCandidateNotFound
}
func (r *stubCandRepo) ListByTenant(_ context.Context, _ shared.TenantID) ([]*entities.Candidate, error) {
	return nil, nil
}
func (r *stubCandRepo) UpdateProfileEmbedding(_ context.Context, _ uuid.UUID, _ shared.TenantID, _ []float32) error {
	return nil
}
func (r *stubCandRepo) EraseCascade(_ context.Context, _ shared.TenantID, _ uuid.UUID) ([]string, error) {
	return nil, repositories.ErrCandidateNotFound
}

// stubEnc is a reversible encryptor: Encrypt prepends "ENC:", Decrypt strips it.
type stubEnc struct{}

func (stubEnc) Encrypt(_ context.Context, _ shared.TenantID, p string) (string, error) {
	return "ENC:" + p, nil
}
func (stubEnc) Decrypt(_ context.Context, _ shared.TenantID, ct string) (string, error) {
	if len(ct) >= 4 && ct[:4] == "ENC:" {
		return ct[4:], nil
	}
	return ct, nil
}

func newHandler(t *testing.T) (*v1.SourcingHandler, *memRepo, *memStorage) {
	repo := newMemRepo()
	store := newMemStorage()
	upload := commands.NewUploadResumeBatchHandler(repo, store, commands.UploadConfig{MaxFileBytes: 1 << 20})
	status := queries.NewGetBatchStatusHandler(repo)
	candRepo := newStubCandRepo()
	candHandler := queries.NewGetCandidateHandler(candRepo, stubEnc{}, auditinfra.NewNoopAuditWriter())
	// nil for listApplications, transition, and eraseCandidate — slice-1/2 tests don't exercise those endpoints.
	return v1.NewSourcingHandler(v1.SourcingHandlerDeps{Upload: upload, Status: status, Candidate: candHandler, Logger: zerolog.Nop()}), repo, store
}

// withIdentity injects an auth.Identity into the request context — required by requireIdentity().
func withIdentity(r *http.Request, tenant shared.TenantID) *http.Request {
	return r.WithContext(auth.WithIdentity(r.Context(), auth.Identity{
		TenantID:    tenant,
		RecruiterID: shared.NewRecruiterID(),
	}))
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

func TestBatchUpload_ValidFiles_Returns200WithItems(t *testing.T) {
	h, _, _ := newHandler(t)
	router := chi.NewRouter()
	v1.Mount(router, h)

	body, ct := writeMultipart(t, map[string][]byte{
		"alice.pdf": []byte(pdfMagic),
		"bob.pdf":   []byte(pdfMagic + "different content "),
	})
	intentID := uuid.New().String()
	req := httptest.NewRequest(http.MethodPost,
		"/intents/"+intentID+"/resumes:batch", body)
	req.Header.Set("Content-Type", ct)
	req = withIdentity(req, shared.NewTenantID())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp v1.BatchUploadResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Len(t, resp.Items, 2)
	assert.NotEmpty(t, resp.BatchID)
	for _, it := range resp.Items {
		assert.Contains(t, []string{"queued", "deduplicated"}, it.Status, it.Filename)
	}
}

func TestBatchUpload_NoFiles_Returns400(t *testing.T) {
	h, _, _ := newHandler(t)
	router := chi.NewRouter()
	v1.Mount(router, h)

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	require.NoError(t, mw.Close()) // empty form

	req := httptest.NewRequest(http.MethodPost,
		"/intents/"+uuid.New().String()+"/resumes:batch", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req = withIdentity(req, shared.NewTenantID())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestBatchUpload_MissingIdentity_Returns401(t *testing.T) {
	h, _, _ := newHandler(t)
	router := chi.NewRouter()
	v1.Mount(router, h)

	body, ct := writeMultipart(t, map[string][]byte{"x.pdf": []byte(pdfMagic)})
	req := httptest.NewRequest(http.MethodPost,
		"/intents/"+uuid.New().String()+"/resumes:batch", body)
	req.Header.Set("Content-Type", ct)
	// No withIdentity — identity missing.

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestBatchUpload_InvalidIntentID_Returns400(t *testing.T) {
	h, _, _ := newHandler(t)
	router := chi.NewRouter()
	v1.Mount(router, h)

	body, ct := writeMultipart(t, map[string][]byte{"x.pdf": []byte(pdfMagic)})
	req := httptest.NewRequest(http.MethodPost,
		"/intents/not-a-uuid/resumes:batch", body)
	req.Header.Set("Content-Type", ct)
	req = withIdentity(req, shared.NewTenantID())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetBatchStatus_ReturnsRows(t *testing.T) {
	h, _, _ := newHandler(t)
	router := chi.NewRouter()
	v1.Mount(router, h)

	// First upload a batch via the API to get a real batch_id.
	body, ct := writeMultipart(t, map[string][]byte{"alice.pdf": []byte(pdfMagic)})
	tenant := shared.NewTenantID()
	req := httptest.NewRequest(http.MethodPost,
		"/intents/"+uuid.New().String()+"/resumes:batch", body)
	req.Header.Set("Content-Type", ct)
	req = withIdentity(req, tenant)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var uploadResp v1.BatchUploadResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&uploadResp))

	// GET status of the batch.
	statusReq := httptest.NewRequest(http.MethodGet,
		"/resumes/batches/"+uploadResp.BatchID, nil)
	statusReq = withIdentity(statusReq, tenant)
	statusRec := httptest.NewRecorder()
	router.ServeHTTP(statusRec, statusReq)
	require.Equal(t, http.StatusOK, statusRec.Code, statusRec.Body.String())

	var statusResp v1.BatchStatusResponse
	require.NoError(t, json.NewDecoder(statusRec.Body).Decode(&statusResp))
	assert.Equal(t, 1, statusResp.Summary.Total)
	assert.Len(t, statusResp.Items, 1)
}

// Ensure mime check rejects non-pdf via the API.
func TestBatchUpload_BadMimeFile_AppearsAsItemError(t *testing.T) {
	h, _, _ := newHandler(t)
	router := chi.NewRouter()
	v1.Mount(router, h)

	body, ct := writeMultipart(t, map[string][]byte{
		"evil.png": {0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/intents/"+uuid.New().String()+"/resumes:batch", body)
	req.Header.Set("Content-Type", ct)
	req = withIdentity(req, shared.NewTenantID())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp v1.BatchUploadResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Items, 1)
	require.NotNil(t, resp.Items[0].Error)
	assert.Equal(t, "mime_unsupported", resp.Items[0].Error.Code)
	// Sanity: didn't return a redirect or 5xx.
	assert.NotContains(t, strings.ToLower(rec.Body.String()), "internal")
}

// newCandidateForHandler builds a Candidate with predictable encrypted PII for handler tests.
func newCandidateForHandler(t *testing.T, tenant shared.TenantID) *entities.Candidate {
	t.Helper()
	h, err := vo.NewContentHash("eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	require.NoError(t, err)
	profile := vo.NewParsedProfile()
	profile.Personal.FullName = "Bob"
	c, err := entities.NewCandidate(entities.NewCandidateInput{
		TenantID:    tenant,
		ContentHash: h,
		Profile:     profile,
		Encrypted: entities.EncryptedPersonal{
			FullName: "ENC:Bob",
			Email:    "ENC:bob@example.com",
			Phone:    "ENC:+91-999",
		},
		Location: "Mumbai",
		Headline: "Engineer",
		Source:   "manual_upload",
	})
	require.NoError(t, err)
	return c
}

func TestGetCandidate_HappyPath(t *testing.T) {
	tenant := shared.NewTenantID()
	cand := newCandidateForHandler(t, tenant)

	candRepo := newStubCandRepo()
	candRepo.byID[cand.ID().String()] = cand
	candHandler := queries.NewGetCandidateHandler(candRepo, stubEnc{}, auditinfra.NewNoopAuditWriter())

	repo := newMemRepo()
	store := newMemStorage()
	upload := commands.NewUploadResumeBatchHandler(repo, store, commands.UploadConfig{MaxFileBytes: 1 << 20})
	status := queries.NewGetBatchStatusHandler(repo)
	h := v1.NewSourcingHandler(v1.SourcingHandlerDeps{Upload: upload, Status: status, Candidate: candHandler, Logger: zerolog.Nop()})

	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodGet, "/candidates/"+cand.ID().String(), nil)
	req = withIdentity(req, tenant)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp v1.CandidateDetailResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, cand.ID().String(), resp.ID)
	assert.Equal(t, "Bob", resp.Personal.FullName)
	assert.Equal(t, "bob@example.com", resp.Personal.Email)
	assert.Equal(t, "+91-999", resp.Personal.Phone)
	assert.Equal(t, "Mumbai", resp.Location)
	assert.NotEmpty(t, resp.CreatedAt)
}

func TestGetCandidate_NotFound_Returns404(t *testing.T) {
	candRepo := newStubCandRepo() // empty — nothing stored
	candHandler := queries.NewGetCandidateHandler(candRepo, stubEnc{}, auditinfra.NewNoopAuditWriter())

	repo := newMemRepo()
	store := newMemStorage()
	upload := commands.NewUploadResumeBatchHandler(repo, store, commands.UploadConfig{MaxFileBytes: 1 << 20})
	status := queries.NewGetBatchStatusHandler(repo)
	h := v1.NewSourcingHandler(v1.SourcingHandlerDeps{Upload: upload, Status: status, Candidate: candHandler, Logger: zerolog.Nop()})

	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodGet, "/candidates/"+uuid.New().String(), nil)
	req = withIdentity(req, shared.NewTenantID())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetCandidate_NoAuth_Returns401(t *testing.T) {
	candRepo := newStubCandRepo()
	candHandler := queries.NewGetCandidateHandler(candRepo, stubEnc{}, auditinfra.NewNoopAuditWriter())

	repo := newMemRepo()
	store := newMemStorage()
	upload := commands.NewUploadResumeBatchHandler(repo, store, commands.UploadConfig{MaxFileBytes: 1 << 20})
	status := queries.NewGetBatchStatusHandler(repo)
	h := v1.NewSourcingHandler(v1.SourcingHandlerDeps{Upload: upload, Status: status, Candidate: candHandler, Logger: zerolog.Nop()})

	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodGet, "/candidates/"+uuid.New().String(), nil)
	// No withIdentity — no auth context.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// capturingAuditWriter records every Write call for assertion in handler tests.
type capturingAuditWriter struct {
	events []auditdomain.AuditEvent
	err    error
}

func (w *capturingAuditWriter) Write(_ context.Context, e auditdomain.AuditEvent) error {
	if w.err != nil {
		return w.err
	}
	w.events = append(w.events, e)
	return nil
}

func TestGetCandidate_AuditWrittenOnHappyPath(t *testing.T) {
	tenant := shared.NewTenantID()
	cand := newCandidateForHandler(t, tenant)

	candRepo := newStubCandRepo()
	candRepo.byID[cand.ID().String()] = cand
	capture := &capturingAuditWriter{}
	candHandler := queries.NewGetCandidateHandler(candRepo, stubEnc{}, capture)

	repo := newMemRepo()
	store := newMemStorage()
	upload := commands.NewUploadResumeBatchHandler(repo, store, commands.UploadConfig{MaxFileBytes: 1 << 20})
	status := queries.NewGetBatchStatusHandler(repo)
	h := v1.NewSourcingHandler(v1.SourcingHandlerDeps{Upload: upload, Status: status, Candidate: candHandler, Logger: zerolog.Nop()})

	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodGet, "/candidates/"+cand.ID().String(), nil)
	req = withIdentity(req, tenant)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, capture.events, 1, "expected exactly one audit event")
	ev := capture.events[0]
	assert.Equal(t, "candidate_read", ev.Action)
	assert.Equal(t, "candidate", ev.ResourceKind)
	assert.Equal(t, cand.ID(), ev.ResourceID)
}

func TestGetCandidate_AuditFailure_Returns500(t *testing.T) {
	tenant := shared.NewTenantID()
	cand := newCandidateForHandler(t, tenant)

	candRepo := newStubCandRepo()
	candRepo.byID[cand.ID().String()] = cand
	auditErr := errors.New("audit db down")
	capture := &capturingAuditWriter{err: auditErr}
	candHandler := queries.NewGetCandidateHandler(candRepo, stubEnc{}, capture)

	repo := newMemRepo()
	store := newMemStorage()
	upload := commands.NewUploadResumeBatchHandler(repo, store, commands.UploadConfig{MaxFileBytes: 1 << 20})
	status := queries.NewGetBatchStatusHandler(repo)
	h := v1.NewSourcingHandler(v1.SourcingHandlerDeps{Upload: upload, Status: status, Candidate: candHandler, Logger: zerolog.Nop()})

	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodGet, "/candidates/"+cand.ID().String(), nil)
	req = withIdentity(req, tenant)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ---------------------------------------------------------------------------
// ListApplications tests
// ---------------------------------------------------------------------------

// stubAppRepo is an in-memory ApplicationRepository for handler tests.
type stubAppRepo struct {
	apps []*entities.Application
}

func (r *stubAppRepo) Save(_ context.Context, _ *entities.Application) error { return nil }
func (r *stubAppRepo) FindByID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.Application, error) {
	return nil, repositories.ErrApplicationNotFound
}
func (r *stubAppRepo) FindByCandidateAndIntent(_ context.Context, _ shared.TenantID, _, _ uuid.UUID) (*entities.Application, error) {
	return nil, repositories.ErrApplicationNotFound
}
func (r *stubAppRepo) ListByIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID, _ repositories.ApplicationListFilter) ([]*entities.Application, error) {
	return r.apps, nil
}
func (r *stubAppRepo) ClaimNextNew(_ context.Context) (*entities.Application, error) {
	return nil, repositories.ErrApplicationNotFound
}
func (r *stubAppRepo) TopByCoarseScoreForIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID, _ int) ([]*entities.Application, error) {
	return nil, nil
}
func (r *stubAppRepo) InvalidateJudgmentsForIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID) error {
	return nil
}

// buildListApplicationsHandler creates a SourcingHandler wired with a
// ListApplicationsHandler backed by the given app and candidate repos.
func buildListApplicationsHandler(t *testing.T, appRepo repositories.ApplicationRepository, candRepo repositories.CandidateRepository) *v1.SourcingHandler {
	t.Helper()
	repo := newMemRepo()
	store := newMemStorage()
	upload := commands.NewUploadResumeBatchHandler(repo, store, commands.UploadConfig{MaxFileBytes: 1 << 20})
	status := queries.NewGetBatchStatusHandler(repo)
	listAppsHandler := queries.NewListApplicationsHandler(appRepo, candRepo, stubEnc{})
	return v1.NewSourcingHandler(v1.SourcingHandlerDeps{Upload: upload, Status: status, ListApplications: listAppsHandler, Logger: zerolog.Nop()})
}

// buildScoredApplicationForHandler returns a scored Application with the given candidate.
func buildScoredApplicationForHandler(t *testing.T, tenant shared.TenantID, candidateID, intentID uuid.UUID) *entities.Application {
	t.Helper()
	app, err := entities.NewApplication(entities.NewApplicationInput{
		TenantID:             tenant,
		CandidateID:          candidateID,
		IntentID:             intentID,
		IntentSpecVersion:    1,
		ProfileSchemaVersion: 1,
	})
	require.NoError(t, err)
	rules := vo.RuleMatchReport{
		Results: []vo.RuleResult{
			{Criterion: vo.RuleCriterion{Type: "skill", Name: "Go", Required: true}, Passed: true},
		},
	}
	require.NoError(t, app.RecordRuleMatch(rules))
	require.NoError(t, app.RecordEmbeddingScore(0.85))
	require.NoError(t, app.MarkScored(nil))
	_ = app.PullEvents()
	return app
}

func TestListApplications_HappyPath_Returns200WithItems(t *testing.T) {
	tenant := shared.NewTenantID()
	intentID := uuid.New()

	hashA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashB := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	hA, err := vo.NewContentHash(hashA)
	require.NoError(t, err)
	profileA := vo.NewParsedProfile()
	profileA.Personal.FullName = "Alice"
	candA, err := entities.NewCandidate(entities.NewCandidateInput{
		TenantID:    tenant,
		ContentHash: hA,
		Profile:     profileA,
		Encrypted:   entities.EncryptedPersonal{FullName: "ENC:Alice", Email: "ENC:alice@example.com"},
		Location:    "Bangalore",
		Headline:    "Go Engineer",
		Source:      "manual_upload",
	})
	require.NoError(t, err)
	_ = candA.PullEvents()

	hB, err := vo.NewContentHash(hashB)
	require.NoError(t, err)
	profileB := vo.NewParsedProfile()
	profileB.Personal.FullName = "Bob"
	candB, err := entities.NewCandidate(entities.NewCandidateInput{
		TenantID:    tenant,
		ContentHash: hB,
		Profile:     profileB,
		Encrypted:   entities.EncryptedPersonal{FullName: "ENC:Bob", Email: "ENC:bob@example.com"},
		Location:    "Mumbai",
		Headline:    "Backend Engineer",
		Source:      "manual_upload",
	})
	require.NoError(t, err)
	_ = candB.PullEvents()

	appA := buildScoredApplicationForHandler(t, tenant, candA.ID(), intentID)
	appB := buildScoredApplicationForHandler(t, tenant, candB.ID(), intentID)

	candRepo := newStubCandRepo()
	candRepo.byID[candA.ID().String()] = candA
	candRepo.byID[candB.ID().String()] = candB

	appRepo := &stubAppRepo{apps: []*entities.Application{appA, appB}}
	h := buildListApplicationsHandler(t, appRepo, candRepo)

	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodGet, "/intents/"+intentID.String()+"/applications", nil)
	req = withIdentity(req, tenant)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp v1.ApplicationListResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, 2, resp.Total)
	assert.Len(t, resp.Items, 2)

	for _, item := range resp.Items {
		assert.NotEmpty(t, item.ApplicationID)
		assert.NotEmpty(t, item.Candidate.ID)
		require.NotNil(t, item.Score.EmbeddingScore)
		assert.InDelta(t, 0.85, *item.Score.EmbeddingScore, 1e-9)
		assert.NotEmpty(t, item.Candidate.FullNameMasked)
		assert.True(t, strings.HasSuffix(item.Candidate.FullNameMasked, "***"))
		assert.NotEmpty(t, item.Status)
	}
}

func TestListApplications_NoAuth_Returns401(t *testing.T) {
	appRepo := &stubAppRepo{apps: []*entities.Application{}}
	candRepo := newStubCandRepo()
	h := buildListApplicationsHandler(t, appRepo, candRepo)

	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodGet, "/intents/"+uuid.New().String()+"/applications", nil)
	// No withIdentity — missing auth context.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestListApplications_InvalidIntentID_Returns400(t *testing.T) {
	appRepo := &stubAppRepo{apps: []*entities.Application{}}
	candRepo := newStubCandRepo()
	h := buildListApplicationsHandler(t, appRepo, candRepo)

	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodGet, "/intents/not-a-uuid/applications", nil)
	req = withIdentity(req, shared.NewTenantID())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------------
// Transition endpoint tests (shortlist / reject / hire)
// ---------------------------------------------------------------------------

// transitionAppRepo is an in-memory ApplicationRepository that supports
// FindByID with a pre-seeded map, for testing lifecycle HTTP endpoints.
type transitionAppRepo struct {
	byID    map[uuid.UUID]*entities.Application
	saveErr error
}

func newTransitionAppRepo() *transitionAppRepo {
	return &transitionAppRepo{byID: map[uuid.UUID]*entities.Application{}}
}

func (r *transitionAppRepo) Save(_ context.Context, a *entities.Application) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	_ = a.PullEvents()
	r.byID[a.ID()] = a
	return nil
}
func (r *transitionAppRepo) FindByID(_ context.Context, _ shared.TenantID, id uuid.UUID) (*entities.Application, error) {
	if a, ok := r.byID[id]; ok {
		return a, nil
	}
	return nil, repositories.ErrApplicationNotFound
}
func (r *transitionAppRepo) FindByCandidateAndIntent(_ context.Context, _ shared.TenantID, _, _ uuid.UUID) (*entities.Application, error) {
	return nil, repositories.ErrApplicationNotFound
}
func (r *transitionAppRepo) ListByIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID, _ repositories.ApplicationListFilter) ([]*entities.Application, error) {
	return nil, nil
}
func (r *transitionAppRepo) ClaimNextNew(_ context.Context) (*entities.Application, error) {
	return nil, repositories.ErrApplicationNotFound
}
func (r *transitionAppRepo) TopByCoarseScoreForIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID, _ int) ([]*entities.Application, error) {
	return nil, nil
}
func (r *transitionAppRepo) InvalidateJudgmentsForIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID) error {
	return nil
}

// failAuditWriter satisfies auditdomain.AuditWriter and always fails.
type failAuditWriter struct{ err error }

func (f *failAuditWriter) Write(_ context.Context, _ auditdomain.AuditEvent) error { return f.err }

// buildTransitionSourcingHandler creates a SourcingHandler wired with a
// TransitionApplicationHandler backed by the given app repo and audit writer.
func buildTransitionSourcingHandler(t *testing.T, appRepo repositories.ApplicationRepository, audit auditdomain.AuditWriter) *v1.SourcingHandler {
	t.Helper()
	repo := newMemRepo()
	store := newMemStorage()
	upload := commands.NewUploadResumeBatchHandler(repo, store, commands.UploadConfig{MaxFileBytes: 1 << 20})
	status := queries.NewGetBatchStatusHandler(repo)
	transitionH := commands.NewTransitionApplicationHandler(appRepo, audit)
	return v1.NewSourcingHandler(v1.SourcingHandlerDeps{Upload: upload, Status: status, Transition: transitionH, Logger: zerolog.Nop()})
}

// buildScoredApp builds a scored Application seeded in the given repo.
func buildScoredApp(t *testing.T, repo *transitionAppRepo, tenant shared.TenantID) *entities.Application {
	t.Helper()
	app, err := entities.NewApplication(entities.NewApplicationInput{
		TenantID:             tenant,
		CandidateID:          uuid.New(),
		IntentID:             uuid.New(),
		IntentSpecVersion:    1,
		ProfileSchemaVersion: 1,
	})
	require.NoError(t, err)
	rules := vo.RuleMatchReport{
		Results: []vo.RuleResult{
			{Criterion: vo.RuleCriterion{Type: "skill", Name: "Go", Required: true}, Passed: true},
		},
	}
	require.NoError(t, app.RecordRuleMatch(rules))
	require.NoError(t, app.RecordEmbeddingScore(0.8))
	require.NoError(t, app.MarkScored(nil))
	_ = app.PullEvents()
	repo.byID[app.ID()] = app
	return app
}

func TestShortlistApplication_HappyPath_Returns204(t *testing.T) {
	tenant := shared.NewTenantID()
	appRepo := newTransitionAppRepo()
	app := buildScoredApp(t, appRepo, tenant)

	h := buildTransitionSourcingHandler(t, appRepo, auditinfra.NewNoopAuditWriter())
	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodPost, "/applications/"+app.ID().String()+":shortlist", nil)
	req = withIdentity(req, tenant)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

func TestShortlistApplication_NotFound_Returns404(t *testing.T) {
	appRepo := newTransitionAppRepo() // empty

	h := buildTransitionSourcingHandler(t, appRepo, auditinfra.NewNoopAuditWriter())
	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodPost, "/applications/"+uuid.New().String()+":shortlist", nil)
	req = withIdentity(req, shared.NewTenantID())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestShortlistApplication_NoAuth_Returns401(t *testing.T) {
	appRepo := newTransitionAppRepo()

	h := buildTransitionSourcingHandler(t, appRepo, auditinfra.NewNoopAuditWriter())
	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodPost, "/applications/"+uuid.New().String()+":shortlist", nil)
	// no withIdentity
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestShortlistApplication_InvalidTransition_Returns400(t *testing.T) {
	tenant := shared.NewTenantID()
	appRepo := newTransitionAppRepo()
	// Build a rejected app — cannot shortlist.
	app := buildScoredApp(t, appRepo, tenant)
	require.NoError(t, app.Reject(uuid.New(), "not a fit"))
	_ = app.PullEvents()

	h := buildTransitionSourcingHandler(t, appRepo, auditinfra.NewNoopAuditWriter())
	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodPost, "/applications/"+app.ID().String()+":shortlist", nil)
	req = withIdentity(req, tenant)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestShortlistApplication_AuditFailure_Returns500(t *testing.T) {
	tenant := shared.NewTenantID()
	appRepo := newTransitionAppRepo()
	app := buildScoredApp(t, appRepo, tenant)

	auditErr := errors.New("db down")
	h := buildTransitionSourcingHandler(t, appRepo, &failAuditWriter{err: auditErr})
	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodPost, "/applications/"+app.ID().String()+":shortlist", nil)
	req = withIdentity(req, tenant)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestRejectApplication_HappyPath_Returns204(t *testing.T) {
	tenant := shared.NewTenantID()
	appRepo := newTransitionAppRepo()
	app := buildScoredApp(t, appRepo, tenant)

	h := buildTransitionSourcingHandler(t, appRepo, auditinfra.NewNoopAuditWriter())
	router := chi.NewRouter()
	v1.Mount(router, h)

	body := strings.NewReader(`{"reason":"does not meet requirements"}`)
	req := httptest.NewRequest(http.MethodPost, "/applications/"+app.ID().String()+":reject", body)
	req.Header.Set("Content-Type", "application/json")
	req = withIdentity(req, tenant)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

func TestRejectApplication_MissingReason_Returns400(t *testing.T) {
	tenant := shared.NewTenantID()
	appRepo := newTransitionAppRepo()
	app := buildScoredApp(t, appRepo, tenant)

	h := buildTransitionSourcingHandler(t, appRepo, auditinfra.NewNoopAuditWriter())
	router := chi.NewRouter()
	v1.Mount(router, h)

	body := strings.NewReader(`{"reason":""}`)
	req := httptest.NewRequest(http.MethodPost, "/applications/"+app.ID().String()+":reject", body)
	req.Header.Set("Content-Type", "application/json")
	req = withIdentity(req, tenant)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRejectApplication_InvalidBody_Returns400(t *testing.T) {
	appRepo := newTransitionAppRepo()

	h := buildTransitionSourcingHandler(t, appRepo, auditinfra.NewNoopAuditWriter())
	router := chi.NewRouter()
	v1.Mount(router, h)

	body := strings.NewReader(`not json`)
	req := httptest.NewRequest(http.MethodPost, "/applications/"+uuid.New().String()+":reject", body)
	req.Header.Set("Content-Type", "application/json")
	req = withIdentity(req, shared.NewTenantID())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRejectApplication_NotFound_Returns404(t *testing.T) {
	appRepo := newTransitionAppRepo()

	h := buildTransitionSourcingHandler(t, appRepo, auditinfra.NewNoopAuditWriter())
	router := chi.NewRouter()
	v1.Mount(router, h)

	body := strings.NewReader(`{"reason":"not a fit"}`)
	req := httptest.NewRequest(http.MethodPost, "/applications/"+uuid.New().String()+":reject", body)
	req.Header.Set("Content-Type", "application/json")
	req = withIdentity(req, shared.NewTenantID())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRejectApplication_NoAuth_Returns401(t *testing.T) {
	appRepo := newTransitionAppRepo()

	h := buildTransitionSourcingHandler(t, appRepo, auditinfra.NewNoopAuditWriter())
	router := chi.NewRouter()
	v1.Mount(router, h)

	body := strings.NewReader(`{"reason":"overqualified"}`)
	req := httptest.NewRequest(http.MethodPost, "/applications/"+uuid.New().String()+":reject", body)
	req.Header.Set("Content-Type", "application/json")
	// no withIdentity
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHireApplication_HappyPath_Returns204(t *testing.T) {
	tenant := shared.NewTenantID()
	appRepo := newTransitionAppRepo()
	app := buildScoredApp(t, appRepo, tenant)

	h := buildTransitionSourcingHandler(t, appRepo, auditinfra.NewNoopAuditWriter())
	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodPost, "/applications/"+app.ID().String()+":hire", nil)
	req = withIdentity(req, tenant)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

func TestHireApplication_NotFound_Returns404(t *testing.T) {
	appRepo := newTransitionAppRepo()

	h := buildTransitionSourcingHandler(t, appRepo, auditinfra.NewNoopAuditWriter())
	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodPost, "/applications/"+uuid.New().String()+":hire", nil)
	req = withIdentity(req, shared.NewTenantID())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHireApplication_InvalidTransition_Returns400(t *testing.T) {
	tenant := shared.NewTenantID()
	appRepo := newTransitionAppRepo()
	// Build a New (unscored) app — cannot hire.
	app, err := entities.NewApplication(entities.NewApplicationInput{
		TenantID:             tenant,
		CandidateID:          uuid.New(),
		IntentID:             uuid.New(),
		IntentSpecVersion:    1,
		ProfileSchemaVersion: 1,
	})
	require.NoError(t, err)
	appRepo.byID[app.ID()] = app

	h := buildTransitionSourcingHandler(t, appRepo, auditinfra.NewNoopAuditWriter())
	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodPost, "/applications/"+app.ID().String()+":hire", nil)
	req = withIdentity(req, tenant)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHireApplication_NoAuth_Returns401(t *testing.T) {
	appRepo := newTransitionAppRepo()

	h := buildTransitionSourcingHandler(t, appRepo, auditinfra.NewNoopAuditWriter())
	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodPost, "/applications/"+uuid.New().String()+":hire", nil)
	// no withIdentity
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ---------------------------------------------------------------------------
// RetryUpload endpoint tests
// ---------------------------------------------------------------------------

// retryRepo is a minimal ResumeUploadRepository for retry endpoint tests.
type retryRepo struct {
	byID  map[uuid.UUID]*entities.ResumeUpload
	saved []*entities.ResumeUpload
}

func newRetryRepo() *retryRepo {
	return &retryRepo{byID: map[uuid.UUID]*entities.ResumeUpload{}}
}

func (r *retryRepo) Save(_ context.Context, u *entities.ResumeUpload) error {
	_ = u.PullEvents()
	r.byID[u.ID()] = u
	r.saved = append(r.saved, u)
	return nil
}
func (r *retryRepo) FindByID(_ context.Context, _ shared.TenantID, id uuid.UUID) (*entities.ResumeUpload, error) {
	if u, ok := r.byID[id]; ok {
		return u, nil
	}
	return nil, repositories.ErrNotFound
}
func (r *retryRepo) FindByContentHash(_ context.Context, _ shared.TenantID, _ string) (*entities.ResumeUpload, error) {
	return nil, repositories.ErrNotFound
}
func (r *retryRepo) FindByContentHashAndIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID, _ string) (*entities.ResumeUpload, error) {
	return nil, repositories.ErrNotFound
}
func (r *retryRepo) ClaimNextPending(_ context.Context) (*entities.ResumeUpload, error) {
	return nil, repositories.ErrNotFound
}
func (r *retryRepo) ListByBatch(_ context.Context, _ shared.TenantID, _ uuid.UUID) ([]*entities.ResumeUpload, error) {
	return nil, nil
}
func (r *retryRepo) BatchExistsForTenant(_ context.Context, _ shared.TenantID, _ uuid.UUID) (bool, error) {
	return false, nil
}

// buildRetryUploadHandler creates a SourcingHandler wired with a
// RetryResumeUploadHandler backed by the given repo.
func buildRetryUploadHandler(t *testing.T, uploadRepo repositories.ResumeUploadRepository) *v1.SourcingHandler {
	t.Helper()
	repo := newMemRepo()
	store := newMemStorage()
	batchUpload := commands.NewUploadResumeBatchHandler(repo, store, commands.UploadConfig{MaxFileBytes: 1 << 20})
	status := queries.NewGetBatchStatusHandler(repo)
	retryH := commands.NewRetryResumeUploadHandler(uploadRepo)
	return v1.NewSourcingHandler(v1.SourcingHandlerDeps{Upload: batchUpload, Status: status, RetryUpload: retryH, Logger: zerolog.Nop()})
}

// seedUploadInStatus rehydrates a ResumeUpload in the given status into repo.
func seedUploadInStatus(repo *retryRepo, tenant shared.TenantID, status vo.UploadStatus) *entities.ResumeUpload {
	u := entities.RehydrateResumeUpload(entities.RehydrateInput{
		ID:           uuid.New(),
		TenantID:     tenant,
		IntentID:     uuid.New(),
		BatchID:      uuid.New(),
		StorageKey:   "key/file.pdf",
		OriginalName: "resume.pdf",
		Status:       status,
		AttemptCount: 2,
		LastError:    "previous error",
	})
	repo.byID[u.ID()] = u
	return u
}

func TestRetryUpload_FromFailed_Returns204(t *testing.T) {
	tenant := shared.NewTenantID()
	rr := newRetryRepo()
	upload := seedUploadInStatus(rr, tenant, vo.StatusFailed)

	h := buildRetryUploadHandler(t, rr)
	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodPost, "/resumes/"+upload.ID().String()+":retry", nil)
	req = withIdentity(req, tenant)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

func TestRetryUpload_FromQuarantined_Returns204(t *testing.T) {
	tenant := shared.NewTenantID()
	rr := newRetryRepo()
	upload := seedUploadInStatus(rr, tenant, vo.StatusQuarantined)

	h := buildRetryUploadHandler(t, rr)
	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodPost, "/resumes/"+upload.ID().String()+":retry", nil)
	req = withIdentity(req, tenant)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

func TestRetryUpload_NotFound_Returns404(t *testing.T) {
	rr := newRetryRepo() // empty

	h := buildRetryUploadHandler(t, rr)
	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodPost, "/resumes/"+uuid.New().String()+":retry", nil)
	req = withIdentity(req, shared.NewTenantID())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRetryUpload_NotRetryableStatus_Returns400(t *testing.T) {
	tenant := shared.NewTenantID()
	rr := newRetryRepo()
	upload := seedUploadInStatus(rr, tenant, vo.StatusPending)

	h := buildRetryUploadHandler(t, rr)
	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodPost, "/resumes/"+upload.ID().String()+":retry", nil)
	req = withIdentity(req, tenant)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRetryUpload_NoAuth_Returns401(t *testing.T) {
	rr := newRetryRepo()

	h := buildRetryUploadHandler(t, rr)
	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodPost, "/resumes/"+uuid.New().String()+":retry", nil)
	// no withIdentity
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ---------------------------------------------------------------------------
// RescoreIntent endpoint tests
// ---------------------------------------------------------------------------

// stubScoreIntentDispatcher is a local fake that satisfies the unexported
// scoreIntentDispatcher interface inside the commands package. It is used
// to construct a real *commands.RescoreIntentHandler without needing the full
// ScoreIntentHandler dependency graph.
type stubScoreIntentDispatcher struct{ err error }

func (s *stubScoreIntentDispatcher) Handle(_ context.Context, _ commands.ScoreIntentInput) error {
	return s.err
}

// buildRescoreIntentHandler creates a SourcingHandler wired with a
// RescoreIntentHandler backed by the given application repo.
func buildRescoreIntentHandler(t *testing.T, appRepo repositories.ApplicationRepository) *v1.SourcingHandler {
	t.Helper()
	repo := newMemRepo()
	store := newMemStorage()
	batchUpload := commands.NewUploadResumeBatchHandler(repo, store, commands.UploadConfig{MaxFileBytes: 1 << 20})
	status := queries.NewGetBatchStatusHandler(repo)
	dispatcher := &stubScoreIntentDispatcher{}
	rescoreH := commands.NewRescoreIntentHandler(appRepo, dispatcher, auditinfra.NewNoopAuditWriter())
	return v1.NewSourcingHandler(v1.SourcingHandlerDeps{Upload: batchUpload, Status: status, RescoreIntent: rescoreH, Logger: zerolog.Nop()})
}

func TestRescoreIntent_HappyPath_Returns202(t *testing.T) {
	tenant := shared.NewTenantID()
	appRepo := &stubAppRepo{}

	h := buildRescoreIntentHandler(t, appRepo)
	router := chi.NewRouter()
	v1.Mount(router, h)

	intentID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/intents/"+intentID.String()+"/applications:rescore", nil)
	req = withIdentity(req, tenant)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
}

func TestRescoreIntent_NoAuth_Returns401(t *testing.T) {
	appRepo := &stubAppRepo{}

	h := buildRescoreIntentHandler(t, appRepo)
	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodPost, "/intents/"+uuid.New().String()+"/applications:rescore", nil)
	// no withIdentity
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRescoreIntent_InvalidIntentID_Returns400(t *testing.T) {
	appRepo := &stubAppRepo{}

	h := buildRescoreIntentHandler(t, appRepo)
	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodPost, "/intents/not-a-uuid/applications:rescore", nil)
	req = withIdentity(req, shared.NewTenantID())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------------
// BatchEvents SSE endpoint tests
// ---------------------------------------------------------------------------

// newSSEHandler builds a SourcingHandler wired with a real BatchEventFanout
// and a configurable heartbeat interval (for fast testing). Returns the
// memRepo so tests can register batch_ids they want to be discoverable by
// the tenant-ownership gate.
func newSSEHandler(t *testing.T, heartbeat time.Duration) (*v1.SourcingHandler, *sse.BatchEventFanout, *memRepo) {
	t.Helper()
	fanout := sse.NewBatchEventFanout(zerolog.Nop())
	repo := newMemRepo()
	store := newMemStorage()
	upload := commands.NewUploadResumeBatchHandler(repo, store, commands.UploadConfig{MaxFileBytes: 1 << 20})
	status := queries.NewGetBatchStatusHandler(repo)
	h := v1.NewSourcingHandler(v1.SourcingHandlerDeps{Upload: upload, Status: status, Fanout: fanout, Heartbeat: heartbeat, Logger: zerolog.Nop()})
	return h, fanout, repo
}

// markBatchExists registers a batch_id with the memRepo so BatchExistsForTenant
// returns true for it. Used by SSE happy-path tests that fire fake events
// against the fanout without going through the full upload pipeline.
func (r *memRepo) markBatchExists(batchID uuid.UUID) {
	r.batches[batchID.String()] = []*entities.ResumeUpload{nil}
}

func TestBatchEvents_StreamsEvent(t *testing.T) {
	const timeout = 5 * time.Second
	h, fanout, repo := newSSEHandler(t, 30*time.Second) // long heartbeat so only real events fire

	batchID := uuid.New()
	tenant := shared.NewTenantID()
	repo.markBatchExists(batchID)

	// Inject identity via middleware since withIdentity only works on
	// httptest.Request; a real httptest.NewServer needs a middleware approach.
	authRouter := chi.NewRouter()
	authRouter.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = r.WithContext(auth.WithIdentity(r.Context(), auth.Identity{
				TenantID:    tenant,
				RecruiterID: shared.NewRecruiterID(),
			}))
			next.ServeHTTP(w, r)
		})
	})
	v1.Mount(authRouter, h)

	authSrv := httptest.NewServer(authRouter)
	defer authSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	authURL := authSrv.URL + "/resumes/batches/" + batchID.String() + "/events"
	authReq, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(authReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	// Read bytes from the SSE body in a goroutine.
	received := make(chan string, 8)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				received <- string(buf[:n])
			}
			if readErr != nil {
				close(received)
				return
			}
		}
	}()

	// Give the subscriber a moment to register, then fire a real domain event.
	time.Sleep(20 * time.Millisecond)
	ev := domainevents.ResumeUploadAccepted{
		UploadID:    uuid.New(),
		TenantID:    tenant,
		IntentID:    uuid.New(),
		BatchID:     batchID,
		ContentHash: "abc123",
		OccurredAt:  time.Now().UTC(),
	}
	require.NoError(t, fanout.OnEvent(context.Background(), ev))

	select {
	case got, ok := <-received:
		require.True(t, ok, "channel closed before receiving data")
		assert.Contains(t, got, "item_accepted")
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SSE event")
	}
}

func TestBatchEvents_Heartbeat(t *testing.T) {
	const heartbeatInterval = 50 * time.Millisecond
	h, _, repo := newSSEHandler(t, heartbeatInterval)

	tenant := shared.NewTenantID()
	batchID := uuid.New()
	repo.markBatchExists(batchID)

	authRouter := chi.NewRouter()
	authRouter.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = r.WithContext(auth.WithIdentity(r.Context(), auth.Identity{
				TenantID:    tenant,
				RecruiterID: shared.NewRecruiterID(),
			}))
			next.ServeHTTP(w, r)
		})
	})
	v1.Mount(authRouter, h)

	srv := httptest.NewServer(authRouter)
	defer srv.Close()

	url := srv.URL + "/resumes/batches/" + batchID.String() + "/events"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	received := make(chan string, 8)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				received <- string(buf[:n])
			}
			if err != nil {
				close(received)
				return
			}
		}
	}()

	// Wait for a heartbeat — should arrive within 200ms (4x the 50ms interval).
	select {
	case got, ok := <-received:
		require.True(t, ok)
		assert.Contains(t, got, ":ping")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for SSE heartbeat")
	}
}

func TestBatchEvents_BadUUID_Returns400(t *testing.T) {
	h, _, _ := newSSEHandler(t, 30*time.Second)

	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodGet, "/resumes/batches/not-a-uuid/events", nil)
	req = withIdentity(req, shared.NewTenantID())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestBatchEvents_NoIdentity_Returns401(t *testing.T) {
	h, _, _ := newSSEHandler(t, 30*time.Second)

	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodGet, "/resumes/batches/"+uuid.New().String()+"/events", nil)
	// no withIdentity
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestBatchEvents_UnknownBatch_Returns404 verifies that a batch_id which
// doesn't exist for the caller's tenant is rejected with 404. The Postgres
// repo's BatchExistsForTenant filters by tenant_id, so a cross-tenant attempt
// (knowing another tenant's batch UUID) also lands here. The 404 — rather
// than 403 — avoids leaking the existence of batches in other tenants.
// Cross-tenant scoping is verified end-to-end by the Postgres integration
// test for BatchExistsForTenant; this unit test covers the handler's gate.
func TestBatchEvents_UnknownBatch_Returns404(t *testing.T) {
	h, _, _ := newSSEHandler(t, 30*time.Second)

	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodGet,
		"/resumes/batches/"+uuid.New().String()+"/events", nil)
	req = withIdentity(req, shared.NewTenantID())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code,
		"unknown batch must return 404")
	assert.Contains(t, rec.Body.String(), "batch_not_found")
}
