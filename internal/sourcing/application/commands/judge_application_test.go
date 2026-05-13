package commands_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

func newJudgeApplicationHandler(
	appRepo *fakeApplicationRepo,
	candidateRepo *fakeExtendedCandidateRepo,
	intentReader *fakeIntentReader,
	judge *fakeLLMJudge,
	judgeJobRepo *fakeJudgeJobRepo,
) *commands.JudgeApplicationHandler {
	return commands.NewJudgeApplicationHandler(
		appRepo,
		candidateRepo,
		intentReader,
		judge,
		judgeJobRepo,
		commands.JudgeApplicationConfig{},
	)
}

// makeReadyJudgeJob creates a Pending JudgeJob and seeds the appRepo + candidateRepo
// + intentReader so Handle can load all dependencies.
func makeReadyJudgeJob(
	t *testing.T,
	tenantID shared.TenantID,
	appRepo *fakeApplicationRepo,
	candidateRepo *fakeExtendedCandidateRepo,
	intentReader *fakeIntentReader,
) (*entities.JudgeJob, *entities.Application) {
	t.Helper()

	intent := makeIntent(tenantID)
	intentReader.addIntent(intent)

	candidate := makeCandidate(t, tenantID)
	candidateRepo.addCandidate(candidate)

	app := makeScoredApplication(t, tenantID, candidate.ID(), intent.ID)
	require.NoError(t, appRepo.Save(context.Background(), app))
	appRepo.saves = 0 // reset counter after seeding

	job := entities.NewJudgeJob(entities.NewJudgeJobInput{
		TenantID:      tenantID,
		ApplicationID: app.ID(),
		IntentID:      intent.ID,
		CoarseScore:   117.0,
	})
	return job, app
}

func TestJudgeApplication_HappyPath_RecordsJudgment(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	appRepo := newFakeApplicationRepo()
	candidateRepo := newFakeExtendedCandidateRepo()
	intentReader := newFakeIntentReader()

	job, app := makeReadyJudgeJob(t, tenantID, appRepo, candidateRepo, intentReader)

	judgeJobRepo := newFakeJudgeJobRepo()
	judge := &fakeLLMJudge{
		judgment: vo.LLMJudgment{
			Score:         82,
			Summary:       "Strong Go developer",
			PromptVersion: "v1",
		},
	}

	handler := newJudgeApplicationHandler(appRepo, candidateRepo, intentReader, judge, judgeJobRepo)
	err := handler.Handle(ctx, job)

	require.NoError(t, err)
	assert.Equal(t, entities.JobDone, job.Status(), "expected job status Done")
	assert.NotNil(t, app.OverallScore(), "expected overall_score populated after judgment")
	assert.InDelta(t, 82.0, *app.OverallScore(), 0.001, "overall_score should match judgment score")
	assert.NotNil(t, app.LLMJudgment(), "expected llm_judgment stored on application")
}

func TestJudgeApplication_RetryableError_FailsJobForRetry(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	appRepo := newFakeApplicationRepo()
	candidateRepo := newFakeExtendedCandidateRepo()
	intentReader := newFakeIntentReader()

	job, app := makeReadyJudgeJob(t, tenantID, appRepo, candidateRepo, intentReader)

	judgeJobRepo := newFakeJudgeJobRepo()
	judge := &fakeLLMJudge{
		err: services.JudgeError{Retryable: true, Reason: "rate_limit"},
	}

	handler := newJudgeApplicationHandler(appRepo, candidateRepo, intentReader, judge, judgeJobRepo)
	err := handler.Handle(ctx, job)

	require.NoError(t, err, "retryable judge error should not propagate")
	// Job should be re-queued for a retry (Pending) or still running if retry schedule is empty.
	// With default non-empty retry schedule, Fail advances attempt and keeps Pending.
	assert.NotEqual(t, entities.JobDone, job.Status(), "job should not be Done after retryable error")
	// Application should remain unchanged (still Scored).
	assert.Equal(t, vo.AppStatusScored, app.Status(), "application should be unchanged after retryable error")
	assert.Nil(t, app.OverallScore(), "overall_score should not be set on retryable failure")
}

func TestJudgeApplication_NonRetryableError_MarksAppJudgeFailed(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	appRepo := newFakeApplicationRepo()
	candidateRepo := newFakeExtendedCandidateRepo()
	intentReader := newFakeIntentReader()

	job, app := makeReadyJudgeJob(t, tenantID, appRepo, candidateRepo, intentReader)

	judgeJobRepo := newFakeJudgeJobRepo()
	judge := &fakeLLMJudge{
		err: services.JudgeError{Retryable: false, Reason: "context_too_long"},
	}

	handler := newJudgeApplicationHandler(appRepo, candidateRepo, intentReader, judge, judgeJobRepo)
	err := handler.Handle(ctx, job)

	require.NoError(t, err, "non-retryable judge error should not propagate as handler error")
	assert.Equal(t, vo.AppStatusJudgeFailed, app.Status(), "application should be marked JudgeFailed")
	assert.Equal(t, entities.JobFailed, job.Status(), "job should be terminally Failed")
}
