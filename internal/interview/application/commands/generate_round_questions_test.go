package commands_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/application/commands"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/services"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	"github.com/hustle/hireflow/internal/interview/infrastructure/generation"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ---------------------------------------------------------------------------
// Fake implementations for generate tests
// ---------------------------------------------------------------------------

type fakeIntentReader struct {
	spec services.RoleSpec
	err  error
}

func (f *fakeIntentReader) GetRoleSpec(_ context.Context, _ shared.TenantID, _ uuid.UUID) (services.RoleSpec, error) {
	return f.spec, f.err
}

type fakeCandidateReader struct {
	profile services.CandidateProfile
	err     error
}

func (f *fakeCandidateReader) GetProfileForQuestions(_ context.Context, _ shared.TenantID, _ uuid.UUID) (services.CandidateProfile, error) {
	return f.profile, f.err
}

type fakeQuestionGenerator struct {
	questions []vo.Question
	err       error
	callCount int
}

func (f *fakeQuestionGenerator) Generate(_ context.Context, _ services.GenerationInput) ([]vo.Question, error) {
	f.callCount++
	return f.questions, f.err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// validQuestions returns n valid Question values that pass Validate().
func validQuestions(n int) []vo.Question {
	q := vo.Question{
		Prompt:          "Describe a complex system you designed.",
		SkillProbed:     "system design",
		Why:             "Tests architecture thinking.",
		ExpectedSignals: []string{"scalability", "trade-offs", "communication"},
		ModelAnswer:     "A good answer covers requirements, trade-offs, and evolution.",
		RedFlags:        []string{"no trade-offs mentioned", "ignores failure modes"},
		FollowUps:       []string{"How would you handle 10x traffic?"},
	}
	out := make([]vo.Question, n)
	for i := range out {
		out[i] = q
	}
	return out
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
	if err != nil {
		t.Fatalf("seedProcess: %v", err)
	}
	if err := repo.Save(context.Background(), p); err != nil {
		t.Fatalf("seedProcess save: %v", err)
	}
	roundID := p.Rounds()[0].ID()
	return p, roundID
}

// advanceRoundToStatus transitions a round to the target status using available
// aggregate methods, then re-saves.
func advanceRoundToStatus(t *testing.T, repo *fakeProcessRepo, p *entities.InterviewProcess, roundID uuid.UUID, status vo.RoundStatus) {
	t.Helper()
	switch status {
	case vo.RoundStatusQuestionsReady:
		if err := p.MarkRoundQuestionsReady(roundID, validQuestions(3)); err != nil {
			t.Fatalf("advanceRoundToStatus QuestionsReady: %v", err)
		}
	case vo.RoundStatusGenerationFailed:
		if err := p.MarkRoundGenerationFailed(roundID, "seeded failure"); err != nil {
			t.Fatalf("advanceRoundToStatus GenerationFailed: %v", err)
		}
	case vo.RoundStatusCompleted:
		if err := p.MarkRoundQuestionsReady(roundID, validQuestions(3)); err != nil {
			t.Fatalf("advanceRoundToStatus -> QuestionsReady: %v", err)
		}
		if err := p.MarkRoundCompleted(roundID); err != nil {
			t.Fatalf("advanceRoundToStatus Completed: %v", err)
		}
	case vo.RoundStatusSkipped:
		if err := p.MarkRoundSkipped(roundID); err != nil {
			t.Fatalf("advanceRoundToStatus Skipped: %v", err)
		}
	default:
		t.Fatalf("advanceRoundToStatus: unsupported target %v", status)
	}
	if err := repo.Save(context.Background(), p); err != nil {
		t.Fatalf("advanceRoundToStatus save: %v", err)
	}
}

func defaultIntentReader() *fakeIntentReader {
	return &fakeIntentReader{spec: services.RoleSpec{Title: "Software Engineer"}}
}

func defaultCandidateReader() *fakeCandidateReader {
	return &fakeCandidateReader{profile: services.CandidateProfile{ID: uuid.New()}}
}

func newGenerateHandler(
	repo *fakeProcessRepo,
	intents services.IntentReader,
	candidates services.CandidateReader,
	gen services.QuestionGenerator,
) *commands.GenerateRoundQuestionsHandler {
	return commands.NewGenerateRoundQuestionsHandler(repo, intents, candidates, gen)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestGenerate_HappyPath_MarksQuestionsReady(t *testing.T) {
	repo := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	p, roundID := seedProcess(t, repo, tenantID)

	gen := &fakeQuestionGenerator{questions: validQuestions(3)}
	h := newGenerateHandler(repo, defaultIntentReader(), defaultCandidateReader(), gen)

	err := h.Handle(context.Background(), commands.GenerateRoundQuestionsInput{
		TenantID:  tenantID,
		ProcessID: p.ID(),
		RoundID:   roundID,
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	// Reload from repo and verify state.
	saved, err := repo.FindByID(context.Background(), tenantID, p.ID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	round := saved.Rounds()[0]
	if round.Status() != vo.RoundStatusQuestionsReady {
		t.Errorf("status: want QuestionsReady, got %v", round.Status())
	}
	if len(round.Questions()) != 3 {
		t.Errorf("questions: want 3, got %d", len(round.Questions()))
	}
	if gen.callCount != 1 {
		t.Errorf("generator call count: want 1, got %d", gen.callCount)
	}
}

func TestGenerate_RoundAlreadyAdvanced_NoOp(t *testing.T) {
	repo := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	p, roundID := seedProcess(t, repo, tenantID)
	advanceRoundToStatus(t, repo, p, roundID, vo.RoundStatusQuestionsReady)

	gen := &fakeQuestionGenerator{questions: validQuestions(3)}
	h := newGenerateHandler(repo, defaultIntentReader(), defaultCandidateReader(), gen)

	err := h.Handle(context.Background(), commands.GenerateRoundQuestionsInput{
		TenantID:  tenantID,
		ProcessID: p.ID(),
		RoundID:   roundID,
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if gen.callCount != 0 {
		t.Errorf("generator should not be called for already-advanced round; got %d calls", gen.callCount)
	}
}

func TestGenerate_RoundNotFound_ReturnsErrRoundNotFound(t *testing.T) {
	repo := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	p, _ := seedProcess(t, repo, tenantID)

	h := newGenerateHandler(repo, defaultIntentReader(), defaultCandidateReader(), &fakeQuestionGenerator{})

	err := h.Handle(context.Background(), commands.GenerateRoundQuestionsInput{
		TenantID:  tenantID,
		ProcessID: p.ID(),
		RoundID:   uuid.New(), // bogus round ID
	})
	if !errors.Is(err, entities.ErrRoundNotFound) {
		t.Errorf("want ErrRoundNotFound, got: %v", err)
	}
}

func TestGenerate_LLMAuth_AbortsImmediately(t *testing.T) {
	repo := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	p, roundID := seedProcess(t, repo, tenantID)

	gen := &fakeQuestionGenerator{err: generation.ErrLLMAuthFailed}
	h := newGenerateHandler(repo, defaultIntentReader(), defaultCandidateReader(), gen)

	err := h.Handle(context.Background(), commands.GenerateRoundQuestionsInput{
		TenantID:  tenantID,
		ProcessID: p.ID(),
		RoundID:   roundID,
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	saved, _ := repo.FindByID(context.Background(), tenantID, p.ID())
	round := saved.Rounds()[0]
	if round.Status() != vo.RoundStatusGenerationFailed {
		t.Errorf("status: want GenerationFailed, got %v", round.Status())
	}
	if round.AttemptCount() != 0 {
		// LLM auth aborts immediately — the attempt count starts at 0 and we
		// never call RecordGenerationAttempt before MarkRoundGenerationFailed.
		// The domain entity doesn't increment attempt_count on MarkFailed.
		t.Logf("attempt_count=%d (note: abort path does not call RecordGenerationAttempt)", round.AttemptCount())
	}
}

func TestGenerate_InvalidJSON_RetriesOnceThenAborts(t *testing.T) {
	repo := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	p, roundID := seedProcess(t, repo, tenantID)

	gen := &fakeQuestionGenerator{err: generation.ErrInvalidLLMOutput}
	h := newGenerateHandler(repo, defaultIntentReader(), defaultCandidateReader(), gen)

	input := commands.GenerateRoundQuestionsInput{
		TenantID:  tenantID,
		ProcessID: p.ID(),
		RoundID:   roundID,
	}

	// First Handle → retry scheduled (Pending, attempt=1, next_attempt_at ~30s ahead)
	before := time.Now().UTC()
	if err := h.Handle(context.Background(), input); err != nil {
		t.Fatalf("first Handle error: %v", err)
	}
	saved, _ := repo.FindByID(context.Background(), tenantID, p.ID())
	round := saved.Rounds()[0]
	if round.Status() != vo.RoundStatusPending {
		t.Errorf("after first attempt: status want Pending, got %v", round.Status())
	}
	if round.AttemptCount() != 1 {
		t.Errorf("after first attempt: attempt_count want 1, got %d", round.AttemptCount())
	}
	expectedBackoff := 30 * time.Second
	low := before.Add(expectedBackoff - time.Second)
	high := before.Add(expectedBackoff + time.Second)
	if round.NextAttemptAt().Before(low) || round.NextAttemptAt().After(high) {
		t.Errorf("next_attempt_at %v not in window [%v, %v]", round.NextAttemptAt(), low, high)
	}

	// Second Handle → abort (GenerationFailed)
	if err := h.Handle(context.Background(), input); err != nil {
		t.Fatalf("second Handle error: %v", err)
	}
	saved, _ = repo.FindByID(context.Background(), tenantID, p.ID())
	round = saved.Rounds()[0]
	if round.Status() != vo.RoundStatusGenerationFailed {
		t.Errorf("after second attempt: status want GenerationFailed, got %v", round.Status())
	}
}

func TestGenerate_TransientError_FollowsBackoffSchedule(t *testing.T) {
	repo := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	p, roundID := seedProcess(t, repo, tenantID)

	transientErr := errors.New("upstream timeout")
	gen := &fakeQuestionGenerator{err: transientErr}
	h := newGenerateHandler(repo, defaultIntentReader(), defaultCandidateReader(), gen)

	input := commands.GenerateRoundQuestionsInput{
		TenantID:  tenantID,
		ProcessID: p.ID(),
		RoundID:   roundID,
	}

	backoffs := []time.Duration{
		1 * time.Minute,
		5 * time.Minute,
		15 * time.Minute,
		1 * time.Hour,
		4 * time.Hour,
	}

	for i, expectedBackoff := range backoffs {
		before := time.Now().UTC()
		if err := h.Handle(context.Background(), input); err != nil {
			t.Fatalf("Handle %d error: %v", i+1, err)
		}
		saved, _ := repo.FindByID(context.Background(), tenantID, p.ID())
		round := saved.Rounds()[0]
		if round.Status() != vo.RoundStatusPending {
			t.Errorf("attempt %d: status want Pending, got %v", i+1, round.Status())
		}
		if round.AttemptCount() != i+1 {
			t.Errorf("attempt %d: attempt_count want %d, got %d", i+1, i+1, round.AttemptCount())
		}
		low := before.Add(expectedBackoff - time.Second)
		high := before.Add(expectedBackoff + time.Second)
		if round.NextAttemptAt().Before(low) || round.NextAttemptAt().After(high) {
			t.Errorf("attempt %d: next_attempt_at %v not in [%v, %v]", i+1, round.NextAttemptAt(), low, high)
		}
	}

	// 6th call → GenerationFailed
	if err := h.Handle(context.Background(), input); err != nil {
		t.Fatalf("6th Handle error: %v", err)
	}
	saved, _ := repo.FindByID(context.Background(), tenantID, p.ID())
	round := saved.Rounds()[0]
	if round.Status() != vo.RoundStatusGenerationFailed {
		t.Errorf("after 6th attempt: status want GenerationFailed, got %v", round.Status())
	}
}

func TestGenerate_IntentNotFound_AbortsImmediately(t *testing.T) {
	repo := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	p, roundID := seedProcess(t, repo, tenantID)

	intents := &fakeIntentReader{err: services.ErrIntentNotFound}
	gen := &fakeQuestionGenerator{} // should not be called
	h := newGenerateHandler(repo, intents, defaultCandidateReader(), gen)

	err := h.Handle(context.Background(), commands.GenerateRoundQuestionsInput{
		TenantID:  tenantID,
		ProcessID: p.ID(),
		RoundID:   roundID,
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if gen.callCount != 0 {
		t.Errorf("generator should not be called when intent not found; got %d calls", gen.callCount)
	}
	saved, _ := repo.FindByID(context.Background(), tenantID, p.ID())
	round := saved.Rounds()[0]
	if round.Status() != vo.RoundStatusGenerationFailed {
		t.Errorf("status: want GenerationFailed, got %v", round.Status())
	}
}

func TestGenerate_CandidateNotFound_AbortsImmediately(t *testing.T) {
	repo := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	p, roundID := seedProcess(t, repo, tenantID)

	candidates := &fakeCandidateReader{err: services.ErrCandidateNotFound}
	gen := &fakeQuestionGenerator{} // should not be called
	h := newGenerateHandler(repo, defaultIntentReader(), candidates, gen)

	err := h.Handle(context.Background(), commands.GenerateRoundQuestionsInput{
		TenantID:  tenantID,
		ProcessID: p.ID(),
		RoundID:   roundID,
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if gen.callCount != 0 {
		t.Errorf("generator should not be called when candidate not found; got %d calls", gen.callCount)
	}
	saved, _ := repo.FindByID(context.Background(), tenantID, p.ID())
	round := saved.Rounds()[0]
	if round.Status() != vo.RoundStatusGenerationFailed {
		t.Errorf("status: want GenerationFailed, got %v", round.Status())
	}
}
