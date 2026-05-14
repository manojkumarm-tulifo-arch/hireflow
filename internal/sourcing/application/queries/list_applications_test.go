package queries_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/queries"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// stubListApplicationRepo supports ListByIntent with a preset list.
type stubListApplicationRepo struct {
	apps    []*entities.Application
	listErr error
}

func (r *stubListApplicationRepo) ListByIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID, _ repositories.ApplicationListFilter) ([]*entities.Application, error) {
	return r.apps, r.listErr
}
func (r *stubListApplicationRepo) Save(_ context.Context, _ *entities.Application) error {
	return nil
}
func (r *stubListApplicationRepo) FindByID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.Application, error) {
	return nil, repositories.ErrApplicationNotFound
}
func (r *stubListApplicationRepo) FindByCandidateAndIntent(_ context.Context, _ shared.TenantID, _, _ uuid.UUID) (*entities.Application, error) {
	return nil, repositories.ErrApplicationNotFound
}
func (r *stubListApplicationRepo) ClaimNextNew(_ context.Context) (*entities.Application, error) {
	return nil, repositories.ErrApplicationNotFound
}
func (r *stubListApplicationRepo) TopByCoarseScoreForIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID, _ int) ([]*entities.Application, error) {
	return nil, nil
}
func (r *stubListApplicationRepo) InvalidateJudgmentsForIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID) error {
	return nil
}

// stubListCandidateRepo supports FindByID with a preset map.
type stubListCandidateRepo struct {
	byID map[uuid.UUID]*entities.Candidate
}

func (r *stubListCandidateRepo) Save(_ context.Context, c *entities.Candidate) (*entities.Candidate, error) {
	return c, nil
}
func (r *stubListCandidateRepo) FindByID(_ context.Context, _ shared.TenantID, id uuid.UUID) (*entities.Candidate, error) {
	if c, ok := r.byID[id]; ok {
		return c, nil
	}
	return nil, repositories.ErrCandidateNotFound
}
func (r *stubListCandidateRepo) FindByContentHash(_ context.Context, _ shared.TenantID, _ string) (*entities.Candidate, error) {
	return nil, repositories.ErrCandidateNotFound
}
func (r *stubListCandidateRepo) ListByTenant(_ context.Context, _ shared.TenantID) ([]*entities.Candidate, error) {
	return nil, nil
}
func (r *stubListCandidateRepo) UpdateProfileEmbedding(_ context.Context, _ uuid.UUID, _ shared.TenantID, _ []float32) error {
	return nil
}
func (r *stubListCandidateRepo) EraseCascade(_ context.Context, _ shared.TenantID, id uuid.UUID) ([]string, error) {
	return nil, repositories.ErrCandidateNotFound
}

// stubListEncryptor stores plaintext as "ENC:<plaintext>" and reverses it.
type stubListEncryptor struct{}

