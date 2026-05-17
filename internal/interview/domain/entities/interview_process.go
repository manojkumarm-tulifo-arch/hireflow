package entities

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/domain/events"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ErrInvalidTransition is returned when an attempted state transition is not
// permitted by the round status state machine.
var ErrInvalidTransition = errors.New("interview: invalid state transition")

// ErrRoundNotFound is returned by aggregate methods that look up a round by id.
var ErrRoundNotFound = errors.New("interview: round not found")

// InterviewRound lives inside the InterviewProcess aggregate. Has no
// independent lifecycle; transitions are driven by methods on the parent.
type InterviewRound struct {
	id            uuid.UUID
	kind          vo.RoundKind
	sequence      int
	status        vo.RoundStatus
	questions     []vo.Question // nil until QuestionsReady
	attemptCount  int
	lastError     string
	nextAttemptAt time.Time
	createdAt     time.Time
	updatedAt     time.Time
}

// Accessors.
func (r *InterviewRound) ID() uuid.UUID            { return r.id }
func (r *InterviewRound) Kind() vo.RoundKind       { return r.kind }
func (r *InterviewRound) Sequence() int            { return r.sequence }
func (r *InterviewRound) Status() vo.RoundStatus   { return r.status }
func (r *InterviewRound) Questions() []vo.Question { return append([]vo.Question(nil), r.questions...) }
func (r *InterviewRound) AttemptCount() int        { return r.attemptCount }
func (r *InterviewRound) LastError() string        { return r.lastError }
func (r *InterviewRound) NextAttemptAt() time.Time { return r.nextAttemptAt }
func (r *InterviewRound) CreatedAt() time.Time     { return r.createdAt }
func (r *InterviewRound) UpdatedAt() time.Time     { return r.updatedAt }

// InterviewProcess is the aggregate root for one shortlisted candidate's
// interview loop.
type InterviewProcess struct {
	id            uuid.UUID
	tenantID      shared.TenantID
	applicationID uuid.UUID
	candidateID   uuid.UUID
	intentID      uuid.UUID
	status        vo.ProcessStatus
	rounds        []*InterviewRound
	createdAt     time.Time
	updatedAt     time.Time
	pendingEvents []events.Event
}

// NewInterviewProcessInput is the constructor input.
type NewInterviewProcessInput struct {
	TenantID      shared.TenantID
	ApplicationID uuid.UUID
	CandidateID   uuid.UUID
	IntentID      uuid.UUID
	Rounds        []TemplateRound // sequences MUST be contiguous starting at 1
	Now           func() time.Time
	ID            uuid.UUID
}

// NewInterviewProcess constructs a fresh process with all rounds in Pending.
// Emits InterviewProcessCreated.
func NewInterviewProcess(in NewInterviewProcessInput) (*InterviewProcess, error) {
	// Note: TenantID validation uses IsZero() since the struct has a private field.
	if in.TenantID.IsZero() {
		return nil, errors.New("process: tenant_id required")
	}
	if in.ApplicationID == uuid.Nil {
		return nil, errors.New("process: application_id required")
	}
	if in.CandidateID == uuid.Nil {
		return nil, errors.New("process: candidate_id required")
	}
	if in.IntentID == uuid.Nil {
		return nil, errors.New("process: intent_id required")
	}
	if len(in.Rounds) == 0 {
		return nil, errors.New("process: rounds must be non-empty")
	}

	// Validate rounds: contiguous sequences starting at 1, all kinds valid.
	seen := make(map[int]bool, len(in.Rounds))
	maxSeq := 0
	for _, r := range in.Rounds {
		if _, err := vo.ParseRoundKind(string(r.Kind)); err != nil {
			return nil, err
		}
		if r.Sequence < 1 || seen[r.Sequence] {
			return nil, errors.New("process: invalid round sequence")
		}
		seen[r.Sequence] = true
		if r.Sequence > maxSeq {
			maxSeq = r.Sequence
		}
	}
	if maxSeq != len(in.Rounds) {
		return nil, errors.New("process: sequences must be contiguous starting at 1")
	}

	now := time.Now().UTC
	if in.Now != nil {
		now = in.Now
	}
	id := in.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	t := now()

	rounds := make([]*InterviewRound, 0, len(in.Rounds))
	for _, r := range in.Rounds {
		rounds = append(rounds, &InterviewRound{
			id:            uuid.New(),
			kind:          r.Kind,
			sequence:      r.Sequence,
			status:        vo.RoundStatusPending,
			nextAttemptAt: t,
			createdAt:     t,
			updatedAt:     t,
		})
	}

	p := &InterviewProcess{
		id:            id,
		tenantID:      in.TenantID,
		applicationID: in.ApplicationID,
		candidateID:   in.CandidateID,
		intentID:      in.IntentID,
		status:        vo.ProcessStatusNew,
		rounds:        rounds,
		createdAt:     t,
		updatedAt:     t,
	}
	p.emit(events.InterviewProcessCreated{
		ProcessID:     p.id,
		TenantID:      p.tenantID,
		ApplicationID: p.applicationID,
		CandidateID:   p.candidateID,
		IntentID:      p.intentID,
		OccurredAt:    t,
	})
	return p, nil
}

