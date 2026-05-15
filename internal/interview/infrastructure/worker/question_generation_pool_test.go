package worker_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/interview/application/commands"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	"github.com/hustle/hireflow/internal/interview/infrastructure/worker"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestProcess builds a minimal InterviewProcess for pool tests.
func newTestProcess(t *testing.T) *entities.InterviewProcess {
	t.Helper()
	p, err := entities.NewInterviewProcess(entities.NewInterviewProcessInput{
		TenantID:      shared.NewTenantID(),
		ApplicationID: uuid.New(),
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
		Rounds: []entities.TemplateRound{
			{Kind: vo.RoundKindScreen, Sequence: 1},
		},
		Now: func() time.Time { return time.Now().UTC() },
	})
	require.NoError(t, err)
	return p
}

// ---------------------------------------------------------------------------
// Fake ProcessRepository implementations
// ---------------------------------------------------------------------------

// oneShotProcessRepo serves one (process, roundID) then ErrProcessNotFound.
type oneShotProcessRepo struct {
	served  atomic.Bool
	process *entities.InterviewProcess
	roundID uuid.UUID
}

func (r *oneShotProcessRepo) Save(_ context.Context, _ *entities.InterviewProcess) error {
	return nil
}
func (r *oneShotProcessRepo) FindByID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.InterviewProcess, error) {
	return nil, repositories.ErrProcessNotFound
}
func (r *oneShotProcessRepo) FindByApplicationID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.InterviewProcess, error) {
	return nil, repositories.ErrProcessNotFound
}
func (r *oneShotProcessRepo) FindByRoundID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.InterviewProcess, error) {
	return nil, repositories.ErrProcessNotFound
}
func (r *oneShotProcessRepo) ListByTenant(_ context.Context, _ shared.TenantID, _ repositories.ProcessListFilter) ([]*entities.InterviewProcess, error) {
	return nil, nil
}
func (r *oneShotProcessRepo) ClaimNextPendingRound(_ context.Context) (*entities.InterviewProcess, uuid.UUID, error) {
	if r.served.CompareAndSwap(false, true) {
		return r.process, r.roundID, nil
	}
	return nil, uuid.Nil, repositories.ErrProcessNotFound
}

// neverClaimableRepo always returns ErrProcessNotFound from ClaimNextPendingRound.
type neverClaimableRepo struct{}

func (r *neverClaimableRepo) Save(_ context.Context, _ *entities.InterviewProcess) error {
	return nil
}
func (r *neverClaimableRepo) FindByID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.InterviewProcess, error) {
	return nil, repositories.ErrProcessNotFound
}
func (r *neverClaimableRepo) FindByApplicationID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.InterviewProcess, error) {
	return nil, repositories.ErrProcessNotFound
}
func (r *neverClaimableRepo) FindByRoundID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.InterviewProcess, error) {
	return nil, repositories.ErrProcessNotFound
}
func (r *neverClaimableRepo) ListByTenant(_ context.Context, _ shared.TenantID, _ repositories.ProcessListFilter) ([]*entities.InterviewProcess, error) {
	return nil, nil
}
func (r *neverClaimableRepo) ClaimNextPendingRound(_ context.Context) (*entities.InterviewProcess, uuid.UUID, error) {
	return nil, uuid.Nil, repositories.ErrProcessNotFound
}

// twoShotProcessRepo serves up to N claims then ErrProcessNotFound.
type twoShotProcessRepo struct {
	remaining int64
	process   *entities.InterviewProcess
	roundID   uuid.UUID
}

func (r *twoShotProcessRepo) Save(_ context.Context, _ *entities.InterviewProcess) error {
	return nil
}
func (r *twoShotProcessRepo) FindByID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.InterviewProcess, error) {
	return nil, repositories.ErrProcessNotFound
}
func (r *twoShotProcessRepo) FindByApplicationID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.InterviewProcess, error) {
	return nil, repositories.ErrProcessNotFound
}
func (r *twoShotProcessRepo) FindByRoundID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.InterviewProcess, error) {
	return nil, repositories.ErrProcessNotFound
}
func (r *twoShotProcessRepo) ListByTenant(_ context.Context, _ shared.TenantID, _ repositories.ProcessListFilter) ([]*entities.InterviewProcess, error) {
	return nil, nil
}
func (r *twoShotProcessRepo) ClaimNextPendingRound(_ context.Context) (*entities.InterviewProcess, uuid.UUID, error) {
	if atomic.AddInt64(&r.remaining, -1) >= 0 {
		return r.process, r.roundID, nil
	}
	return nil, uuid.Nil, repositories.ErrProcessNotFound
}

