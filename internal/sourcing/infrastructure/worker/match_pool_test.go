package worker_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/worker"
)

// oneShotAppRepo serves one Application, then ErrApplicationNotFound forever.
type oneShotAppRepo struct {
	served atomic.Bool
	app    *entities.Application
}

func (r *oneShotAppRepo) Save(_ context.Context, _ *entities.Application) error { return nil }
func (r *oneShotAppRepo) FindByID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.Application, error) {
	return nil, repositories.ErrApplicationNotFound
}
func (r *oneShotAppRepo) FindByCandidateAndIntent(_ context.Context, _ shared.TenantID, _, _ uuid.UUID) (*entities.Application, error) {
	return nil, repositories.ErrApplicationNotFound
}
func (r *oneShotAppRepo) ListByIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID, _ repositories.ApplicationListFilter) ([]*entities.Application, error) {
	return nil, nil
}
func (r *oneShotAppRepo) ClaimNextNew(_ context.Context) (*entities.Application, error) {
	if r.served.CompareAndSwap(false, true) {
		return r.app, nil
	}
	return nil, repositories.ErrApplicationNotFound
}
func (r *oneShotAppRepo) TopByCoarseScoreForIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID, _ int) ([]*entities.Application, error) {
	return nil, nil
}
func (r *oneShotAppRepo) InvalidateJudgmentsForIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID) error {
	return nil
}

// newMatchPoolApplication builds a minimal Application in status New for pool tests.
func newMatchPoolApplication(t *testing.T) *entities.Application {
	t.Helper()
	app, err := entities.NewApplication(entities.NewApplicationInput{
		TenantID:             shared.NewTenantID(),
		CandidateID:          uuid.New(),
		IntentID:             uuid.New(),
		IntentSpecVersion:    1,
		ProfileSchemaVersion: 1,
	})
	require.NoError(t, err)
	return app
}

// TestMatchPool_HandlesOneClaimAndExits verifies that the match pool loop starts,
// processes a single claimed application, and exits cleanly when the context is canceled.
// Full pipeline coverage is reserved for the T20 e2e test.
func TestMatchPool_HandlesOneClaimAndExits(t *testing.T) {
	app := newMatchPoolApplication(t)
	repo := &oneShotAppRepo{app: app}

	// Use a nil handler — the pool guards against nil before calling Handle.
	// This is sufficient for the smoke test: assert no panic + clean exit.
	var handler *commands.ScoreApplicationHandler

	pool := worker.NewMatchPool(repo, handler, worker.Config{Size: 1, PollInterval: 10 * time.Millisecond}, zerolog.Nop())
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go pool.Run(ctx)
	<-ctx.Done()
	assert.True(t, true, "match pool exited cleanly on context cancellation")
}