// Accessors.
func (p *InterviewProcess) ID() uuid.UUID             { return p.id }
func (p *InterviewProcess) TenantID() shared.TenantID { return p.tenantID }
func (p *InterviewProcess) ApplicationID() uuid.UUID  { return p.applicationID }
func (p *InterviewProcess) CandidateID() uuid.UUID    { return p.candidateID }
func (p *InterviewProcess) IntentID() uuid.UUID       { return p.intentID }
func (p *InterviewProcess) Status() vo.ProcessStatus  { return p.status }
func (p *InterviewProcess) Rounds() []*InterviewRound { return p.rounds }
func (p *InterviewProcess) CreatedAt() time.Time      { return p.createdAt }
func (p *InterviewProcess) UpdatedAt() time.Time      { return p.updatedAt }

// PullEvents returns all pending events and drains the slice.
func (p *InterviewProcess) PullEvents() []events.Event {
	out := p.pendingEvents
	p.pendingEvents = nil
	return out
}

func (p *InterviewProcess) emit(e events.Event) { p.pendingEvents = append(p.pendingEvents, e) }
func (p *InterviewProcess) touch(t time.Time)   { p.updatedAt = t }

func (p *InterviewProcess) findRound(roundID uuid.UUID) (*InterviewRound, error) {
	for _, r := range p.rounds {
		if r.id == roundID {
			return r, nil
		}
	}
	return nil, ErrRoundNotFound
}

// MarkRoundQuestionsReady transitions a Pending round to QuestionsReady,
// stores the generated questions, and emits InterviewQuestionsGenerated.
func (p *InterviewProcess) MarkRoundQuestionsReady(roundID uuid.UUID, questions []vo.Question) error {
	r, err := p.findRound(roundID)
	if err != nil {
		return err
	}
	if !r.status.CanTransitionTo(vo.RoundStatusQuestionsReady) {
		return ErrInvalidTransition
	}
	if len(questions) == 0 {
		return errors.New("process: questions must be non-empty")
	}
	for _, q := range questions {
		if err := q.Validate(); err != nil {
			return err
		}
	}
	t := time.Now().UTC()
	r.status = vo.RoundStatusQuestionsReady
	r.questions = append([]vo.Question(nil), questions...)
	r.lastError = ""
	r.updatedAt = t
	p.touch(t)
	p.emit(events.InterviewQuestionsGenerated{
		RoundID:       r.id,
		ProcessID:     p.id,
		Kind:          string(r.kind),
		QuestionCount: len(questions),
		TenantID:      p.tenantID,
		OccurredAt:    t,
	})
	return nil
}

// MarkRoundGenerationFailed transitions a Pending round to GenerationFailed
// after retries are exhausted.
func (p *InterviewProcess) MarkRoundGenerationFailed(roundID uuid.UUID, reason string) error {
	r, err := p.findRound(roundID)
	if err != nil {
		return err
	}
	if !r.status.CanTransitionTo(vo.RoundStatusGenerationFailed) {
		return ErrInvalidTransition
	}
	t := time.Now().UTC()
	r.status = vo.RoundStatusGenerationFailed
	r.lastError = reason
	r.updatedAt = t
	p.touch(t)
	return nil
}

// RecordGenerationAttempt increments attempt_count and stores the failure
// detail + next_attempt_at. Used between retries before reaching abort.
func (p *InterviewProcess) RecordGenerationAttempt(roundID uuid.UUID, detail string, nextAttempt time.Time) error {
	r, err := p.findRound(roundID)
	if err != nil {
		return err
	}
	if r.status != vo.RoundStatusPending {
		return ErrInvalidTransition
	}
	t := time.Now().UTC()
	r.attemptCount++
	r.lastError = detail
	r.nextAttemptAt = nextAttempt
	r.updatedAt = t
	p.touch(t)
	return nil
}

