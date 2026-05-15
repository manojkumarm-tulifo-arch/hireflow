package commands_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/application/commands"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// seedProcessWithRoundStatus creates a process whose first (and only) round is
// in the given status. Returns the process and its round ID.
func seedProcessWithRoundStatus(t *testing.T, repo *fakeProcessRepo, tenantID shared.TenantID, status vo.RoundStatus) (*entities.InterviewProcess, uuid.UUID) {
	t.Helper()
	p, roundID := seedProcess(t, repo, tenantID)
	if status != vo.RoundStatusPending {
		advanceRoundToStatus(t, repo, p, roundID, status)
	}
	return p, roundID
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRegenerate_FromQuestionsReady_ResetsToPending(t *testing.T) {
	repo := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	p, roundID := seedProcessWithRoundStatus(t, repo, tenantID, vo.RoundStatusQuestionsReady)

	h := commands.NewRegenerateRoundQuestionsHandler(repo)
	err := h.Handle(context.Background(), commands.RegenerateRoundQuestionsInput{
		TenantID: tenantID,
		RoundID:  roundID,
	})
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	saved, _ := repo.FindByID(context.Background(), tenantID, p.ID())
	round := saved.Rounds()[0]
	if round.Status() != vo.RoundStatusPending {
		t.Errorf("status: want Pending, got %v", round.Status())
	}
	if round.AttemptCount() != 0 {
		t.Errorf("attempt_count: want 0, got %d", round.AttemptCount())
	}
	if len(round.Questions()) != 0 {
		t.Errorf("questions: want nil/empty, got %d questions", len(round.Questions()))
	}
}

func TestRegenerate_FromGenerationFailed_ResetsToPending(t *testing.T) {
	repo := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	p, roundID := seedProcessWithRoundStatus(t, repo, tenantID, vo.RoundStatusGenerationFailed)

	h := commands.NewRegenerateRoundQuestionsHandler(repo)
	err := h.Handle(context.Background(), commands.RegenerateRoundQuestionsInput{
		TenantID: tenantID,
		RoundID:  roundID,
	})
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	saved, _ := repo.FindByID(context.Background(), tenantID, p.ID())
	round := saved.Rounds()[0]
	if round.Status() != vo.RoundStatusPending {
		t.Errorf("status: want Pending, got %v", round.Status())
	}
}

func TestRegenerate_FromCompleted_ReturnsErrRoundNotRegenerable(t *testing.T) {
	repo := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	_, roundID := seedProcessWithRoundStatus(t, repo, tenantID, vo.RoundStatusCompleted)

	h := commands.NewRegenerateRoundQuestionsHandler(repo)
	err := h.Handle(context.Background(), commands.RegenerateRoundQuestionsInput{
		TenantID: tenantID,
		RoundID:  roundID,
	})
	if !errors.Is(err, commands.ErrRoundNotRegenerable) {
		t.Errorf("want ErrRoundNotRegenerable, got: %v", err)
	}
}

func TestRegenerate_FromSkipped_ReturnsErrRoundNotRegenerable(t *testing.T) {
	repo := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	_, roundID := seedProcessWithRoundStatus(t, repo, tenantID, vo.RoundStatusSkipped)

	h := commands.NewRegenerateRoundQuestionsHandler(repo)
	err := h.Handle(context.Background(), commands.RegenerateRoundQuestionsInput{
		TenantID: tenantID,
		RoundID:  roundID,
	})
	if !errors.Is(err, commands.ErrRoundNotRegenerable) {
		t.Errorf("want ErrRoundNotRegenerable, got: %v", err)
	}
}

func TestRegenerate_RoundNotFound_ReturnsErrRoundNotFound(t *testing.T) {
	repo := newFakeProcessRepo()
	tenantID := shared.NewTenantID()

	h := commands.NewRegenerateRoundQuestionsHandler(repo)
	err := h.Handle(context.Background(), commands.RegenerateRoundQuestionsInput{
		TenantID: tenantID,
		RoundID:  uuid.New(), // bogus round ID — no matching process
	})
	if !errors.Is(err, entities.ErrRoundNotFound) {
		t.Errorf("want ErrRoundNotFound, got: %v", err)
	}
}

func TestRegenerate_TenantScoped(t *testing.T) {
	repo := newFakeProcessRepo()
	tenantA := shared.NewTenantID()
	tenantB := shared.NewTenantID()

	// Seed process under tenantA.
	_, roundID := seedProcessWithRoundStatus(t, repo, tenantA, vo.RoundStatusQuestionsReady)

	// However the fake FindByRoundID doesn't filter by tenant — let's verify
	// the command correctly isolates by looking up via tenantB which should
	// not be able to find tenantA's round.
	// The fakeProcessRepo.FindByRoundID ignores the tenant arg (T10's fake).
	// We extend the fake to be tenant-aware for this test by seeding a second
	// process under tenantB without that round — the fake scan finds the
	// tenantA process by roundID regardless of tenant param.
	//
	// NOTE: the in-memory fake does NOT filter by tenant on FindByRoundID.
	// Real Postgres does. We add a tenant check here to validate the contract
	// even with the simple fake by checking the returned process's tenant.
	//
	// The production command doesn't re-check tenant after FindByRoundID; it
	// relies on the DB being tenant-scoped. For the fake to enforce this we
	// need the fake to check tenant. Let's extend the fake approach: we'll
	// use a separate fakeProcessRepoTenantScoped.

	// Use a tenant-scoped fake for this particular test.
	scopedRepo := newFakeTenantScopedProcessRepo()
	_, roundID2 := seedProcessForTenantScopedRepo(t, scopedRepo, tenantA)

	h := commands.NewRegenerateRoundQuestionsHandler(scopedRepo)
	err := h.Handle(context.Background(), commands.RegenerateRoundQuestionsInput{
		TenantID: tenantB,   // wrong tenant
		RoundID:  roundID2,  // round belongs to tenantA
	})
	if !errors.Is(err, entities.ErrRoundNotFound) {
		t.Errorf("tenantB should not find tenantA's round; want ErrRoundNotFound, got: %v", err)
	}

	// Suppress unused warning for roundID from the first repo.
	_ = roundID
}

// ---------------------------------------------------------------------------
// Tenant-scoped fake for TestRegenerate_TenantScoped
// ---------------------------------------------------------------------------

// fakeTenantScopedProcessRepo is a variant of fakeProcessRepo that enforces
// tenant matching in FindByRoundID.
type fakeTenantScopedProcessRepo struct {
	inner *fakeProcessRepo
}

func newFakeTenantScopedProcessRepo() *fakeTenantScopedProcessRepo {
	return &fakeTenantScopedProcessRepo{inner: newFakeProcessRepo()}
}

func seedProcessForTenantScopedRepo(t *testing.T, repo *fakeTenantScopedProcessRepo, tenantID shared.TenantID) (*entities.InterviewProcess, uuid.UUID) {
	t.Helper()
	p, roundID := seedProcess(t, repo.inner, tenantID)
	return p, roundID
}

func (r *fakeTenantScopedProcessRepo) Save(ctx context.Context, p *entities.InterviewProcess) error {
	return r.inner.Save(ctx, p)
}

func (r *fakeTenantScopedProcessRepo) FindByID(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (*entities.InterviewProcess, error) {
	return r.inner.FindByID(ctx, tenant, id)
}

func (r *fakeTenantScopedProcessRepo) FindByApplicationID(ctx context.Context, tenant shared.TenantID, applicationID uuid.UUID) (*entities.InterviewProcess, error) {
	return r.inner.FindByApplicationID(ctx, tenant, applicationID)
}

func (r *fakeTenantScopedProcessRepo) FindByRoundID(ctx context.Context, tenant shared.TenantID, roundID uuid.UUID) (*entities.InterviewProcess, error) {
	p, err := r.inner.FindByRoundID(ctx, tenant, roundID)
	if err != nil {
		return nil, err
	}
	// Enforce tenant scoping: if the process belongs to a different tenant,
	// return not found — just like the real Postgres query would.
	if p.TenantID() != tenant {
		return nil, repositories.ErrProcessNotFound
	}
	return p, nil
}

func (r *fakeTenantScopedProcessRepo) ListByTenant(ctx context.Context, tenant shared.TenantID, filter repositories.ProcessListFilter) ([]*entities.InterviewProcess, error) {
	return nil, nil
}

func (r *fakeTenantScopedProcessRepo) ClaimNextPendingRound(ctx context.Context) (*entities.InterviewProcess, uuid.UUID, error) {
	return nil, uuid.Nil, nil
}
