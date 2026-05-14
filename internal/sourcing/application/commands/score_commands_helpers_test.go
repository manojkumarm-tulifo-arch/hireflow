package commands_test

// score_commands_test_helpers.go
// Shared in-memory fakes for ScoreCandidate, ScoreIntent, ScoreApplication,
// and JudgeApplication tests. All types live in the commands_test package so
// they are only compiled during testing.

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// errIntentNotFound is used by fakeIntentReader when a requested ID is absent.
var errIntentNotFound = errors.New("intent not found")

// ---------------------------------------------------------------------------
// fakeIntentReader — implements services.IntentReader
// ---------------------------------------------------------------------------

type fakeIntentReader struct {
	mu         sync.Mutex
	byID       map[uuid.UUID]services.IntentSnapshot
	confirmed  []services.IntentSnapshot
	findErr    error
	listErr    error
}

func newFakeIntentReader() *fakeIntentReader {
	return &fakeIntentReader{byID: make(map[uuid.UUID]services.IntentSnapshot)}
}

func (f *fakeIntentReader) addIntent(snap services.IntentSnapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[snap.ID] = snap
	if snap.Status == "Confirmed" {
		f.confirmed = append(f.confirmed, snap)
	}
}

func (f *fakeIntentReader) FindByID(_ context.Context, _ shared.TenantID, id uuid.UUID) (services.IntentSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.findErr != nil {
		return services.IntentSnapshot{}, f.findErr
	}
	snap, ok := f.byID[id]
	if !ok {
		return services.IntentSnapshot{}, errIntentNotFound
	}
	return snap, nil
}

func (f *fakeIntentReader) ListConfirmedIntents(_ context.Context, _ shared.TenantID) ([]services.IntentSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]services.IntentSnapshot(nil), f.confirmed...), nil
}

// ---------------------------------------------------------------------------
// fakeEmbedder — implements services.Embedder
// ---------------------------------------------------------------------------

type fakeEmbedder struct {
	mu        sync.Mutex
	vec       []float32 // returned on success
	err       error
	callCount int
}

func newFakeEmbedder() *fakeEmbedder {
	// Return a stable 1024-dim unit vector.
	vec := make([]float32, 1024)
	vec[0] = 1.0
	return &fakeEmbedder{vec: vec}
}

func (f *fakeEmbedder) EmbedDocument(_ context.Context, _ string) ([]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callCount++
	if f.err != nil {
		return nil, f.err
	}
	out := make([]float32, len(f.vec))
	copy(out, f.vec)
	return out, nil
}

func (f *fakeEmbedder) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callCount
}

// ---------------------------------------------------------------------------
// fakeMatchScorer — implements services.MatchScorer
// ---------------------------------------------------------------------------

type fakeMatchScorer struct {
	out services.MatchOutput
	err error
}

func (f *fakeMatchScorer) Score(_ context.Context, _ services.MatchInput) (services.MatchOutput, error) {
	return f.out, f.err
}

// passingMatchOutput returns a MatchOutput where all required rules pass and
// an embedding score is set so that MarkScored can be called.
func passingMatchOutput() services.MatchOutput {
	score := 0.85
	coarse := 100.0 + score*20
	return services.MatchOutput{
		Rules: vo.RuleMatchReport{
			Results: []vo.RuleResult{
				{Criterion: vo.RuleCriterion{Type: "skill", Name: "Go", Required: true}, Passed: true},
			},
		},
		EmbeddingScore: &score,
		CoarseScore:    &coarse,
	}
}

// failingMatchOutput returns a MatchOutput where a required rule did NOT pass
// (no embedding score), so the handler should call Exclude.
func failingMatchOutput() services.MatchOutput {
	return services.MatchOutput{
		Rules: vo.RuleMatchReport{
			Results: []vo.RuleResult{
				{Criterion: vo.RuleCriterion{Type: "skill", Name: "Go", Required: true}, Passed: false},
			},
		},
		EmbeddingScore: nil,
		CoarseScore:    nil,
	}
}

