package entities_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/events"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

func validAppInput(t *testing.T) entities.NewApplicationInput {
	t.Helper()
	return entities.NewApplicationInput{
		TenantID:             shared.NewTenantID(),
		CandidateID:          uuid.New(),
		IntentID:             uuid.New(),
		IntentSpecVersion:    1,
		ProfileSchemaVersion: 1,
	}
}

func passedRuleMatch() vo.RuleMatchReport {
	return vo.RuleMatchReport{
		Results: []vo.RuleResult{
			{
				Criterion: vo.RuleCriterion{Type: "skill", Name: "Go", Required: true},
				Passed:    true,
			},
		},
	}
}

func failedRuleMatch() vo.RuleMatchReport {
	return vo.RuleMatchReport{
		Results: []vo.RuleResult{
			{
				Criterion: vo.RuleCriterion{Type: "skill", Name: "Java", Required: true},
				Passed:    false,
			},
		},
	}
}

// ── NewApplication ────────────────────────────────────────────────────────────

func TestNewApplication_HappyPath(t *testing.T) {
	in := validAppInput(t)
	app, err := entities.NewApplication(in)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, app.ID())
	assert.Equal(t, vo.AppStatusNew, app.Status())
	assert.Nil(t, app.OverallScore())
	assert.Nil(t, app.ScoreBand())
	assert.Empty(t, app.PullEvents(), "NewApplication must NOT emit events")
}

