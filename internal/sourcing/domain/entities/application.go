package entities

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/events"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// NewApplicationInput is the constructor input for NewApplication.
type NewApplicationInput struct {
	TenantID             shared.TenantID
	CandidateID          uuid.UUID
	IntentID             uuid.UUID
	IntentSpecVersion    int
	ProfileSchemaVersion int
	// Optional overrides for deterministic tests; nil → real values.
	Now func() time.Time
	ID  uuid.UUID
}

// Application is the per-(Candidate, Intent) scoring aggregate.
// Its lifecycle tracks the match-scoring pipeline: New → Scored | Excluded |
// EmbedFailed, and Scored → JudgeFailed | Stale.
type Application struct {
	id                   uuid.UUID
	tenantID             shared.TenantID
	candidateID          uuid.UUID
	intentID             uuid.UUID
	intentSpecVersion    int
	profileSchemaVersion int
	status               vo.ApplicationStatus
	overallScore         *float64
	scoreBand            *vo.ScoreBand
	ruleMatch            vo.RuleMatchReport
	ruleMatchRecorded    bool // true once RecordRuleMatch has been called
	embeddingScore       *float64
	llmJudgment          *vo.LLMJudgment
	lastError            string
	attemptCount         int
	nextAttemptAt        time.Time
	scoredAt             *time.Time
	createdAt            time.Time
	updatedAt            time.Time
	pendingEvents        []events.Event
}

// NewApplication constructs a fresh Application in status New.
// Validation: tenant present, candidate present, intent present,
// intent_spec_version > 0, profile_schema_version > 0.
// Does NOT emit events on construction.
func NewApplication(in NewApplicationInput) (*Application, error) {
	if in.TenantID == (shared.TenantID{}) {
		return nil, errors.New("tenant_id required")
	}
	if in.CandidateID == uuid.Nil {
		return nil, errors.New("candidate_id required")
	}
	if in.IntentID == uuid.Nil {
		return nil, errors.New("intent_id required")
	}
	if in.IntentSpecVersion <= 0 {
		return nil, errors.New("intent_spec_version must be > 0")
	}
	if in.ProfileSchemaVersion <= 0 {
		return nil, errors.New("profile_schema_version must be > 0")
	}

	now := time.Now().UTC
	if in.Now != nil {
		now = in.Now
	}
	id := in.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	t := now().UTC()

	return &Application{
		id:                   id,
		tenantID:             in.TenantID,
		candidateID:          in.CandidateID,
		intentID:             in.IntentID,
		intentSpecVersion:    in.IntentSpecVersion,
		profileSchemaVersion: in.ProfileSchemaVersion,
		status:               vo.AppStatusNew,
		nextAttemptAt:        t,
		createdAt:            t,
		updatedAt:            t,
	}, nil
}

// Accessors.
func (a *Application) ID() uuid.UUID                    { return a.id }
func (a *Application) TenantID() shared.TenantID        { return a.tenantID }
func (a *Application) CandidateID() uuid.UUID           { return a.candidateID }
func (a *Application) IntentID() uuid.UUID              { return a.intentID }
func (a *Application) IntentSpecVersion() int           { return a.intentSpecVersion }
func (a *Application) ProfileSchemaVersion() int        { return a.profileSchemaVersion }
func (a *Application) Status() vo.ApplicationStatus     { return a.status }
func (a *Application) OverallScore() *float64           { return a.overallScore }
func (a *Application) ScoreBand() *vo.ScoreBand         { return a.scoreBand }
func (a *Application) RuleMatch() vo.RuleMatchReport    { return a.ruleMatch }
func (a *Application) RuleMatchRecorded() bool          { return a.ruleMatchRecorded }
func (a *Application) EmbeddingScore() *float64         { return a.embeddingScore }
func (a *Application) LLMJudgment() *vo.LLMJudgment     { return a.llmJudgment }
func (a *Application) LastError() string                { return a.lastError }
func (a *Application) AttemptCount() int                { return a.attemptCount }
func (a *Application) NextAttemptAt() time.Time         { return a.nextAttemptAt }
func (a *Application) ScoredAt() *time.Time             { return a.scoredAt }
func (a *Application) CreatedAt() time.Time             { return a.createdAt }
func (a *Application) UpdatedAt() time.Time             { return a.updatedAt }

// PullEvents returns and drains the aggregate's pending events.
func (a *Application) PullEvents() []events.Event {
	out := a.pendingEvents
	a.pendingEvents = nil
	return out
}

func (a *Application) emit(ev events.Event) {
	a.pendingEvents = append(a.pendingEvents, ev)
}

func (a *Application) touch(t time.Time) { a.updatedAt = t }

// RecordRuleMatch stores the rule match report on the aggregate.
// Only valid when status == New.
func (a *Application) RecordRuleMatch(report vo.RuleMatchReport) error {
	if a.status != vo.AppStatusNew {
		return ErrInvalidTransition
	}
	a.ruleMatch = report
	a.ruleMatchRecorded = true
	a.updatedAt = time.Now().UTC()
	return nil
}