// ---------------------------------------------------------------------------
// fakeLLMJudge — implements services.LLMJudge
// ---------------------------------------------------------------------------

type fakeLLMJudge struct {
	judgment vo.LLMJudgment
	err      error
}

func (f *fakeLLMJudge) Judge(_ context.Context, _ vo.ParsedProfile, _ services.RoleSpec, _ vo.RuleMatchReport) (vo.LLMJudgment, error) {
	return f.judgment, f.err
}

// ---------------------------------------------------------------------------
// fakeApplicationRepo — implements repositories.ApplicationRepository
// ---------------------------------------------------------------------------

type fakeApplicationRepo struct {
	mu      sync.Mutex
	byID    map[uuid.UUID]*entities.Application
	byPair  map[[2]uuid.UUID]*entities.Application // [candidateID, intentID]
	topK    []*entities.Application
	saves   int
	saveErr error
	findErr error
}

func newFakeApplicationRepo() *fakeApplicationRepo {
	return &fakeApplicationRepo{
		byID:   make(map[uuid.UUID]*entities.Application),
		byPair: make(map[[2]uuid.UUID]*entities.Application),
	}
}

func (r *fakeApplicationRepo) Save(_ context.Context, a *entities.Application) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saveErr != nil {
		return r.saveErr
	}
	_ = a.PullEvents() // drain events like the real repo would
	r.byID[a.ID()] = a
	r.byPair[[2]uuid.UUID{a.CandidateID(), a.IntentID()}] = a
	r.saves++
	return nil
}

func (r *fakeApplicationRepo) FindByID(_ context.Context, _ shared.TenantID, id uuid.UUID) (*entities.Application, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.findErr != nil {
		return nil, r.findErr
	}
	a, ok := r.byID[id]
	if !ok {
		return nil, repositories.ErrApplicationNotFound
	}
	return a, nil
}

func (r *fakeApplicationRepo) FindByCandidateAndIntent(_ context.Context, _ shared.TenantID, candidateID, intentID uuid.UUID) (*entities.Application, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.byPair[[2]uuid.UUID{candidateID, intentID}]
	if !ok {
		return nil, repositories.ErrApplicationNotFound
	}
	return a, nil
}

func (r *fakeApplicationRepo) ListByIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID, _ repositories.ApplicationListFilter) ([]*entities.Application, error) {
	return nil, nil
}

func (r *fakeApplicationRepo) ClaimNextNew(_ context.Context) (*entities.Application, error) {
	return nil, repositories.ErrApplicationNotFound
}

func (r *fakeApplicationRepo) TopByCoarseScoreForIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID, _ int) ([]*entities.Application, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*entities.Application(nil), r.topK...), nil
}

func (r *fakeApplicationRepo) InvalidateJudgmentsForIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID) error {
	return nil
}

func (r *fakeApplicationRepo) savedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saves
}

// ---------------------------------------------------------------------------
// fakeIntentEmbeddingRepo — implements repositories.IntentEmbeddingRepository
// ---------------------------------------------------------------------------

type fakeIntentEmbeddingRepo struct {
	mu      sync.Mutex
	store   map[[2]interface{}][]float32 // key: (intentID, specVersion)
	findErr error
	saveErr error
}

func newFakeIntentEmbeddingRepo() *fakeIntentEmbeddingRepo {
	return &fakeIntentEmbeddingRepo{store: make(map[[2]interface{}][]float32)}
}

func (r *fakeIntentEmbeddingRepo) Save(_ context.Context, intentID uuid.UUID, _ shared.TenantID, specVersion int, vector []float32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saveErr != nil {
		return r.saveErr
	}
	out := make([]float32, len(vector))
	copy(out, vector)
	r.store[[2]interface{}{intentID, specVersion}] = out
	return nil
}