func TestNewApplication_RejectsZeroTenantID(t *testing.T) {
	in := validAppInput(t)
	in.TenantID = shared.TenantID{}
	_, err := entities.NewApplication(in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant_id")
}

func TestNewApplication_RejectsZeroCandidateID(t *testing.T) {
	in := validAppInput(t)
	in.CandidateID = uuid.Nil
	_, err := entities.NewApplication(in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "candidate_id")
}

func TestNewApplication_RejectsZeroIntentID(t *testing.T) {
	in := validAppInput(t)
	in.IntentID = uuid.Nil
	_, err := entities.NewApplication(in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "intent_id")
}

func TestNewApplication_RejectsZeroIntentSpecVersion(t *testing.T) {
	in := validAppInput(t)
	in.IntentSpecVersion = 0
	_, err := entities.NewApplication(in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "intent_spec_version")
}

func TestNewApplication_RejectsZeroProfileSchemaVersion(t *testing.T) {
	in := validAppInput(t)
	in.ProfileSchemaVersion = 0
	_, err := entities.NewApplication(in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "profile_schema_version")
}

// ── RecordRuleMatch ───────────────────────────────────────────────────────────

func TestRecordRuleMatch_ValidWhenNew(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	err := app.RecordRuleMatch(passedRuleMatch())
	require.NoError(t, err)
	assert.True(t, app.RuleMatchRecorded())
}

func TestRecordRuleMatch_RejectsWhenNotNew(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	_ = app.RecordRuleMatch(passedRuleMatch())
	_ = app.RecordEmbeddingScore(0.8)
	_ = app.MarkScored(nil)
	err := app.RecordRuleMatch(passedRuleMatch())
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

// ── Exclude ───────────────────────────────────────────────────────────────────

func TestExclude_TransitionsToExcluded_EmitsEvent(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	err := app.Exclude("failed required skills")
	require.NoError(t, err)
	assert.Equal(t, vo.AppStatusExcluded, app.Status())
	assert.Equal(t, "failed required skills", app.LastError())

	evs := app.PullEvents()
	require.Len(t, evs, 1)
	exc, ok := evs[0].(events.ApplicationExcluded)
	require.True(t, ok)
	assert.Equal(t, app.ID(), exc.ApplicationID)
	assert.Equal(t, "failed required skills", exc.Reason)
}

func TestExclude_RejectsWhenNotNew(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	_ = app.Exclude("reason")
	_ = app.PullEvents()
	err := app.Exclude("again")
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

// ── RecordEmbeddingScore ──────────────────────────────────────────────────────

func TestRecordEmbeddingScore_RequiresRuleMatchFirst(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	err := app.RecordEmbeddingScore(0.8)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rule match")
}

func TestRecordEmbeddingScore_RejectsWhenRequiredRulesFailed(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	_ = app.RecordRuleMatch(failedRuleMatch())
	err := app.RecordEmbeddingScore(0.8)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required rules")
}

func TestRecordEmbeddingScore_HappyPath(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	_ = app.RecordRuleMatch(passedRuleMatch())
	err := app.RecordEmbeddingScore(0.82)
	require.NoError(t, err)
	require.NotNil(t, app.EmbeddingScore())
	assert.InDelta(t, 0.82, *app.EmbeddingScore(), 1e-9)
}

// ── MarkScored ────────────────────────────────────────────────────────────────

func TestMarkScored_RequiresRuleMatch(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	err := app.MarkScored(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rule match")
}

func TestMarkScored_RequiresEmbeddingScore(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	_ = app.RecordRuleMatch(passedRuleMatch())
	err := app.MarkScored(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "embedding score")
}

func TestMarkScored_NilOverallScore_EmitsEvent_NoBand(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	_ = app.RecordRuleMatch(passedRuleMatch())
	_ = app.RecordEmbeddingScore(0.75)
	err := app.MarkScored(nil)
	require.NoError(t, err)
	assert.Equal(t, vo.AppStatusScored, app.Status())
	assert.Nil(t, app.OverallScore())
	assert.Nil(t, app.ScoreBand())
	assert.NotNil(t, app.ScoredAt())

	evs := app.PullEvents()
	require.Len(t, evs, 1)
	scored, ok := evs[0].(events.ApplicationScored)
	require.True(t, ok)
	assert.Nil(t, scored.OverallScore)
	assert.Empty(t, scored.ScoreBand)
	assert.InDelta(t, 0.75, scored.EmbeddingScore, 1e-9)
}

func TestMarkScored_WithOverallScore_DerivesBand(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	_ = app.RecordRuleMatch(passedRuleMatch())
	_ = app.RecordEmbeddingScore(0.85)
	score := 85.0
	err := app.MarkScored(&score)
	require.NoError(t, err)
	require.NotNil(t, app.OverallScore())
	assert.InDelta(t, 85.0, *app.OverallScore(), 1e-9)
	require.NotNil(t, app.ScoreBand())
	assert.Equal(t, vo.BandStrong, *app.ScoreBand())

	evs := app.PullEvents()
	require.Len(t, evs, 1)
	scored := evs[0].(events.ApplicationScored)
	assert.Equal(t, "strong", scored.ScoreBand)
}

// ── RecordLLMJudgment ─────────────────────────────────────────────────────────

func TestRecordLLMJudgment_HappyPath_UpdatesScoreAndBand(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	_ = app.RecordRuleMatch(passedRuleMatch())
	_ = app.RecordEmbeddingScore(0.7)
	_ = app.MarkScored(nil)
	_ = app.PullEvents()

	j := vo.LLMJudgment{Score: 72, Summary: "good fit", PromptVersion: "v1"}
	err := app.RecordLLMJudgment(j)
	require.NoError(t, err)
	assert.Equal(t, vo.AppStatusScored, app.Status(), "status must stay Scored")
	require.NotNil(t, app.OverallScore())
	assert.InDelta(t, 72.0, *app.OverallScore(), 1e-9)
	require.NotNil(t, app.ScoreBand())
	assert.Equal(t, vo.BandModerate, *app.ScoreBand())
	assert.Empty(t, app.PullEvents(), "RecordLLMJudgment must not emit a new event")
}

func TestRecordLLMJudgment_RejectsWhenNotScored(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	j := vo.LLMJudgment{Score: 80, PromptVersion: "v1"}
	err := app.RecordLLMJudgment(j)
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

// ── MarkJudgeFailed ───────────────────────────────────────────────────────────

func TestMarkJudgeFailed_FromScored_EmitsEvent(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	_ = app.RecordRuleMatch(passedRuleMatch())
	_ = app.RecordEmbeddingScore(0.6)
	_ = app.MarkScored(nil)
	_ = app.PullEvents()

	err := app.MarkJudgeFailed("llm timeout")
	require.NoError(t, err)
	assert.Equal(t, vo.AppStatusJudgeFailed, app.Status())

	evs := app.PullEvents()
	require.Len(t, evs, 1)
	failed, ok := evs[0].(events.ApplicationJudgeFailed)
	require.True(t, ok)
	assert.Equal(t, "llm timeout", failed.Reason)
}

func TestMarkJudgeFailed_RejectsWhenNotScored(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	err := app.MarkJudgeFailed("reason")
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

// ── MarkEmbedFailed ───────────────────────────────────────────────────────────

func TestMarkEmbedFailed_TransitionsToEmbedFailed_EmitsEvent(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	err := app.MarkEmbedFailed("voyage timeout")
	require.NoError(t, err)
	assert.Equal(t, vo.AppStatusEmbedFailed, app.Status())
	assert.Equal(t, "voyage timeout", app.LastError())

	evs := app.PullEvents()
	require.Len(t, evs, 1)
	_, ok := evs[0].(events.ApplicationEmbedFailed)
	assert.True(t, ok)
}

func TestMarkEmbedFailed_RejectsWhenNotNew(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	_ = app.MarkEmbedFailed("first")
	_ = app.PullEvents()
	err := app.MarkEmbedFailed("second")
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

// ── MarkStale ─────────────────────────────────────────────────────────────────

func TestMarkStale_FromNew(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	err := app.MarkStale()
	require.NoError(t, err)
	assert.Equal(t, vo.AppStatusStale, app.Status())
}

func TestMarkStale_FromScored(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	_ = app.RecordRuleMatch(passedRuleMatch())
	_ = app.RecordEmbeddingScore(0.5)
	_ = app.MarkScored(nil)
	_ = app.PullEvents()

	err := app.MarkStale()
	require.NoError(t, err)
	assert.Equal(t, vo.AppStatusStale, app.Status())
}

func TestMarkStale_RejectsFromTerminal(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	_ = app.Exclude("reasons")
	_ = app.PullEvents()
	err := app.MarkStale()
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

// ── ScheduleRetry ─────────────────────────────────────────────────────────────

func TestScheduleRetry_BumpsAttemptCount(t *testing.T) {
	app, _ := entities.NewApplication(validAppInput(t))
	now := time.Now()
	schedule := []time.Duration{5 * time.Second, 30 * time.Second}
	app.ScheduleRetry("transient error", now, schedule)
	assert.Equal(t, 1, app.AttemptCount())
	assert.Equal(t, "transient error", app.LastError())
	assert.Equal(t, now.Add(5*time.Second), app.NextAttemptAt())
	assert.Equal(t, vo.AppStatusNew, app.Status(), "ScheduleRetry must not change status")
}

// ── RehydrateApplication ──────────────────────────────────────────────────────

func TestRehydrateApplication_BypassesEvents(t *testing.T) {
	in := validAppInput(t)
	app, _ := entities.NewApplication(in)
	_ = app.PullEvents()

	now := time.Now().UTC()
	rh := entities.RehydrateApplication(entities.RehydrateApplicationInput{
		ID:                   app.ID(),
		TenantID:             app.TenantID(),
		CandidateID:          app.CandidateID(),
		IntentID:             app.IntentID(),
		IntentSpecVersion:    app.IntentSpecVersion(),
		ProfileSchemaVersion: app.ProfileSchemaVersion(),
		Status:               vo.AppStatusScored,
		RuleMatchRecorded:    true,
		CreatedAt:            now,
		UpdatedAt:            now,
	})
	assert.Equal(t, app.ID(), rh.ID())
	assert.Equal(t, vo.AppStatusScored, rh.Status())
	assert.Empty(t, rh.PullEvents(), "RehydrateApplication must not emit events")
}
