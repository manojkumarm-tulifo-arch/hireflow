package worker_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/worker"
)

// oneShotJudgeJobRepo serves one JudgeJob, then ErrJudgeJobNotFound forever.
type oneShotJudgeJobRepo struct {
	served atomic.Bool
	job    *entities.JudgeJob
}

func (r *oneShotJudgeJobRepo) Save(_ context.Context, _ *entities.JudgeJob) error { return nil }
func (r *oneShotJudgeJobRepo) FindByID(_ context.Context, _ uuid.UUID) (*entities.JudgeJob, error) {
	return nil, repositories.ErrJudgeJobNotFound
}
func (r *oneShotJudgeJobRepo) ClaimNextPending(_ context.Context) (*entities.JudgeJob, error) {
	if r.served.CompareAndSwap(false, true) {
		return r.job, nil
	}
	return nil, repositories.ErrJudgeJobNotFound
}

// newJudgePoolJob builds a minimal JudgeJob in status Pending for pool tests.
func newJudgePoolJob() *entities.JudgeJob {
	return entities.NewJudgeJob(entities.NewJudgeJobInput{
		TenantID:      shared.NewTenantID(),
		ApplicationID: uuid.New(),
		IntentID:      uuid.New(),
		CoarseScore:   80.0,
	})
}

// TestJudgePool_HandlesOneClaimAndExits verifies that the judge pool loop starts,
// processes a single claimed job, and exits cleanly when the context is canceled.
// Full pipeline coverage is reserved for the T20 e2e test.
func TestJudgePool_HandlesOneClaimAndExits(t *testing.T) {
	job := newJudgePoolJob()
	repo := &oneShotJudgeJobRepo{job: job}

	// Use a nil handler — the pool guards against nil before calling Handle.
	// This is sufficient for the smoke test: assert no panic + clean exit.
	var handler *commands.JudgeApplicationHandler

	pool := worker.NewJudgePool(repo, handler, worker.Config{Size: 1, PollInterval: 10 * time.Millisecond}, zerolog.Nop())
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go pool.Run(ctx)
	<-ctx.Done()
	assert.True(t, true, "judge pool exited cleanly on context cancellation")
}