func (r *fakeIntentEmbeddingRepo) Find(_ context.Context, intentID uuid.UUID, specVersion int) ([]float32, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.findErr != nil {
		return nil, r.findErr
	}
	v, ok := r.store[[2]interface{}{intentID, specVersion}]
	if !ok {
		return nil, repositories.ErrIntentEmbeddingNotFound
	}
	out := make([]float32, len(v))
	copy(out, v)
	return out, nil
}

// ---------------------------------------------------------------------------
// fakeJudgeJobRepo — implements repositories.JudgeJobRepository
// ---------------------------------------------------------------------------

type fakeJudgeJobRepo struct {
	mu      sync.Mutex
	byID    map[uuid.UUID]*entities.JudgeJob
	saves   int
	saveErr error
}

func newFakeJudgeJobRepo() *fakeJudgeJobRepo {
	return &fakeJudgeJobRepo{byID: make(map[uuid.UUID]*entities.JudgeJob)}
}

func (r *fakeJudgeJobRepo) Save(_ context.Context, j *entities.JudgeJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saveErr != nil {
		return r.saveErr
	}
	r.byID[j.ID()] = j
	r.saves++
	return nil
}

func (r *fakeJudgeJobRepo) ClaimNextPending(_ context.Context) (*entities.JudgeJob, error) {
	return nil, repositories.ErrJudgeJobNotFound
}

func (r *fakeJudgeJobRepo) FindByID(_ context.Context, id uuid.UUID) (*entities.JudgeJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.byID[id]
	if !ok {
		return nil, repositories.ErrJudgeJobNotFound
	}
	return j, nil
}

func (r *fakeJudgeJobRepo) savedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saves
}

// ---------------------------------------------------------------------------
// fakeExtendedCandidateRepo — extends fakeCandidateRepo with controllable
// FindByID and ListByTenant for the scoring command tests.
// ---------------------------------------------------------------------------

type fakeExtendedCandidateRepo struct {
	mu               sync.Mutex
	candidatesByID   map[uuid.UUID]*entities.Candidate
	tenantCandidates []*entities.Candidate
	findErr          error
	listErr          error
	updateEmbCalls   int
}

func newFakeExtendedCandidateRepo() *fakeExtendedCandidateRepo {
	return &fakeExtendedCandidateRepo{
		candidatesByID: make(map[uuid.UUID]*entities.Candidate),
	}
}

func (r *fakeExtendedCandidateRepo) addCandidate(c *entities.Candidate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.candidatesByID[c.ID()] = c
	r.tenantCandidates = append(r.tenantCandidates, c)
}

func (r *fakeExtendedCandidateRepo) Save(_ context.Context, c *entities.Candidate) (*entities.Candidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = c.PullEvents()
	r.candidatesByID[c.ID()] = c
	return c, nil
}

func (r *fakeExtendedCandidateRepo) FindByID(_ context.Context, _ shared.TenantID, id uuid.UUID) (*entities.Candidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.findErr != nil {
		return nil, r.findErr
	}
	c, ok := r.candidatesByID[id]
	if !ok {
		return nil, repositories.ErrCandidateNotFound
	}
	return c, nil
}

func (r *fakeExtendedCandidateRepo) FindByContentHash(_ context.Context, _ shared.TenantID, _ string) (*entities.Candidate, error) {
	return nil, repositories.ErrCandidateNotFound
}

func (r *fakeExtendedCandidateRepo) ListByTenant(_ context.Context, _ shared.TenantID) ([]*entities.Candidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, r.listErr
	}
	return append([]*entities.Candidate(nil), r.tenantCandidates...), nil
}

func (r *fakeExtendedCandidateRepo) UpdateProfileEmbedding(_ context.Context, _ uuid.UUID, _ shared.TenantID, _ []float32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updateEmbCalls++
	return nil
}

