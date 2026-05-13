// Package worker drives the sourcing pipeline.
// match_pool.go — MatchPool claims New Applications and hands them to ScoreApplicationHandler.
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

// MatchPool runs Size goroutines, each polling the repository for the next
// claimable Application in New status and handing it to ScoreApplicationHandler.
type MatchPool struct {
	repo    repositories.ApplicationRepository
	handler *commands.ScoreApplicationHandler
	cfg     Config
	logger  zerolog.Logger
}

// NewMatchPool wires the pool.
func NewMatchPool(repo repositories.ApplicationRepository, handler *commands.ScoreApplicationHandler, cfg Config, logger zerolog.Logger) *MatchPool {
	if cfg.Size <= 0 {
		cfg.Size = 1
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = time.Second
	}
	return &MatchPool{repo: repo, handler: handler, cfg: cfg, logger: logger}
}

// Run blocks until ctx is canceled.
func (p *MatchPool) Run(ctx context.Context) {
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

func (p *MatchPool) loop(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		app, err := p.repo.ClaimNextNew(ctx)
		if err != nil {
			if errors.Is(err, repositories.ErrApplicationNotFound) {
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
			p.logger.Error().Err(err).Int("worker", id).Msg("match claim failed")
			time.Sleep(p.cfg.PollInterval)
			continue
		}

		if p.handler == nil {
			continue
		}
		if err := p.handler.Handle(ctx, app); err != nil {
			p.logger.Error().Err(err).Str("application_id", app.ID().String()).Msg("score application failed")
		}
	}
}