// Exclude transitions New → Excluded and emits ApplicationExcluded.
// Only valid when status == New.
func (a *Application) Exclude(reason string) error {
	if a.status != vo.AppStatusNew {
		return ErrInvalidTransition
	}
	t := time.Now().UTC()
	a.status = vo.AppStatusExcluded
	a.lastError = reason
	a.touch(t)
	a.emit(events.ApplicationExcluded{
		ApplicationID: a.id,
		CandidateID:   a.candidateID,
		IntentID:      a.intentID,
		TenantID:      a.tenantID,
		Reason:        reason,
		OccurredAt:    t,
	})
	return nil
}

// RecordEmbeddingScore records the cosine similarity score for the application.
// Only valid when status == New AND RecordRuleMatch has been called AND the
// rule report passed required criteria.
func (a *Application) RecordEmbeddingScore(score float64) error {
	if a.status != vo.AppStatusNew {
		return ErrInvalidTransition
	}
	if !a.ruleMatchRecorded {
		return errors.New("rule match must be recorded before embedding score")
	}
	if !a.ruleMatch.PassedRequired() {
		return errors.New("cannot record embedding score when required rules did not pass")
	}
	a.embeddingScore = &score
	a.updatedAt = time.Now().UTC()
	return nil
}

// MarkEmbedFailed transitions New → EmbedFailed, sets last_error, and emits
// ApplicationEmbedFailed.
func (a *Application) MarkEmbedFailed(reason string) error {
	if a.status != vo.AppStatusNew {
		return ErrInvalidTransition
	}
	t := time.Now().UTC()
	a.status = vo.AppStatusEmbedFailed
	a.lastError = reason
	a.touch(t)
	a.emit(events.ApplicationEmbedFailed{
		ApplicationID: a.id,
		TenantID:      a.tenantID,
		Reason:        reason,
		OccurredAt:    t,
	})
	return nil
}

// MarkScored transitions New → Scored. Requires that both RecordRuleMatch and
// RecordEmbeddingScore have been called. Sets scored_at. If overallScore is
// non-nil, sets overall_score and derives score_band. Emits ApplicationScored.
func (a *Application) MarkScored(overallScore *float64) error {
	if a.status != vo.AppStatusNew {
		return ErrInvalidTransition
	}
	if !a.ruleMatchRecorded {
		return errors.New("rule match must be recorded before marking scored")
	}
	if a.embeddingScore == nil {
		return errors.New("embedding score must be recorded before marking scored")
	}
	t := time.Now().UTC()
	a.status = vo.AppStatusScored
	a.scoredAt = &t
	a.touch(t)

	var scoreBandStr string
	if overallScore != nil {
		a.overallScore = overallScore
		band := vo.DeriveBand(*overallScore)
		a.scoreBand = &band
		scoreBandStr = string(band)
	}

	a.emit(events.ApplicationScored{
		ApplicationID:  a.id,
		CandidateID:    a.candidateID,
		IntentID:       a.intentID,
		TenantID:       a.tenantID,
		OverallScore:   overallScore,
		ScoreBand:      scoreBandStr,
		EmbeddingScore: *a.embeddingScore,
		OccurredAt:     t,
	})
	return nil
}

// RecordLLMJudgment stores the LLM judgment on a Scored application.
// Only valid when status == Scored. Updates overall_score and score_band.
// Does NOT emit a new event — the row is already Scored.
func (a *Application) RecordLLMJudgment(j vo.LLMJudgment) error {
	if a.status != vo.AppStatusScored {
		return ErrInvalidTransition
	}
	t := time.Now().UTC()
	a.llmJudgment = &j
	score := float64(j.Score)
	a.overallScore = &score
	band := vo.DeriveBand(score)
	a.scoreBand = &band
	a.touch(t)
	return nil
}

// MarkJudgeFailed transitions Scored → JudgeFailed, sets last_error, and emits
// ApplicationJudgeFailed.
func (a *Application) MarkJudgeFailed(reason string) error {
	if a.status != vo.AppStatusScored {
		return ErrInvalidTransition
	}
	t := time.Now().UTC()
	a.status = vo.AppStatusJudgeFailed
	a.lastError = reason
	a.touch(t)
	a.emit(events.ApplicationJudgeFailed{
		ApplicationID: a.id,
		TenantID:      a.tenantID,
		Reason:        reason,
		OccurredAt:    t,
	})
	return nil
}

// MarkStale transitions Scored → Stale.
// The permitted source states are governed by CanTransitionTo; today only Scored qualifies.
func (a *Application) MarkStale() error {
	if !a.status.CanTransitionTo(vo.AppStatusStale) {
		return ErrInvalidTransition
	}
	t := time.Now().UTC()
	a.status = vo.AppStatusStale
	a.touch(t)
	return nil
}

