package v1_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/shared/infrastructure/auth"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/application/queries"
	v1 "github.com/hustle/hireflow/internal/sourcing/delivery/http/v1"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
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

func (r *memRepo) ClaimNextPending(context.Context) (*entities.ResumeUpload, error) {
	return nil, repositories.ErrNotFound
}

func (r *memRepo) ListByBatch(_ context.Context, _ shared.TenantID, b uuid.UUID) ([]*entities.ResumeUpload, error) {
	return r.batches[b.String()], nil
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
	candHandler := queries.NewGetCandidateHandler(candRepo, stubEnc{})
	// nil for listApplications — slice-1/2 tests don't exercise that endpoint.
	return v1.NewSourcingHandler(upload, status, candHandler, nil, zerolog.Nop()), repo, store
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
	candHandler := queries.NewGetCandidateHandler(candRepo, stubEnc{})

	repo := newMemRepo()
	store := newMemStorage()
	upload := commands.NewUploadResumeBatchHandler(repo, store, commands.UploadConfig{MaxFileBytes: 1 << 20})
	status := queries.NewGetBatchStatusHandler(repo)
	h := v1.NewSourcingHandler(upload, status, candHandler, nil, zerolog.Nop())

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
	candHandler := queries.NewGetCandidateHandler(candRepo, stubEnc{})

	repo := newMemRepo()
	store := newMemStorage()
	upload := commands.NewUploadResumeBatchHandler(repo, store, commands.UploadConfig{MaxFileBytes: 1 << 20})
	status := queries.NewGetBatchStatusHandler(repo)
	h := v1.NewSourcingHandler(upload, status, candHandler, nil, zerolog.Nop())

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
	candHandler := queries.NewGetCandidateHandler(candRepo, stubEnc{})

	repo := newMemRepo()
	store := newMemStorage()
	upload := commands.NewUploadResumeBatchHandler(repo, store, commands.UploadConfig{MaxFileBytes: 1 << 20})
	status := queries.NewGetBatchStatusHandler(repo)
	h := v1.NewSourcingHandler(upload, status, candHandler, nil, zerolog.Nop())

	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodGet, "/candidates/"+uuid.New().String(), nil)
	// No withIdentity — no auth context.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
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

// buildListApplicationsHandler creates a SourcingHandler wired with a
// ListApplicationsHandler backed by the given app and candidate repos.
func buildListApplicationsHandler(t *testing.T, appRepo repositories.ApplicationRepository, candRepo repositories.CandidateRepository) *v1.SourcingHandler {
	t.Helper()
	repo := newMemRepo()
	store := newMemStorage()
	upload := commands.NewUploadResumeBatchHandler(repo, store, commands.UploadConfig{MaxFileBytes: 1 << 20})
	status := queries.NewGetBatchStatusHandler(repo)
	listAppsHandler := queries.NewListApplicationsHandler(appRepo, candRepo, stubEnc{})
	return v1.NewSourcingHandler(upload, status, nil, listAppsHandler, zerolog.Nop())
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
