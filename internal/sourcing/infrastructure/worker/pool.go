// Package worker drives the sourcing pipeline by claiming resume_uploads rows
// and handing them to ProcessUploadHandler.
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

// Config tunes the pool.
type Config struct {
	Size         int           // number of concurrent goroutines (default 1)
	PollInterval time.Duration // gap between empty-claim retries (default 1s)
}

// Pool runs Size goroutines, each polling the repository for the next
// claimable upload and handing it to ProcessUploadHandler.
type Pool struct {
	repo    repositories.ResumeUploadRepository
	handler *commands.ProcessUploadHandler
	cfg     Config
	logger  zerolog.Logger
}

// NewPool wires the pool.
func NewPool(repo repositories.ResumeUploadRepository, handler *commands.ProcessUploadHandler, cfg Config, logger zerolog.Logger) *Pool {
	if cfg.Size <= 0 {
		cfg.Size = 1
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = time.Second
	}
	return &Pool{repo: repo, handler: handler, cfg: cfg, logger: logger}
}

// Run blocks until ctx is canceled.
func (p *Pool) Run(ctx context.Context) {
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

func (p *Pool) loop(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		u, err := p.repo.ClaimNextPending(ctx)
		if err != nil {
			if errors.Is(err, repositories.ErrNotFound) {
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
			p.logger.Error().Err(err).Int("worker", id).Msg("claim failed")
			time.Sleep(p.cfg.PollInterval)
			continue
		}

		if p.handler == nil {
			continue
		}
		if err := p.handler.Handle(ctx, u); err != nil {
			p.logger.Error().Err(err).Str("upload_id", u.ID().String()).Msg("process failed")
		}
	}
}
