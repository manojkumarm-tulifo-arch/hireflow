// Package worker holds the interview context's background workers.
package worker

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/hustle/hireflow/internal/interview/application/commands"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
)

// GenerateRoundHandler is the narrow interface over GenerateRoundQuestionsHandler
// that the pool uses internally. Exposed so tests can inject fakes.
type GenerateRoundHandler interface {
	Handle(ctx context.Context, in commands.GenerateRoundQuestionsInput) error
}

// Config controls pool size and polling cadence.
type Config struct {
	Size         int
	PollInterval time.Duration
}

// QuestionGenerationPool repeatedly claims a Pending round and dispatches
// GenerateRoundQuestions. Same shape as sourcing.MatchPool and JudgePool.
type QuestionGenerationPool struct {
	processes repositories.ProcessRepository
	handler   GenerateRoundHandler
	cfg       Config
	logger    zerolog.Logger
}

// NewQuestionGenerationPool wires the pool. The handler parameter is the
// concrete *commands.GenerateRoundQuestionsHandler which satisfies the
// GenerateRoundHandler interface implicitly.
func NewQuestionGenerationPool(
	processes repositories.ProcessRepository,
	handler *commands.GenerateRoundQuestionsHandler,
	cfg Config,
	logger zerolog.Logger,
) *QuestionGenerationPool {
	return newQuestionGenerationPool(processes, handler, cfg, logger)
}

// NewQuestionGenerationPoolForTest wires the pool with any GenerateRoundHandler
// implementation. Intended for unit tests only.
func NewQuestionGenerationPoolForTest(
	processes repositories.ProcessRepository,
	handler GenerateRoundHandler,
	cfg Config,
	logger zerolog.Logger,
) *QuestionGenerationPool {
	return newQuestionGenerationPool(processes, handler, cfg, logger)
}

func newQuestionGenerationPool(
	processes repositories.ProcessRepository,
	handler GenerateRoundHandler,
	cfg Config,
	logger zerolog.Logger,
) *QuestionGenerationPool {
	if cfg.Size <= 0 {
		cfg.Size = 2
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	return &QuestionGenerationPool{
		processes: processes,
		handler:   handler,
		cfg:       cfg,
		logger:    logger.With().Str("component", "interview_qgen_pool").Logger(),
	}
}

// Run starts cfg.Size workers and blocks until ctx is done.
func (p *QuestionGenerationPool) Run(ctx context.Context) {
	p.logger.Info().Int("size", p.cfg.Size).Msg("pool started")
	defer p.logger.Info().Msg("pool stopped")
	var wg sync.WaitGroup
	for i := 0; i < p.cfg.Size; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			p.workerLoop(ctx, id)
		}(i)
	}
	wg.Wait()
}

func (p *QuestionGenerationPool) workerLoop(ctx context.Context, id int) {
	t := time.NewTicker(p.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := p.claimAndProcess(ctx); err != nil {
				if !errors.Is(err, repositories.ErrProcessNotFound) {
					p.logger.Error().Err(err).Int("worker", id).Msg("claim/process failed")
				}
			}
		}
	}
}

func (p *QuestionGenerationPool) claimAndProcess(ctx context.Context) error {
	process, roundID, err := p.processes.ClaimNextPendingRound(ctx)
	if err != nil {
		return err
	}
	return p.handler.Handle(ctx, commands.GenerateRoundQuestionsInput{
		TenantID:  process.TenantID(),
		ProcessID: process.ID(),
		RoundID:   roundID,
	})
}