// ---------------------------------------------------------------------------
// Test-data helpers
// ---------------------------------------------------------------------------

// makeCandidate builds a minimal valid Candidate for use in scoring tests.
func makeCandidate(t interface{ Helper(); Fatal(...interface{}) }, tenantID shared.TenantID) *entities.Candidate {
	t.Helper()
	hash, err := vo.NewContentHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	profile := vo.NewParsedProfile()
	profile.Personal.FullName = "Test Candidate"
	profile.Personal.Email = "test@example.com"
	cand, err := entities.NewCandidate(entities.NewCandidateInput{
		TenantID:    tenantID,
		ContentHash: hash,
		Profile:     profile,
		Encrypted:   entities.EncryptedPersonal{FullName: "enc:Test", Email: "enc:test@example.com"},
		Location:    "Remote",
		Headline:    "Engineer",
		Source:      "manual_upload",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = cand.PullEvents()
	return cand
}

// makeCandidateWithEmbedding builds a Candidate that already has a profile embedding
// so that ScoreApplicationHandler skips the embedder call for the candidate.
func makeCandidateWithEmbedding(t interface{ Helper(); Fatal(...interface{}) }, tenantID shared.TenantID) *entities.Candidate {
	t.Helper()
	hash, err := vo.NewContentHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	profile := vo.NewParsedProfile()
	profile.Personal.FullName = "Embedded Candidate"
	profile.Personal.Email = "embedded@example.com"
	vec := make([]float32, 1024)
	vec[0] = 1.0
	cand := entities.RehydrateCandidate(entities.RehydrateCandidateInput{
		ID:               uuid.New(),
		TenantID:         tenantID,
		ContentHash:      hash,
		EncryptedFullName: "enc:Embedded",
		EncryptedEmail:   "enc:embedded@example.com",
		Location:         "Remote",
		Headline:         "Engineer",
		Profile:          profile,
		ProfileEmbedding: vec,
		Source:           "manual_upload",
	})
	return cand
}

// makeIntent builds a minimal confirmed IntentSnapshot.
func makeIntent(tenantID shared.TenantID) services.IntentSnapshot {
	return services.IntentSnapshot{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Status:      "Confirmed",
		SpecVersion: 1,
		Role: services.RoleSpec{
			Title:          "Software Engineer",
			RequiredSkills: []services.SkillSpec{{Name: "Go", MinYears: 2}},
		},
	}
}

// makeNewApplication builds a fresh Application in status New for the given
// (candidate, intent) pair.
func makeNewApplication(
	t interface{ Helper(); Fatal(...interface{}) },
	tenantID shared.TenantID,
	candidateID, intentID uuid.UUID,
) *entities.Application {
	t.Helper()
	app, err := entities.NewApplication(entities.NewApplicationInput{
		TenantID:             tenantID,
		CandidateID:          candidateID,
		IntentID:             intentID,
		IntentSpecVersion:    1,
		ProfileSchemaVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

// makeScoredApplication builds an Application that has been through rule-match
// and embedding scoring, landing in status Scored. Used as the pre-condition
// for JudgeApplication tests.
func makeScoredApplication(
	t interface{ Helper(); Fatal(...interface{}) },
	tenantID shared.TenantID,
	candidateID, intentID uuid.UUID,
) *entities.Application {
	t.Helper()
	app := makeNewApplication(t, tenantID, candidateID, intentID)
	rules := vo.RuleMatchReport{
		Results: []vo.RuleResult{
			{Criterion: vo.RuleCriterion{Type: "skill", Name: "Go", Required: true}, Passed: true},
		},
	}
	if err := app.RecordRuleMatch(rules); err != nil {
		t.Fatal(err)
	}
	if err := app.RecordEmbeddingScore(0.85); err != nil {
		t.Fatal(err)
	}
	if err := app.MarkScored(nil); err != nil {
		t.Fatal(err)
	}
	_ = app.PullEvents()
	return app
}