// ResetRoundForRegeneration transitions QuestionsReady or GenerationFailed
// back to Pending. Resets attempt_count and last_error. Used by recruiter
// regeneration. The round will be picked up by the worker pool again.
func (p *InterviewProcess) ResetRoundForRegeneration(roundID uuid.UUID) error {
	r, err := p.findRound(roundID)
	if err != nil {
		return err
	}
	if !r.status.CanTransitionTo(vo.RoundStatusPending) {
		return ErrInvalidTransition
	}
	t := time.Now().UTC()
	r.status = vo.RoundStatusPending
	r.attemptCount = 0
	r.lastError = ""
	r.questions = nil
	r.nextAttemptAt = t
	r.updatedAt = t
	p.touch(t)
	return nil
}

// MarkRoundCompleted transitions QuestionsReady to Completed.
func (p *InterviewProcess) MarkRoundCompleted(roundID uuid.UUID) error {
	r, err := p.findRound(roundID)
	if err != nil {
		return err
	}
	if !r.status.CanTransitionTo(vo.RoundStatusCompleted) {
		return ErrInvalidTransition
	}
	t := time.Now().UTC()
	r.status = vo.RoundStatusCompleted
	r.updatedAt = t
	p.touch(t)
	return nil
}

// MarkRoundSkipped transitions any non-terminal round to Skipped.
func (p *InterviewProcess) MarkRoundSkipped(roundID uuid.UUID) error {
	r, err := p.findRound(roundID)
	if err != nil {
		return err
	}
	if !r.status.CanTransitionTo(vo.RoundStatusSkipped) {
		return ErrInvalidTransition
	}
	t := time.Now().UTC()
	r.status = vo.RoundStatusSkipped
	r.updatedAt = t
	p.touch(t)
	return nil
}

// Complete transitions the process to Completed. All rounds must be terminal
// (Completed or Skipped).
func (p *InterviewProcess) Complete() error {
	if p.status.IsTerminal() {
		return ErrInvalidTransition
	}
	for _, r := range p.rounds {
		if !r.status.IsTerminal() {
			return errors.New("process: cannot complete; round " + r.id.String() + " is " + string(r.status))
		}
	}
	t := time.Now().UTC()
	p.status = vo.ProcessStatusCompleted
	p.touch(t)
	return nil
}

// Cancel transitions the process to Cancelled from any non-terminal state.
func (p *InterviewProcess) Cancel() error {
	if p.status.IsTerminal() {
		return ErrInvalidTransition
	}
	t := time.Now().UTC()
	p.status = vo.ProcessStatusCancelled
	p.touch(t)
	return nil
}

// RehydrateInterviewProcessInput is for loading from persistence.
type RehydrateInterviewProcessInput struct {
	ID            uuid.UUID
	TenantID      shared.TenantID
	ApplicationID uuid.UUID
	CandidateID   uuid.UUID
	IntentID      uuid.UUID
	Status        vo.ProcessStatus
	Rounds        []RehydrateRoundInput
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// RehydrateRoundInput is for loading a single round from persistence.
type RehydrateRoundInput struct {
	ID            uuid.UUID
	Kind          vo.RoundKind
	Sequence      int
	Status        vo.RoundStatus
	Questions     []vo.Question
	AttemptCount  int
	LastError     string
	NextAttemptAt time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// RehydrateInterviewProcess builds from persisted values without re-validating
// invariants or emitting events. Use only from the repository.
func RehydrateInterviewProcess(in RehydrateInterviewProcessInput) *InterviewProcess {
	rounds := make([]*InterviewRound, 0, len(in.Rounds))
	for _, r := range in.Rounds {
		rounds = append(rounds, &InterviewRound{
			id:            r.ID,
			kind:          r.Kind,
			sequence:      r.Sequence,
			status:        r.Status,
			questions:     append([]vo.Question(nil), r.Questions...),
			attemptCount:  r.AttemptCount,
			lastError:     r.LastError,
			nextAttemptAt: r.NextAttemptAt,
			createdAt:     r.CreatedAt,
			updatedAt:     r.UpdatedAt,
		})
	}
	return &InterviewProcess{
		id:            in.ID,
		tenantID:      in.TenantID,
		applicationID: in.ApplicationID,
		candidateID:   in.CandidateID,
		intentID:      in.IntentID,
		status:        in.Status,
		rounds:        rounds,
		createdAt:     in.CreatedAt,
		updatedAt:     in.UpdatedAt,
	}
}