// ScheduleRetry bumps attempt_count, sets last_error, and advances
// next_attempt_at via the backoff schedule. Status is NOT changed here —
// the caller is responsible for calling MarkEmbedFailed / MarkJudgeFailed
// when retries are exhausted.
func (a *Application) ScheduleRetry(reason string, now time.Time, schedule []time.Duration) {
	a.attemptCount++
	a.lastError = reason
	if a.attemptCount <= len(schedule) {
		a.nextAttemptAt = now.Add(schedule[a.attemptCount-1])
	}
	a.updatedAt = now.UTC()
}

// Shortlist transitions Scored → Shortlisted. Emits ApplicationShortlisted.
func (a *Application) Shortlist(actorUserID uuid.UUID) error {
	if !a.status.CanTransitionTo(vo.AppStatusShortlisted) {
		return ErrInvalidTransition
	}
	t := time.Now().UTC()
	a.status = vo.AppStatusShortlisted
	a.touch(t)
	a.emit(events.ApplicationShortlisted{
		ApplicationID: a.id,
		CandidateID:   a.candidateID,
		IntentID:      a.intentID,
		TenantID:      a.tenantID,
		ActorUserID:   actorUserID,
		OccurredAt:    t,
	})
	return nil
}

// Reject transitions Scored | Shortlisted | Interviewing → Rejected.
// reason is required (>=1 char, whitespace-only counts as empty).
// Emits ApplicationRejected with the reason and actor.
func (a *Application) Reject(actorUserID uuid.UUID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errors.New("reject: reason required")
	}
	if !a.status.CanTransitionTo(vo.AppStatusRejected) {
		return ErrInvalidTransition
	}
	t := time.Now().UTC()
	a.status = vo.AppStatusRejected
	a.lastError = reason
	a.touch(t)
	a.emit(events.ApplicationRejected{
		ApplicationID: a.id,
		CandidateID:   a.candidateID,
		IntentID:      a.intentID,
		TenantID:      a.tenantID,
		ActorUserID:   actorUserID,
		Reason:        reason,
		OccurredAt:    t,
	})
	return nil
}

// Hire transitions Scored | Shortlisted | Interviewing → Hired.
// Emits ApplicationHired.
func (a *Application) Hire(actorUserID uuid.UUID) error {
	if !a.status.CanTransitionTo(vo.AppStatusHired) {
		return ErrInvalidTransition
	}
	t := time.Now().UTC()
	a.status = vo.AppStatusHired
	a.touch(t)
	a.emit(events.ApplicationHired{
		ApplicationID: a.id,
		CandidateID:   a.candidateID,
		IntentID:      a.intentID,
		TenantID:      a.tenantID,
		ActorUserID:   actorUserID,
		OccurredAt:    t,
	})
	return nil
}

// MoveToInterviewing transitions Shortlisted → Interviewing.
// Emits ApplicationMovedToInterviewing.
func (a *Application) MoveToInterviewing(actorUserID uuid.UUID) error {
	if !a.status.CanTransitionTo(vo.AppStatusInterviewing) {
		return ErrInvalidTransition
	}
	t := time.Now().UTC()
	a.status = vo.AppStatusInterviewing
	a.touch(t)
	a.emit(events.ApplicationMovedToInterviewing{
		ApplicationID: a.id,
		CandidateID:   a.candidateID,
		IntentID:      a.intentID,
		TenantID:      a.tenantID,
		ActorUserID:   actorUserID,
		OccurredAt:    t,
	})
	return nil
}

// RehydrateApplicationInput is for repository reads — bypasses event emission.
type RehydrateApplicationInput struct {
	ID                   uuid.UUID
	TenantID             shared.TenantID
	CandidateID          uuid.UUID
	IntentID             uuid.UUID
	IntentSpecVersion    int
	ProfileSchemaVersion int
	Status               vo.ApplicationStatus
	OverallScore         *float64
	ScoreBand            *vo.ScoreBand
	RuleMatch            vo.RuleMatchReport
	RuleMatchRecorded    bool
	EmbeddingScore       *float64
	LLMJudgment          *vo.LLMJudgment
	LastError            string
	AttemptCount         int
	NextAttemptAt        time.Time
	ScoredAt             *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// RehydrateApplication reconstructs an Application aggregate from a persisted row.
// Repositories use this; application code must not.
func RehydrateApplication(in RehydrateApplicationInput) *Application {
	return &Application{
		id:                   in.ID,
		tenantID:             in.TenantID,
		candidateID:          in.CandidateID,
		intentID:             in.IntentID,
		intentSpecVersion:    in.IntentSpecVersion,
		profileSchemaVersion: in.ProfileSchemaVersion,
		status:               in.Status,
		overallScore:         in.OverallScore,
		scoreBand:            in.ScoreBand,
		ruleMatch:            in.RuleMatch,
		ruleMatchRecorded:    in.RuleMatchRecorded,
		embeddingScore:       in.EmbeddingScore,
		llmJudgment:          in.LLMJudgment,
		lastError:            in.LastError,
		attemptCount:         in.AttemptCount,
		nextAttemptAt:        in.NextAttemptAt,
		scoredAt:             in.ScoredAt,
		createdAt:            in.CreatedAt,
		updatedAt:            in.UpdatedAt,
	}
}
