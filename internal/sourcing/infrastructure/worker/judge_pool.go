// judge_pool.go — JudgePool claims Pending JudgeJobs and hands them to JudgeApplicationHandler.
package worker

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
)

// JudgePool runs Size goroutines, each polling the repository for the next
// claimable JudgeJob in Pending status and handing it to JudgeApplicationHandler.
type JudgePool struct {
	repo    repositories.JudgeJobRepository
	handler *commands.JudgeApplicationHandler
	cfg     Config
	logger  zerolog.Logger
}

// NewJudgePool wires the pool.
func NewJudgePool(repo repositories.JudgeJobRepository, handler *commands.JudgeApplicationHandler, cfg Config, logger zerolog.Logger) *JudgePool {
	if cfg.Size <= 0 {
		cfg.Size = 1
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = time.Second
	}
	return &JudgePool{repo: repo, handler: handler, cfg: cfg, logger: logger}
}

// Run blocks until ctx is canceled.
func (p *JudgePool) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < p.cfg.Size; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			p.loop(ctx, id)
		}(i)
	}
	wg.Wait()
}

func (p *JudgePool) loop(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		job, err := p.repo.ClaimNextPending(ctx)
		if err != nil {
			if errors.Is(err, repositories.ErrJudgeJobNotFound) {
				select {
				case <-ctx.Done():
					return
				case <-time.After(p.cfg.PollInterval):
				}
				continue
			}
			if errors.Is(err, context.Canceled) {
				return
			}
			p.logger.Error().Err(err).Int("worker", id).Msg("judge claim failed")
			time.Sleep(p.cfg.PollInterval)
			continue
		}

		if p.handler == nil {
			continue
		}
		if err := p.handler.Handle(ctx, job); err != nil {
			p.logger.Error().Err(err).Str("job_id", job.ID().String()).Msg("judge application failed")
		}
	}
}