func (stubListEncryptor) Encrypt(_ context.Context, _ shared.TenantID, p string) (string, error) {
	return "ENC:" + p, nil
}
func (stubListEncryptor) Decrypt(_ context.Context, _ shared.TenantID, ct string) (string, error) {
	if len(ct) >= 4 && ct[:4] == "ENC:" {
		return ct[4:], nil
	}
	return ct, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTenant() shared.TenantID { return shared.NewTenantID() }

func makeTestCandidate(t *testing.T, tenant shared.TenantID, fullName, encName string) *entities.Candidate {
	t.Helper()
	hash, err := vo.NewContentHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.NoError(t, err)
	profile := vo.NewParsedProfile()
	profile.Personal.FullName = fullName
	c, err := entities.NewCandidate(entities.NewCandidateInput{
		TenantID:    tenant,
		ContentHash: hash,
		Profile:     profile,
		Encrypted:   entities.EncryptedPersonal{FullName: encName, Email: "ENC:test@example.com"},
		Location:    "Bangalore",
		Headline:    "Senior Engineer",
		Source:      "manual_upload",
	})
	require.NoError(t, err)
	_ = c.PullEvents()
	return c
}

// makeTestCandidateUnique is like makeTestCandidate but uses a different content_hash.
func makeTestCandidateUnique(t *testing.T, tenant shared.TenantID, fullName, encName, hashHex string) *entities.Candidate {
	t.Helper()
	hash, err := vo.NewContentHash(hashHex)
	require.NoError(t, err)
	profile := vo.NewParsedProfile()
	profile.Personal.FullName = fullName
	c, err := entities.NewCandidate(entities.NewCandidateInput{
		TenantID:    tenant,
		ContentHash: hash,
		Profile:     profile,
		Encrypted:   entities.EncryptedPersonal{FullName: encName, Email: "ENC:" + fullName + "@example.com"},
		Location:    "Remote",
		Headline:    fullName + " headline",
		Source:      "manual_upload",
	})
	require.NoError(t, err)
	_ = c.PullEvents()
	return c
}

// buildNewApp returns a fresh Application in status New.
func buildNewApp(t *testing.T, tenant shared.TenantID, candidateID, intentID uuid.UUID) *entities.Application {
	t.Helper()
	app, err := entities.NewApplication(entities.NewApplicationInput{
		TenantID:             tenant,
		CandidateID:          candidateID,
		IntentID:             intentID,
		IntentSpecVersion:    1,
		ProfileSchemaVersion: 1,
	})
	require.NoError(t, err)
	return app
}

// buildScoredApp builds an Application in status Scored with a non-nil embedding score.
func buildScoredApp(t *testing.T, tenant shared.TenantID, candidateID, intentID uuid.UUID, embScore float64) *entities.Application {
	t.Helper()
	app := buildNewApp(t, tenant, candidateID, intentID)
	rules := vo.RuleMatchReport{
		Results: []vo.RuleResult{
			{Criterion: vo.RuleCriterion{Type: "skill", Name: "Go", Required: true}, Passed: true},
		},
	}
	require.NoError(t, app.RecordRuleMatch(rules))
	require.NoError(t, app.RecordEmbeddingScore(embScore))
	require.NoError(t, app.MarkScored(nil))
	_ = app.PullEvents()
	return app
}

// buildJudgedApp builds an Application that has been LLM-judged with a given overall score.
func buildJudgedApp(t *testing.T, tenant shared.TenantID, candidateID, intentID uuid.UUID, embScore float64, overallScore int) *entities.Application {
	t.Helper()
	app := buildScoredApp(t, tenant, candidateID, intentID, embScore)
	judgment := vo.LLMJudgment{
		Score:         overallScore,
		Summary:       "Great fit",
		PromptVersion: "v1",
	}
	require.NoError(t, app.RecordLLMJudgment(judgment))
	return app
}

// buildExcludedApp returns an Application in status Excluded.
func buildExcludedApp(t *testing.T, tenant shared.TenantID, candidateID, intentID uuid.UUID) *entities.Application {
	t.Helper()
	app := buildNewApp(t, tenant, candidateID, intentID)
	require.NoError(t, app.Exclude("missing required skill"))
	_ = app.PullEvents()
	return app
}

// buildEmbedFailedApp returns an Application in status EmbedFailed.
func buildEmbedFailedApp(t *testing.T, tenant shared.TenantID, candidateID, intentID uuid.UUID) *entities.Application {
	t.Helper()
	app := buildNewApp(t, tenant, candidateID, intentID)
	require.NoError(t, app.MarkEmbedFailed("voyage api error"))
	_ = app.PullEvents()
	return app
}

func newHandler(appRepo repositories.ApplicationRepository, candRepo repositories.CandidateRepository) *queries.ListApplicationsHandler {
	return queries.NewListApplicationsHandler(appRepo, candRepo, stubListEncryptor{})
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestListApplications_EmptyList verifies that an empty repo returns a
// zero-item response with all facets at 0.
func TestListApplications_EmptyList(t *testing.T) {
	tenant := newTenant()
	appRepo := &stubListApplicationRepo{apps: []*entities.Application{}}
	candRepo := &stubListCandidateRepo{byID: map[uuid.UUID]*entities.Candidate{}}

	h := newHandler(appRepo, candRepo)
	resp, err := h.Handle(context.Background(), queries.ListApplicationsInput{
		TenantID: tenant,
		IntentID: uuid.New(),
	})

	require.NoError(t, err)
	assert.Empty(t, resp.Items)
	assert.Equal(t, 0, resp.Total)
	assert.Equal(t, 0, resp.Facets.Strong)
	assert.Equal(t, 0, resp.Facets.Moderate)
	assert.Equal(t, 0, resp.Facets.Weak)
}

// TestListApplications_MixedStatuses verifies facets when there are apps in
// various states: only judged rows (with a score band) count in facets.
//
// Setup: 5 apps — 3 Scored/judged (strong, moderate, weak), 1 Excluded, 1 EmbedFailed.
func TestListApplications_MixedStatuses(t *testing.T) {
	tenant := newTenant()
	intentID := uuid.New()
	hashes := []string{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	}
	names := []string{"Alice", "Bob", "Carol", "Dave", "Eve"}

	byID := make(map[uuid.UUID]*entities.Candidate, 5)
	apps := make([]*entities.Application, 0, 5)

	// Judged: overall 85 → strong
	c0 := makeTestCandidateUnique(t, tenant, names[0], "ENC:"+names[0], hashes[0])
	byID[c0.ID()] = c0
	apps = append(apps, buildJudgedApp(t, tenant, c0.ID(), intentID, 0.9, 85))

	// Judged: overall 70 → moderate
	c1 := makeTestCandidateUnique(t, tenant, names[1], "ENC:"+names[1], hashes[1])
	byID[c1.ID()] = c1
	apps = append(apps, buildJudgedApp(t, tenant, c1.ID(), intentID, 0.7, 70))

	// Judged: overall 50 → weak
	c2 := makeTestCandidateUnique(t, tenant, names[2], "ENC:"+names[2], hashes[2])
	byID[c2.ID()] = c2
	apps = append(apps, buildJudgedApp(t, tenant, c2.ID(), intentID, 0.5, 50))

	// Excluded — no score band
	c3 := makeTestCandidateUnique(t, tenant, names[3], "ENC:"+names[3], hashes[3])
	byID[c3.ID()] = c3
	apps = append(apps, buildExcludedApp(t, tenant, c3.ID(), intentID))

	// EmbedFailed — no score band
	c4 := makeTestCandidateUnique(t, tenant, names[4], "ENC:"+names[4], hashes[4])
	byID[c4.ID()] = c4
	apps = append(apps, buildEmbedFailedApp(t, tenant, c4.ID(), intentID))

	appRepo := &stubListApplicationRepo{apps: apps}
	candRepo := &stubListCandidateRepo{byID: byID}
	h := newHandler(appRepo, candRepo)

	resp, err := h.Handle(context.Background(), queries.ListApplicationsInput{
		TenantID: tenant,
		IntentID: intentID,
	})

	require.NoError(t, err)
	assert.Equal(t, 5, resp.Total)
	assert.Len(t, resp.Items, 5)
	assert.Equal(t, 1, resp.Facets.Strong)
	assert.Equal(t, 1, resp.Facets.Moderate)
	assert.Equal(t, 1, resp.Facets.Weak)
}

// TestListApplications_PIIMasking verifies that the decrypted full name is
// never exposed — only the masked form (first char + "***") is returned.
func TestListApplications_PIIMasking(t *testing.T) {
	tenant := newTenant()
	intentID := uuid.New()

	c := makeTestCandidate(t, tenant, "Alice", "ENC:Alice")
	app := buildScoredApp(t, tenant, c.ID(), intentID, 0.8)

	appRepo := &stubListApplicationRepo{apps: []*entities.Application{app}}
	candRepo := &stubListCandidateRepo{byID: map[uuid.UUID]*entities.Candidate{c.ID(): c}}
	h := newHandler(appRepo, candRepo)

	resp, err := h.Handle(context.Background(), queries.ListApplicationsInput{
		TenantID: tenant,
		IntentID: intentID,
	})

	require.NoError(t, err)
	require.Len(t, resp.Items, 1)
	item := resp.Items[0]

	// Must be masked.
	assert.Equal(t, "A***", item.CandidateName)
	// Must NOT contain the full cleartext name.
	assert.NotContains(t, item.CandidateName, "Alice")
}

// TestListApplications_PIIMasking_MultiChar verifies masking for names longer
// than one character and for multi-byte runes.
func TestListApplications_PIIMasking_MultiChar(t *testing.T) {
	tenant := newTenant()
	intentID := uuid.New()

	hash, err := vo.NewContentHash("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	require.NoError(t, err)
	profile := vo.NewParsedProfile()
	c, err := entities.NewCandidate(entities.NewCandidateInput{
		TenantID:    tenant,
		ContentHash: hash,
		Profile:     profile,
		Encrypted:   entities.EncryptedPersonal{FullName: "ENC:Björn Pettersson", Email: "ENC:bjorn@example.com"},
		Location:    "Stockholm",
		Headline:    "Björn's headline",
		Source:      "manual_upload",
	})
	require.NoError(t, err)
	_ = c.PullEvents()

	app := buildScoredApp(t, tenant, c.ID(), intentID, 0.75)
	appRepo := &stubListApplicationRepo{apps: []*entities.Application{app}}
	candRepo := &stubListCandidateRepo{byID: map[uuid.UUID]*entities.Candidate{c.ID(): c}}
	h := newHandler(appRepo, candRepo)

	resp, err := h.Handle(context.Background(), queries.ListApplicationsInput{
		TenantID: tenant,
		IntentID: intentID,
	})
	require.NoError(t, err)
	require.Len(t, resp.Items, 1)

	// "Björn Pettersson" → first rune 'B' + "***"
	assert.Equal(t, "B***", resp.Items[0].CandidateName)
	assert.NotContains(t, resp.Items[0].CandidateName, "Björn")
}

// TestListApplications_SortByScoreDesc verifies that the handler passes the
// filter through to the repo layer unchanged. Sort ordering is the repo's
// responsibility; the handler must not re-sort. We verify this by returning
// apps in the order [90, 70, nil] from the stub and asserting the handler
// preserves that order.
func TestListApplications_SortByScoreDesc(t *testing.T) {
	tenant := newTenant()
	intentID := uuid.New()
	hashes := []string{
		"1111111111111111111111111111111111111111111111111111111111111111",
		"2222222222222222222222222222222222222222222222222222222222222222",
		"3333333333333333333333333333333333333333333333333333333333333333",
	}
	names := []string{"Cathy", "David", "Eliza"}
	overallScores := []int{90, 70, 50} // repo returns in score_desc order

	byID := make(map[uuid.UUID]*entities.Candidate, 3)
	apps := make([]*entities.Application, 0, 3)

	for i, name := range names {
		c := makeTestCandidateUnique(t, tenant, name, "ENC:"+name, hashes[i])
		byID[c.ID()] = c
		apps = append(apps, buildJudgedApp(t, tenant, c.ID(), intentID, 0.8, overallScores[i]))
	}

	appRepo := &stubListApplicationRepo{apps: apps}
	candRepo := &stubListCandidateRepo{byID: byID}
	h := newHandler(appRepo, candRepo)

	resp, err := h.Handle(context.Background(), queries.ListApplicationsInput{
		TenantID: tenant,
		IntentID: intentID,
		Filter:   repositories.ApplicationListFilter{Sort: "score_desc"},
	})

	require.NoError(t, err)
	require.Len(t, resp.Items, 3)

	scores := [3]float64{
		*resp.Items[0].OverallScore,
		*resp.Items[1].OverallScore,
		*resp.Items[2].OverallScore,
	}
	assert.Equal(t, float64(90), scores[0])
	assert.Equal(t, float64(70), scores[1])
	assert.Equal(t, float64(50), scores[2])
}

// TestListApplications_FilterByStatus verifies that the filter is passed
// through to the repo and that the handler returns only what the repo returns.
// (The handler does NOT apply secondary filtering — that is the repo's job.)
func TestListApplications_FilterByStatus(t *testing.T) {
	tenant := newTenant()
	intentID := uuid.New()

	status := vo.AppStatusScored
	// Stub repo returns only Scored apps, as if the DB filter was applied.
	c := makeTestCandidate(t, tenant, "Fiona", "ENC:Fiona")
	app := buildScoredApp(t, tenant, c.ID(), intentID, 0.8)

	capturedFilter := new(repositories.ApplicationListFilter)
	capturing := &filterCapturingAppRepo{
		apps:    []*entities.Application{app},
		capture: capturedFilter,
	}

	candRepo := &stubListCandidateRepo{byID: map[uuid.UUID]*entities.Candidate{c.ID(): c}}
	h := queries.NewListApplicationsHandler(capturing, candRepo, stubListEncryptor{})

	resp, err := h.Handle(context.Background(), queries.ListApplicationsInput{
		TenantID: tenant,
		IntentID: intentID,
		Filter:   repositories.ApplicationListFilter{Status: &status},
	})

	require.NoError(t, err)
	require.Len(t, resp.Items, 1)
	// Verify the filter was forwarded: status pointer must match.
	require.NotNil(t, capturedFilter.Status)
	assert.Equal(t, vo.AppStatusScored, *capturedFilter.Status)
}

// TestListApplications_LLMJudgmentPopulated verifies that LLMJudgment is
// serialised as non-empty JSON for judged rows, and nil/empty for unjudged rows.
func TestListApplications_LLMJudgmentPopulated(t *testing.T) {
	tenant := newTenant()
	intentID := uuid.New()

	hashes := []string{
		"aaaa1111111111111111111111111111111111111111111111111111111111aa",
		"bbbb2222222222222222222222222222222222222222222222222222222222bb",
	}

	// App 1: judged — LLMJudgment should be populated.
	c1 := makeTestCandidateUnique(t, tenant, "Greta", "ENC:Greta", hashes[0])
	judgedApp := buildJudgedApp(t, tenant, c1.ID(), intentID, 0.85, 82)

	// App 2: scored but not judged — LLMJudgment should be nil/empty.
	c2 := makeTestCandidateUnique(t, tenant, "Henry", "ENC:Henry", hashes[1])
	scoredApp := buildScoredApp(t, tenant, c2.ID(), intentID, 0.75)

	byID := map[uuid.UUID]*entities.Candidate{c1.ID(): c1, c2.ID(): c2}
	appRepo := &stubListApplicationRepo{apps: []*entities.Application{judgedApp, scoredApp}}
	candRepo := &stubListCandidateRepo{byID: byID}
	h := newHandler(appRepo, candRepo)

	resp, err := h.Handle(context.Background(), queries.ListApplicationsInput{
		TenantID: tenant,
		IntentID: intentID,
	})

	require.NoError(t, err)
	require.Len(t, resp.Items, 2)

	judgedItem := resp.Items[0]
	scoredItem := resp.Items[1]

	// Judged row has LLMJudgment JSON.
	assert.NotEmpty(t, judgedItem.LLMJudgment)

	// Unjudged row has no LLMJudgment.
	assert.Empty(t, scoredItem.LLMJudgment)
}

// ---------------------------------------------------------------------------
// filterCapturingAppRepo — captures the ApplicationListFilter on ListByIntent.
// ---------------------------------------------------------------------------

type filterCapturingAppRepo struct {
	apps    []*entities.Application
	capture *repositories.ApplicationListFilter
	listErr error
}

func (r *filterCapturingAppRepo) ListByIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID, f repositories.ApplicationListFilter) ([]*entities.Application, error) {
	*r.capture = f
	return r.apps, r.listErr
}
func (r *filterCapturingAppRepo) Save(_ context.Context, _ *entities.Application) error {
	return nil
}
func (r *filterCapturingAppRepo) FindByID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.Application, error) {
	return nil, repositories.ErrApplicationNotFound
}
func (r *filterCapturingAppRepo) FindByCandidateAndIntent(_ context.Context, _ shared.TenantID, _, _ uuid.UUID) (*entities.Application, error) {
	return nil, repositories.ErrApplicationNotFound
}
func (r *filterCapturingAppRepo) ClaimNextNew(_ context.Context) (*entities.Application, error) {
	return nil, repositories.ErrApplicationNotFound
}
func (r *filterCapturingAppRepo) TopByCoarseScoreForIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID, _ int) ([]*entities.Application, error) {
	return nil, nil
}
func (r *filterCapturingAppRepo) InvalidateJudgmentsForIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID) error {
	return nil
}