// ---------------------------------------------------------------------------
// Fake GenerateRoundHandler
// ---------------------------------------------------------------------------

// countingHandler records each Handle call and optionally returns an error on
// a specific invocation number (1-based).
type countingHandler struct {
	calls     atomic.Int64
	firstErr  error
	errOnCall int64 // 1-based; 0 means never return an error
}

func (h *countingHandler) Handle(_ context.Context, _ commands.GenerateRoundQuestionsInput) error {
	n := h.calls.Add(1)
	if h.errOnCall > 0 && n == h.errOnCall && h.firstErr != nil {
		return h.firstErr
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestPool_ClaimsAndDispatches verifies that when a pending round is available,
// the pool claims it and calls the handler exactly once within 200ms.
func TestPool_ClaimsAndDispatches(t *testing.T) {
	p := newTestProcess(t)
	roundID := p.Rounds()[0].ID()

	repo := &oneShotProcessRepo{process: p, roundID: roundID}
	h := &countingHandler{}

	pool := worker.NewQuestionGenerationPoolForTest(
		repo, h,
		worker.Config{Size: 1, PollInterval: 20 * time.Millisecond},
		zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go pool.Run(ctx)
	<-ctx.Done()

	assert.EqualValues(t, 1, h.calls.Load(), "handler should be called exactly once")
}

// TestPool_NothingClaimable_DoesNotCallHandler verifies that when no rounds
// are claimable, the handler is never called.
func TestPool_NothingClaimable_DoesNotCallHandler(t *testing.T) {
	h := &countingHandler{}

	pool := worker.NewQuestionGenerationPoolForTest(
		&neverClaimableRepo{}, h,
		worker.Config{Size: 1, PollInterval: 10 * time.Millisecond},
		zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	go pool.Run(ctx)
	<-ctx.Done()

	assert.EqualValues(t, 0, h.calls.Load(), "handler should not be called when queue is empty")
}

// TestPool_ContinuesAfterHandlerError verifies that the pool does not crash
// when the handler returns an error; it keeps processing on subsequent ticks.
func TestPool_ContinuesAfterHandlerError(t *testing.T) {
	p := newTestProcess(t)
	roundID := p.Rounds()[0].ID()

	repo := &twoShotProcessRepo{process: p, roundID: roundID, remaining: 2}
	h := &countingHandler{firstErr: errors.New("generation failed"), errOnCall: 1}

	pool := worker.NewQuestionGenerationPoolForTest(
		repo, h,
		worker.Config{Size: 1, PollInterval: 20 * time.Millisecond},
		zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	go pool.Run(ctx)
	<-ctx.Done()

	assert.GreaterOrEqual(t, h.calls.Load(), int64(2),
		"pool should have called handler at least twice — once (error) + once (success)")
}

// TestPool_RespectsContextCancel verifies that Run returns promptly after ctx
// is cancelled (within poll-interval + 50ms grace).
func TestPool_RespectsContextCancel(t *testing.T) {
	h := &countingHandler{}

	pollInterval := 50 * time.Millisecond
	pool := worker.NewQuestionGenerationPoolForTest(
		&neverClaimableRepo{}, h,
		worker.Config{Size: 2, PollInterval: pollInterval},
		zerolog.Nop(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		pool.Run(ctx)
		close(done)
	}()

	// Cancel after one poll interval.
	time.Sleep(pollInterval + 10*time.Millisecond)
	cancel()

	select {
	case <-done:
		// Pool exited cleanly.
	case <-time.After(pollInterval + 50*time.Millisecond):
		t.Fatal("Run did not return within poll-interval + 50ms after context cancel")
	}
}
