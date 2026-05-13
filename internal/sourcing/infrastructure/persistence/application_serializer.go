package persistence

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// applicationRow mirrors the applications table columns.
type applicationRow struct {
	id                   uuid.UUID
	tenantID             string
	candidateID          uuid.UUID
	intentID             uuid.UUID
	intentSpecVersion    int
	profileSchemaVersion int
	status               string
	overallScore         *float64
	scoreBand            *string
	ruleMatch            []byte
	embeddingScore       *float64
	llmJudgment          []byte // NULL when no judgment yet
	lastError            *string
	attemptCount         int
	nextAttemptAt        time.Time
	scoredAt             *time.Time
	createdAt            time.Time
	updatedAt            time.Time
}

func serializeApplication(a *entities.Application) (applicationRow, error) {
	ruleMatchBytes, err := a.RuleMatch().Marshal()
	if err != nil {
		return applicationRow{}, fmt.Errorf("marshal rule_match: %w", err)
	}

	row := applicationRow{
		id:                   a.ID(),
		tenantID:             a.TenantID().String(),
		candidateID:          a.CandidateID(),
		intentID:             a.IntentID(),
		intentSpecVersion:    a.IntentSpecVersion(),
		profileSchemaVersion: a.ProfileSchemaVersion(),
		status:               string(a.Status()),
		overallScore:         a.OverallScore(),
		embeddingScore:       a.EmbeddingScore(),
		ruleMatch:            ruleMatchBytes,
		attemptCount:         a.AttemptCount(),
		nextAttemptAt:        a.NextAttemptAt(),
		scoredAt:             a.ScoredAt(),
		createdAt:            a.CreatedAt(),
		updatedAt:            a.UpdatedAt(),
	}

	if a.ScoreBand() != nil {
		s := string(*a.ScoreBand())
		row.scoreBand = &s
	}

	if a.LastError() != "" {
		e := a.LastError()
		row.lastError = &e
	}

	if a.LLMJudgment() != nil {
		b, err := json.Marshal(a.LLMJudgment())
		if err != nil {
			return applicationRow{}, fmt.Errorf("marshal llm_judgment: %w", err)
		}
		row.llmJudgment = b
	}

	return row, nil
}

func hydrateApplication(r applicationRow) (*entities.Application, error) {
	tenant, err := shared.ParseTenantID(r.tenantID)
	if err != nil {
		return nil, fmt.Errorf("tenant: %w", err)
	}

	status, err := vo.ParseApplicationStatus(r.status)
	if err != nil {
		return nil, fmt.Errorf("status: %w", err)
	}

	ruleMatch, err := vo.UnmarshalRuleMatch(r.ruleMatch)
	if err != nil {
		return nil, fmt.Errorf("rule_match: %w", err)
	}

	var scoreBand *vo.ScoreBand
	if r.scoreBand != nil {
		b := vo.ScoreBand(*r.scoreBand)
		scoreBand = &b
	}

	var lastErr string
	if r.lastError != nil {
		lastErr = *r.lastError
	}

	var llmJudgment *vo.LLMJudgment
	if len(r.llmJudgment) > 0 {
		j, err := vo.UnmarshalLLMJudgment(r.llmJudgment)
		if err != nil {
			return nil, fmt.Errorf("llm_judgment: %w", err)
		}
		llmJudgment = &j
	}

	// ruleMatchRecorded is true when the rule_match JSON has any results or the
	// status is past the New/unscored stage, i.e. rule match was previously set.
	ruleMatchRecorded := len(ruleMatch.Results) > 0 || status != vo.AppStatusNew

	return entities.RehydrateApplication(entities.RehydrateApplicationInput{
		ID:                   r.id,
		TenantID:             tenant,
		CandidateID:          r.candidateID,
		IntentID:             r.intentID,
		IntentSpecVersion:    r.intentSpecVersion,
		ProfileSchemaVersion: r.profileSchemaVersion,
		Status:               status,
		OverallScore:         r.overallScore,
		ScoreBand:            scoreBand,
		RuleMatch:            ruleMatch,
		RuleMatchRecorded:    ruleMatchRecorded,
		EmbeddingScore:       r.embeddingScore,
		LLMJudgment:          llmJudgment,
		LastError:            lastErr,
		AttemptCount:         r.attemptCount,
		NextAttemptAt:        r.nextAttemptAt,
		ScoredAt:             r.scoredAt,
		CreatedAt:            r.createdAt,
		UpdatedAt:            r.updatedAt,
	}), nil
}
