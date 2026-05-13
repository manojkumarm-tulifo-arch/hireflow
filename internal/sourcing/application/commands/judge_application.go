package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
)

// JudgeApplicationConfig holds configuration for JudgeApplicationHandler.
type JudgeApplicationConfig struct {
	// RetryBackoff is the sequence of delays for LLM judge retries.
	// Attempt n uses schedule[n-1]; schedule exhaustion → MarkJudgeFailed.
	RetryBackoff []time.Duration
}

// JudgeApplicationHandler is the judge worker entry point.
// It is invoked by the judge worker pool (T17 JudgePool) for each JudgeJob
// that is ready.
//
// Responsibilities:
//  1. Mark the JudgeJob as Running.
//  2. Load Application + Candidate + Intent.
//  3. Call LLMJudge.Judge with profile, role, and rule_match.
//  4. On success: record judgment on Application, mark Application saved, complete job.
//  5. On retryable judge error: fail the job with retry schedule, leave Application unchanged.
//  6. On non-retryable judge error: mark Application as JudgeFailed, fail job terminally.
type JudgeApplicationHandler struct {
	appRepo      repositories.ApplicationRepository
	candidateRepo repositories.CandidateRepository
	intentReader  services.IntentReader
	judge         services.LLMJudge
	judgeJobRepo  repositories.JudgeJobRepository
	cfg           JudgeApplicationConfig
}

// NewJudgeApplicationHandler wires the handler.
func NewJudgeApplicationHandler(
	appRepo repositories.ApplicationRepository,
	candidateRepo repositories.CandidateRepository,
	intentReader services.IntentReader,
	judge services.LLMJudge,
	judgeJobRepo repositories.JudgeJobRepository,
	cfg JudgeApplicationConfig,
) *JudgeApplicationHandler {
	if len(cfg.RetryBackoff) == 0 {
		cfg.RetryBackoff = []time.Duration{10 * time.Second, time.Minute, 5 * time.Minute}
	}
	return &JudgeApplicationHandler{
		appRepo:       appRepo,
		candidateRepo: candidateRepo,
		intentReader:  intentReader,
		judge:         judge,
		judgeJobRepo:  judgeJobRepo,
		cfg:           cfg,
	}
}

// Handle processes one JudgeJob.
//
// Precondition: the job has already been transitioned to Running by
// JudgeJobRepository.ClaimNextPending (atomic in a single tx). The handler
// does not re-issue BeginRunning — doing so would be a double state-change
// against the already-Running aggregate.
func (h *JudgeApplicationHandler) Handle(ctx context.Context, job *entities.JudgeJob) error {
	// Step 2: load Application.
	app, err := h.appRepo.FindByID(ctx, job.TenantID(), job.ApplicationID())
	if err != nil {
		return fmt.Errorf("judge_application: find application: %w", err)
	}

	// Step 2b: load Candidate.
	candidate, err := h.candidateRepo.FindByID(ctx, job.TenantID(), app.CandidateID())
	if err != nil {
		return fmt.Errorf("judge_application: find candidate: %w", err)
	}

	// Step 2c: load Intent.
	intent, err := h.intentReader.FindByID(ctx, job.TenantID(), app.IntentID())
	if err != nil {
		return fmt.Errorf("judge_application: find intent: %w", err)
	}

	// Step 3: call LLM judge.
	judgment, judgeErr := h.judge.Judge(ctx, candidate.Profile(), intent.Role, app.RuleMatch())
	if judgeErr != nil {
		var je services.JudgeError
		if errors.As(judgeErr, &je) {
			if je.Retryable {
				// Step 5: retryable — schedule job retry, leave Application unchanged.
				job.Fail(je.Reason, time.Now().UTC(), h.cfg.RetryBackoff)
				if err := h.judgeJobRepo.Save(ctx, job); err != nil {
					return fmt.Errorf("judge_application: save retried job: %w", err)
				}
				return nil
			}
			// Step 6: non-retryable — mark Application JudgeFailed, fail job terminally.
			return h.fatalJudgeError(ctx, app, job, je.Reason)
		}
		// Unknown error — treat as retryable.
		job.Fail("judge_unknown: "+judgeErr.Error(), time.Now().UTC(), h.cfg.RetryBackoff)
		if err := h.judgeJobRepo.Save(ctx, job); err != nil {
			return fmt.Errorf("judge_application: save retried job (unknown): %w", err)
		}
		return nil
	}

	// Step 4: success — record judgment on Application.
	if err := app.RecordLLMJudgment(judgment); err != nil {
		return fmt.Errorf("judge_application: record judgment: %w", err)
	}
	if err := h.appRepo.Save(ctx, app); err != nil {
		return fmt.Errorf("judge_application: save application: %w", err)
	}

	// Complete the job.
	job.Complete()
	if err := h.judgeJobRepo.Save(ctx, job); err != nil {
		return fmt.Errorf("judge_application: save completed job: %w", err)
	}

	return nil
}

// fatalJudgeError marks the Application as JudgeFailed and the job as terminally Failed.
func (h *JudgeApplicationHandler) fatalJudgeError(ctx context.Context, app *entities.Application, job *entities.JudgeJob, reason string) error {
	if err := app.MarkJudgeFailed(reason); err != nil {
		return fmt.Errorf("judge_application: mark judge failed: %w", err)
	}
	if err := h.appRepo.Save(ctx, app); err != nil {
		return fmt.Errorf("judge_application: save judge-failed application: %w", err)
	}
	// Fail the job with no retry schedule → immediately sets status to Failed.
	job.Fail(reason, time.Now().UTC(), nil)
	if err := h.judgeJobRepo.Save(ctx, job); err != nil {
		return fmt.Errorf("judge_application: save failed job: %w", err)
	}
	return nil
}
