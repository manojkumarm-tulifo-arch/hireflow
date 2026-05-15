# Interview Slice 1 — Question Generation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the foundation of a new `internal/interview/` bounded context. On `sourcing.ApplicationShortlisted`, create an `InterviewProcess` with rounds from a per-intent loop template (or hardcoded default). For each round, asynchronously generate tailored interview questions via the existing Anthropic client. Recruiter captures structured per-round feedback. End-to-end product surface; foundation for subsequent AI-interviewer slices.

**Architecture:** New bounded context peer of `sourcing`, `hiringintent`, `jobposting`, `auth`. Owns its tables (`migrations/interview/`), its outbox dispatcher, its HTTP routes. Does NOT import `internal/sourcing/...` — cross-context reads via ACL ports. Subscribes to the in-process event bus for `sourcing.ApplicationShortlisted`. Question generation runs in a worker pool mirroring `sourcing.MatchPool` / `JudgePool`.

**Tech Stack:** Same as slices 1-4. No new Go dependencies. Reuses `internal/shared/infrastructure/llm/anthropic` for question generation and `internal/shared/audit` for audit-log integration. New tables under a new `MIGRATE_INTERVIEW` make variable.

**Spec reference:** `docs/superpowers/specs/2026-05-14-interview-slice-1-question-generation-design.md`. Locked decisions from the 2026-05-14 brainstorm:

| # | Decision |
|---|---|
| I1-D1 | Per-intent loop template. Round taxonomy: fixed enum `screen \| technical \| system_design \| behavioral \| bar_raiser`. |
| I1-D2 | Question shape: structured probes with `prompt`, `skill_probed`, `why`, `expected_signals`, `model_answer`, `red_flags`, `follow_ups`. |
| I1-D3 | Eager async generation: worker pool picks up `Pending` rounds after process creation. |
| I1-D4 | Recruiter-entered feedback only. Free-text interviewer name + email. `submitted_by` = recruiter UUID from identity. |
| I1-D5 | Feedback decision enum: `strong_yes \| yes \| mixed \| no \| strong_no`. Recruiter decides the hire; no auto loop-back to sourcing. |
| I1-D6 | Round states: `Pending \| QuestionsReady \| Completed \| Skipped \| GenerationFailed`. No `InProgress` state. |
| I1-D7 | Default loop fallback (`screen → technical → bar_raiser`) when no template exists for the intent. Template upserts do NOT retroactively mutate existing processes. |
| I1-D8 | Cross-context reads via interview-context-owned `IntentReader` + `CandidateReader` ports — Postgres adapters read sourcing/hiringintent tables directly via the shared pool; no Go-level cross-import. |
| I1-D9 | Audit-log: PII reads (`GetInterviewProcess`), lifecycle transitions (feedback, round completion/skip, process completion/cancel, template upsert). Same load-bearing semantics as slice 4. |
| I1-D10 | Out of scope: voice/video AI interviewer, magic-link feedback, scheduling/calendar, candidate-facing UX, loop-template versioning, tenant-specific prompts, per-answer LLM evaluation. |

---

## File structure

### Files created

```
migrations/interview/
    000001_create_interview_tables.up.sql
    000001_create_interview_tables.down.sql

internal/interview/
    domain/
        valueobjects/
            round_kind.go                       RoundKind enum + Parse
            round_kind_test.go
            process_status.go                   ProcessStatus enum + transitions
            process_status_test.go
            round_status.go                     RoundStatus enum + transitions
            round_status_test.go
            feedback_decision.go                FeedbackDecision enum + Parse
            feedback_decision_test.go
            question.go                         Question value object + Validate
            question_test.go
            feedback.go                         Feedback value object + Validate
            feedback_test.go
            retry_decision.go                   RetryDecision for question generation
            retry_decision_test.go
        entities/
            loop_template.go                    LoopTemplate aggregate
            loop_template_test.go
            interview_process.go                InterviewProcess aggregate + InterviewRound
            interview_process_test.go
        events/
            process_events.go                   Process/Questions/Feedback events
            process_events_test.go
        repositories/
            process_repository.go               port
            loop_template_repository.go         port
            feedback_repository.go              port
        services/
            intent_reader.go                    port + RoleSpec DTO
            candidate_reader.go                 port + CandidateProfile DTO
            question_generator.go               port + GenerationInput DTO
    application/
        dto/
            interview_dtos.go                   command/query DTOs
        commands/
            start_interview_process.go
            start_interview_process_test.go
            upsert_loop_template.go
            upsert_loop_template_test.go
            generate_round_questions.go
            generate_round_questions_test.go
            regenerate_round_questions.go
            regenerate_round_questions_test.go
            record_feedback.go
            record_feedback_test.go
            mark_round_completed.go
            mark_round_completed_test.go
            mark_round_skipped.go
            mark_round_skipped_test.go
            complete_process.go
            complete_process_test.go
            cancel_process.go
            cancel_process_test.go
        queries/
            get_interview_process.go
            get_interview_process_test.go
            list_interview_processes.go
            list_interview_processes_test.go
            get_loop_template.go
            get_loop_template_test.go
    infrastructure/
        persistence/
            postgres_process_repository.go
            postgres_process_repository_test.go     integration-tagged
            postgres_loop_template_repository.go
            postgres_loop_template_repository_test.go integration-tagged
            postgres_feedback_repository.go
            postgres_feedback_repository_test.go    integration-tagged
            serializers.go                          shared row → entity helpers
        clients/
            postgres_intent_reader.go
            postgres_intent_reader_test.go          integration-tagged
            postgres_candidate_reader.go
            postgres_candidate_reader_test.go       integration-tagged
        generation/
            anthropic_generator.go                  AnthropicQuestionGenerator
            anthropic_generator_test.go
            prompts.go                              per-RoundKind prompt template builder
            prompts_test.go
        messaging/
            event_publisher.go                      BusPublisher
            event_publisher_test.go
            outbox_dispatcher.go                    OutboxDispatcher
            outbox_dispatcher_test.go               integration-tagged
        worker/
            question_generation_pool.go
            question_generation_pool_test.go
        subscribers/
            application_shortlisted_consumer.go
            application_shortlisted_consumer_test.go
    delivery/
        http/v1/
            handlers.go
            handlers_test.go
            dto.go
            routes.go

tests/
    interview_slice1_e2e_test.go                    full lifecycle e2e

docs/modules/interview/
    README.md                                       new module doc
docs/api/v1/
    interview.openapi.yaml                          new OpenAPI spec
```

### Files modified

- `Makefile` — add `MIGRATE_INTERVIEW`, chain into `migrate-up` / `migrate-down`.
- `cmd/api/main.go` — wire repositories, readers, generator, commands, queries, handlers, outbox, worker pool, subscriber.
- `internal/sourcing/infrastructure/persistence/postgres_resume_upload_repository_test.go` — extend `newPool` TRUNCATE list to include `interview_processes, interview_rounds, interview_feedback, intent_loops, interview_outbox`.
- `internal/sourcing/infrastructure/clients/intent_reader_test.go` — same TRUNCATE list extension.
- `tests/sourcing_slice1_e2e_test.go` — same TRUNCATE list extension.
- `tests/sourcing_slice3_e2e_test.go` — same TRUNCATE list extension.
- `README.md` — add interview row.
- `developer.md` — note new `MIGRATE_INTERVIEW` target + `INTERVIEW_QGEN_POOL` / `INTERVIEW_QGEN_POLL` env vars.

---

## Conventions baked into every task

- **Working branch:** start from `main`: `feat/interview-slice-1`.
- **Module path:** `github.com/hustle/hireflow`.
- **Tests:** unit `_test.go`; integration `//go:build integration`-gated; e2e under `tests/`.
- **Per-test isolation:** the `newPool(t)` / `newPgvectorPool(t)` helpers TRUNCATE on entry. T1 extends them with the new interview tables.
- **Commit cadence:** one commit per task. **No `Co-Authored-By: Claude` trailers.**
- **`make test-integration`** runs with `-p 1` so per-test TRUNCATE doesn't race across packages.
- **No cross-context Go imports:** `internal/interview/` does not import `internal/sourcing/...`. Cross-context reads go through the interview-owned reader ports.

---

## Task 1: `interview` migration namespace + Makefile wiring

**Files:**
- Create: `migrations/interview/000001_create_interview_tables.up.sql`
- Create: `migrations/interview/000001_create_interview_tables.down.sql`
- Modify: `Makefile`
- Modify: `internal/sourcing/infrastructure/persistence/postgres_resume_upload_repository_test.go` (TRUNCATE list)
- Modify: `internal/sourcing/infrastructure/clients/intent_reader_test.go` (TRUNCATE list)
- Modify: `tests/sourcing_slice1_e2e_test.go` (TRUNCATE list)
- Modify: `tests/sourcing_slice3_e2e_test.go` (TRUNCATE list)

- [ ] **Step 1: Up migration**

Create `migrations/interview/000001_create_interview_tables.up.sql`:

```sql
-- intent_loops: per-intent template defining the sequence of rounds for the
-- interview process. Recruiter sets it via UpsertLoopTemplate. If absent
-- when an InterviewProcess is created, the StartInterviewProcess command
-- uses a hardcoded default (screen → technical → bar_raiser).
CREATE TABLE intent_loops (
    id          UUID         PRIMARY KEY,
    tenant_id   UUID         NOT NULL,
    intent_id   UUID         NOT NULL,
    rounds      JSONB        NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),

    UNIQUE (tenant_id, intent_id),
    CONSTRAINT intent_loops_rounds_nonempty CHECK (jsonb_array_length(rounds) > 0)
);

-- interview_processes: one per shortlisted application.
CREATE TABLE interview_processes (
    id              UUID         PRIMARY KEY,
    tenant_id       UUID         NOT NULL,
    application_id  UUID         NOT NULL,
    candidate_id    UUID         NOT NULL,
    intent_id       UUID         NOT NULL,
    status          TEXT         NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),

    UNIQUE (tenant_id, application_id),
    CONSTRAINT interview_processes_status_valid
        CHECK (status IN ('New','InProgress','Completed','Cancelled'))
);

CREATE INDEX interview_processes_intent_idx
    ON interview_processes (tenant_id, intent_id, status, created_at DESC);

-- interview_rounds: one per round per process.
CREATE TABLE interview_rounds (
    id               UUID         PRIMARY KEY,
    tenant_id        UUID         NOT NULL,
    process_id       UUID         NOT NULL,
    kind             TEXT         NOT NULL,
    sequence         INT          NOT NULL,
    status           TEXT         NOT NULL,
    questions        JSONB,
    attempt_count    INT          NOT NULL DEFAULT 0,
    last_error       TEXT         NOT NULL DEFAULT '',
    next_attempt_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT interview_rounds_kind_valid
        CHECK (kind IN ('screen','technical','system_design','behavioral','bar_raiser')),
    CONSTRAINT interview_rounds_status_valid
        CHECK (status IN ('Pending','QuestionsReady','Completed','Skipped','GenerationFailed')),
    CONSTRAINT interview_rounds_sequence_positive CHECK (sequence > 0),
    UNIQUE (tenant_id, process_id, sequence)
);

-- Worker poll index — claim next Pending round whose backoff has elapsed.
CREATE INDEX interview_rounds_pending_idx
    ON interview_rounds (next_attempt_at)
    WHERE status = 'Pending';

-- interview_feedback: append-only. Multiple rows per round allowed (panel).
CREATE TABLE interview_feedback (
    id                 UUID         PRIMARY KEY,
    tenant_id          UUID         NOT NULL,
    round_id           UUID         NOT NULL,
    interviewer_name   TEXT         NOT NULL,
    interviewer_email  TEXT         NOT NULL DEFAULT '',
    decision           TEXT         NOT NULL,
    notes              TEXT         NOT NULL DEFAULT '',
    submitted_by       UUID         NOT NULL,
    submitted_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT interview_feedback_decision_valid
        CHECK (decision IN ('strong_yes','yes','mixed','no','strong_no')),
    CONSTRAINT interview_feedback_interviewer_name_nonempty
        CHECK (length(interviewer_name) > 0)
);

CREATE INDEX interview_feedback_round_idx
    ON interview_feedback (tenant_id, round_id, submitted_at DESC);

-- interview_outbox: same shape as sourcing_outbox. Dispatched by the
-- interview context's own OutboxDispatcher.
CREATE TABLE interview_outbox (
    id             BIGSERIAL    PRIMARY KEY,
    event_name     TEXT         NOT NULL,
    aggregate_id   UUID         NOT NULL,
    tenant_id      UUID         NOT NULL,
    payload        JSONB        NOT NULL,
    occurred_at    TIMESTAMPTZ  NOT NULL,
    dispatched_at  TIMESTAMPTZ
);

CREATE INDEX interview_outbox_pending_idx
    ON interview_outbox (id)
    WHERE dispatched_at IS NULL;
```

- [ ] **Step 2: Down migration**

Create `migrations/interview/000001_create_interview_tables.down.sql`:

```sql
DROP TABLE IF EXISTS interview_outbox;
DROP TABLE IF EXISTS interview_feedback;
DROP TABLE IF EXISTS interview_rounds;
DROP TABLE IF EXISTS interview_processes;
DROP TABLE IF EXISTS intent_loops;
```

- [ ] **Step 3: Makefile wiring**

In `Makefile`, after the `MIGRATE_SHARED` line add:

```makefile
MIGRATE_INTERVIEW := migrate -path migrations/interview   -database "$(DATABASE_URL)&x-migrations-table=schema_migrations_interview"
```

In `migrate-up`, append after `$(MIGRATE_SHARED) up`:

```makefile
	$(MIGRATE_INTERVIEW) up
```

In `migrate-down`, prepend before `$(MIGRATE_SHARED) down 1`:

```makefile
	$(MIGRATE_INTERVIEW) down 1
```

- [ ] **Step 4: Extend TRUNCATE lists**

In each of these four files, find the `TRUNCATE ... CASCADE` statement in the test pool helper and append the new tables:

- `internal/sourcing/infrastructure/persistence/postgres_resume_upload_repository_test.go`
- `internal/sourcing/infrastructure/clients/intent_reader_test.go`
- `tests/sourcing_slice1_e2e_test.go`
- `tests/sourcing_slice3_e2e_test.go`

The current TRUNCATE list looks like:

```sql
TRUNCATE applications, hiring_intent_embeddings, judge_jobs,
         resume_uploads, resume_uploads_dedup, candidates,
         sourcing_outbox, hiring_intents, audit_log CASCADE
```

Replace it with:

```sql
TRUNCATE applications, hiring_intent_embeddings, judge_jobs,
         resume_uploads, resume_uploads_dedup, candidates,
         sourcing_outbox, hiring_intents, audit_log,
         interview_processes, interview_rounds, interview_feedback,
         intent_loops, interview_outbox CASCADE
```

- [ ] **Step 5: Apply migrations + verify**

Bring up Postgres if not running (`make db-up`) and export `DATABASE_URL`:

```
export DATABASE_URL="postgres://hireflow:hireflow@localhost:5433/hireflow?sslmode=disable"
make migrate-up
docker exec hireflow-postgres psql -U hireflow -d hireflow -c '\d interview_processes'
docker exec hireflow-postgres psql -U hireflow -d hireflow -c '\d interview_rounds'
docker exec hireflow-postgres psql -U hireflow -d hireflow -c '\d interview_feedback'
docker exec hireflow-postgres psql -U hireflow -d hireflow -c '\d intent_loops'
docker exec hireflow-postgres psql -U hireflow -d hireflow -c '\d interview_outbox'
```

Expected: each `\d` shows the table with its constraints and indexes from Step 1.

- [ ] **Step 6: Commit**

```
git add migrations/interview/ Makefile \
        internal/sourcing/infrastructure/persistence/postgres_resume_upload_repository_test.go \
        internal/sourcing/infrastructure/clients/intent_reader_test.go \
        tests/sourcing_slice1_e2e_test.go \
        tests/sourcing_slice3_e2e_test.go
git commit -m "feat(interview): migration namespace + 5 tables for interview slice 1"
```

---

## Task 2: Round + status value objects

**Files:**
- Create: `internal/interview/domain/valueobjects/round_kind.go` + `_test.go`
- Create: `internal/interview/domain/valueobjects/process_status.go` + `_test.go`
- Create: `internal/interview/domain/valueobjects/round_status.go` + `_test.go`
- Create: `internal/interview/domain/valueobjects/feedback_decision.go` + `_test.go`

- [ ] **Step 1: RoundKind**

Create `internal/interview/domain/valueobjects/round_kind.go`:

```go
// Package valueobjects holds the value objects of the interview context.
package valueobjects

import "errors"

// RoundKind enumerates the supported interview round types. Each value has
// a corresponding prompt template in the AnthropicQuestionGenerator; adding
// a value requires a new template + tests.
type RoundKind string

const (
	RoundKindScreen       RoundKind = "screen"
	RoundKindTechnical    RoundKind = "technical"
	RoundKindSystemDesign RoundKind = "system_design"
	RoundKindBehavioral   RoundKind = "behavioral"
	RoundKindBarRaiser    RoundKind = "bar_raiser"
)

// ErrInvalidRoundKind is returned by ParseRoundKind when the value is unknown.
var ErrInvalidRoundKind = errors.New("invalid round kind")

// ParseRoundKind validates and returns a RoundKind for the given string.
func ParseRoundKind(s string) (RoundKind, error) {
	switch RoundKind(s) {
	case RoundKindScreen, RoundKindTechnical, RoundKindSystemDesign,
		RoundKindBehavioral, RoundKindBarRaiser:
		return RoundKind(s), nil
	default:
		return "", ErrInvalidRoundKind
	}
}

// String returns the canonical string form.
func (k RoundKind) String() string { return string(k) }
```

Create `round_kind_test.go` covering: each valid value parses; "unknown" returns ErrInvalidRoundKind; empty string returns ErrInvalidRoundKind.

- [ ] **Step 2: ProcessStatus**

Create `internal/interview/domain/valueobjects/process_status.go`:

```go
package valueobjects

import "errors"

type ProcessStatus string

const (
	ProcessStatusNew        ProcessStatus = "New"
	ProcessStatusInProgress ProcessStatus = "InProgress"
	ProcessStatusCompleted  ProcessStatus = "Completed"
	ProcessStatusCancelled  ProcessStatus = "Cancelled"
)

var ErrInvalidProcessStatus = errors.New("invalid process status")

func ParseProcessStatus(s string) (ProcessStatus, error) {
	switch ProcessStatus(s) {
	case ProcessStatusNew, ProcessStatusInProgress,
		ProcessStatusCompleted, ProcessStatusCancelled:
		return ProcessStatus(s), nil
	default:
		return "", ErrInvalidProcessStatus
	}
}

// IsTerminal reports whether the status admits no further transitions.
func (s ProcessStatus) IsTerminal() bool {
	return s == ProcessStatusCompleted || s == ProcessStatusCancelled
}

func (s ProcessStatus) String() string { return string(s) }
```

Test covers: each valid value parses; invalid returns error; IsTerminal correct for all four values.

- [ ] **Step 3: RoundStatus**

Create `internal/interview/domain/valueobjects/round_status.go`:

```go
package valueobjects

import "errors"

type RoundStatus string

const (
	RoundStatusPending          RoundStatus = "Pending"
	RoundStatusQuestionsReady   RoundStatus = "QuestionsReady"
	RoundStatusCompleted        RoundStatus = "Completed"
	RoundStatusSkipped          RoundStatus = "Skipped"
	RoundStatusGenerationFailed RoundStatus = "GenerationFailed"
)

var ErrInvalidRoundStatus = errors.New("invalid round status")

func ParseRoundStatus(s string) (RoundStatus, error) {
	switch RoundStatus(s) {
	case RoundStatusPending, RoundStatusQuestionsReady, RoundStatusCompleted,
		RoundStatusSkipped, RoundStatusGenerationFailed:
		return RoundStatus(s), nil
	default:
		return "", ErrInvalidRoundStatus
	}
}

// IsTerminal reports whether the round status admits no further transitions
// (Completed or Skipped). GenerationFailed is NOT terminal — recruiter can
// regenerate.
func (s RoundStatus) IsTerminal() bool {
	return s == RoundStatusCompleted || s == RoundStatusSkipped
}

// CanTransitionTo reports whether s -> next is a permitted state transition.
//
// Permitted transitions:
//
//	Pending           -> QuestionsReady | GenerationFailed | Skipped
//	QuestionsReady    -> Completed | Skipped | Pending  (recruiter regenerates)
//	GenerationFailed  -> Pending | Skipped                (recruiter regenerates or skips)
//	Completed, Skipped (terminals) — no outbound transitions
func (s RoundStatus) CanTransitionTo(next RoundStatus) bool {
	switch s {
	case RoundStatusPending:
		switch next {
		case RoundStatusQuestionsReady, RoundStatusGenerationFailed, RoundStatusSkipped:
			return true
		}
	case RoundStatusQuestionsReady:
		switch next {
		case RoundStatusCompleted, RoundStatusSkipped, RoundStatusPending:
			return true
		}
	case RoundStatusGenerationFailed:
		switch next {
		case RoundStatusPending, RoundStatusSkipped:
			return true
		}
	}
	return false
}

func (s RoundStatus) String() string { return string(s) }
```

Test covers: each valid value parses; invalid returns error; IsTerminal correct; each documented CanTransitionTo edge returns true, every other edge returns false. Use a table-driven test for the transition matrix.

- [ ] **Step 4: FeedbackDecision**

Create `internal/interview/domain/valueobjects/feedback_decision.go`:

```go
package valueobjects

import "errors"

type FeedbackDecision string

const (
	FeedbackDecisionStrongYes FeedbackDecision = "strong_yes"
	FeedbackDecisionYes       FeedbackDecision = "yes"
	FeedbackDecisionMixed     FeedbackDecision = "mixed"
	FeedbackDecisionNo        FeedbackDecision = "no"
	FeedbackDecisionStrongNo  FeedbackDecision = "strong_no"
)

var ErrInvalidFeedbackDecision = errors.New("invalid feedback decision")

func ParseFeedbackDecision(s string) (FeedbackDecision, error) {
	switch FeedbackDecision(s) {
	case FeedbackDecisionStrongYes, FeedbackDecisionYes, FeedbackDecisionMixed,
		FeedbackDecisionNo, FeedbackDecisionStrongNo:
		return FeedbackDecision(s), nil
	default:
		return "", ErrInvalidFeedbackDecision
	}
}

func (d FeedbackDecision) String() string { return string(d) }
```

Test: each valid value parses; invalid returns error.

- [ ] **Step 5: Verify + commit**

```
go test ./internal/interview/domain/valueobjects/... -count=1 -race
make build
git add internal/interview/domain/valueobjects/
git commit -m "feat(interview): RoundKind, ProcessStatus, RoundStatus, FeedbackDecision value objects"
```

---

## Task 3: Question, Feedback, RetryDecision value objects

**Files:**
- Create: `internal/interview/domain/valueobjects/question.go` + `_test.go`
- Create: `internal/interview/domain/valueobjects/feedback.go` + `_test.go`
- Create: `internal/interview/domain/valueobjects/retry_decision.go` + `_test.go`

- [ ] **Step 1: Question**

Create `internal/interview/domain/valueobjects/question.go`:

```go
package valueobjects

import (
	"errors"
	"strings"
)

// Question is one generated probe for an interview round. Immutable value object.
type Question struct {
	Prompt          string   `json:"prompt"`
	SkillProbed     string   `json:"skill_probed"`
	Why             string   `json:"why"`
	ExpectedSignals []string `json:"expected_signals"`
	ModelAnswer     string   `json:"model_answer"`
	RedFlags        []string `json:"red_flags"`
	FollowUps       []string `json:"follow_ups"`
}

// ErrInvalidQuestion is returned by Validate when a question fails its shape
// requirements.
var ErrInvalidQuestion = errors.New("invalid question")

// Validate enforces minimum invariants. Used both by the LLM-output parser in
// AnthropicQuestionGenerator and as a sanity check on round persistence.
func (q Question) Validate() error {
	if strings.TrimSpace(q.Prompt) == "" {
		return errors.New("question: prompt required")
	}
	if strings.TrimSpace(q.SkillProbed) == "" {
		return errors.New("question: skill_probed required")
	}
	if strings.TrimSpace(q.Why) == "" {
		return errors.New("question: why required")
	}
	if len(q.ExpectedSignals) < 3 {
		return errors.New("question: expected_signals must have at least 3 entries")
	}
	if strings.TrimSpace(q.ModelAnswer) == "" {
		return errors.New("question: model_answer required")
	}
	if len(q.RedFlags) < 2 {
		return errors.New("question: red_flags must have at least 2 entries")
	}
	if len(q.FollowUps) < 1 {
		return errors.New("question: follow_ups must have at least 1 entry")
	}
	return nil
}
```

Create `question_test.go` covering: happy path validates; missing prompt fails; missing skill_probed fails; missing why fails; too-few expected_signals fails; empty model_answer fails; too-few red_flags fails; missing follow_ups fails. Use a table-driven test where each case mutates a known-valid base Question.

- [ ] **Step 2: Feedback**

Create `internal/interview/domain/valueobjects/feedback.go`:

```go
package valueobjects

import (
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Feedback is one recruiter-entered feedback row for an interview round.
// Immutable value object; appended via FeedbackRepository.Save.
type Feedback struct {
	InterviewerName  string
	InterviewerEmail string // optional
	Decision         FeedbackDecision
	Notes            string
	SubmittedBy      uuid.UUID
	SubmittedAt      time.Time
}

// Validate enforces minimum invariants.
func (f Feedback) Validate() error {
	if strings.TrimSpace(f.InterviewerName) == "" {
		return errors.New("feedback: interviewer_name required")
	}
	if f.InterviewerEmail != "" {
		if _, err := mail.ParseAddress(f.InterviewerEmail); err != nil {
			return errors.New("feedback: interviewer_email invalid")
		}
	}
	if _, err := ParseFeedbackDecision(string(f.Decision)); err != nil {
		return err
	}
	if f.SubmittedBy == uuid.Nil {
		return errors.New("feedback: submitted_by required")
	}
	return nil
}
```

Test covers: happy path validates; missing name fails; bad email fails; invalid decision fails; nil submitted_by fails; empty email is OK (optional).

- [ ] **Step 3: RetryDecision**

Create `internal/interview/domain/valueobjects/retry_decision.go`:

```go
package valueobjects

import "time"

// RetryAction describes what the worker should do with a failed generation.
type RetryAction int

const (
	// RetryActionAbort marks the round as GenerationFailed (terminal until
	// the recruiter manually regenerates).
	RetryActionAbort RetryAction = iota
	// RetryActionRetry schedules a fresh attempt after the returned backoff.
	RetryActionRetry
)

// RetryDecision is the worker's response to a generation failure. It is built
// from the failure characteristics (transient, LLM auth, invalid output, etc.)
// and the current attempt count.
type RetryDecision struct {
	Action   RetryAction
	Backoff  time.Duration // honored only when Action == RetryActionRetry
	Detail   string        // free-text for the round's last_error column
}

// FailureKind classifies an upstream Anthropic failure.
type FailureKind int

const (
	FailureKindUnknown FailureKind = iota
	FailureKindTransient
	FailureKindLLMAuth
	FailureKindInvalidJSON
)

// DecideRetry returns the next action for a given failure + attempt count.
// Attempt count is 1-indexed (i.e., the just-completed attempt was attempt N).
//
// Schedules (from the spec):
//   transient: [1m, 5m, 15m, 1h, 4h] then abort
//   llm_auth: abort immediately
//   invalid_json: one retry at 30s then abort
//   unknown: [1m, 5m, 15m] then abort
func DecideRetry(kind FailureKind, attempt int, detail string) RetryDecision {
	switch kind {
	case FailureKindLLMAuth:
		return RetryDecision{Action: RetryActionAbort, Detail: detail}
	case FailureKindInvalidJSON:
		if attempt == 1 {
			return RetryDecision{Action: RetryActionRetry, Backoff: 30 * time.Second, Detail: detail}
		}
		return RetryDecision{Action: RetryActionAbort, Detail: detail}
	case FailureKindTransient:
		schedule := []time.Duration{
			1 * time.Minute, 5 * time.Minute, 15 * time.Minute,
			1 * time.Hour, 4 * time.Hour,
		}
		if attempt <= len(schedule) {
			return RetryDecision{Action: RetryActionRetry, Backoff: schedule[attempt-1], Detail: detail}
		}
		return RetryDecision{Action: RetryActionAbort, Detail: detail}
	default:
		schedule := []time.Duration{1 * time.Minute, 5 * time.Minute, 15 * time.Minute}
		if attempt <= len(schedule) {
			return RetryDecision{Action: RetryActionRetry, Backoff: schedule[attempt-1], Detail: detail}
		}
		return RetryDecision{Action: RetryActionAbort, Detail: detail}
	}
}
```

Test covers: llm_auth → abort immediately; invalid_json attempt 1 → retry 30s; invalid_json attempt 2 → abort; transient attempts 1-5 → retry with correct backoff each; transient attempt 6 → abort; unknown attempts 1-3 → retry; unknown attempt 4 → abort.

- [ ] **Step 4: Verify + commit**

```
go test ./internal/interview/domain/valueobjects/... -count=1 -race
make build
git add internal/interview/domain/valueobjects/question.go \
        internal/interview/domain/valueobjects/question_test.go \
        internal/interview/domain/valueobjects/feedback.go \
        internal/interview/domain/valueobjects/feedback_test.go \
        internal/interview/domain/valueobjects/retry_decision.go \
        internal/interview/domain/valueobjects/retry_decision_test.go
git commit -m "feat(interview): Question + Feedback + RetryDecision value objects"
```

---

## Task 4: LoopTemplate aggregate

**Files:**
- Create: `internal/interview/domain/entities/loop_template.go` + `_test.go`

- [ ] **Step 1: LoopTemplate**

Create `internal/interview/domain/entities/loop_template.go`:

```go
package entities

import (
	"errors"
	"time"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

// TemplateRound describes one round position in a loop template.
type TemplateRound struct {
	Kind     vo.RoundKind
	Sequence int
}

// LoopTemplate is a per-intent definition of the rounds an InterviewProcess
// should contain when a candidate is shortlisted for that intent. Aggregate
// root.
type LoopTemplate struct {
	id        uuid.UUID
	tenantID  shared.TenantID
	intentID  uuid.UUID
	rounds    []TemplateRound
	createdAt time.Time
	updatedAt time.Time
}

// NewLoopTemplateInput is the constructor input.
type NewLoopTemplateInput struct {
	TenantID shared.TenantID
	IntentID uuid.UUID
	Rounds   []TemplateRound
	// Optional overrides for deterministic tests; zero values mean "use real values".
	Now func() time.Time
	ID  uuid.UUID
}

// NewLoopTemplate constructs a validated LoopTemplate. Validation:
//   - tenant required
//   - intent required
//   - rounds non-empty
//   - sequences contiguous starting at 1 (after sorting)
//   - all rounds have valid kinds
//   - no duplicate sequence numbers
func NewLoopTemplate(in NewLoopTemplateInput) (*LoopTemplate, error) {
	if in.TenantID == (shared.TenantID{}) {
		return nil, errors.New("loop_template: tenant_id required")
	}
	if in.IntentID == uuid.Nil {
		return nil, errors.New("loop_template: intent_id required")
	}
	if len(in.Rounds) == 0 {
		return nil, errors.New("loop_template: rounds must be non-empty")
	}

	seen := make(map[int]bool, len(in.Rounds))
	maxSeq := 0
	for _, r := range in.Rounds {
		if _, err := vo.ParseRoundKind(string(r.Kind)); err != nil {
			return nil, err
		}
		if r.Sequence < 1 {
			return nil, errors.New("loop_template: sequence must be >= 1")
		}
		if seen[r.Sequence] {
			return nil, errors.New("loop_template: duplicate sequence")
		}
		seen[r.Sequence] = true
		if r.Sequence > maxSeq {
			maxSeq = r.Sequence
		}
	}
	// Contiguous from 1 means {1..maxSeq} all present.
	if maxSeq != len(in.Rounds) {
		return nil, errors.New("loop_template: sequences must be contiguous starting at 1")
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
	return &LoopTemplate{
		id:        id,
		tenantID:  in.TenantID,
		intentID:  in.IntentID,
		rounds:    append([]TemplateRound(nil), in.Rounds...), // defensive copy
		createdAt: t,
		updatedAt: t,
	}, nil
}

// RehydrateLoopTemplateInput is for loading from persistence.
type RehydrateLoopTemplateInput struct {
	ID        uuid.UUID
	TenantID  shared.TenantID
	IntentID  uuid.UUID
	Rounds    []TemplateRound
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RehydrateLoopTemplate constructs from persisted values without re-validating
// invariants. Use only from the repository.
func RehydrateLoopTemplate(in RehydrateLoopTemplateInput) *LoopTemplate {
	return &LoopTemplate{
		id:        in.ID,
		tenantID:  in.TenantID,
		intentID:  in.IntentID,
		rounds:    append([]TemplateRound(nil), in.Rounds...),
		createdAt: in.CreatedAt,
		updatedAt: in.UpdatedAt,
	}
}

// Accessors.
func (l *LoopTemplate) ID() uuid.UUID             { return l.id }
func (l *LoopTemplate) TenantID() shared.TenantID { return l.tenantID }
func (l *LoopTemplate) IntentID() uuid.UUID       { return l.intentID }
func (l *LoopTemplate) Rounds() []TemplateRound {
	return append([]TemplateRound(nil), l.rounds...)
}
func (l *LoopTemplate) CreatedAt() time.Time { return l.createdAt }
func (l *LoopTemplate) UpdatedAt() time.Time { return l.updatedAt }

// Replace replaces the rounds and bumps updated_at. Validates the new set
// against the same rules as the constructor.
func (l *LoopTemplate) Replace(rounds []TemplateRound, now func() time.Time) error {
	tmp, err := NewLoopTemplate(NewLoopTemplateInput{
		TenantID: l.tenantID,
		IntentID: l.intentID,
		Rounds:   rounds,
		Now:      now,
		ID:       l.id,
	})
	if err != nil {
		return err
	}
	l.rounds = tmp.rounds
	if now != nil {
		l.updatedAt = now()
	} else {
		l.updatedAt = time.Now().UTC()
	}
	return nil
}
```

Create `loop_template_test.go` covering:

- happy construction with three rounds (sequences 1, 2, 3)
- empty rounds → error
- missing tenant → error
- missing intent → error
- invalid round kind → error
- zero sequence → error
- duplicate sequence → error
- non-contiguous sequence (e.g. [1, 3]) → error
- Replace with valid set updates rounds + updated_at
- Replace with invalid set leaves aggregate unchanged

- [ ] **Step 2: Verify + commit**

```
go test ./internal/interview/domain/entities/... -count=1 -race
make build
git add internal/interview/domain/entities/loop_template.go \
        internal/interview/domain/entities/loop_template_test.go
git commit -m "feat(interview): LoopTemplate aggregate with validation"
```

---

## Task 5: InterviewProcess + InterviewRound aggregates

**Files:**
- Create: `internal/interview/domain/entities/interview_process.go` + `_test.go`
- Create: `internal/interview/domain/events/process_events.go` + `_test.go`

### Domain events first (needed by the entity's emit calls)

- [ ] **Step 1: Events**

Create `internal/interview/domain/events/process_events.go`:

```go
// Package events defines the domain events emitted by the interview context.
package events

import (
	"time"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// Event is the minimum interface every interview event satisfies, matching
// the shape consumed by the outbox dispatcher.
type Event interface {
	EventName() string
	AggregateID() uuid.UUID
	Tenant() shared.TenantID
	At() time.Time
}

// InterviewProcessCreated is emitted after a new InterviewProcess is created
// in response to ApplicationShortlisted.
type InterviewProcessCreated struct {
	ProcessID     uuid.UUID       `json:"process_id"`
	TenantID      shared.TenantID `json:"tenant_id"`
	ApplicationID uuid.UUID       `json:"application_id"`
	CandidateID   uuid.UUID       `json:"candidate_id"`
	IntentID      uuid.UUID       `json:"intent_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
}

func (e InterviewProcessCreated) EventName() string       { return "interview.InterviewProcessCreated" }
func (e InterviewProcessCreated) AggregateID() uuid.UUID  { return e.ProcessID }
func (e InterviewProcessCreated) Tenant() shared.TenantID { return e.TenantID }
func (e InterviewProcessCreated) At() time.Time           { return e.OccurredAt }

// InterviewQuestionsGenerated is emitted after a round's questions are
// successfully generated.
type InterviewQuestionsGenerated struct {
	RoundID       uuid.UUID       `json:"round_id"`
	ProcessID     uuid.UUID       `json:"process_id"`
	Kind          string          `json:"kind"`
	QuestionCount int             `json:"question_count"`
	TenantID      shared.TenantID `json:"tenant_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
}

func (e InterviewQuestionsGenerated) EventName() string       { return "interview.InterviewQuestionsGenerated" }
func (e InterviewQuestionsGenerated) AggregateID() uuid.UUID  { return e.RoundID }
func (e InterviewQuestionsGenerated) Tenant() shared.TenantID { return e.TenantID }
func (e InterviewQuestionsGenerated) At() time.Time           { return e.OccurredAt }

// InterviewFeedbackRecorded is emitted after a feedback row is persisted.
type InterviewFeedbackRecorded struct {
	FeedbackID uuid.UUID       `json:"feedback_id"`
	RoundID    uuid.UUID       `json:"round_id"`
	Decision   string          `json:"decision"`
	TenantID   shared.TenantID `json:"tenant_id"`
	OccurredAt time.Time       `json:"occurred_at"`
}

func (e InterviewFeedbackRecorded) EventName() string       { return "interview.InterviewFeedbackRecorded" }
func (e InterviewFeedbackRecorded) AggregateID() uuid.UUID  { return e.FeedbackID }
func (e InterviewFeedbackRecorded) Tenant() shared.TenantID { return e.TenantID }
func (e InterviewFeedbackRecorded) At() time.Time           { return e.OccurredAt }
```

Create `process_events_test.go` covering: each event's `EventName()` returns the expected string; `AggregateID()`/`Tenant()`/`At()` return the expected field. Plus a JSON round-trip test that marshals + unmarshals each event and asserts field equality.

### InterviewProcess aggregate

- [ ] **Step 2: Aggregate**

Create `internal/interview/domain/entities/interview_process.go`:

```go
package entities

import (
	"errors"
	"time"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/domain/events"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

// ErrInvalidTransition is returned when an attempted state transition is not
// permitted by the round status state machine.
var ErrInvalidTransition = errors.New("interview: invalid state transition")

// ErrRoundNotFound is returned by aggregate methods that look up a round by id.
var ErrRoundNotFound = errors.New("interview: round not found")

// InterviewRound lives inside the InterviewProcess aggregate. Has no
// independent lifecycle; transitions are driven by methods on the parent.
type InterviewRound struct {
	id              uuid.UUID
	kind            vo.RoundKind
	sequence        int
	status          vo.RoundStatus
	questions       []vo.Question // nil until QuestionsReady
	attemptCount    int
	lastError       string
	nextAttemptAt   time.Time
	createdAt       time.Time
	updatedAt       time.Time
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
	if in.TenantID == (shared.TenantID{}) {
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
func (p *InterviewProcess) ID() uuid.UUID            { return p.id }
func (p *InterviewProcess) TenantID() shared.TenantID { return p.tenantID }
func (p *InterviewProcess) ApplicationID() uuid.UUID  { return p.applicationID }
func (p *InterviewProcess) CandidateID() uuid.UUID    { return p.candidateID }
func (p *InterviewProcess) IntentID() uuid.UUID       { return p.intentID }
func (p *InterviewProcess) Status() vo.ProcessStatus  { return p.status }
func (p *InterviewProcess) Rounds() []*InterviewRound { return p.rounds }
func (p *InterviewProcess) CreatedAt() time.Time      { return p.createdAt }
func (p *InterviewProcess) UpdatedAt() time.Time      { return p.updatedAt }
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
```

- [ ] **Step 3: Tests**

Create `internal/interview/domain/entities/interview_process_test.go`. Tests should cover:

- Constructor happy path with 3 rounds; emitted event has the expected fields
- Missing tenant / application / candidate / intent → error
- Empty rounds → error
- Non-contiguous sequences → error
- Duplicate sequence → error
- MarkRoundQuestionsReady from Pending with valid questions → state change + event emitted
- MarkRoundQuestionsReady from Completed → ErrInvalidTransition
- MarkRoundQuestionsReady with empty questions → error
- MarkRoundQuestionsReady with one invalid question → error from Validate
- MarkRoundGenerationFailed from Pending → state change
- MarkRoundGenerationFailed from QuestionsReady → ErrInvalidTransition
- RecordGenerationAttempt increments count + sets last_error + next_attempt_at
- ResetRoundForRegeneration from QuestionsReady → Pending with cleared fields + cleared questions
- ResetRoundForRegeneration from GenerationFailed → Pending
- ResetRoundForRegeneration from Completed → ErrInvalidTransition
- MarkRoundCompleted from QuestionsReady → Completed
- MarkRoundCompleted from Pending → ErrInvalidTransition
- MarkRoundSkipped from Pending → Skipped (also valid from QuestionsReady and GenerationFailed)
- MarkRoundSkipped from Completed → ErrInvalidTransition
- Complete with all rounds terminal → Completed
- Complete with one Pending round → error
- Complete with one GenerationFailed round → error
- Complete on already-Completed process → ErrInvalidTransition
- Cancel from New → Cancelled
- Cancel on already-Cancelled → ErrInvalidTransition
- findRound with unknown id → ErrRoundNotFound (assert via any method that calls it)
- PullEvents drains the slice (subsequent calls return nil)

- [ ] **Step 4: Verify + commit**

```
go test ./internal/interview/domain/entities/... ./internal/interview/domain/events/... -count=1 -race
make build
git add internal/interview/domain/entities/interview_process.go \
        internal/interview/domain/entities/interview_process_test.go \
        internal/interview/domain/events/process_events.go \
        internal/interview/domain/events/process_events_test.go
git commit -m "feat(interview): InterviewProcess aggregate + 3 domain events"
```

---

## Task 6: Repository ports + Postgres adapters

**Files:**
- Create: `internal/interview/domain/repositories/process_repository.go`
- Create: `internal/interview/domain/repositories/loop_template_repository.go`
- Create: `internal/interview/domain/repositories/feedback_repository.go`
- Create: `internal/interview/infrastructure/persistence/postgres_process_repository.go` + `_test.go`
- Create: `internal/interview/infrastructure/persistence/postgres_loop_template_repository.go` + `_test.go`
- Create: `internal/interview/infrastructure/persistence/postgres_feedback_repository.go` + `_test.go`
- Create: `internal/interview/infrastructure/persistence/serializers.go`

- [ ] **Step 1: Repository ports**

Create `internal/interview/domain/repositories/process_repository.go`:

```go
// Package repositories defines the persistence ports of the interview context.
package repositories

import (
	"context"
	"errors"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
)

// ErrProcessNotFound is returned when a process lookup finds no row.
var ErrProcessNotFound = errors.New("interview: process not found")

// ErrProcessDuplicate is returned by Save when (tenant_id, application_id)
// already exists. ApplicationShortlistedConsumer treats this as a no-op
// (idempotent re-delivery of the bus event).
var ErrProcessDuplicate = errors.New("interview: process duplicate")

// ProcessListFilter controls which rows ListByIntent returns.
type ProcessListFilter struct {
	IntentID uuid.UUID
	Status   string // empty = all
	Limit    int
	Offset   int
}

// ProcessRepository persists InterviewProcess aggregates (including their
// rounds). All methods are tenant-scoped.
type ProcessRepository interface {
	// Save upserts the process and all its rounds in a single transaction,
	// then drains pending events into interview_outbox. Returns
	// ErrProcessDuplicate on (tenant_id, application_id) conflict for a
	// not-yet-existing row.
	Save(ctx context.Context, p *entities.InterviewProcess) error

	// FindByID returns the process + rounds for the given id, scoped to tenant.
	FindByID(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (*entities.InterviewProcess, error)

	// FindByApplicationID returns the process tied to the given application_id.
	FindByApplicationID(ctx context.Context, tenant shared.TenantID, applicationID uuid.UUID) (*entities.InterviewProcess, error)

	// ListByTenant returns processes for the given tenant filtered + paginated.
	ListByTenant(ctx context.Context, tenant shared.TenantID, filter ProcessListFilter) ([]*entities.InterviewProcess, error)

	// ClaimNextPendingRound returns the next round with status=Pending and
	// next_attempt_at <= now() across all tenants. Used by the worker pool.
	// Returns ErrProcessNotFound when nothing is claimable.
	//
	// Implementation note: slice 1 uses load-then-save, mirroring the slice-3
	// scoring workers. A later slice may harden with FOR UPDATE SKIP LOCKED.
	ClaimNextPendingRound(ctx context.Context) (*entities.InterviewProcess, uuid.UUID, error)
}
```

Create `internal/interview/domain/repositories/loop_template_repository.go`:

```go
package repositories

import (
	"context"
	"errors"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
)

// ErrLoopTemplateNotFound is returned when a template lookup finds no row.
var ErrLoopTemplateNotFound = errors.New("interview: loop template not found")

// LoopTemplateRepository persists LoopTemplate aggregates. Tenant-scoped.
type LoopTemplateRepository interface {
	// Save upserts on (tenant_id, intent_id).
	Save(ctx context.Context, t *entities.LoopTemplate) error

	// FindByIntent returns the template for (tenant, intent), or
	// ErrLoopTemplateNotFound when none exists.
	FindByIntent(ctx context.Context, tenant shared.TenantID, intentID uuid.UUID) (*entities.LoopTemplate, error)
}
```

Create `internal/interview/domain/repositories/feedback_repository.go`:

```go
package repositories

import (
	"context"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

// FeedbackRow is the persisted shape of an interview feedback entry. It's
// not an aggregate — feedback is append-only — so it's modeled as a plain
// row plus the FeedbackRepository contract below.
type FeedbackRow struct {
	ID       uuid.UUID
	TenantID shared.TenantID
	RoundID  uuid.UUID
	vo.Feedback
}

// FeedbackRepository appends and lists feedback rows. Tenant-scoped.
type FeedbackRepository interface {
	// Append inserts a single feedback row. Returns the assigned id.
	Append(ctx context.Context, row FeedbackRow) error

	// ListByRound returns all feedback for the given round, newest first.
	ListByRound(ctx context.Context, tenant shared.TenantID, roundID uuid.UUID) ([]FeedbackRow, error)
}
```

- [ ] **Step 2: Serializers**

Create `internal/interview/infrastructure/persistence/serializers.go`:

```go
package persistence

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

type processRow struct {
	id            uuid.UUID
	tenantID      string
	applicationID uuid.UUID
	candidateID   uuid.UUID
	intentID      uuid.UUID
	status        string
	createdAt     time.Time
	updatedAt     time.Time
}

type roundRow struct {
	id              uuid.UUID
	tenantID        string
	processID       uuid.UUID
	kind            string
	sequence        int
	status          string
	questionsJSON   []byte // nullable
	attemptCount    int
	lastError       string
	nextAttemptAt   time.Time
	createdAt       time.Time
	updatedAt       time.Time
}

// serializeProcess produces a processRow and the slice of roundRows for upsert.
func serializeProcess(p *entities.InterviewProcess) (processRow, []roundRow, error) {
	pr := processRow{
		id:            p.ID(),
		tenantID:      p.TenantID().String(),
		applicationID: p.ApplicationID(),
		candidateID:   p.CandidateID(),
		intentID:      p.IntentID(),
		status:        string(p.Status()),
		createdAt:     p.CreatedAt(),
		updatedAt:     p.UpdatedAt(),
	}
	rows := make([]roundRow, 0, len(p.Rounds()))
	for _, r := range p.Rounds() {
		var qbytes []byte
		if qs := r.Questions(); len(qs) > 0 {
			b, err := json.Marshal(qs)
			if err != nil {
				return processRow{}, nil, fmt.Errorf("marshal questions: %w", err)
			}
			qbytes = b
		}
		rows = append(rows, roundRow{
			id:            r.ID(),
			tenantID:      p.TenantID().String(),
			processID:     p.ID(),
			kind:          string(r.Kind()),
			sequence:      r.Sequence(),
			status:        string(r.Status()),
			questionsJSON: qbytes,
			attemptCount:  r.AttemptCount(),
			lastError:     r.LastError(),
			nextAttemptAt: r.NextAttemptAt(),
			createdAt:     r.CreatedAt(),
			updatedAt:     r.UpdatedAt(),
		})
	}
	return pr, rows, nil
}

// hydrateProcess builds an InterviewProcess from a processRow + its rounds.
func hydrateProcess(pr processRow, rrs []roundRow) (*entities.InterviewProcess, error) {
	if len(rrs) == 0 {
		return nil, errors.New("hydrate: process with no rounds")
	}
	tenant, err := shared.ParseTenantID(pr.tenantID)
	if err != nil {
		return nil, fmt.Errorf("tenant: %w", err)
	}
	status, err := vo.ParseProcessStatus(pr.status)
	if err != nil {
		return nil, err
	}
	rounds := make([]entities.RehydrateRoundInput, 0, len(rrs))
	for _, rr := range rrs {
		kind, err := vo.ParseRoundKind(rr.kind)
		if err != nil {
			return nil, err
		}
		rstatus, err := vo.ParseRoundStatus(rr.status)
		if err != nil {
			return nil, err
		}
		var qs []vo.Question
		if len(rr.questionsJSON) > 0 {
			if err := json.Unmarshal(rr.questionsJSON, &qs); err != nil {
				return nil, fmt.Errorf("unmarshal questions: %w", err)
			}
		}
		rounds = append(rounds, entities.RehydrateRoundInput{
			ID:            rr.id,
			Kind:          kind,
			Sequence:      rr.sequence,
			Status:        rstatus,
			Questions:     qs,
			AttemptCount:  rr.attemptCount,
			LastError:     rr.lastError,
			NextAttemptAt: rr.nextAttemptAt,
			CreatedAt:     rr.createdAt,
			UpdatedAt:     rr.updatedAt,
		})
	}
	return entities.RehydrateInterviewProcess(entities.RehydrateInterviewProcessInput{
		ID:            pr.id,
		TenantID:      tenant,
		ApplicationID: pr.applicationID,
		CandidateID:   pr.candidateID,
		IntentID:      pr.intentID,
		Status:        status,
		Rounds:        rounds,
		CreatedAt:     pr.createdAt,
		UpdatedAt:     pr.updatedAt,
	}), nil
}
```

- [ ] **Step 3: PostgresProcessRepository**

Create `internal/interview/infrastructure/persistence/postgres_process_repository.go`:

```go
// Package persistence holds Postgres-backed implementations of the interview
// repositories.
package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
)

// PostgresProcessRepository persists InterviewProcess aggregates.
type PostgresProcessRepository struct {
	pool *pgxpool.Pool
}

var _ repositories.ProcessRepository = (*PostgresProcessRepository)(nil)

// NewPostgresProcessRepository wires the repository.
func NewPostgresProcessRepository(pool *pgxpool.Pool) *PostgresProcessRepository {
	return &PostgresProcessRepository{pool: pool}
}

const processUpsertSQL = `
INSERT INTO interview_processes (
    id, tenant_id, application_id, candidate_id, intent_id,
    status, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (id) DO UPDATE SET
    status     = EXCLUDED.status,
    updated_at = EXCLUDED.updated_at`

const roundUpsertSQL = `
INSERT INTO interview_rounds (
    id, tenant_id, process_id, kind, sequence, status, questions,
    attempt_count, last_error, next_attempt_at, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (id) DO UPDATE SET
    status          = EXCLUDED.status,
    questions       = EXCLUDED.questions,
    attempt_count   = EXCLUDED.attempt_count,
    last_error      = EXCLUDED.last_error,
    next_attempt_at = EXCLUDED.next_attempt_at,
    updated_at      = EXCLUDED.updated_at`

const processSelectSQL = `
SELECT id, tenant_id, application_id, candidate_id, intent_id,
       status, created_at, updated_at
FROM interview_processes`

const roundSelectSQL = `
SELECT id, tenant_id, process_id, kind, sequence, status, questions,
       attempt_count, last_error, next_attempt_at, created_at, updated_at
FROM interview_rounds`

// Save upserts the process + rounds in a single transaction and drains
// pending events into interview_outbox. Returns ErrProcessDuplicate when
// (tenant_id, application_id) conflicts.
func (r *PostgresProcessRepository) Save(ctx context.Context, p *entities.InterviewProcess) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	pr, rrs, err := serializeProcess(p)
	if err != nil {
		return fmt.Errorf("serialize: %w", err)
	}

	if _, err := tx.Exec(ctx, processUpsertSQL,
		pr.id, pr.tenantID, pr.applicationID, pr.candidateID, pr.intentID,
		pr.status, pr.createdAt, pr.updatedAt,
	); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return repositories.ErrProcessDuplicate
		}
		return fmt.Errorf("upsert process: %w", err)
	}

	for _, rr := range rrs {
		if _, err := tx.Exec(ctx, roundUpsertSQL,
			rr.id, rr.tenantID, rr.processID, rr.kind, rr.sequence, rr.status,
			rr.questionsJSON, rr.attemptCount, rr.lastError, rr.nextAttemptAt,
			rr.createdAt, rr.updatedAt,
		); err != nil {
			return fmt.Errorf("upsert round: %w", err)
		}
	}

	// Drain events.
	for _, ev := range p.PullEvents() {
		payload, mErr := json.Marshal(ev)
		if mErr != nil {
			return fmt.Errorf("marshal event %s: %w", ev.EventName(), mErr)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO interview_outbox (event_name, aggregate_id, tenant_id, payload, occurred_at)
			VALUES ($1, $2, $3, $4, $5)
		`, ev.EventName(), ev.AggregateID(), ev.Tenant().String(), payload, ev.At()); err != nil {
			return fmt.Errorf("insert outbox: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// FindByID returns the process + rounds for the given id, scoped to tenant.
func (r *PostgresProcessRepository) FindByID(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (*entities.InterviewProcess, error) {
	pr, err := r.findProcessRow(ctx, processSelectSQL+` WHERE tenant_id=$1 AND id=$2`, tenant.String(), id)
	if err != nil {
		return nil, err
	}
	rrs, err := r.findRoundRows(ctx, pr.id)
	if err != nil {
		return nil, err
	}
	return hydrateProcess(pr, rrs)
}

// FindByApplicationID returns the process tied to the given application_id.
func (r *PostgresProcessRepository) FindByApplicationID(ctx context.Context, tenant shared.TenantID, applicationID uuid.UUID) (*entities.InterviewProcess, error) {
	pr, err := r.findProcessRow(ctx, processSelectSQL+` WHERE tenant_id=$1 AND application_id=$2`,
		tenant.String(), applicationID)
	if err != nil {
		return nil, err
	}
	rrs, err := r.findRoundRows(ctx, pr.id)
	if err != nil {
		return nil, err
	}
	return hydrateProcess(pr, rrs)
}

func (r *PostgresProcessRepository) findProcessRow(ctx context.Context, sql string, args ...any) (processRow, error) {
	row := r.pool.QueryRow(ctx, sql, args...)
	var pr processRow
	err := row.Scan(&pr.id, &pr.tenantID, &pr.applicationID, &pr.candidateID, &pr.intentID,
		&pr.status, &pr.createdAt, &pr.updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return processRow{}, repositories.ErrProcessNotFound
		}
		return processRow{}, fmt.Errorf("scan process: %w", err)
	}
	return pr, nil
}

func (r *PostgresProcessRepository) findRoundRows(ctx context.Context, processID uuid.UUID) ([]roundRow, error) {
	rows, err := r.pool.Query(ctx, roundSelectSQL+` WHERE process_id=$1 ORDER BY sequence ASC`, processID)
	if err != nil {
		return nil, fmt.Errorf("query rounds: %w", err)
	}
	defer rows.Close()
	var out []roundRow
	for rows.Next() {
		var rr roundRow
		if err := rows.Scan(
			&rr.id, &rr.tenantID, &rr.processID, &rr.kind, &rr.sequence, &rr.status,
			&rr.questionsJSON, &rr.attemptCount, &rr.lastError, &rr.nextAttemptAt,
			&rr.createdAt, &rr.updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan round: %w", err)
		}
		out = append(out, rr)
	}
	return out, rows.Err()
}

// ListByTenant returns processes filtered + paginated.
func (r *PostgresProcessRepository) ListByTenant(ctx context.Context, tenant shared.TenantID, filter repositories.ProcessListFilter) ([]*entities.InterviewProcess, error) {
	args := []any{tenant.String(), filter.IntentID}
	q := processSelectSQL + ` WHERE tenant_id=$1 AND intent_id=$2`
	idx := 3
	if filter.Status != "" {
		q += fmt.Sprintf(" AND status=$%d", idx)
		args = append(args, filter.Status)
		idx++
	}
	q += ` ORDER BY created_at DESC`
	if filter.Limit > 0 {
		q += fmt.Sprintf(" LIMIT $%d", idx)
		args = append(args, filter.Limit)
		idx++
	}
	if filter.Offset > 0 {
		q += fmt.Sprintf(" OFFSET $%d", idx)
		args = append(args, filter.Offset)
	}

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var prs []processRow
	for rows.Next() {
		var pr processRow
		if err := rows.Scan(&pr.id, &pr.tenantID, &pr.applicationID, &pr.candidateID, &pr.intentID,
			&pr.status, &pr.createdAt, &pr.updatedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		prs = append(prs, pr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]*entities.InterviewProcess, 0, len(prs))
	for _, pr := range prs {
		rrs, err := r.findRoundRows(ctx, pr.id)
		if err != nil {
			return nil, err
		}
		p, err := hydrateProcess(pr, rrs)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// ClaimNextPendingRound returns the next Pending round whose backoff has
// elapsed. Uses load-then-save without locking; idempotent worker behavior
// tolerates rare double-claim. Returns ErrProcessNotFound on empty queue.
func (r *PostgresProcessRepository) ClaimNextPendingRound(ctx context.Context) (*entities.InterviewProcess, uuid.UUID, error) {
	var roundID, processID uuid.UUID
	var tenantID string
	err := r.pool.QueryRow(ctx, `
		SELECT id, process_id, tenant_id FROM interview_rounds
		WHERE status='Pending' AND next_attempt_at <= now()
		ORDER BY next_attempt_at ASC
		LIMIT 1`).Scan(&roundID, &processID, &tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, uuid.Nil, repositories.ErrProcessNotFound
		}
		return nil, uuid.Nil, fmt.Errorf("claim: %w", err)
	}
	tenant, err := shared.ParseTenantID(tenantID)
	if err != nil {
		return nil, uuid.Nil, err
	}
	p, err := r.FindByID(ctx, tenant, processID)
	if err != nil {
		return nil, uuid.Nil, err
	}
	return p, roundID, nil
}
```

- [ ] **Step 4: PostgresLoopTemplateRepository**

Create `internal/interview/infrastructure/persistence/postgres_loop_template_repository.go`:

```go
package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

type PostgresLoopTemplateRepository struct {
	pool *pgxpool.Pool
}

var _ repositories.LoopTemplateRepository = (*PostgresLoopTemplateRepository)(nil)

func NewPostgresLoopTemplateRepository(pool *pgxpool.Pool) *PostgresLoopTemplateRepository {
	return &PostgresLoopTemplateRepository{pool: pool}
}

type roundJSON struct {
	Kind     string `json:"kind"`
	Sequence int    `json:"sequence"`
}

const templateUpsertSQL = `
INSERT INTO intent_loops (id, tenant_id, intent_id, rounds, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (tenant_id, intent_id) DO UPDATE SET
    rounds     = EXCLUDED.rounds,
    updated_at = EXCLUDED.updated_at`

const templateSelectSQL = `
SELECT id, tenant_id, intent_id, rounds, created_at, updated_at
FROM intent_loops
WHERE tenant_id=$1 AND intent_id=$2`

func (r *PostgresLoopTemplateRepository) Save(ctx context.Context, t *entities.LoopTemplate) error {
	rounds := t.Rounds()
	jrounds := make([]roundJSON, 0, len(rounds))
	for _, rd := range rounds {
		jrounds = append(jrounds, roundJSON{Kind: string(rd.Kind), Sequence: rd.Sequence})
	}
	payload, err := json.Marshal(jrounds)
	if err != nil {
		return fmt.Errorf("marshal rounds: %w", err)
	}
	if _, err := r.pool.Exec(ctx, templateUpsertSQL,
		t.ID(), t.TenantID().String(), t.IntentID(), payload, t.CreatedAt(), t.UpdatedAt(),
	); err != nil {
		return fmt.Errorf("upsert template: %w", err)
	}
	return nil
}

func (r *PostgresLoopTemplateRepository) FindByIntent(ctx context.Context, tenant shared.TenantID, intentID uuid.UUID) (*entities.LoopTemplate, error) {
	var (
		id        uuid.UUID
		tenantStr string
		intent    uuid.UUID
		payload   []byte
		createdAt = "" // overwritten by Scan
	)
	_ = createdAt
	row := r.pool.QueryRow(ctx, templateSelectSQL, tenant.String(), intentID)
	var (
		createdT, updatedT = newTimeHolder(), newTimeHolder()
	)
	if err := row.Scan(&id, &tenantStr, &intent, &payload, createdT, updatedT); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repositories.ErrLoopTemplateNotFound
		}
		return nil, fmt.Errorf("scan template: %w", err)
	}
	var jrounds []roundJSON
	if err := json.Unmarshal(payload, &jrounds); err != nil {
		return nil, fmt.Errorf("unmarshal rounds: %w", err)
	}
	parsedTenant, err := shared.ParseTenantID(tenantStr)
	if err != nil {
		return nil, err
	}
	rounds := make([]entities.TemplateRound, 0, len(jrounds))
	for _, jr := range jrounds {
		kind, err := vo.ParseRoundKind(jr.Kind)
		if err != nil {
			return nil, err
		}
		rounds = append(rounds, entities.TemplateRound{Kind: kind, Sequence: jr.Sequence})
	}
	return entities.RehydrateLoopTemplate(entities.RehydrateLoopTemplateInput{
		ID:        id,
		TenantID:  parsedTenant,
		IntentID:  intent,
		Rounds:    rounds,
		CreatedAt: createdT.t,
		UpdatedAt: updatedT.t,
	}), nil
}

// timeHolder lets us Scan into *time.Time fields without dealing with pgx's
// scan-by-value vs scan-by-pointer subtlety. Holds the scanned value in t.
type timeHolder struct {
	t timeT
}

type timeT = entities.TemplateRound // placeholder — replaced below

func newTimeHolder() *timeHolder { return &timeHolder{} }
```

**Note:** the `timeHolder` shim above is a placeholder showing the structure but won't compile as-is. Replace the `roundJSON`-based scanning above with the simpler direct pattern below, which matches how slice 1-3 scan time columns:

Replace `FindByIntent` body with the simpler form:

```go
func (r *PostgresLoopTemplateRepository) FindByIntent(ctx context.Context, tenant shared.TenantID, intentID uuid.UUID) (*entities.LoopTemplate, error) {
	var (
		id                   uuid.UUID
		tenantStr            string
		intent               uuid.UUID
		payload              []byte
		createdAt, updatedAt time.Time
	)
	row := r.pool.QueryRow(ctx, templateSelectSQL, tenant.String(), intentID)
	if err := row.Scan(&id, &tenantStr, &intent, &payload, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repositories.ErrLoopTemplateNotFound
		}
		return nil, fmt.Errorf("scan template: %w", err)
	}
	var jrounds []roundJSON
	if err := json.Unmarshal(payload, &jrounds); err != nil {
		return nil, fmt.Errorf("unmarshal rounds: %w", err)
	}
	parsedTenant, err := shared.ParseTenantID(tenantStr)
	if err != nil {
		return nil, err
	}
	rounds := make([]entities.TemplateRound, 0, len(jrounds))
	for _, jr := range jrounds {
		kind, err := vo.ParseRoundKind(jr.Kind)
		if err != nil {
			return nil, err
		}
		rounds = append(rounds, entities.TemplateRound{Kind: kind, Sequence: jr.Sequence})
	}
	return entities.RehydrateLoopTemplate(entities.RehydrateLoopTemplateInput{
		ID:        id,
		TenantID:  parsedTenant,
		IntentID:  intent,
		Rounds:    rounds,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}), nil
}
```

Add `import "time"` at the top of the file. Remove the `timeHolder` / `newTimeHolder` / `timeT` stubs from the file.

- [ ] **Step 5: PostgresFeedbackRepository**

Create `internal/interview/infrastructure/persistence/postgres_feedback_repository.go`:

```go
package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

type PostgresFeedbackRepository struct {
	pool *pgxpool.Pool
}

var _ repositories.FeedbackRepository = (*PostgresFeedbackRepository)(nil)

func NewPostgresFeedbackRepository(pool *pgxpool.Pool) *PostgresFeedbackRepository {
	return &PostgresFeedbackRepository{pool: pool}
}

const feedbackInsertSQL = `
INSERT INTO interview_feedback (
    id, tenant_id, round_id, interviewer_name, interviewer_email,
    decision, notes, submitted_by, submitted_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

const feedbackSelectSQL = `
SELECT id, tenant_id, round_id, interviewer_name, interviewer_email,
       decision, notes, submitted_by, submitted_at
FROM interview_feedback`

// Append inserts a single feedback row. Validates the embedded Feedback first.
func (r *PostgresFeedbackRepository) Append(ctx context.Context, row repositories.FeedbackRow) error {
	if err := row.Feedback.Validate(); err != nil {
		return err
	}
	if row.ID == uuid.Nil {
		return errors.New("feedback: id required")
	}
	if row.RoundID == uuid.Nil {
		return errors.New("feedback: round_id required")
	}
	if _, err := r.pool.Exec(ctx, feedbackInsertSQL,
		row.ID, row.TenantID.String(), row.RoundID,
		row.Feedback.InterviewerName, row.Feedback.InterviewerEmail,
		string(row.Feedback.Decision), row.Feedback.Notes,
		row.Feedback.SubmittedBy, row.Feedback.SubmittedAt,
	); err != nil {
		return fmt.Errorf("insert feedback: %w", err)
	}
	return nil
}

// ListByRound returns all feedback rows for the given round, newest first.
func (r *PostgresFeedbackRepository) ListByRound(ctx context.Context, tenant shared.TenantID, roundID uuid.UUID) ([]repositories.FeedbackRow, error) {
	rows, err := r.pool.Query(ctx, feedbackSelectSQL+` WHERE tenant_id=$1 AND round_id=$2 ORDER BY submitted_at DESC`,
		tenant.String(), roundID)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var out []repositories.FeedbackRow
	for rows.Next() {
		var (
			id          uuid.UUID
			tenantStr   string
			rID         uuid.UUID
			name        string
			email       string
			decisionStr string
			notes       string
			submittedBy uuid.UUID
			submittedAt time.Time
		)
		if err := rows.Scan(&id, &tenantStr, &rID, &name, &email, &decisionStr, &notes, &submittedBy, &submittedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		t, err := shared.ParseTenantID(tenantStr)
		if err != nil {
			return nil, err
		}
		dec, err := vo.ParseFeedbackDecision(decisionStr)
		if err != nil {
			return nil, err
		}
		out = append(out, repositories.FeedbackRow{
			ID:       id,
			TenantID: t,
			RoundID:  rID,
			Feedback: vo.Feedback{
				InterviewerName:  name,
				InterviewerEmail: email,
				Decision:         dec,
				Notes:            notes,
				SubmittedBy:      submittedBy,
				SubmittedAt:      submittedAt,
			},
		})
	}
	return out, rows.Err()
}
```

- [ ] **Step 6: Integration tests**

Create `internal/interview/infrastructure/persistence/postgres_process_repository_test.go` with `//go:build integration` and `package persistence_test`. Add a `newPool(t *testing.T) *pgxpool.Pool` helper that skips on missing `DATABASE_URL` and TRUNCATES `interview_processes, interview_rounds, interview_feedback, intent_loops, interview_outbox`. Tests:

- `TestProcessSave_PersistsRowsAndOutbox` — construct via `NewInterviewProcess` with 3 rounds, Save, then SELECT counts on `interview_processes` (=1), `interview_rounds` (=3), `interview_outbox` (=1 row with event_name='interview.InterviewProcessCreated').
- `TestProcessFindByID_RehydratesRounds` — Save then FindByID, assert all fields including rounds + their statuses.
- `TestProcessSave_DuplicateApplicationID_ReturnsErrProcessDuplicate` — Save two distinct processes with the same `(tenant_id, application_id)`; second Save returns ErrProcessDuplicate.
- `TestProcessFindByApplicationID_TenantScoped` — Save under tenantA; FindByApplicationID with tenantB returns ErrProcessNotFound.
- `TestProcessListByTenant_FiltersAndPaginates` — Save 3 processes with different statuses under the same intent; ListByTenant with filter.Status="New" returns exactly those.
- `TestProcessClaimNextPendingRound_ReturnsOldestPending` — Save two processes with different next_attempt_at values; ClaimNextPendingRound returns the round with the earliest backoff.
- `TestProcessClaimNextPendingRound_NoneClaimable_ReturnsNotFound` — empty table → ErrProcessNotFound.
- `TestProcessSave_RoundQuestionsRoundTrip` — Save a process; MarkRoundQuestionsReady with 2 valid questions; Save again; FindByID → questions decode correctly.

Create `internal/interview/infrastructure/persistence/postgres_loop_template_repository_test.go`:

- `TestLoopTemplateSave_PersistsRow` — Save a template with 3 rounds, FindByIntent returns it.
- `TestLoopTemplateSave_UpsertOnConflict` — Save with rounds=[A,B], Save again with rounds=[A,B,C], FindByIntent returns the new shape.
- `TestLoopTemplateFindByIntent_NotFound` — FindByIntent on a missing intent returns ErrLoopTemplateNotFound.
- `TestLoopTemplateFindByIntent_TenantScoped` — Save under tenantA, FindByIntent with tenantB returns ErrLoopTemplateNotFound.

Create `internal/interview/infrastructure/persistence/postgres_feedback_repository_test.go`:

- `TestFeedbackAppend_PersistsRow` — Append a valid Feedback, ListByRound returns it.
- `TestFeedbackAppend_InvalidDecision_RejectedAtDB` — bypass the in-Go enum (build a FeedbackRow with `Decision: "garbage"`) and assert Postgres rejects via CHECK constraint (the error contains "audit_log_action_nonempty"-style constraint failure — adjust to the actual constraint name `interview_feedback_decision_valid`).
- `TestFeedbackListByRound_OrdersNewestFirst` — Append 3 rows with increasing submitted_at; ListByRound returns them in reverse order.
- `TestFeedbackListByRound_TenantScoped` — Append under tenantA; ListByRound with tenantB returns empty.

- [ ] **Step 7: Verify + commit**

```
go test ./internal/interview/domain/... ./internal/interview/infrastructure/persistence/... -count=1 -race
export DATABASE_URL="postgres://hireflow:hireflow@localhost:5433/hireflow?sslmode=disable"
go test -tags=integration ./internal/interview/infrastructure/persistence/... -count=1 -race
make build
git add internal/interview/domain/repositories/ \
        internal/interview/infrastructure/persistence/
git commit -m "feat(interview): repository ports + Postgres adapters (process, template, feedback)"
```

---

## Task 7: Cross-context reader ports + adapters

**Files:**
- Create: `internal/interview/domain/services/intent_reader.go`
- Create: `internal/interview/domain/services/candidate_reader.go`
- Create: `internal/interview/infrastructure/clients/postgres_intent_reader.go` + `_test.go`
- Create: `internal/interview/infrastructure/clients/postgres_candidate_reader.go` + `_test.go`

- [ ] **Step 1: Reader ports + DTOs**

Create `internal/interview/domain/services/intent_reader.go`:

```go
// Package services holds the port interfaces consumed by interview-context
// commands (cross-context readers, question generator).
package services

import (
	"context"
	"errors"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ErrIntentNotFound is returned by IntentReader when the intent doesn't exist
// for the tenant. Cross-context lookup failure surfaces this back to the
// generation worker.
var ErrIntentNotFound = errors.New("interview: intent not found")

// RoleSpec is the interview-context-local DTO for the role definition that
// drives question generation. Field set is intentionally narrow — only what
// the generator needs.
type RoleSpec struct {
	Title       string
	Skills      []SkillRequirement
	YearsMin    int     // 0 if not specified
	YearsMax    int     // 0 if not specified
	Seniority   string  // e.g., "senior", "staff"; empty if not specified
	Reports     string  // "reports_to" free text from intents; empty if absent
	Team        string  // free text; empty if absent
}

// SkillRequirement is one role-spec skill entry.
type SkillRequirement struct {
	Name     string
	Required bool
}

// IntentReader returns the interview-context-shaped RoleSpec for an intent.
// Cross-context: reads the hiringintent context's table directly via the
// shared pool; does not import sourcing or hiringintent Go packages.
type IntentReader interface {
	GetRoleSpec(ctx context.Context, tenant shared.TenantID, intentID uuid.UUID) (RoleSpec, error)
}
```

Create `internal/interview/domain/services/candidate_reader.go`:

```go
package services

import (
	"context"
	"errors"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ErrCandidateNotFound is returned when the candidate doesn't exist for the
// tenant. Used by the generation worker to abort the round when a candidate
// has been GDPR-erased mid-process.
var ErrCandidateNotFound = errors.New("interview: candidate not found")

// CandidateProfile is the interview-context-local DTO for the candidate
// information used to tailor questions. Excludes encrypted PII columns
// (name/email/phone) — questions don't need them, and we don't want to
// audit-write a PII read for every generation.
type CandidateProfile struct {
	ID             uuid.UUID
	Headline       string
	Location       string
	Skills         []string
	Experiences    []Experience
	Education      []EducationEntry
	Certifications []string
	SchemaVersion  int
}

// Experience is one work-experience entry on a candidate profile.
type Experience struct {
	Title    string
	Company  string
	Duration string // free text from parsed profile, e.g., "2020-2025" or "3y 6m"
	Summary  string // optional bullet-style summary
}

// EducationEntry is one education entry on a candidate profile.
type EducationEntry struct {
	Degree      string
	Field       string
	Institution string
	Year        string
}

// CandidateReader returns the interview-context-shaped CandidateProfile.
type CandidateReader interface {
	GetProfileForQuestions(ctx context.Context, tenant shared.TenantID, candidateID uuid.UUID) (CandidateProfile, error)
}
```

- [ ] **Step 2: PostgresIntentReader adapter**

Create `internal/interview/infrastructure/clients/postgres_intent_reader.go`:

```go
// Package clients holds interview-context Postgres adapters for cross-context
// reads. These read tables owned by other contexts (hiringintent.hiring_intents,
// sourcing.candidates) via the shared pool — there is no Go-level import
// from the interview package into sourcing or hiringintent.
package clients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/domain/services"
)

// PostgresIntentReader reads RoleSpec from the hiring_intents.role JSONB.
type PostgresIntentReader struct {
	pool *pgxpool.Pool
}

var _ services.IntentReader = (*PostgresIntentReader)(nil)

func NewPostgresIntentReader(pool *pgxpool.Pool) *PostgresIntentReader {
	return &PostgresIntentReader{pool: pool}
}

// roleJSON mirrors the shape stored in hiring_intents.role. The hiringintent
// context owns the canonical struct; this is a duplicated narrow shape that
// pulls only the fields the question generator needs.
type roleJSON struct {
	Title     string         `json:"title"`
	Skills    []skillJSON    `json:"skills"`
	YearsMin  int            `json:"years_min"`
	YearsMax  int            `json:"years_max"`
	Seniority string         `json:"seniority"`
}

type skillJSON struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
}

func (r *PostgresIntentReader) GetRoleSpec(ctx context.Context, tenant shared.TenantID, intentID uuid.UUID) (services.RoleSpec, error) {
	var (
		payload []byte
		reports string
		team    string
	)
	err := r.pool.QueryRow(ctx, `
		SELECT role, reports_to, team
		FROM hiring_intents
		WHERE tenant_id=$1 AND id=$2`,
		tenant.String(), intentID,
	).Scan(&payload, &reports, &team)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return services.RoleSpec{}, services.ErrIntentNotFound
		}
		return services.RoleSpec{}, fmt.Errorf("scan intent: %w", err)
	}
	var role roleJSON
	if err := json.Unmarshal(payload, &role); err != nil {
		return services.RoleSpec{}, fmt.Errorf("unmarshal role: %w", err)
	}
	skills := make([]services.SkillRequirement, 0, len(role.Skills))
	for _, s := range role.Skills {
		skills = append(skills, services.SkillRequirement{Name: s.Name, Required: s.Required})
	}
	return services.RoleSpec{
		Title:     role.Title,
		Skills:    skills,
		YearsMin:  role.YearsMin,
		YearsMax:  role.YearsMax,
		Seniority: role.Seniority,
		Reports:   reports,
		Team:      team,
	}, nil
}
```

- [ ] **Step 3: PostgresCandidateReader adapter**

Create `internal/interview/infrastructure/clients/postgres_candidate_reader.go`:

```go
package clients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/domain/services"
)

type PostgresCandidateReader struct {
	pool *pgxpool.Pool
}

var _ services.CandidateReader = (*PostgresCandidateReader)(nil)

func NewPostgresCandidateReader(pool *pgxpool.Pool) *PostgresCandidateReader {
	return &PostgresCandidateReader{pool: pool}
}

// profileJSON mirrors the parsed_profile shape from sourcing. Only the
// fields the question generator uses are extracted; PII fields (full_name,
// email, phone) are deliberately skipped — they live in encrypted columns
// the interview context does not have key access to.
type profileJSON struct {
	Skills         []string             `json:"skills"`
	Experiences    []experienceJSON     `json:"experiences"`
	Education      []educationJSON      `json:"education"`
	Certifications []string             `json:"certifications"`
}

type experienceJSON struct {
	Title    string `json:"title"`
	Company  string `json:"company"`
	Duration string `json:"duration"`
	Summary  string `json:"summary"`
}

type educationJSON struct {
	Degree      string `json:"degree"`
	Field       string `json:"field"`
	Institution string `json:"institution"`
	Year        string `json:"year"`
}

func (r *PostgresCandidateReader) GetProfileForQuestions(ctx context.Context, tenant shared.TenantID, candidateID uuid.UUID) (services.CandidateProfile, error) {
	var (
		headline string
		location string
		schema   int
		payload  []byte
	)
	err := r.pool.QueryRow(ctx, `
		SELECT headline, location, profile_schema, parsed_profile
		FROM candidates
		WHERE tenant_id=$1 AND id=$2`,
		tenant.String(), candidateID,
	).Scan(&headline, &location, &schema, &payload)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return services.CandidateProfile{}, services.ErrCandidateNotFound
		}
		return services.CandidateProfile{}, fmt.Errorf("scan candidate: %w", err)
	}
	var prof profileJSON
	if err := json.Unmarshal(payload, &prof); err != nil {
		return services.CandidateProfile{}, fmt.Errorf("unmarshal profile: %w", err)
	}
	exps := make([]services.Experience, 0, len(prof.Experiences))
	for _, e := range prof.Experiences {
		exps = append(exps, services.Experience{
			Title: e.Title, Company: e.Company, Duration: e.Duration, Summary: e.Summary,
		})
	}
	edus := make([]services.EducationEntry, 0, len(prof.Education))
	for _, e := range prof.Education {
		edus = append(edus, services.EducationEntry{
			Degree: e.Degree, Field: e.Field, Institution: e.Institution, Year: e.Year,
		})
	}
	return services.CandidateProfile{
		ID:             candidateID,
		Headline:       headline,
		Location:       location,
		Skills:         append([]string(nil), prof.Skills...),
		Experiences:    exps,
		Education:      edus,
		Certifications: append([]string(nil), prof.Certifications...),
		SchemaVersion:  schema,
	}, nil
}
```

- [ ] **Step 4: Integration tests**

Create `internal/interview/infrastructure/clients/postgres_intent_reader_test.go` with `//go:build integration` and a `newPool` helper. Tests:

- `TestIntentReader_ReadsRoleSpec` — INSERT a row into `hiring_intents` with a known role JSON (title="Senior Backend", skills=[{Go,required:true},{Kafka,required:false}], seniority="senior", reports_to="VP Eng", team="Payments", priority="medium", status="Confirmed"). Call GetRoleSpec, assert all fields round-trip.
- `TestIntentReader_TenantScoped` — INSERT under tenantA; GetRoleSpec with tenantB returns ErrIntentNotFound.
- `TestIntentReader_NotFound` — missing intent → ErrIntentNotFound.

Use raw SQL to seed `hiring_intents` (don't import the hiringintent context). Example seed:

```go
_, err := pool.Exec(ctx, `
    INSERT INTO hiring_intents (id, tenant_id, recruiter_id, role, priority, status, created_at, updated_at, reports_to, team)
    VALUES ($1, $2, $3, $4::jsonb, 'medium', 'Confirmed', now(), now(), $5, $6)
`, intentID, tenant.String(), uuid.New(), roleJSONStr, "VP Eng", "Payments")
require.NoError(t, err)
```

Create `internal/interview/infrastructure/clients/postgres_candidate_reader_test.go`:

- `TestCandidateReader_ReadsProfile` — INSERT a row into `candidates` with a known parsed_profile JSON. Call GetProfileForQuestions, assert skills/experiences/education round-trip correctly. Verify PII columns are NOT read (the SELECT doesn't include them).
- `TestCandidateReader_TenantScoped` — same pattern.
- `TestCandidateReader_NotFound` — ErrCandidateNotFound.

- [ ] **Step 5: Verify + commit**

```
go test ./internal/interview/domain/services/... -count=1 -race
export DATABASE_URL="postgres://hireflow:hireflow@localhost:5433/hireflow?sslmode=disable"
go test -tags=integration ./internal/interview/infrastructure/clients/... -count=1 -race
make build
git add internal/interview/domain/services/ internal/interview/infrastructure/clients/
git commit -m "feat(interview): cross-context IntentReader + CandidateReader ports + Postgres adapters"
```

---

## Task 8: QuestionGenerator port + AnthropicQuestionGenerator adapter

**Files:**
- Create: `internal/interview/domain/services/question_generator.go`
- Create: `internal/interview/infrastructure/generation/prompts.go` + `_test.go`
- Create: `internal/interview/infrastructure/generation/anthropic_generator.go` + `_test.go`

- [ ] **Step 1: Port**

Create `internal/interview/domain/services/question_generator.go`:

```go
package services

import (
	"context"

	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

// GenerationInput is the input to a question-generation call.
type GenerationInput struct {
	RoundKind        vo.RoundKind
	RoleSpec         RoleSpec
	CandidateProfile CandidateProfile
	// Steering is optional recruiter-provided text appended to the prompt
	// (used by RegenerateRoundQuestions; empty on initial generation).
	Steering string
}

// QuestionGenerator produces a list of structured probe questions for the
// given (round, role, candidate). Concrete adapter is the Anthropic-backed
// implementation in infrastructure/generation/.
type QuestionGenerator interface {
	Generate(ctx context.Context, in GenerationInput) ([]vo.Question, error)
}
```

- [ ] **Step 2: Prompt builder**

Create `internal/interview/infrastructure/generation/prompts.go`:

```go
// Package generation holds the AnthropicQuestionGenerator + per-round prompt
// templates.
package generation

import (
	"fmt"
	"strings"

	"github.com/hustle/hireflow/internal/interview/domain/services"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

// QuestionCounts is the per-round-kind target question count. Tunable via
// env in main.go; this map is the default if no override is configured.
var QuestionCounts = map[vo.RoundKind]int{
	vo.RoundKindScreen:       4,
	vo.RoundKindTechnical:    6,
	vo.RoundKindSystemDesign: 6,
	vo.RoundKindBehavioral:   4,
	vo.RoundKindBarRaiser:    4,
}

// roundKindBriefs describe what each round should probe. Used inside the
// prompt to anchor the LLM on the round's purpose.
var roundKindBriefs = map[vo.RoundKind]string{
	vo.RoundKindScreen: `An initial screen call between the recruiter / hiring manager and the candidate.
Focus: role-fit, motivation, level alignment, deal-breaker questions on location / comp / start-date.
Avoid deep technical drilling — that belongs in later rounds.`,
	vo.RoundKindTechnical: `A hands-on technical round probing the candidate's craft in the role's primary skills.
Focus: depth on the skills listed as required in the role spec. Coding-by-discussion is fine; design at
implementation level. Each question should connect to a specific skill the candidate claims experience in.`,
	vo.RoundKindSystemDesign: `An architecture / scaling round.
Focus: how the candidate decomposes a non-trivial system, makes trade-offs (consistency vs availability,
latency vs durability), reasons about failure modes, scale, and operational concerns. Tailor to the
candidate's prior systems experience.`,
	vo.RoundKindBehavioral: `A STAR-style past-experience round.
Focus: how the candidate handled past situations — conflict, ambiguity, ownership, failure, mentoring.
Each question should target a specific competency the role spec implies (e.g., leadership for a staff role).`,
	vo.RoundKindBarRaiser: `A broader judgment / leadership / culture round.
Focus: the candidate's principles, decisions under uncertainty, hiring bar, cross-functional collaboration,
how they raise the level of teams they join. This is the "would we hire them again" lens.`,
}

// BuildPrompt produces the system+user prompt pair for an Anthropic
// generation call. Returns:
//   - system: the role-anchoring system message
//   - user:   the user message asking for the structured JSON output
func BuildPrompt(in services.GenerationInput) (system string, user string) {
	count := QuestionCounts[in.RoundKind]
	if count == 0 {
		count = 4 // safe default for any unmapped kind
	}
	brief := roundKindBriefs[in.RoundKind]

	roleBrief := formatRoleSpec(in.RoleSpec)
	candidateBrief := formatCandidateProfile(in.CandidateProfile)

	system = `You are designing an interview round for a specific role and candidate.
You will return a JSON array of question objects. Each question must be tailored to BOTH the role spec
and the candidate's actual experience — generic questions are not acceptable.

The interviewer running this round may not be a deep expert in every domain the candidate claims.
Your model_answer paragraph MUST be concrete and specific enough that a domain-generalist interviewer can
use it as a real-time reference to evaluate the candidate's response.`

	steering := ""
	if strings.TrimSpace(in.Steering) != "" {
		steering = "\n\nAdditional recruiter steering for this regeneration:\n" + in.Steering
	}

	user = fmt.Sprintf(`Round type: %s

%s

Role spec:
%s

Candidate profile:
%s
%s

Produce exactly %d question objects as a JSON array. Each object must have these fields, all required:

- prompt: string — the question the interviewer asks.
- skill_probed: string — which skill from the role spec this targets.
- why: string — one sentence tying this question to something in the candidate's profile.
- expected_signals: string[] (>= 3) — what a strong answer demonstrates.
- model_answer: string (one paragraph) — a concrete sketch of what a strong answer looks like, written
  so a domain-generalist interviewer can compare it to the candidate's response in real time.
- red_flags: string[] (>= 2) — specific weak-answer patterns to watch for.
- follow_ups: string[] (>= 1) — deeper probes if the candidate's first pass is shallow.

Return ONLY the JSON array. No prose, no commentary.`,
		in.RoundKind, brief, roleBrief, candidateBrief, steering, count)

	return system, user
}

func formatRoleSpec(s services.RoleSpec) string {
	var sb strings.Builder
	if s.Title != "" {
		sb.WriteString("- Title: " + s.Title + "\n")
	}
	if s.Seniority != "" {
		sb.WriteString("- Seniority: " + s.Seniority + "\n")
	}
	if s.YearsMin > 0 || s.YearsMax > 0 {
		sb.WriteString(fmt.Sprintf("- Years experience: %d-%d\n", s.YearsMin, s.YearsMax))
	}
	if s.Team != "" {
		sb.WriteString("- Team: " + s.Team + "\n")
	}
	if s.Reports != "" {
		sb.WriteString("- Reports to: " + s.Reports + "\n")
	}
	if len(s.Skills) > 0 {
		sb.WriteString("- Skills:\n")
		for _, sk := range s.Skills {
			marker := ""
			if sk.Required {
				marker = " (required)"
			}
			sb.WriteString("  - " + sk.Name + marker + "\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func formatCandidateProfile(p services.CandidateProfile) string {
	var sb strings.Builder
	if p.Headline != "" {
		sb.WriteString("- Headline: " + p.Headline + "\n")
	}
	if p.Location != "" {
		sb.WriteString("- Location: " + p.Location + "\n")
	}
	if len(p.Skills) > 0 {
		sb.WriteString("- Skills: " + strings.Join(p.Skills, ", ") + "\n")
	}
	if len(p.Experiences) > 0 {
		sb.WriteString("- Experience:\n")
		for _, e := range p.Experiences {
			line := "  - " + e.Title
			if e.Company != "" {
				line += " at " + e.Company
			}
			if e.Duration != "" {
				line += " (" + e.Duration + ")"
			}
			sb.WriteString(line + "\n")
			if e.Summary != "" {
				sb.WriteString("    " + e.Summary + "\n")
			}
		}
	}
	if len(p.Education) > 0 {
		sb.WriteString("- Education:\n")
		for _, e := range p.Education {
			sb.WriteString("  - " + e.Degree + " in " + e.Field + ", " + e.Institution + " (" + e.Year + ")\n")
		}
	}
	if len(p.Certifications) > 0 {
		sb.WriteString("- Certifications: " + strings.Join(p.Certifications, ", ") + "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}
```

Create `prompts_test.go` covering:

- `TestBuildPrompt_ContainsRoundKind` — for each of the 5 kinds, BuildPrompt returns a user string containing the kind name.
- `TestBuildPrompt_IncludesRoleBrief` — role spec fields appear in the user prompt.
- `TestBuildPrompt_IncludesCandidateExperience` — candidate experiences appear.
- `TestBuildPrompt_AppendsSteering` — when Steering is set, the user prompt contains it.
- `TestBuildPrompt_DefaultQuestionCount` — for an unmapped (hypothetical) round kind, count defaults to 4.

These are simple string-contains assertions; they snapshot the prompt's *information content* without locking the wording.

- [ ] **Step 3: Anthropic adapter**

Create `internal/interview/infrastructure/generation/anthropic_generator.go`:

```go
package generation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/hustle/hireflow/internal/interview/domain/services"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

// AnthropicSDK is the subset of the Anthropic SDK this adapter needs. Defined
// here so tests can inject a fake.
type AnthropicSDK interface {
	Messages() interface {
		New(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error)
	}
}

// AnthropicQuestionGenerator generates interview questions via Anthropic.
type AnthropicQuestionGenerator struct {
	sdk   AnthropicSDK
	model string
}

var _ services.QuestionGenerator = (*AnthropicQuestionGenerator)(nil)

// NewAnthropicQuestionGenerator constructs the adapter.
func NewAnthropicQuestionGenerator(sdk AnthropicSDK, model string) *AnthropicQuestionGenerator {
	return &AnthropicQuestionGenerator{sdk: sdk, model: model}
}

// ErrInvalidLLMOutput is returned when the LLM response cannot be parsed
// into the Question schema. The worker handles this via the RetryDecision
// with FailureKindInvalidJSON.
var ErrInvalidLLMOutput = errors.New("interview: invalid LLM output")

// ErrLLMAuthFailed is returned on Anthropic 401/403. The worker maps this to
// FailureKindLLMAuth and aborts the round.
var ErrLLMAuthFailed = errors.New("interview: LLM auth failed")

// Generate calls Anthropic and returns the parsed Question slice.
func (g *AnthropicQuestionGenerator) Generate(ctx context.Context, in services.GenerationInput) ([]vo.Question, error) {
	system, user := BuildPrompt(in)
	resp, err := g.sdk.Messages().New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(g.model),
		MaxTokens: 4000,
		System: []anthropic.TextBlockParam{{Text: system}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
	})
	if err != nil {
		// Classify the error. Anthropic SDK wraps HTTP errors in *anthropic.Error.
		var aErr *anthropic.Error
		if errors.As(err, &aErr) {
			switch aErr.StatusCode {
			case http.StatusUnauthorized, http.StatusForbidden:
				return nil, fmt.Errorf("%w: %v", ErrLLMAuthFailed, err)
			}
		}
		return nil, fmt.Errorf("anthropic call: %w", err)
	}

	// Extract text block content.
	var text string
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			text += tb.Text
		}
	}
	if text == "" {
		return nil, fmt.Errorf("%w: empty response", ErrInvalidLLMOutput)
	}

	// Trim any markdown fences around the JSON.
	text = trimJSONFences(text)

	var questions []vo.Question
	if err := json.Unmarshal([]byte(text), &questions); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidLLMOutput, err)
	}
	if len(questions) == 0 {
		return nil, fmt.Errorf("%w: empty array", ErrInvalidLLMOutput)
	}
	for i, q := range questions {
		if err := q.Validate(); err != nil {
			return nil, fmt.Errorf("%w: question %d: %v", ErrInvalidLLMOutput, i, err)
		}
	}
	return questions, nil
}

// trimJSONFences strips ```json ... ``` or ``` ... ``` wrappers if present.
func trimJSONFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// Strip opening fence (with optional language hint).
		if newline := strings.Index(s, "\n"); newline >= 0 {
			s = s[newline+1:]
		}
		if strings.HasSuffix(s, "```") {
			s = s[:len(s)-3]
		}
	}
	return strings.TrimSpace(s)
}
```

Add the missing `strings` import at the top of the file.

- [ ] **Step 4: Adapter tests**

Create `anthropic_generator_test.go`. Use a fake SDK that satisfies the `AnthropicSDK` interface and returns canned responses:

```go
type fakeMessages struct{ resp *anthropic.Message; err error }
func (f *fakeMessages) New(_ context.Context, _ anthropic.MessageNewParams) (*anthropic.Message, error) {
    return f.resp, f.err
}
type fakeSDK struct{ m *fakeMessages }
func (s fakeSDK) Messages() interface { New(context.Context, anthropic.MessageNewParams) (*anthropic.Message, error) } {
    return s.m
}
```

Tests:

- `TestAnthropicGenerator_HappyPath_ParsesQuestions` — fake returns a TextBlock containing a valid 2-question JSON array; Generate returns 2 questions matching the inputs.
- `TestAnthropicGenerator_StripsMarkdownFences` — same as above but the response is wrapped in ` ```json ... ``` `; still parses.
- `TestAnthropicGenerator_EmptyResponse_ReturnsErrInvalidLLMOutput` — fake returns no text blocks; errors.Is(err, ErrInvalidLLMOutput).
- `TestAnthropicGenerator_MalformedJSON_ReturnsErrInvalidLLMOutput` — fake returns "not json"; errors.Is.
- `TestAnthropicGenerator_ValidationFailure_ReturnsErrInvalidLLMOutput` — fake returns a JSON array with a question missing `model_answer`; errors.Is.
- `TestAnthropicGenerator_HTTP401_ReturnsErrLLMAuthFailed` — fake returns an `*anthropic.Error{StatusCode: 401}`; errors.Is(err, ErrLLMAuthFailed).

- [ ] **Step 5: Verify + commit**

```
go test ./internal/interview/domain/services/... ./internal/interview/infrastructure/generation/... -count=1 -race
make build
git add internal/interview/domain/services/question_generator.go \
        internal/interview/infrastructure/generation/
git commit -m "feat(interview): AnthropicQuestionGenerator + per-RoundKind prompt templates"
```

---

## Task 9: Outbox + event publisher + dispatcher

**Files:**
- Create: `internal/interview/infrastructure/messaging/event_publisher.go` + `_test.go`
- Create: `internal/interview/infrastructure/messaging/outbox_dispatcher.go` + `_test.go`

- [ ] **Step 1: BusPublisher**

Create `internal/interview/infrastructure/messaging/event_publisher.go`:

```go
// Package messaging holds the interview context's outbox publisher +
// dispatcher. Same shape as sourcing/infrastructure/messaging.
package messaging

import (
	"context"

	"github.com/hustle/hireflow/internal/interview/domain/events"
)

// EventPublisher publishes domain events to a downstream broker / in-process bus.
type EventPublisher interface {
	Publish(ctx context.Context, event events.Event) error
}

// Bus is the minimum surface BusPublisher needs from a process-local event bus.
type Bus interface {
	Publish(ctx context.Context, eventName string, event any) error
}

// BusPublisher forwards into a process-local Bus.
type BusPublisher struct{ bus Bus }

// NewBusPublisher wires the publisher.
func NewBusPublisher(bus Bus) *BusPublisher { return &BusPublisher{bus: bus} }

func (p *BusPublisher) Publish(ctx context.Context, ev events.Event) error {
	return p.bus.Publish(ctx, ev.EventName(), ev)
}
```

Create `event_publisher_test.go`:

```go
package messaging_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/domain/events"
	"github.com/hustle/hireflow/internal/interview/infrastructure/messaging"
)

type recordingBus struct {
	calls []struct {
		name  string
		event any
	}
}

func (r *recordingBus) Publish(_ context.Context, name string, event any) error {
	r.calls = append(r.calls, struct {
		name  string
		event any
	}{name, event})
	return nil
}

func TestBusPublisher_ForwardsEventName(t *testing.T) {
	bus := &recordingBus{}
	pub := messaging.NewBusPublisher(bus)

	ev := events.InterviewProcessCreated{
		ProcessID:     uuid.New(),
		TenantID:      shared.NewTenantID(),
		ApplicationID: uuid.New(),
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
		OccurredAt:    time.Now().UTC(),
	}
	require.NoError(t, pub.Publish(context.Background(), ev))
	require.Len(t, bus.calls, 1)
	assert.Equal(t, "interview.InterviewProcessCreated", bus.calls[0].name)
}
```

- [ ] **Step 2: OutboxDispatcher**

Create `internal/interview/infrastructure/messaging/outbox_dispatcher.go`:

```go
package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/domain/events"
)

// DispatcherConfig controls polling behavior.
type DispatcherConfig struct {
	BatchSize    int           // default 50
	PollInterval time.Duration // default 1s
}

// OutboxDispatcher polls interview_outbox, decodes pending rows, and
// publishes each event via the EventPublisher. Same loop as
// sourcing.OutboxDispatcher.
type OutboxDispatcher struct {
	pool   *pgxpool.Pool
	pub    EventPublisher
	logger zerolog.Logger
	cfg    DispatcherConfig
}

func NewOutboxDispatcher(pool *pgxpool.Pool, pub EventPublisher, logger zerolog.Logger, cfg DispatcherConfig) *OutboxDispatcher {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	return &OutboxDispatcher{
		pool:   pool,
		pub:    pub,
		logger: logger.With().Str("component", "interview_outbox_dispatcher").Logger(),
		cfg:    cfg,
	}
}

// Run loops until ctx is done, dispatching pending events on each tick.
func (d *OutboxDispatcher) Run(ctx context.Context) {
	d.logger.Info().Int("batch", d.cfg.BatchSize).Msg("dispatcher started")
	defer d.logger.Info().Msg("dispatcher stopped")
	t := time.NewTicker(d.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := d.dispatchBatch(ctx); err != nil {
				d.logger.Error().Err(err).Msg("dispatch batch failed")
			}
		}
	}
}

func (d *OutboxDispatcher) dispatchBatch(ctx context.Context) error {
	rows, err := d.pool.Query(ctx, `
		SELECT id, event_name, aggregate_id, tenant_id, payload, occurred_at
		FROM interview_outbox
		WHERE dispatched_at IS NULL
		ORDER BY id
		LIMIT $1
	`, d.cfg.BatchSize)
	if err != nil {
		return fmt.Errorf("query outbox: %w", err)
	}
	defer rows.Close()

	type pending struct {
		id          int64
		eventName   string
		aggregateID uuid.UUID
		tenantID    string
		payload     []byte
		occurredAt  time.Time
	}
	var batch []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.eventName, &p.aggregateID, &p.tenantID, &p.payload, &p.occurredAt); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		batch = append(batch, p)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows.Err: %w", err)
	}

	for _, p := range batch {
		tenantID, err := shared.ParseTenantID(p.tenantID)
		if err != nil {
			d.logger.Error().Err(err).Str("event", p.eventName).Int64("id", p.id).Msg("invalid tenant_id; leaving row undispatched")
			continue
		}
		ev, err := decodeEvent(p.eventName, p.aggregateID, tenantID, p.occurredAt, p.payload)
		if err != nil {
			d.logger.Error().Err(err).Str("event", p.eventName).Msg("decode failed; leaving row undispatched")
			continue
		}
		if err := d.pub.Publish(ctx, ev); err != nil {
			d.logger.Error().Err(err).Str("event", p.eventName).Msg("publish failed; leaving row undispatched")
			continue
		}
		if _, err := d.pool.Exec(ctx, `UPDATE interview_outbox SET dispatched_at=now() WHERE id=$1`, p.id); err != nil {
			d.logger.Error().Err(err).Int64("id", p.id).Msg("mark dispatched failed")
		}
	}
	return nil
}

// decodeEvent inflates a payload into the matching event struct.
func decodeEvent(name string, aggID uuid.UUID, tenant shared.TenantID, at time.Time, payload []byte) (events.Event, error) {
	_ = aggID
	_ = tenant
	_ = at
	switch name {
	case "interview.InterviewProcessCreated":
		var e events.InterviewProcessCreated
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, err
		}
		return e, nil
	case "interview.InterviewQuestionsGenerated":
		var e events.InterviewQuestionsGenerated
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, err
		}
		return e, nil
	case "interview.InterviewFeedbackRecorded":
		var e events.InterviewFeedbackRecorded
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, err
		}
		return e, nil
	}
	return nil, errors.New("unknown event name: " + name)
}
```

Create `outbox_dispatcher_test.go` with `//go:build integration`. Add a `newPool` helper (TRUNCATEs all interview tables) and a `recordingPublisher`:

```go
type recordingPublisher struct{ calls []events.Event }
func (r *recordingPublisher) Publish(_ context.Context, ev events.Event) error {
    r.calls = append(r.calls, ev)
    return nil
}
```

Tests:

- `TestDispatcher_DispatchesPendingEvents` — INSERT 3 rows (one of each event type) with `dispatched_at` NULL; call `dispatchBatch`; verify the publisher received 3 calls in id order and `dispatched_at` is now non-NULL.
- `TestDispatcher_SkipsAlreadyDispatched` — INSERT 1 row with `dispatched_at = now()`; call dispatchBatch; verify publisher received 0 calls.
- `TestDispatcher_DecodeFailure_LeavesRowUndispatched` — INSERT 1 row with garbage payload; call dispatchBatch; verify `dispatched_at` is still NULL and no publisher call.

- [ ] **Step 3: Verify + commit**

```
go test ./internal/interview/infrastructure/messaging/... -count=1 -race
export DATABASE_URL="postgres://hireflow:hireflow@localhost:5433/hireflow?sslmode=disable"
go test -tags=integration ./internal/interview/infrastructure/messaging/... -count=1 -race
make build
git add internal/interview/infrastructure/messaging/
git commit -m "feat(interview): outbox dispatcher + bus publisher"
```

---

## Task 10: Commands — StartInterviewProcess + UpsertLoopTemplate

**Files:**
- Create: `internal/interview/application/commands/start_interview_process.go` + `_test.go`
- Create: `internal/interview/application/commands/upsert_loop_template.go` + `_test.go`

- [ ] **Step 1: StartInterviewProcess**

Create `internal/interview/application/commands/start_interview_process.go`:

```go
// Package commands holds the interview context's write-side handlers.
package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	auditdomain "github.com/hustle/hireflow/internal/shared/audit/domain"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

// DefaultLoop is the hardcoded fallback loop when no per-intent template
// exists at process-creation time. Spec decision I1-D7.
var DefaultLoop = []entities.TemplateRound{
	{Kind: vo.RoundKindScreen, Sequence: 1},
	{Kind: vo.RoundKindTechnical, Sequence: 2},
	{Kind: vo.RoundKindBarRaiser, Sequence: 3},
}

// StartInterviewProcessInput is the input for StartInterviewProcessHandler.
type StartInterviewProcessInput struct {
	TenantID      shared.TenantID
	ApplicationID uuid.UUID
	CandidateID   uuid.UUID
	IntentID      uuid.UUID
}

// StartInterviewProcessHandler creates an InterviewProcess for a newly-
// shortlisted application. Fired by ApplicationShortlistedConsumer.
//
// Idempotency: if a process already exists for (tenant, application_id),
// returns nil (no-op). The unique constraint enforces this at the repo level;
// the handler also pre-checks via FindByApplicationID to avoid an
// always-published "already exists" log line on every redelivery.
type StartInterviewProcessHandler struct {
	processes    repositories.ProcessRepository
	templates    repositories.LoopTemplateRepository
}

// NewStartInterviewProcessHandler wires the handler.
func NewStartInterviewProcessHandler(
	processes repositories.ProcessRepository,
	templates repositories.LoopTemplateRepository,
) *StartInterviewProcessHandler {
	return &StartInterviewProcessHandler{processes: processes, templates: templates}
}

// Handle creates the process. Returns nil on duplicate (idempotent).
func (h *StartInterviewProcessHandler) Handle(ctx context.Context, in StartInterviewProcessInput) error {
	// Idempotency pre-check.
	if existing, err := h.processes.FindByApplicationID(ctx, in.TenantID, in.ApplicationID); err == nil && existing != nil {
		return nil
	} else if err != nil && !errors.Is(err, repositories.ErrProcessNotFound) {
		return fmt.Errorf("idempotency check: %w", err)
	}

	// Load the per-intent template, or fall back to DefaultLoop.
	rounds := DefaultLoop
	tmpl, err := h.templates.FindByIntent(ctx, in.TenantID, in.IntentID)
	if err == nil {
		rounds = tmpl.Rounds()
	} else if !errors.Is(err, repositories.ErrLoopTemplateNotFound) {
		return fmt.Errorf("load template: %w", err)
	}

	process, err := entities.NewInterviewProcess(entities.NewInterviewProcessInput{
		TenantID:      in.TenantID,
		ApplicationID: in.ApplicationID,
		CandidateID:   in.CandidateID,
		IntentID:      in.IntentID,
		Rounds:        rounds,
		Now:           func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return fmt.Errorf("construct process: %w", err)
	}

	if err := h.processes.Save(ctx, process); err != nil {
		// Lost the idempotency race — duplicate at the DB level.
		if errors.Is(err, repositories.ErrProcessDuplicate) {
			return nil
		}
		return fmt.Errorf("save process: %w", err)
	}
	return nil
}

// Suppress unused import in case audit grows back here later.
var _ auditdomain.AuditWriter = nil
```

Remove the `_ auditdomain.AuditWriter = nil` line and the `auditdomain` import if `go build` complains — they were added to keep the import slot reserved during early development. The handler itself does not write audit rows; only HTTP-triggered commands (template upsert, feedback, lifecycle) do.

Create `start_interview_process_test.go` with in-memory fakes:

```go
type fakeProcessRepo struct {
    byID    map[uuid.UUID]*entities.InterviewProcess
    byAppID map[uuid.UUID]*entities.InterviewProcess
    saveErr error
}
// ... implement ProcessRepository, returning ErrProcessNotFound where appropriate ...

type fakeTemplateRepo struct {
    byIntent map[uuid.UUID]*entities.LoopTemplate
}
// ... implement LoopTemplateRepository ...
```

Tests:

- `TestStartInterviewProcess_NewApplication_CreatesWithDefaultLoop` — empty templates; Handle creates a process with 3 rounds matching DefaultLoop (screen, technical, bar_raiser).
- `TestStartInterviewProcess_UsesIntentTemplate` — seed a template with 4 rounds; Handle creates a process with those 4 rounds.
- `TestStartInterviewProcess_IdempotentOnSecondCall` — call Handle twice with the same input; second call returns nil and process count stays at 1.
- `TestStartInterviewProcess_HandlesSaveDuplicate` — fake repo returns ErrProcessDuplicate on Save; Handle returns nil (race idempotency).
- `TestStartInterviewProcess_SaveFailure_Propagates` — fake repo returns a generic error; Handle wraps it.
- `TestStartInterviewProcess_TemplateLookupFailure_Propagates` — fake template repo returns a generic error (not ErrLoopTemplateNotFound); Handle wraps it.

- [ ] **Step 2: UpsertLoopTemplate**

Create `internal/interview/application/commands/upsert_loop_template.go`:

```go
package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	auditdomain "github.com/hustle/hireflow/internal/shared/audit/domain"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
)

// UpsertLoopTemplateInput is the input for UpsertLoopTemplateHandler.
type UpsertLoopTemplateInput struct {
	TenantID    shared.TenantID
	ActorUserID uuid.UUID
	IntentID    uuid.UUID
	Rounds      []entities.TemplateRound
}

// UpsertLoopTemplateHandler creates or replaces a per-intent loop template.
// Existing processes are NOT retroactively mutated (spec decision I1-D7).
type UpsertLoopTemplateHandler struct {
	templates repositories.LoopTemplateRepository
	audit     auditdomain.AuditWriter
}

// NewUpsertLoopTemplateHandler wires the handler.
func NewUpsertLoopTemplateHandler(templates repositories.LoopTemplateRepository, audit auditdomain.AuditWriter) *UpsertLoopTemplateHandler {
	return &UpsertLoopTemplateHandler{templates: templates, audit: audit}
}

// Handle saves the template and audits the action.
func (h *UpsertLoopTemplateHandler) Handle(ctx context.Context, in UpsertLoopTemplateInput) error {
	now := func() time.Time { return time.Now().UTC() }

	// Try to find existing — if present, Replace; else NewLoopTemplate.
	var tmpl *entities.LoopTemplate
	existing, err := h.templates.FindByIntent(ctx, in.TenantID, in.IntentID)
	switch {
	case err == nil:
		if err := existing.Replace(in.Rounds, now); err != nil {
			return fmt.Errorf("replace rounds: %w", err)
		}
		tmpl = existing
	default:
		if tmpl, err = entities.NewLoopTemplate(entities.NewLoopTemplateInput{
			TenantID: in.TenantID,
			IntentID: in.IntentID,
			Rounds:   in.Rounds,
			Now:      now,
		}); err != nil {
			return fmt.Errorf("construct template: %w", err)
		}
	}

	if err := h.templates.Save(ctx, tmpl); err != nil {
		return fmt.Errorf("save template: %w", err)
	}

	// Audit. Load-bearing per slice 4 conventions.
	if err := h.audit.Write(ctx, auditdomain.AuditEvent{
		ActorUserID:  in.ActorUserID,
		TenantID:     in.TenantID,
		Action:       "interview_loop_template_upserted",
		ResourceKind: "intent",
		ResourceID:   in.IntentID,
		Payload:      map[string]any{"round_count": len(in.Rounds)},
		OccurredAt:   now(),
	}); err != nil {
		return err
	}
	return nil
}
```

Create `upsert_loop_template_test.go`:

- `TestUpsertLoopTemplate_CreatesWhenAbsent` — empty repo; Handle creates a template.
- `TestUpsertLoopTemplate_ReplacesWhenPresent` — seed a 2-round template; Handle with 3 rounds; assert the rounds replaced.
- `TestUpsertLoopTemplate_AuditWritten` — capturing audit fake; Handle; assert audit row with action="interview_loop_template_upserted".
- `TestUpsertLoopTemplate_AuditFailurePropagates` — fake audit returns error; Handle returns wrapped error.
- `TestUpsertLoopTemplate_InvalidRounds_RejectedByConstructor` — pass duplicate-sequence rounds; Handle errors.

- [ ] **Step 3: Verify + commit**

```
go test ./internal/interview/application/commands/... -count=1 -race
make build
git add internal/interview/application/commands/start_interview_process.go \
        internal/interview/application/commands/start_interview_process_test.go \
        internal/interview/application/commands/upsert_loop_template.go \
        internal/interview/application/commands/upsert_loop_template_test.go
git commit -m "feat(interview): StartInterviewProcess + UpsertLoopTemplate commands"
```

---

## Task 11: Commands — Generate + Regenerate round questions

**Files:**
- Create: `internal/interview/application/commands/generate_round_questions.go` + `_test.go`
- Create: `internal/interview/application/commands/regenerate_round_questions.go` + `_test.go`

- [ ] **Step 1: GenerateRoundQuestions**

Create `internal/interview/application/commands/generate_round_questions.go`:

```go
package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	"github.com/hustle/hireflow/internal/interview/domain/services"
	"github.com/hustle/hireflow/internal/interview/infrastructure/generation"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

// GenerateRoundQuestionsInput is the input for GenerateRoundQuestionsHandler.
type GenerateRoundQuestionsInput struct {
	TenantID  shared.TenantID
	ProcessID uuid.UUID
	RoundID   uuid.UUID
}

// GenerateRoundQuestionsHandler runs one generation attempt for one round.
// Fired by QuestionGenerationPool. Returns nil on success (questions saved
// and round transitioned to QuestionsReady) AND on retry-scheduled (round
// stays Pending with updated next_attempt_at) AND on abort (round
// transitioned to GenerationFailed) — i.e., the handler never returns nil
// indicating "you should retry me immediately"; it persists the desired
// next state and returns nil. A non-nil return indicates an unexpected
// infrastructure error (DB write failed, etc.).
type GenerateRoundQuestionsHandler struct {
	processes repositories.ProcessRepository
	intents   services.IntentReader
	candidates services.CandidateReader
	generator services.QuestionGenerator
}

// NewGenerateRoundQuestionsHandler wires the handler.
func NewGenerateRoundQuestionsHandler(
	processes repositories.ProcessRepository,
	intents services.IntentReader,
	candidates services.CandidateReader,
	generator services.QuestionGenerator,
) *GenerateRoundQuestionsHandler {
	return &GenerateRoundQuestionsHandler{
		processes:  processes,
		intents:    intents,
		candidates: candidates,
		generator:  generator,
	}
}

// Handle runs one generation attempt.
func (h *GenerateRoundQuestionsHandler) Handle(ctx context.Context, in GenerateRoundQuestionsInput) error {
	process, err := h.processes.FindByID(ctx, in.TenantID, in.ProcessID)
	if err != nil {
		return fmt.Errorf("load process: %w", err)
	}

	// Locate the round.
	var round *entities.InterviewRound
	for _, r := range process.Rounds() {
		if r.ID() == in.RoundID {
			round = r
			break
		}
	}
	if round == nil {
		return entities.ErrRoundNotFound
	}
	if round.Status() != vo.RoundStatusPending {
		// Another worker already advanced it; idempotent no-op.
		return nil
	}

	roleSpec, err := h.intents.GetRoleSpec(ctx, in.TenantID, process.IntentID())
	if err != nil {
		return h.handleFailure(ctx, process, in.RoundID, classifyError(err), err.Error())
	}
	candidateProfile, err := h.candidates.GetProfileForQuestions(ctx, in.TenantID, process.CandidateID())
	if err != nil {
		return h.handleFailure(ctx, process, in.RoundID, classifyError(err), err.Error())
	}

	questions, err := h.generator.Generate(ctx, services.GenerationInput{
		RoundKind:        round.Kind(),
		RoleSpec:         roleSpec,
		CandidateProfile: candidateProfile,
	})
	if err != nil {
		return h.handleFailure(ctx, process, in.RoundID, classifyError(err), err.Error())
	}

	if err := process.MarkRoundQuestionsReady(in.RoundID, questions); err != nil {
		return fmt.Errorf("mark ready: %w", err)
	}
	if err := h.processes.Save(ctx, process); err != nil {
		return fmt.Errorf("save process: %w", err)
	}
	return nil
}

// classifyError maps a concrete error to a FailureKind for retry decisions.
func classifyError(err error) vo.FailureKind {
	if errors.Is(err, generation.ErrLLMAuthFailed) {
		return vo.FailureKindLLMAuth
	}
	if errors.Is(err, generation.ErrInvalidLLMOutput) {
		return vo.FailureKindInvalidJSON
	}
	if errors.Is(err, services.ErrIntentNotFound) || errors.Is(err, services.ErrCandidateNotFound) {
		// Cross-context dependency permanently missing — treat as auth-class
		// (abort immediately rather than retry forever).
		return vo.FailureKindLLMAuth
	}
	// Everything else looks transient.
	return vo.FailureKindTransient
}

// handleFailure applies the retry decision: either re-save the process with
// an incremented attempt count + scheduled next_attempt_at, or mark the
// round GenerationFailed.
func (h *GenerateRoundQuestionsHandler) handleFailure(
	ctx context.Context,
	process *entities.InterviewProcess,
	roundID uuid.UUID,
	kind vo.FailureKind,
	detail string,
) error {
	var attempt int
	for _, r := range process.Rounds() {
		if r.ID() == roundID {
			attempt = r.AttemptCount() + 1
			break
		}
	}
	decision := vo.DecideRetry(kind, attempt, detail)
	switch decision.Action {
	case vo.RetryActionRetry:
		nextAt := time.Now().UTC().Add(decision.Backoff)
		if err := process.RecordGenerationAttempt(roundID, decision.Detail, nextAt); err != nil {
			return fmt.Errorf("record attempt: %w", err)
		}
	case vo.RetryActionAbort:
		if err := process.MarkRoundGenerationFailed(roundID, decision.Detail); err != nil {
			return fmt.Errorf("mark failed: %w", err)
		}
	}
	if err := h.processes.Save(ctx, process); err != nil {
		return fmt.Errorf("save process: %w", err)
	}
	return nil
}
```

Create `generate_round_questions_test.go`. Use in-memory fakes for `ProcessRepository`, `IntentReader`, `CandidateReader`, `QuestionGenerator`. Tests:

- `TestGenerate_HappyPath_MarksQuestionsReady` — fake generator returns 3 valid Questions; Handle results in round.Status() == QuestionsReady and saved process has the questions.
- `TestGenerate_RoundAlreadyAdvanced_NoOp` — seed process with round in QuestionsReady; Handle returns nil without calling the generator.
- `TestGenerate_RoundNotFound_ReturnsErrRoundNotFound` — pass a bogus round_id.
- `TestGenerate_LLMAuth_AbortsImmediately` — generator returns `generation.ErrLLMAuthFailed`; assert round.Status() == GenerationFailed after one attempt.
- `TestGenerate_InvalidJSON_RetriesOnceThenAborts` — first call returns `generation.ErrInvalidLLMOutput`; Handle once → round still Pending with attempt=1, next_attempt_at ~30s in future. Run Handle again → round → GenerationFailed.
- `TestGenerate_TransientError_FollowsBackoffSchedule` — generator returns a generic error; run Handle 5 times → round Pending each time, next_attempt_at increments per schedule (1m, 5m, 15m, 1h, 4h). 6th call → round → GenerationFailed.
- `TestGenerate_IntentNotFound_AbortsImmediately` — IntentReader returns `services.ErrIntentNotFound`; round → GenerationFailed.
- `TestGenerate_CandidateNotFound_AbortsImmediately` — CandidateReader returns `services.ErrCandidateNotFound`; round → GenerationFailed.

For the time-sensitive tests use `time.Now()` directly in the handler — verify `next_attempt_at` is within a small delta of the expected value rather than exact equality.

- [ ] **Step 2: RegenerateRoundQuestions**

Create `internal/interview/application/commands/regenerate_round_questions.go`:

```go
package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

// ErrRoundNotRegenerable is returned when the round is in a terminal state
// (Completed or Skipped) — HTTP 409 territory.
var ErrRoundNotRegenerable = errors.New("interview: round not regenerable")

// RegenerateRoundQuestionsInput is the input for the handler.
type RegenerateRoundQuestionsInput struct {
	TenantID shared.TenantID
	RoundID  uuid.UUID
	// Steering is optional recruiter-provided text appended to the regenerate
	// prompt by the worker (looked up on the saved round_state — slice 1
	// does not persist steering between regenerations beyond the next one).
	// Slice 1 leaves steering threading as a future-slice concern; the field
	// is accepted on the HTTP DTO but ignored by the worker in this slice.
	Steering string
}

// RegenerateRoundQuestionsHandler resets a round to Pending so the worker
// pool picks it up again. Only valid from QuestionsReady or GenerationFailed.
type RegenerateRoundQuestionsHandler struct {
	processes repositories.ProcessRepository
}

// NewRegenerateRoundQuestionsHandler wires the handler.
func NewRegenerateRoundQuestionsHandler(processes repositories.ProcessRepository) *RegenerateRoundQuestionsHandler {
	return &RegenerateRoundQuestionsHandler{processes: processes}
}

// Handle locates the round, validates source state, resets, saves.
func (h *RegenerateRoundQuestionsHandler) Handle(ctx context.Context, in RegenerateRoundQuestionsInput) error {
	// Find the process by scanning — we don't have an index by round_id.
	// Slice 1 cost is fine; future-slice optimization can add a round_id
	// → process_id lookup if hot-path warrants it.
	process, err := h.findProcessByRoundID(ctx, in.TenantID, in.RoundID)
	if err != nil {
		return err
	}
	if err := process.ResetRoundForRegeneration(in.RoundID); err != nil {
		if errors.Is(err, entities.ErrInvalidTransition) {
			return ErrRoundNotRegenerable
		}
		return fmt.Errorf("reset round: %w", err)
	}
	if err := h.processes.Save(ctx, process); err != nil {
		return fmt.Errorf("save process: %w", err)
	}
	return nil
}

// findProcessByRoundID finds the process containing the given round. Loads
// every process for the tenant; OK for slice 1 (low cardinality). Returns
// entities.ErrRoundNotFound when not found.
func (h *RegenerateRoundQuestionsHandler) findProcessByRoundID(ctx context.Context, tenant shared.TenantID, roundID uuid.UUID) (*entities.InterviewProcess, error) {
	processes, err := h.processes.ListByTenant(ctx, tenant, repositories.ProcessListFilter{Limit: 0})
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	for _, p := range processes {
		for _, r := range p.Rounds() {
			if r.ID() == roundID {
				return p, nil
			}
		}
	}
	_ = vo.RoundStatusPending // keep vo import "used" — referenced elsewhere if needed
	return nil, entities.ErrRoundNotFound
}
```

**Note on the linear scan:** `ListByTenant` with `IntentID = uuid.Nil` returns nothing because the SQL filters `intent_id=$2`. Fix: add a dedicated repo method `FindByRoundID` for this lookup instead of the scan. Replace the handler body and add the method to the port + Postgres impl:

In `internal/interview/domain/repositories/process_repository.go`, add to the `ProcessRepository` interface:

```go
// FindByRoundID returns the InterviewProcess containing the given round.
// Returns ErrProcessNotFound when no process contains that round id.
FindByRoundID(ctx context.Context, tenant shared.TenantID, roundID uuid.UUID) (*entities.InterviewProcess, error)
```

In `internal/interview/infrastructure/persistence/postgres_process_repository.go`, add:

```go
// FindByRoundID returns the process containing the given round id.
func (r *PostgresProcessRepository) FindByRoundID(ctx context.Context, tenant shared.TenantID, roundID uuid.UUID) (*entities.InterviewProcess, error) {
	var processID uuid.UUID
	err := r.pool.QueryRow(ctx,
		`SELECT process_id FROM interview_rounds WHERE tenant_id=$1 AND id=$2`,
		tenant.String(), roundID,
	).Scan(&processID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repositories.ErrProcessNotFound
		}
		return nil, fmt.Errorf("scan round: %w", err)
	}
	return r.FindByID(ctx, tenant, processID)
}
```

Replace the handler's `findProcessByRoundID` with:

```go
func (h *RegenerateRoundQuestionsHandler) findProcessByRoundID(ctx context.Context, tenant shared.TenantID, roundID uuid.UUID) (*entities.InterviewProcess, error) {
	p, err := h.processes.FindByRoundID(ctx, tenant, roundID)
	if err != nil {
		if errors.Is(err, repositories.ErrProcessNotFound) {
			return nil, entities.ErrRoundNotFound
		}
		return nil, err
	}
	return p, nil
}
```

Remove the unused `vo` import (and the `_ = vo.RoundStatusPending` line). Update all in-memory `ProcessRepository` fakes (in T10's tests and forthcoming command tests) to implement `FindByRoundID` — scan their internal map; return ErrProcessNotFound if absent. **This applies retroactively to T10 fakes too** — add the method there.

Create `regenerate_round_questions_test.go`:

- `TestRegenerate_FromQuestionsReady_ResetsToPending` — seed a process with one round in QuestionsReady; Handle → round.Status() == Pending, attempt_count=0, questions=nil.
- `TestRegenerate_FromGenerationFailed_ResetsToPending` — seed Round in GenerationFailed; Handle → Pending.
- `TestRegenerate_FromCompleted_ReturnsErrRoundNotRegenerable` — Round in Completed; Handle → ErrRoundNotRegenerable.
- `TestRegenerate_FromSkipped_ReturnsErrRoundNotRegenerable` — Round in Skipped; same.
- `TestRegenerate_RoundNotFound_ReturnsErrRoundNotFound` — bogus round_id.
- `TestRegenerate_TenantScoped` — round exists under tenantA; Handle with tenantB returns ErrRoundNotFound.

- [ ] **Step 3: Verify + commit**

```
go test ./internal/interview/... -count=1 -race
make build
git add internal/interview/application/commands/generate_round_questions.go \
        internal/interview/application/commands/generate_round_questions_test.go \
        internal/interview/application/commands/regenerate_round_questions.go \
        internal/interview/application/commands/regenerate_round_questions_test.go \
        internal/interview/domain/repositories/process_repository.go \
        internal/interview/infrastructure/persistence/postgres_process_repository.go
git commit -m "feat(interview): GenerateRoundQuestions + RegenerateRoundQuestions commands"
```

---

## Task 12: Commands — Feedback + lifecycle (round + process)

**Files:**
- Create: `internal/interview/application/commands/record_feedback.go` + `_test.go`
- Create: `internal/interview/application/commands/mark_round_completed.go` + `_test.go`
- Create: `internal/interview/application/commands/mark_round_skipped.go` + `_test.go`
- Create: `internal/interview/application/commands/complete_process.go` + `_test.go`
- Create: `internal/interview/application/commands/cancel_process.go` + `_test.go`

- [ ] **Step 1: RecordFeedback**

Create `internal/interview/application/commands/record_feedback.go`:

```go
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	auditdomain "github.com/hustle/hireflow/internal/shared/audit/domain"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/domain/events"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

// RecordFeedbackInput is the input for the handler.
type RecordFeedbackInput struct {
	TenantID    shared.TenantID
	ActorUserID uuid.UUID
	RoundID     uuid.UUID
	Feedback    vo.Feedback
}

// RecordFeedbackHandler appends a feedback row + writes an audit + emits
// an InterviewFeedbackRecorded event via direct outbox write.
//
// Note: feedback is append-only and not part of an aggregate. The
// InterviewFeedbackRecorded event is emitted by inserting directly into the
// interview_outbox table (mirrors the sourcing.CandidateErased pattern from
// slice 4).
type RecordFeedbackHandler struct {
	feedback  repositories.FeedbackRepository
	processes repositories.ProcessRepository
	audit     auditdomain.AuditWriter
	outbox    OutboxAppender
}

// OutboxAppender is the narrow interface for writing one event row directly
// into the interview_outbox table.
type OutboxAppender interface {
	Append(ctx context.Context, event events.Event) error
}

// NewRecordFeedbackHandler wires the handler.
func NewRecordFeedbackHandler(
	feedback repositories.FeedbackRepository,
	processes repositories.ProcessRepository,
	audit auditdomain.AuditWriter,
	outbox OutboxAppender,
) *RecordFeedbackHandler {
	return &RecordFeedbackHandler{feedback: feedback, processes: processes, audit: audit, outbox: outbox}
}

// Handle validates, persists, audits, emits the event.
func (h *RecordFeedbackHandler) Handle(ctx context.Context, in RecordFeedbackInput) error {
	// Validate the round exists for this tenant and is in a state that can
	// receive feedback (QuestionsReady — round is being conducted).
	process, err := h.processes.FindByRoundID(ctx, in.TenantID, in.RoundID)
	if err != nil {
		return err
	}
	var round *vo.RoundStatus
	for _, r := range process.Rounds() {
		if r.ID() == in.RoundID {
			s := r.Status()
			round = &s
			break
		}
	}
	if round == nil {
		return fmt.Errorf("round not in returned process")
	}
	if *round != vo.RoundStatusQuestionsReady {
		return fmt.Errorf("feedback: round must be QuestionsReady, was %s", *round)
	}

	id := uuid.New()
	now := time.Now().UTC()
	in.Feedback.SubmittedAt = now
	if err := h.feedback.Append(ctx, repositories.FeedbackRow{
		ID:       id,
		TenantID: in.TenantID,
		RoundID:  in.RoundID,
		Feedback: in.Feedback,
	}); err != nil {
		return fmt.Errorf("append feedback: %w", err)
	}

	if err := h.outbox.Append(ctx, events.InterviewFeedbackRecorded{
		FeedbackID: id,
		RoundID:    in.RoundID,
		Decision:   string(in.Feedback.Decision),
		TenantID:   in.TenantID,
		OccurredAt: now,
	}); err != nil {
		return fmt.Errorf("emit event: %w", err)
	}

	if err := h.audit.Write(ctx, auditdomain.AuditEvent{
		ActorUserID:  in.ActorUserID,
		TenantID:     in.TenantID,
		Action:       "interview_round_feedback_recorded",
		ResourceKind: "interview_round",
		ResourceID:   in.RoundID,
		Payload:      map[string]any{"decision": string(in.Feedback.Decision)},
		OccurredAt:   now,
	}); err != nil {
		return err
	}
	return nil
}

// Compile-time guard against json import unused if we drop the payload.
var _ = json.Marshal
```

Add a Postgres implementation of `OutboxAppender` in `internal/interview/infrastructure/messaging/outbox_dispatcher.go` (or a new file in the same package). The simplest place to put it is alongside the dispatcher:

```go
// PostgresOutboxAppender writes one row to interview_outbox synchronously.
// Used by commands that emit events without going through an aggregate Save
// (e.g., RecordFeedback — feedback is not an aggregate).
type PostgresOutboxAppender struct{ pool *pgxpool.Pool }

func NewPostgresOutboxAppender(pool *pgxpool.Pool) *PostgresOutboxAppender {
	return &PostgresOutboxAppender{pool: pool}
}

func (a *PostgresOutboxAppender) Append(ctx context.Context, ev events.Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	_, err = a.pool.Exec(ctx, `
		INSERT INTO interview_outbox (event_name, aggregate_id, tenant_id, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5)
	`, ev.EventName(), ev.AggregateID(), ev.Tenant().String(), payload, ev.At())
	if err != nil {
		return fmt.Errorf("insert outbox: %w", err)
	}
	return nil
}
```

Create `record_feedback_test.go` covering:

- `TestRecordFeedback_HappyPath_AppendsRowAuditAndEvent` — fake repo, audit, outbox; Handle; assert each captured exactly once.
- `TestRecordFeedback_RoundNotFound_ReturnsErr` — fake processes returns ErrProcessNotFound.
- `TestRecordFeedback_RoundNotInQuestionsReady_ReturnsErr` — round in Completed; Handle returns "must be QuestionsReady" error.
- `TestRecordFeedback_InvalidFeedback_RejectedByRepoValidate` — feedback with empty name; the FeedbackRepository fake calls Validate via its Append; returns ErrInvalidFeedback.
- `TestRecordFeedback_AuditFailurePropagates` — audit fake returns error; Handle returns it.

- [ ] **Step 2: MarkRoundCompleted + MarkRoundSkipped**

Create `internal/interview/application/commands/mark_round_completed.go`:

```go
package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	auditdomain "github.com/hustle/hireflow/internal/shared/audit/domain"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
)

// ErrRoundInvalidTransition is returned when the round can't be marked done
// from its current state. HTTP 409.
var ErrRoundInvalidTransition = errors.New("interview: invalid round state for transition")

type MarkRoundCompletedInput struct {
	TenantID    shared.TenantID
	ActorUserID uuid.UUID
	RoundID     uuid.UUID
}

type MarkRoundCompletedHandler struct {
	processes repositories.ProcessRepository
	audit     auditdomain.AuditWriter
}

func NewMarkRoundCompletedHandler(processes repositories.ProcessRepository, audit auditdomain.AuditWriter) *MarkRoundCompletedHandler {
	return &MarkRoundCompletedHandler{processes: processes, audit: audit}
}

func (h *MarkRoundCompletedHandler) Handle(ctx context.Context, in MarkRoundCompletedInput) error {
	process, err := h.processes.FindByRoundID(ctx, in.TenantID, in.RoundID)
	if err != nil {
		return err
	}
	if err := process.MarkRoundCompleted(in.RoundID); err != nil {
		if errors.Is(err, entities.ErrInvalidTransition) {
			return ErrRoundInvalidTransition
		}
		return fmt.Errorf("mark completed: %w", err)
	}
	if err := h.processes.Save(ctx, process); err != nil {
		return fmt.Errorf("save process: %w", err)
	}
	return h.audit.Write(ctx, auditdomain.AuditEvent{
		ActorUserID:  in.ActorUserID,
		TenantID:     in.TenantID,
		Action:       "interview_round_completed",
		ResourceKind: "interview_round",
		ResourceID:   in.RoundID,
		OccurredAt:   time.Now().UTC(),
	})
}
```

Create `mark_round_completed_test.go`:

- `TestMarkRoundCompleted_FromQuestionsReady_Succeeds`.
- `TestMarkRoundCompleted_FromPending_ReturnsErrRoundInvalidTransition`.
- `TestMarkRoundCompleted_AuditWritten`.
- `TestMarkRoundCompleted_AuditFailurePropagates`.

Create `internal/interview/application/commands/mark_round_skipped.go` (analogous, but uses `process.MarkRoundSkipped`, action `"interview_round_skipped"`). Source states allowed: Pending, QuestionsReady, GenerationFailed.

Test (`mark_round_skipped_test.go`):

- `TestMarkRoundSkipped_FromPending_Succeeds`.
- `TestMarkRoundSkipped_FromQuestionsReady_Succeeds`.
- `TestMarkRoundSkipped_FromGenerationFailed_Succeeds`.
- `TestMarkRoundSkipped_FromCompleted_ReturnsErr`.
- `TestMarkRoundSkipped_AuditWritten`.

- [ ] **Step 3: CompleteProcess + CancelProcess**

Create `internal/interview/application/commands/complete_process.go`:

```go
package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	auditdomain "github.com/hustle/hireflow/internal/shared/audit/domain"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
)

// ErrProcessInvalidTransition is returned when the process can't be moved
// to the requested state. HTTP 409.
var ErrProcessInvalidTransition = errors.New("interview: invalid process state for transition")

type CompleteProcessInput struct {
	TenantID    shared.TenantID
	ActorUserID uuid.UUID
	ProcessID   uuid.UUID
}

type CompleteProcessHandler struct {
	processes repositories.ProcessRepository
	audit     auditdomain.AuditWriter
}

func NewCompleteProcessHandler(processes repositories.ProcessRepository, audit auditdomain.AuditWriter) *CompleteProcessHandler {
	return &CompleteProcessHandler{processes: processes, audit: audit}
}

func (h *CompleteProcessHandler) Handle(ctx context.Context, in CompleteProcessInput) error {
	process, err := h.processes.FindByID(ctx, in.TenantID, in.ProcessID)
	if err != nil {
		return err
	}
	if err := process.Complete(); err != nil {
		if errors.Is(err, entities.ErrInvalidTransition) {
			return ErrProcessInvalidTransition
		}
		// "cannot complete; round X is ..." errors fall through as plain errors
		return fmt.Errorf("complete: %w", err)
	}
	if err := h.processes.Save(ctx, process); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	return h.audit.Write(ctx, auditdomain.AuditEvent{
		ActorUserID:  in.ActorUserID,
		TenantID:     in.TenantID,
		Action:       "interview_process_completed",
		ResourceKind: "interview_process",
		ResourceID:   in.ProcessID,
		OccurredAt:   time.Now().UTC(),
	})
}
```

Test:

- `TestCompleteProcess_AllRoundsTerminal_Succeeds`.
- `TestCompleteProcess_PendingRound_ReturnsErr`.
- `TestCompleteProcess_AlreadyCompleted_ReturnsErrProcessInvalidTransition`.
- `TestCompleteProcess_AuditWritten`.

Create `cancel_process.go` (analogous, calls `process.Cancel()`, action `"interview_process_cancelled"`):

- `TestCancelProcess_FromNew_Succeeds`.
- `TestCancelProcess_AlreadyCancelled_ReturnsErrProcessInvalidTransition`.
- `TestCancelProcess_AuditWritten`.

- [ ] **Step 4: Verify + commit**

```
go test ./internal/interview/... -count=1 -race
make build
git add internal/interview/application/commands/record_feedback.go \
        internal/interview/application/commands/record_feedback_test.go \
        internal/interview/application/commands/mark_round_completed.go \
        internal/interview/application/commands/mark_round_completed_test.go \
        internal/interview/application/commands/mark_round_skipped.go \
        internal/interview/application/commands/mark_round_skipped_test.go \
        internal/interview/application/commands/complete_process.go \
        internal/interview/application/commands/complete_process_test.go \
        internal/interview/application/commands/cancel_process.go \
        internal/interview/application/commands/cancel_process_test.go \
        internal/interview/infrastructure/messaging/outbox_dispatcher.go
git commit -m "feat(interview): feedback + round/process lifecycle commands"
```

---

## Task 13: Queries

**Files:**
- Create: `internal/interview/application/dto/interview_dtos.go`
- Create: `internal/interview/application/queries/get_interview_process.go` + `_test.go`
- Create: `internal/interview/application/queries/list_interview_processes.go` + `_test.go`
- Create: `internal/interview/application/queries/get_loop_template.go` + `_test.go`

- [ ] **Step 1: DTOs**

Create `internal/interview/application/dto/interview_dtos.go`:

```go
// Package dto holds the input/output shapes for interview commands + queries.
package dto

import (
	"time"

	"github.com/google/uuid"

	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

// InterviewProcessDTO is the read-model returned by GetInterviewProcess.
type InterviewProcessDTO struct {
	ID            uuid.UUID
	TenantID      string
	ApplicationID uuid.UUID
	CandidateID   uuid.UUID
	IntentID      uuid.UUID
	Status        string
	Rounds        []InterviewRoundDTO
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// InterviewRoundDTO is the read-model for one round.
type InterviewRoundDTO struct {
	ID               uuid.UUID
	Kind             string
	Sequence         int
	Status           string
	Questions        []vo.Question
	AttemptCount     int
	LastError        string
	FeedbackSummary  FeedbackSummaryDTO
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// FeedbackSummaryDTO aggregates feedback decision counts for a round.
type FeedbackSummaryDTO struct {
	StrongYes      int
	Yes            int
	Mixed          int
	No             int
	StrongNo       int
	Total          int
	LatestDecision string // empty if no feedback yet
}

// LoopTemplateDTO is the read-model returned by GetLoopTemplate.
type LoopTemplateDTO struct {
	IntentID    uuid.UUID
	Rounds      []LoopTemplateRoundDTO
	IsDefault   bool // true when the intent has no stored template
}

type LoopTemplateRoundDTO struct {
	Kind     string
	Sequence int
}
```

- [ ] **Step 2: GetInterviewProcess**

Create `internal/interview/application/queries/get_interview_process.go`:

```go
// Package queries holds the interview context's read-side handlers.
package queries

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	auditdomain "github.com/hustle/hireflow/internal/shared/audit/domain"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/application/dto"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
)

// GetInterviewProcessHandler returns the read-model for one process,
// including per-round feedback summaries. Writes an audit row because the
// response includes candidate-derived data.
type GetInterviewProcessHandler struct {
	processes repositories.ProcessRepository
	feedback  repositories.FeedbackRepository
	audit     auditdomain.AuditWriter
}

func NewGetInterviewProcessHandler(processes repositories.ProcessRepository, feedback repositories.FeedbackRepository, audit auditdomain.AuditWriter) *GetInterviewProcessHandler {
	return &GetInterviewProcessHandler{processes: processes, feedback: feedback, audit: audit}
}

// Handle returns the DTO. tenantID + actorUserID + processID.
func (h *GetInterviewProcessHandler) Handle(ctx context.Context, tenant shared.TenantID, actorUserID, processID uuid.UUID) (dto.InterviewProcessDTO, error) {
	p, err := h.processes.FindByID(ctx, tenant, processID)
	if err != nil {
		return dto.InterviewProcessDTO{}, err
	}

	out := dto.InterviewProcessDTO{
		ID:            p.ID(),
		TenantID:      p.TenantID().String(),
		ApplicationID: p.ApplicationID(),
		CandidateID:   p.CandidateID(),
		IntentID:      p.IntentID(),
		Status:        string(p.Status()),
		CreatedAt:     p.CreatedAt(),
		UpdatedAt:     p.UpdatedAt(),
	}

	for _, r := range p.Rounds() {
		fs, err := h.summarizeFeedback(ctx, tenant, r.ID())
		if err != nil {
			return dto.InterviewProcessDTO{}, fmt.Errorf("summarize feedback: %w", err)
		}
		out.Rounds = append(out.Rounds, dto.InterviewRoundDTO{
			ID:              r.ID(),
			Kind:            string(r.Kind()),
			Sequence:        r.Sequence(),
			Status:          string(r.Status()),
			Questions:       r.Questions(),
			AttemptCount:    r.AttemptCount(),
			LastError:       r.LastError(),
			FeedbackSummary: fs,
			CreatedAt:       r.CreatedAt(),
			UpdatedAt:       r.UpdatedAt(),
		})
	}

	// Audit AFTER the read succeeds. Load-bearing per slice 4.
	if err := h.audit.Write(ctx, auditdomain.AuditEvent{
		ActorUserID:  actorUserID,
		TenantID:     tenant,
		Action:       "interview_process_read",
		ResourceKind: "interview_process",
		ResourceID:   processID,
		OccurredAt:   time.Now().UTC(),
	}); err != nil {
		return dto.InterviewProcessDTO{}, fmt.Errorf("audit read: %w", err)
	}
	return out, nil
}

func (h *GetInterviewProcessHandler) summarizeFeedback(ctx context.Context, tenant shared.TenantID, roundID uuid.UUID) (dto.FeedbackSummaryDTO, error) {
	rows, err := h.feedback.ListByRound(ctx, tenant, roundID)
	if err != nil {
		return dto.FeedbackSummaryDTO{}, err
	}
	out := dto.FeedbackSummaryDTO{}
	for i, r := range rows {
		switch string(r.Decision) {
		case "strong_yes":
			out.StrongYes++
		case "yes":
			out.Yes++
		case "mixed":
			out.Mixed++
		case "no":
			out.No++
		case "strong_no":
			out.StrongNo++
		}
		out.Total++
		if i == 0 {
			// ListByRound returns newest-first; first row is latest.
			out.LatestDecision = string(r.Decision)
		}
	}
	return out, nil
}
```

Create `get_interview_process_test.go`:

- `TestGet_HappyPath_ReturnsProcessWithRounds` — fake repo with a saved process; assert all fields + 3 rounds; audit row written.
- `TestGet_AggregatesFeedback` — round has 2 strong_yes + 1 mixed; assert counts.
- `TestGet_LatestDecision` — feedback rows in newest-first order; latest_decision == newest row's decision.
- `TestGet_ProcessNotFound_ReturnsErr` — fake returns ErrProcessNotFound; Handle propagates.
- `TestGet_AuditFailurePropagates` — fake audit returns error; Handle returns wrapped error.

- [ ] **Step 3: ListInterviewProcesses**

Create `internal/interview/application/queries/list_interview_processes.go`:

```go
package queries

import (
	"context"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/application/dto"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
)

// ListInterviewProcessesHandler returns a paginated list of processes for an
// intent, optionally filtered by status. No audit (high volume; no PII).
type ListInterviewProcessesHandler struct {
	processes repositories.ProcessRepository
}

func NewListInterviewProcessesHandler(processes repositories.ProcessRepository) *ListInterviewProcessesHandler {
	return &ListInterviewProcessesHandler{processes: processes}
}

// ListInput is the input for the handler.
type ListInput struct {
	TenantID shared.TenantID
	IntentID uuid.UUID
	Status   string
	Limit    int
	Offset   int
}

// Handle returns a list of lightweight DTOs (no rounds, no feedback).
func (h *ListInterviewProcessesHandler) Handle(ctx context.Context, in ListInput) ([]dto.InterviewProcessDTO, error) {
	processes, err := h.processes.ListByTenant(ctx, in.TenantID, repositories.ProcessListFilter{
		IntentID: in.IntentID,
		Status:   in.Status,
		Limit:    in.Limit,
		Offset:   in.Offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]dto.InterviewProcessDTO, 0, len(processes))
	for _, p := range processes {
		out = append(out, dto.InterviewProcessDTO{
			ID:            p.ID(),
			TenantID:      p.TenantID().String(),
			ApplicationID: p.ApplicationID(),
			CandidateID:   p.CandidateID(),
			IntentID:      p.IntentID(),
			Status:        string(p.Status()),
			CreatedAt:     p.CreatedAt(),
			UpdatedAt:     p.UpdatedAt(),
		})
	}
	return out, nil
}
```

Test:

- `TestList_ReturnsProcessesForIntent`.
- `TestList_FiltersByStatus`.
- `TestList_RespectsLimitOffset`.
- `TestList_EmptyResult`.

- [ ] **Step 4: GetLoopTemplate**

Create `internal/interview/application/queries/get_loop_template.go`:

```go
package queries

import (
	"context"
	"errors"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/application/commands"
	"github.com/hustle/hireflow/internal/interview/application/dto"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
)

// GetLoopTemplateHandler returns the per-intent template, or the default
// loop when no template exists. The HTTP layer flag `is_default` lets the
// recruiter dashboard show "uses default" vs the actual template.
type GetLoopTemplateHandler struct {
	templates repositories.LoopTemplateRepository
}

func NewGetLoopTemplateHandler(templates repositories.LoopTemplateRepository) *GetLoopTemplateHandler {
	return &GetLoopTemplateHandler{templates: templates}
}

// Handle returns the template DTO. Falls back to DefaultLoop when absent.
func (h *GetLoopTemplateHandler) Handle(ctx context.Context, tenant shared.TenantID, intentID uuid.UUID) (dto.LoopTemplateDTO, error) {
	tmpl, err := h.templates.FindByIntent(ctx, tenant, intentID)
	if err != nil {
		if errors.Is(err, repositories.ErrLoopTemplateNotFound) {
			rounds := commands.DefaultLoop
			out := dto.LoopTemplateDTO{IntentID: intentID, IsDefault: true}
			for _, r := range rounds {
				out.Rounds = append(out.Rounds, dto.LoopTemplateRoundDTO{
					Kind: string(r.Kind), Sequence: r.Sequence,
				})
			}
			return out, nil
		}
		return dto.LoopTemplateDTO{}, err
	}
	out := dto.LoopTemplateDTO{IntentID: intentID, IsDefault: false}
	for _, r := range tmpl.Rounds() {
		out.Rounds = append(out.Rounds, dto.LoopTemplateRoundDTO{
			Kind: string(r.Kind), Sequence: r.Sequence,
		})
	}
	return out, nil
}
```

Test:

- `TestGetTemplate_ReturnsStoredTemplate_NotDefault`.
- `TestGetTemplate_ReturnsDefault_WhenAbsent` — IsDefault=true, rounds match DefaultLoop.
- `TestGetTemplate_TenantScoped`.

- [ ] **Step 5: Verify + commit**

```
go test ./internal/interview/application/... -count=1 -race
make build
git add internal/interview/application/dto/ internal/interview/application/queries/
git commit -m "feat(interview): GetInterviewProcess + List + GetLoopTemplate queries"
```

---

## Task 14: HTTP delivery layer

**Files:**
- Create: `internal/interview/delivery/http/v1/dto.go`
- Create: `internal/interview/delivery/http/v1/routes.go`
- Create: `internal/interview/delivery/http/v1/handlers.go`
- Create: `internal/interview/delivery/http/v1/handlers_test.go`

- [ ] **Step 1: HTTP DTOs**

Create `internal/interview/delivery/http/v1/dto.go`:

```go
// Package v1 is the v1 HTTP delivery layer of the interview context.
package v1

import (
	"time"

	"github.com/google/uuid"

	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

// UpsertLoopTemplateRequest is the body for PUT /intents/{intent_id}/loop-template.
type UpsertLoopTemplateRequest struct {
	Rounds []TemplateRoundRequest `json:"rounds"`
}

// TemplateRoundRequest is one round in the request body.
type TemplateRoundRequest struct {
	Kind     string `json:"kind"`
	Sequence int    `json:"sequence"`
}

// LoopTemplateResponse is the body for GET /intents/{intent_id}/loop-template.
type LoopTemplateResponse struct {
	IntentID  string                  `json:"intent_id"`
	Rounds    []TemplateRoundResponse `json:"rounds"`
	IsDefault bool                    `json:"is_default"`
}

type TemplateRoundResponse struct {
	Kind     string `json:"kind"`
	Sequence int    `json:"sequence"`
}

// InterviewProcessResponse is the body for GET /interview/processes/{id} and
// list responses.
type InterviewProcessResponse struct {
	ID            string                    `json:"id"`
	ApplicationID string                    `json:"application_id"`
	CandidateID   string                    `json:"candidate_id"`
	IntentID      string                    `json:"intent_id"`
	Status        string                    `json:"status"`
	Rounds        []InterviewRoundResponse  `json:"rounds,omitempty"`
	CreatedAt     time.Time                 `json:"created_at"`
	UpdatedAt     time.Time                 `json:"updated_at"`
}

type InterviewRoundResponse struct {
	ID              string                 `json:"id"`
	Kind            string                 `json:"kind"`
	Sequence        int                    `json:"sequence"`
	Status          string                 `json:"status"`
	Questions       []vo.Question          `json:"questions,omitempty"`
	AttemptCount    int                    `json:"attempt_count"`
	LastError       string                 `json:"last_error,omitempty"`
	FeedbackSummary FeedbackSummaryResponse `json:"feedback_summary"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

type FeedbackSummaryResponse struct {
	StrongYes      int    `json:"strong_yes"`
	Yes            int    `json:"yes"`
	Mixed          int    `json:"mixed"`
	No             int    `json:"no"`
	StrongNo       int    `json:"strong_no"`
	Total          int    `json:"total"`
	LatestDecision string `json:"latest_decision,omitempty"`
}

// ListProcessesResponse is the body for GET /intents/{id}/interview-processes.
type ListProcessesResponse struct {
	Processes []InterviewProcessResponse `json:"processes"`
}

// RecordFeedbackRequest is the body for POST /interview/rounds/{id}/feedback.
type RecordFeedbackRequest struct {
	InterviewerName  string `json:"interviewer_name"`
	InterviewerEmail string `json:"interviewer_email,omitempty"`
	Decision         string `json:"decision"`
	Notes            string `json:"notes,omitempty"`
}

// RegenerateRoundRequest is the body for POST /interview/rounds/{id}:regenerate.
// Optional steering text; ignored by the slice-1 worker (see spec for
// future-slice steering threading).
type RegenerateRoundRequest struct {
	Steering string `json:"steering,omitempty"`
}

// ErrorResponse is the standard error body.
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Unused-import quiet:
var _ = uuid.Nil
```

Remove the `var _ = uuid.Nil` line and the `uuid` import after writing — they were a scratchpad guard.

- [ ] **Step 2: routes.go**

Create `internal/interview/delivery/http/v1/routes.go`:

```go
package v1

import "github.com/go-chi/chi/v5"

// Mount registers the interview context's v1 routes onto the given router.
func Mount(r chi.Router, h *InterviewHandler) {
	r.Put("/intents/{intent_id}/loop-template", h.UpsertLoopTemplate)
	r.Get("/intents/{intent_id}/loop-template", h.GetLoopTemplate)
	r.Get("/intents/{intent_id}/interview-processes", h.ListInterviewProcesses)
	r.Get("/interview/processes/{process_id}", h.GetInterviewProcess)
	r.Post("/interview/processes/{process_id}:complete", h.CompleteProcess)
	r.Post("/interview/processes/{process_id}:cancel", h.CancelProcess)
	r.Post("/interview/rounds/{round_id}/feedback", h.RecordFeedback)
	r.Post("/interview/rounds/{round_id}:regenerate", h.RegenerateRoundQuestions)
	r.Post("/interview/rounds/{round_id}:mark-done", h.MarkRoundCompleted)
	r.Post("/interview/rounds/{round_id}:skip", h.MarkRoundSkipped)
}
```

- [ ] **Step 3: handlers.go**

Create `internal/interview/delivery/http/v1/handlers.go`:

```go
package v1

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/hustle/hireflow/internal/shared/infrastructure/auth"
	"github.com/hustle/hireflow/internal/interview/application/commands"
	"github.com/hustle/hireflow/internal/interview/application/queries"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

// InterviewHandlerDeps bundles all dependencies for InterviewHandler.
type InterviewHandlerDeps struct {
	UpsertTemplate            *commands.UpsertLoopTemplateHandler
	RecordFeedback            *commands.RecordFeedbackHandler
	MarkRoundCompleted        *commands.MarkRoundCompletedHandler
	MarkRoundSkipped          *commands.MarkRoundSkippedHandler
	CompleteProcess           *commands.CompleteProcessHandler
	CancelProcess             *commands.CancelProcessHandler
	RegenerateRoundQuestions  *commands.RegenerateRoundQuestionsHandler
	GetInterviewProcess       *queries.GetInterviewProcessHandler
	ListInterviewProcesses    *queries.ListInterviewProcessesHandler
	GetLoopTemplate           *queries.GetLoopTemplateHandler
	Logger                    zerolog.Logger
}

// InterviewHandler is the v1 HTTP entry point of the interview context.
type InterviewHandler struct {
	deps InterviewHandlerDeps
}

// NewInterviewHandler wires the handler.
func NewInterviewHandler(deps InterviewHandlerDeps) *InterviewHandler {
	return &InterviewHandler{deps: deps}
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, ErrorResponse{Code: code, Message: msg})
}

// --- handlers ---

// UpsertLoopTemplate handles PUT /intents/{intent_id}/loop-template.
func (h *InterviewHandler) UpsertLoopTemplate(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	intentID, err := uuid.Parse(chi.URLParam(r, "intent_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_intent_id", "intent_id must be a uuid")
		return
	}
	var body UpsertLoopTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	rounds := make([]entities.TemplateRound, 0, len(body.Rounds))
	for _, br := range body.Rounds {
		kind, err := vo.ParseRoundKind(br.Kind)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_round_kind", br.Kind)
			return
		}
		rounds = append(rounds, entities.TemplateRound{Kind: kind, Sequence: br.Sequence})
	}
	if err := h.deps.UpsertTemplate.Handle(r.Context(), commands.UpsertLoopTemplateInput{
		TenantID:    identity.TenantID,
		ActorUserID: identity.RecruiterID.UUID(),
		IntentID:    intentID,
		Rounds:      rounds,
	}); err != nil {
		h.deps.Logger.Error().Err(err).Msg("upsert template failed")
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetLoopTemplate handles GET /intents/{intent_id}/loop-template.
func (h *InterviewHandler) GetLoopTemplate(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	intentID, err := uuid.Parse(chi.URLParam(r, "intent_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_intent_id", "intent_id must be a uuid")
		return
	}
	out, err := h.deps.GetLoopTemplate.Handle(r.Context(), identity.TenantID, intentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	resp := LoopTemplateResponse{
		IntentID:  intentID.String(),
		IsDefault: out.IsDefault,
	}
	for _, rd := range out.Rounds {
		resp.Rounds = append(resp.Rounds, TemplateRoundResponse{Kind: rd.Kind, Sequence: rd.Sequence})
	}
	writeJSON(w, http.StatusOK, resp)
}

// ListInterviewProcesses handles GET /intents/{intent_id}/interview-processes.
func (h *InterviewHandler) ListInterviewProcesses(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	intentID, err := uuid.Parse(chi.URLParam(r, "intent_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_intent_id", "intent_id must be a uuid")
		return
	}
	status := r.URL.Query().Get("status")
	out, err := h.deps.ListInterviewProcesses.Handle(r.Context(), queries.ListInput{
		TenantID: identity.TenantID,
		IntentID: intentID,
		Status:   status,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	resp := ListProcessesResponse{}
	for _, p := range out {
		resp.Processes = append(resp.Processes, InterviewProcessResponse{
			ID:            p.ID.String(),
			ApplicationID: p.ApplicationID.String(),
			CandidateID:   p.CandidateID.String(),
			IntentID:      p.IntentID.String(),
			Status:        p.Status,
			CreatedAt:     p.CreatedAt,
			UpdatedAt:     p.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetInterviewProcess handles GET /interview/processes/{process_id}.
func (h *InterviewHandler) GetInterviewProcess(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	processID, err := uuid.Parse(chi.URLParam(r, "process_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_process_id", "process_id must be a uuid")
		return
	}
	out, err := h.deps.GetInterviewProcess.Handle(r.Context(), identity.TenantID, identity.RecruiterID.UUID(), processID)
	if err != nil {
		if errors.Is(err, repositories.ErrProcessNotFound) {
			writeError(w, http.StatusNotFound, "process_not_found", "process not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	resp := InterviewProcessResponse{
		ID:            out.ID.String(),
		ApplicationID: out.ApplicationID.String(),
		CandidateID:   out.CandidateID.String(),
		IntentID:      out.IntentID.String(),
		Status:        out.Status,
		CreatedAt:     out.CreatedAt,
		UpdatedAt:     out.UpdatedAt,
	}
	for _, rd := range out.Rounds {
		resp.Rounds = append(resp.Rounds, InterviewRoundResponse{
			ID: rd.ID.String(), Kind: rd.Kind, Sequence: rd.Sequence, Status: rd.Status,
			Questions: rd.Questions, AttemptCount: rd.AttemptCount, LastError: rd.LastError,
			FeedbackSummary: FeedbackSummaryResponse{
				StrongYes: rd.FeedbackSummary.StrongYes, Yes: rd.FeedbackSummary.Yes,
				Mixed: rd.FeedbackSummary.Mixed, No: rd.FeedbackSummary.No,
				StrongNo: rd.FeedbackSummary.StrongNo, Total: rd.FeedbackSummary.Total,
				LatestDecision: rd.FeedbackSummary.LatestDecision,
			},
			CreatedAt: rd.CreatedAt, UpdatedAt: rd.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// CompleteProcess handles POST /interview/processes/{process_id}:complete.
func (h *InterviewHandler) CompleteProcess(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	processID, err := uuid.Parse(chi.URLParam(r, "process_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_process_id", "process_id must be a uuid")
		return
	}
	if err := h.deps.CompleteProcess.Handle(r.Context(), commands.CompleteProcessInput{
		TenantID:    identity.TenantID,
		ActorUserID: identity.RecruiterID.UUID(),
		ProcessID:   processID,
	}); err != nil {
		switch {
		case errors.Is(err, repositories.ErrProcessNotFound):
			writeError(w, http.StatusNotFound, "process_not_found", "process not found")
		case errors.Is(err, commands.ErrProcessInvalidTransition):
			writeError(w, http.StatusConflict, "invalid_transition", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CancelProcess handles POST /interview/processes/{process_id}:cancel.
func (h *InterviewHandler) CancelProcess(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	processID, err := uuid.Parse(chi.URLParam(r, "process_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_process_id", "process_id must be a uuid")
		return
	}
	if err := h.deps.CancelProcess.Handle(r.Context(), commands.CancelProcessInput{
		TenantID:    identity.TenantID,
		ActorUserID: identity.RecruiterID.UUID(),
		ProcessID:   processID,
	}); err != nil {
		switch {
		case errors.Is(err, repositories.ErrProcessNotFound):
			writeError(w, http.StatusNotFound, "process_not_found", "process not found")
		case errors.Is(err, commands.ErrProcessInvalidTransition):
			writeError(w, http.StatusConflict, "invalid_transition", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RecordFeedback handles POST /interview/rounds/{round_id}/feedback.
func (h *InterviewHandler) RecordFeedback(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	roundID, err := uuid.Parse(chi.URLParam(r, "round_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_round_id", "round_id must be a uuid")
		return
	}
	var body RecordFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	decision, err := vo.ParseFeedbackDecision(body.Decision)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_decision", body.Decision)
		return
	}
	fb := vo.Feedback{
		InterviewerName:  body.InterviewerName,
		InterviewerEmail: body.InterviewerEmail,
		Decision:         decision,
		Notes:            body.Notes,
		SubmittedBy:      identity.RecruiterID.UUID(),
	}
	if err := fb.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_feedback", err.Error())
		return
	}
	if err := h.deps.RecordFeedback.Handle(r.Context(), commands.RecordFeedbackInput{
		TenantID:    identity.TenantID,
		ActorUserID: identity.RecruiterID.UUID(),
		RoundID:     roundID,
		Feedback:    fb,
	}); err != nil {
		if errors.Is(err, repositories.ErrProcessNotFound) {
			writeError(w, http.StatusNotFound, "round_not_found", "round not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// RegenerateRoundQuestions handles POST /interview/rounds/{round_id}:regenerate.
func (h *InterviewHandler) RegenerateRoundQuestions(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	roundID, err := uuid.Parse(chi.URLParam(r, "round_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_round_id", "round_id must be a uuid")
		return
	}
	var body RegenerateRoundRequest
	// Body is optional — ignore decode errors when body is empty.
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := h.deps.RegenerateRoundQuestions.Handle(r.Context(), commands.RegenerateRoundQuestionsInput{
		TenantID: identity.TenantID,
		RoundID:  roundID,
		Steering: body.Steering,
	}); err != nil {
		switch {
		case errors.Is(err, entities.ErrRoundNotFound):
			writeError(w, http.StatusNotFound, "round_not_found", "round not found")
		case errors.Is(err, commands.ErrRoundNotRegenerable):
			writeError(w, http.StatusConflict, "invalid_transition", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// MarkRoundCompleted handles POST /interview/rounds/{round_id}:mark-done.
func (h *InterviewHandler) MarkRoundCompleted(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	roundID, err := uuid.Parse(chi.URLParam(r, "round_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_round_id", "round_id must be a uuid")
		return
	}
	if err := h.deps.MarkRoundCompleted.Handle(r.Context(), commands.MarkRoundCompletedInput{
		TenantID:    identity.TenantID,
		ActorUserID: identity.RecruiterID.UUID(),
		RoundID:     roundID,
	}); err != nil {
		switch {
		case errors.Is(err, repositories.ErrProcessNotFound):
			writeError(w, http.StatusNotFound, "round_not_found", "round not found")
		case errors.Is(err, commands.ErrRoundInvalidTransition):
			writeError(w, http.StatusConflict, "invalid_transition", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// MarkRoundSkipped handles POST /interview/rounds/{round_id}:skip.
func (h *InterviewHandler) MarkRoundSkipped(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	roundID, err := uuid.Parse(chi.URLParam(r, "round_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_round_id", "round_id must be a uuid")
		return
	}
	if err := h.deps.MarkRoundSkipped.Handle(r.Context(), commands.MarkRoundSkippedInput{
		TenantID:    identity.TenantID,
		ActorUserID: identity.RecruiterID.UUID(),
		RoundID:     roundID,
	}); err != nil {
		switch {
		case errors.Is(err, repositories.ErrProcessNotFound):
			writeError(w, http.StatusNotFound, "round_not_found", "round not found")
		case errors.Is(err, commands.ErrRoundInvalidTransition):
			writeError(w, http.StatusConflict, "invalid_transition", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Handler tests**

Create `internal/interview/delivery/http/v1/handlers_test.go`. Build a handler with in-memory fakes for each command + query handler. Tests cover the routing + error-mapping; the command logic itself is unit-tested in T10-T13.

Cover at minimum:

- `TestUpsertLoopTemplate_Returns204` + `_NoAuth_401` + `_BadIntentID_400` + `_InvalidRoundKind_400`.
- `TestGetLoopTemplate_ReturnsTemplate` + `_NoAuth_401` + `_BadIntentID_400`.
- `TestListInterviewProcesses_Returns200WithList` + `_FilterByStatus`.
- `TestGetInterviewProcess_Returns200` + `_NotFound_404` + `_BadProcessID_400`.
- `TestCompleteProcess_Returns204` + `_InvalidTransition_409` + `_NotFound_404`.
- `TestCancelProcess_Returns204`.
- `TestRecordFeedback_Returns201` + `_BadDecision_400` + `_MissingName_400`.
- `TestRegenerateRoundQuestions_Returns202` + `_TerminalRound_409` + `_NotFound_404`.
- `TestMarkRoundCompleted_Returns204` + `_InvalidTransition_409`.
- `TestMarkRoundSkipped_Returns204`.

Use a `withIdentity` test helper (same shape as the sourcing `handlers_test.go`).

- [ ] **Step 5: Verify + commit**

```
go test ./internal/interview/delivery/http/v1/... -count=1 -race
make build
git add internal/interview/delivery/
git commit -m "feat(interview): HTTP v1 delivery layer (10 endpoints)"
```

---

## Task 15: Worker pool + subscriber + main.go wiring

**Files:**
- Create: `internal/interview/infrastructure/worker/question_generation_pool.go` + `_test.go`
- Create: `internal/interview/infrastructure/subscribers/application_shortlisted_consumer.go` + `_test.go`
- Modify: `cmd/api/main.go`
- Modify: `developer.md`

- [ ] **Step 1: QuestionGenerationPool**

Create `internal/interview/infrastructure/worker/question_generation_pool.go`:

```go
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

// Config controls pool size and polling cadence.
type Config struct {
	Size         int
	PollInterval time.Duration
}

// QuestionGenerationPool repeatedly claims a Pending round and dispatches
// GenerateRoundQuestions. Same shape as sourcing.MatchPool and JudgePool.
type QuestionGenerationPool struct {
	processes repositories.ProcessRepository
	handler   *commands.GenerateRoundQuestionsHandler
	cfg       Config
	logger    zerolog.Logger
}

// NewQuestionGenerationPool wires the pool.
func NewQuestionGenerationPool(
	processes repositories.ProcessRepository,
	handler *commands.GenerateRoundQuestionsHandler,
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
```

Create `question_generation_pool_test.go`:

```go
// Tests use a fake ProcessRepository that returns one claimable round
// then ErrProcessNotFound thereafter, and a fake GenerateRoundQuestionsHandler
// that records each invocation. Tests run the pool for ~50ms then cancel
// and assert exactly one Handle call happened.
```

Tests:

- `TestPool_ClaimsAndDispatches` — fake repo serves one round; run for 200ms; assert exactly 1 handler call.
- `TestPool_NothingClaimable_DoesNotCallHandler` — fake repo always returns ErrProcessNotFound; run for 100ms; assert 0 calls.
- `TestPool_ContinuesAfterHandlerError` — fake handler returns an error on first call, success on second; assert 2 calls and pool didn't crash.
- `TestPool_RespectsContextCancel` — start the pool, cancel context, assert Run returns within poll-interval+50ms.

Note: the slice-1 handler design saves state on every outcome (success / retry-scheduled / abort), so a fake repo returning the same row repeatedly would have it cycle through states. The test fake should track state transitions OR simply return ErrProcessNotFound after the first claim to keep tests deterministic.

- [ ] **Step 2: ApplicationShortlistedConsumer**

Create `internal/interview/infrastructure/subscribers/application_shortlisted_consumer.go`:

```go
// Package subscribers wires the interview context to events published on the
// in-process bus by other contexts.
package subscribers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/application/commands"
)

// shortlistedPayload mirrors the JSON shape of sourcing.ApplicationShortlisted.
// We don't import the sourcing event struct (cross-context isolation); we
// unmarshal the bus payload into a local shape with the fields we need.
type shortlistedPayload struct {
	ApplicationID uuid.UUID `json:"application_id"`
	CandidateID   uuid.UUID `json:"candidate_id"`
	IntentID      uuid.UUID `json:"intent_id"`
	TenantID      string    `json:"tenant_id"`
}

// ApplicationShortlistedConsumer translates sourcing.ApplicationShortlisted
// events into StartInterviewProcess commands.
type ApplicationShortlistedConsumer struct {
	start  *commands.StartInterviewProcessHandler
	logger zerolog.Logger
}

// NewApplicationShortlistedConsumer wires the consumer.
func NewApplicationShortlistedConsumer(start *commands.StartInterviewProcessHandler, logger zerolog.Logger) *ApplicationShortlistedConsumer {
	return &ApplicationShortlistedConsumer{start: start, logger: logger.With().Str("component", "application_shortlisted_consumer").Logger()}
}

// Handle is the eventbus.Handler entry point. The bus publishes the event
// struct directly; we re-marshal then unmarshal into our local payload to
// avoid cross-context Go imports.
func (c *ApplicationShortlistedConsumer) Handle(ctx context.Context, event any) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	var p shortlistedPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}
	if p.ApplicationID == uuid.Nil || p.CandidateID == uuid.Nil || p.IntentID == uuid.Nil {
		return errors.New("incomplete payload")
	}
	tenantID, err := shared.ParseTenantID(p.TenantID)
	if err != nil {
		return fmt.Errorf("tenant: %w", err)
	}
	return c.start.Handle(ctx, commands.StartInterviewProcessInput{
		TenantID:      tenantID,
		ApplicationID: p.ApplicationID,
		CandidateID:   p.CandidateID,
		IntentID:      p.IntentID,
	})
}
```

Test (`application_shortlisted_consumer_test.go`):

- `TestConsumer_HappyPath_CallsStartHandler` — fake start handler captures the input; pass a struct with the expected fields; assert capture.
- `TestConsumer_MissingFields_ReturnsErr` — payload with one nil UUID.
- `TestConsumer_BadTenantID_ReturnsErr` — payload with `tenant_id: "not-a-uuid"`.

- [ ] **Step 3: cmd/api/main.go wiring**

Insert a new block after the sourcing wiring section. Sketch:

```go
// ============================================================================
// Interview context (slice 1) — question generation + recruiter feedback.
// ============================================================================

// Repositories.
interviewProcessRepo := interviewpersist.NewPostgresProcessRepository(pool)
interviewTemplateRepo := interviewpersist.NewPostgresLoopTemplateRepository(pool)
interviewFeedbackRepo := interviewpersist.NewPostgresFeedbackRepository(pool)
interviewOutboxAppender := interviewmsg.NewPostgresOutboxAppender(pool)

// Cross-context readers.
interviewIntentReader := interviewclients.NewPostgresIntentReader(pool)
interviewCandidateReader := interviewclients.NewPostgresCandidateReader(pool)

// LLM-backed question generator. Reuses the existing Anthropic client.
interviewGenerator := interviewgen.NewAnthropicQuestionGenerator(anthropicClient.SDK(), anthropicCfg.Model)

// Command + query handlers.
startInterviewProcess := interviewcommands.NewStartInterviewProcessHandler(interviewProcessRepo, interviewTemplateRepo)
upsertLoopTemplate := interviewcommands.NewUpsertLoopTemplateHandler(interviewTemplateRepo, auditWriter)
generateRoundQuestions := interviewcommands.NewGenerateRoundQuestionsHandler(
	interviewProcessRepo, interviewIntentReader, interviewCandidateReader, interviewGenerator,
)
regenerateRoundQuestions := interviewcommands.NewRegenerateRoundQuestionsHandler(interviewProcessRepo)
recordFeedback := interviewcommands.NewRecordFeedbackHandler(interviewFeedbackRepo, interviewProcessRepo, auditWriter, interviewOutboxAppender)
markRoundCompleted := interviewcommands.NewMarkRoundCompletedHandler(interviewProcessRepo, auditWriter)
markRoundSkipped := interviewcommands.NewMarkRoundSkippedHandler(interviewProcessRepo, auditWriter)
completeProcess := interviewcommands.NewCompleteProcessHandler(interviewProcessRepo, auditWriter)
cancelProcess := interviewcommands.NewCancelProcessHandler(interviewProcessRepo, auditWriter)

getInterviewProcess := interviewqueries.NewGetInterviewProcessHandler(interviewProcessRepo, interviewFeedbackRepo, auditWriter)
listInterviewProcesses := interviewqueries.NewListInterviewProcessesHandler(interviewProcessRepo)
getLoopTemplate := interviewqueries.NewGetLoopTemplateHandler(interviewTemplateRepo)

// HTTP handler.
interviewHandler := interviewhttp.NewInterviewHandler(interviewhttp.InterviewHandlerDeps{
	UpsertTemplate:           upsertLoopTemplate,
	RecordFeedback:           recordFeedback,
	MarkRoundCompleted:       markRoundCompleted,
	MarkRoundSkipped:         markRoundSkipped,
	CompleteProcess:          completeProcess,
	CancelProcess:            cancelProcess,
	RegenerateRoundQuestions: regenerateRoundQuestions,
	GetInterviewProcess:      getInterviewProcess,
	ListInterviewProcesses:   listInterviewProcesses,
	GetLoopTemplate:          getLoopTemplate,
	Logger:                   logger,
})

// Outbox + dispatcher (own table + own dispatcher).
interviewPub := interviewmsg.NewBusPublisher(bus)
interviewDispatcher := interviewmsg.NewOutboxDispatcher(pool, interviewPub, logger, interviewmsg.DispatcherConfig{})

// Worker pool.
interviewPoolSize := getenvInt("INTERVIEW_QGEN_POOL", 2)
interviewPollInterval := getenvDuration("INTERVIEW_QGEN_POLL", time.Second)
interviewWorker := interviewworker.NewQuestionGenerationPool(
	interviewProcessRepo, generateRoundQuestions,
	interviewworker.Config{Size: interviewPoolSize, PollInterval: interviewPollInterval},
	logger,
)

// Subscriber.
appShortlistedConsumer := interviewsubs.NewApplicationShortlistedConsumer(startInterviewProcess, logger)
bus.Subscribe("sourcing.ApplicationShortlisted", appShortlistedConsumer.Handle)
```

Add the imports at the top of `cmd/api/main.go`:

```go
interviewcommands "github.com/hustle/hireflow/internal/interview/application/commands"
interviewdto "github.com/hustle/hireflow/internal/interview/application/dto"           // if referenced
interviewqueries "github.com/hustle/hireflow/internal/interview/application/queries"
interviewclients "github.com/hustle/hireflow/internal/interview/infrastructure/clients"
interviewgen "github.com/hustle/hireflow/internal/interview/infrastructure/generation"
interviewmsg "github.com/hustle/hireflow/internal/interview/infrastructure/messaging"
interviewpersist "github.com/hustle/hireflow/internal/interview/infrastructure/persistence"
interviewsubs "github.com/hustle/hireflow/internal/interview/infrastructure/subscribers"
interviewworker "github.com/hustle/hireflow/internal/interview/infrastructure/worker"
interviewhttp "github.com/hustle/hireflow/internal/interview/delivery/http/v1"
```

Remove the `interviewdto` import if it's not used directly in main.go (it usually isn't — only commands/queries reference it).

Add a `getenvDuration` helper in main.go if one doesn't exist:

```go
func getenvDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
```

Mount the routes and start the dispatcher + worker pool in the existing goroutine-spawn block:

```go
interviewhttp.Mount(router, interviewHandler)

go interviewDispatcher.Run(ctx)
go interviewWorker.Run(ctx)
```

- [ ] **Step 4: Developer doc**

In `developer.md`, near the existing `MIGRATE_SHARED` note from slice 4, add:

```markdown
- `migrations/interview/` — interview context (slice 1+). Migration target: `make MIGRATE_INTERVIEW`.
  Env vars: `INTERVIEW_QGEN_POOL` (default 2), `INTERVIEW_QGEN_POLL` (default 1s).
```

- [ ] **Step 5: Verify + commit**

```
make build
go test ./... -count=1 -race
export DATABASE_URL="postgres://hireflow:hireflow@localhost:5433/hireflow?sslmode=disable"
go test -tags=integration ./internal/interview/... -count=1 -race
git add internal/interview/infrastructure/worker/ \
        internal/interview/infrastructure/subscribers/ \
        cmd/api/main.go developer.md
git commit -m "feat(interview): wire question-generation pool + ApplicationShortlisted subscriber + main.go"
```

---

## Task 16: e2e integration test

**Files:**
- Create: `tests/interview_slice1_e2e_test.go`

- [ ] **Step 1: e2e test**

Create `tests/interview_slice1_e2e_test.go` with `//go:build integration` and `package tests`. Reuse helpers already defined in the slice 1-4 e2e files (do NOT redeclare): `newPgvectorPool`, `stubParser`, `stubOCR`, `helloPDFBytes`, `writeMultipart`, `insertHiringIntentForSlice3`, `stubJudge`. Add a new helper for stubbing the interview generator:

```go
type stubGenerator struct {
	questions []vo.Question
	err       error
}

func (s stubGenerator) Generate(_ context.Context, _ services.GenerationInput) ([]vo.Question, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.questions, nil
}
```

Test body (`TestInterviewSlice1_E2E`):

1. **Setup**: bring up pgvectorPool, build a confirmed hiring intent (reuse `insertHiringIntentForSlice3`), construct sourcing scoring infra with stubJudge (mirroring slice 4's setup minus the recruiter-action wiring we don't need beyond shortlist), set up interview infra with the new `stubGenerator` returning a fixed 3-question array.
2. **Wire the bus subscription** for `sourcing.ApplicationShortlisted` → `ApplicationShortlistedConsumer.Handle`.
3. **Start dispatchers** (sourcing outbox + interview outbox) and workers (sourcing match + judge pools + interview qgen pool).
4. **Upload + score**: POST a resume; wait for the Application to reach Scored status (reuse the slice-4 e2e wait helpers).
5. **Shortlist**: POST `/api/v1/applications/{id}:shortlist` (sourcing endpoint). Wait for the `sourcing.ApplicationShortlisted` event to be dispatched.
6. **Assert process created**: poll `GET /interview/processes` (filter by intent_id) until one process appears. Assert it has 3 rounds (DefaultLoop). Each round starts in Pending.
7. **Wait for generation**: poll `GET /interview/processes/{id}` until all 3 rounds reach `QuestionsReady`. Assert each round has the 3 stubGenerator questions.
8. **POST feedback** on round 1: `POST /interview/rounds/{r1}/feedback` with `decision: "yes"`. Assert 201.
9. **Mark round 1 done**: `POST /interview/rounds/{r1}:mark-done`. Assert 204; reload process; assert round 1 is `Completed`.
10. **Skip round 2**: `POST /interview/rounds/{r2}:skip`. Assert 204.
11. **Regenerate round 3**: `POST /interview/rounds/{r3}:regenerate`. Assert 202; poll until round 3 is back at `QuestionsReady` (worker re-fires generation).
12. **Mark round 3 done**.
13. **Complete process**: `POST /interview/processes/{id}:complete`. Assert 204; reload; status `Completed`.
14. **Verify audit rows**: assert via raw SQL that audit_log has entries for `interview_process_read`, `interview_round_feedback_recorded`, `interview_round_completed`, `interview_round_skipped`, `interview_process_completed` (one per action).
15. **Verify outbox events**: assert `interview_outbox` has rows for `interview.InterviewProcessCreated` (×1), `interview.InterviewQuestionsGenerated` (×3 from initial generation + ×1 from regenerate = 4), `interview.InterviewFeedbackRecorded` (×1).

Use the same router-middleware pattern as slice-4 e2e to inject identity on every request:

```go
identity := auth.Identity{TenantID: tenant, RecruiterID: shared.NewRecruiterID()}
router.Use(func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), identity)))
	})
})
```

Wire BOTH the sourcing v1 router AND the interview v1 router on the same chi.Router; tests hit `/applications/{id}:shortlist` (sourcing) and `/interview/...` (interview) through the same `httptest.NewServer`.

Use `t.Helper()` liberally; budget the test for ~10s (it has multiple polling waits — the qgen worker fires ~1s after the row is created). Add a context timeout of 30s on the outer test.

- [ ] **Step 2: Verify + commit**

```
export DATABASE_URL="postgres://hireflow:hireflow@localhost:5433/hireflow?sslmode=disable"
INTEGRATION_TESTS=1 go test -tags=integration ./tests/... -run TestInterviewSlice1_E2E -v -count=1
git add tests/interview_slice1_e2e_test.go
git commit -m "test(interview): slice-1 e2e — shortlist → process → questions → feedback → completed"
```

---

## Task 17: README + module docs + OpenAPI

**Files:**
- Modify: `README.md` — add interview row.
- Create: `docs/modules/interview/README.md`
- Create: `docs/api/v1/interview.openapi.yaml`

- [ ] **Step 1: Root README**

In `README.md`, add a row to the bounded-contexts table (after the sourcing row):

```
| `interview` | Interview-process orchestration: subscribes to `ApplicationShortlisted`, creates loop processes from per-intent templates (or a sensible default), generates tailored questions per round via the existing Anthropic client, captures recruiter feedback. Slice 1 (foundation). | **Live (foundation)** |
```

- [ ] **Step 2: Module README**

Create `docs/modules/interview/README.md`:

```markdown
# interview — bounded context

Slice 1: a new context that takes over after sourcing produces a shortlist.
Subscribes to `sourcing.ApplicationShortlisted`, creates an `InterviewProcess`
with rounds from a per-intent `LoopTemplate` (or a hardcoded default), and
asynchronously generates structured interview questions per round.

## Ubiquitous language

| Term | Meaning |
|---|---|
| **InterviewProcess** | The aggregate created for one shortlisted application. Owns its rounds. |
| **InterviewRound** | One slot in the loop (e.g., "Senior Backend, screen, sequence 1"). State machine driven. |
| **LoopTemplate** | Per-intent definition of the rounds an `InterviewProcess` inherits when created. |
| **Question** | One LLM-generated probe carrying prompt, skill_probed, why, expected_signals, model_answer, red_flags, follow_ups. |
| **Feedback** | One recruiter-entered scorecard per (round, interviewer). Append-only. |
| **RoundKind** | Fixed enum: screen, technical, system_design, behavioral, bar_raiser. |
| **FeedbackDecision** | Fixed enum: strong_yes, yes, mixed, no, strong_no. |

## Pipeline

```
ApplicationShortlisted ──► ApplicationShortlistedConsumer ──► StartInterviewProcess
                                                                       │
                                                                       ▼
                                                        InterviewProcess(rounds: Pending)
                                                                       │
                                          QuestionGenerationPool ──────┘
                                                  │
                                  per round, claim ─►
                                                  │
                          IntentReader + CandidateReader + AnthropicQuestionGenerator
                                                  │
                                                  ▼
                                    InterviewRound: QuestionsReady
                                                  │
                              recruiter conducts the round (out-of-band)
                                                  │
                                  POST /interview/rounds/{id}/feedback ─► append row
                                                  │
                                POST /interview/rounds/{id}:mark-done ─► Completed
                                                  │
                  (after all rounds Completed or Skipped)
                                                  │
                          POST /interview/processes/{id}:complete ─► Process: Completed
```

## Endpoints (slice 1)

| Method | Path | Purpose |
|---|---|---|
| PUT | `/intents/{intent_id}/loop-template` | Upsert a per-intent loop. |
| GET | `/intents/{intent_id}/loop-template` | Return the template or default. |
| GET | `/intents/{intent_id}/interview-processes` | List processes for an intent. |
| GET | `/interview/processes/{id}` | Get process + rounds + feedback summary. |
| POST | `/interview/processes/{id}:complete` | Complete the process. |
| POST | `/interview/processes/{id}:cancel` | Cancel the process. |
| POST | `/interview/rounds/{id}/feedback` | Append feedback. |
| POST | `/interview/rounds/{id}:regenerate` | Reset round → Pending for re-generation. |
| POST | `/interview/rounds/{id}:mark-done` | Round → Completed. |
| POST | `/interview/rounds/{id}:skip` | Round → Skipped. |

## Env vars

| Var | Default | Purpose |
|---|---|---|
| `INTERVIEW_QGEN_POOL` | `2` | Worker pool size for question generation. |
| `INTERVIEW_QGEN_POLL` | `1s` | Worker poll interval. |
| `ANTHROPIC_API_KEY` | — | Shared with sourcing/hiringintent. Required. |
| `ANTHROPIC_MODEL` | `claude-opus-4-7` | Used by the question generator. |

## Out of scope

See `docs/superpowers/specs/2026-05-14-interview-slice-1-question-generation-design.md` §Out of scope.
```

- [ ] **Step 3: OpenAPI spec**

Create `docs/api/v1/interview.openapi.yaml`:

```yaml
openapi: 3.1.0
info:
  title: hireflow — interview (slice 1)
  version: "1.0.0-slice1"
  description: |
    Interview-process orchestration: subscribes to ApplicationShortlisted,
    generates tailored questions per round, captures recruiter feedback.
paths:
  /intents/{intent_id}/loop-template:
    put:
      summary: Upsert a per-intent loop template
      tags: [loop-template]
      security: [{ bearerAuth: [] }]
      parameters:
        - { name: intent_id, in: path, required: true, schema: { type: string, format: uuid } }
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: '#/components/schemas/UpsertLoopTemplateRequest' }
      responses:
        '204': { description: upserted }
        '400': { $ref: '#/components/responses/Error' }
        '401': { $ref: '#/components/responses/Error' }
    get:
      summary: Get the per-intent loop template
      tags: [loop-template]
      security: [{ bearerAuth: [] }]
      parameters:
        - { name: intent_id, in: path, required: true, schema: { type: string, format: uuid } }
      responses:
        '200':
          description: template
          content:
            application/json:
              schema: { $ref: '#/components/schemas/LoopTemplateResponse' }
  /intents/{intent_id}/interview-processes:
    get:
      summary: List interview processes for an intent
      tags: [process]
      security: [{ bearerAuth: [] }]
      parameters:
        - { name: intent_id, in: path, required: true, schema: { type: string, format: uuid } }
        - { name: status, in: query, schema: { type: string } }
      responses:
        '200':
          description: list
          content:
            application/json:
              schema: { $ref: '#/components/schemas/ListProcessesResponse' }
  /interview/processes/{process_id}:
    get:
      summary: Get interview process detail
      tags: [process]
      security: [{ bearerAuth: [] }]
      parameters:
        - { name: process_id, in: path, required: true, schema: { type: string, format: uuid } }
      responses:
        '200':
          description: process
          content:
            application/json:
              schema: { $ref: '#/components/schemas/InterviewProcessResponse' }
        '404': { $ref: '#/components/responses/Error' }
  /interview/processes/{process_id}:complete:
    post:
      summary: Complete an interview process
      tags: [process]
      security: [{ bearerAuth: [] }]
      parameters:
        - { name: process_id, in: path, required: true, schema: { type: string, format: uuid } }
      responses:
        '204': { description: completed }
        '409': { $ref: '#/components/responses/Error' }
        '404': { $ref: '#/components/responses/Error' }
  /interview/processes/{process_id}:cancel:
    post:
      summary: Cancel an interview process
      tags: [process]
      security: [{ bearerAuth: [] }]
      parameters:
        - { name: process_id, in: path, required: true, schema: { type: string, format: uuid } }
      responses:
        '204': { description: cancelled }
  /interview/rounds/{round_id}/feedback:
    post:
      summary: Append feedback to a round
      tags: [feedback]
      security: [{ bearerAuth: [] }]
      parameters:
        - { name: round_id, in: path, required: true, schema: { type: string, format: uuid } }
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: '#/components/schemas/RecordFeedbackRequest' }
      responses:
        '201': { description: created }
        '400': { $ref: '#/components/responses/Error' }
  /interview/rounds/{round_id}:regenerate:
    post:
      summary: Reset a round to Pending so questions are regenerated
      tags: [round]
      security: [{ bearerAuth: [] }]
      parameters:
        - { name: round_id, in: path, required: true, schema: { type: string, format: uuid } }
      requestBody:
        required: false
        content:
          application/json:
            schema: { $ref: '#/components/schemas/RegenerateRoundRequest' }
      responses:
        '202': { description: queued }
        '409': { $ref: '#/components/responses/Error' }
  /interview/rounds/{round_id}:mark-done:
    post:
      summary: Mark a round as Completed
      tags: [round]
      security: [{ bearerAuth: [] }]
      parameters:
        - { name: round_id, in: path, required: true, schema: { type: string, format: uuid } }
      responses:
        '204': { description: completed }
        '409': { $ref: '#/components/responses/Error' }
  /interview/rounds/{round_id}:skip:
    post:
      summary: Skip a round (recruiter escape hatch)
      tags: [round]
      security: [{ bearerAuth: [] }]
      parameters:
        - { name: round_id, in: path, required: true, schema: { type: string, format: uuid } }
      responses:
        '204': { description: skipped }
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
  responses:
    Error:
      description: error
      content:
        application/json:
          schema: { $ref: '#/components/schemas/ErrorResponse' }
  schemas:
    UpsertLoopTemplateRequest:
      type: object
      required: [rounds]
      properties:
        rounds:
          type: array
          minItems: 1
          items:
            type: object
            required: [kind, sequence]
            properties:
              kind:
                type: string
                enum: [screen, technical, system_design, behavioral, bar_raiser]
              sequence: { type: integer, minimum: 1 }
    LoopTemplateResponse:
      type: object
      properties:
        intent_id: { type: string, format: uuid }
        rounds:
          type: array
          items:
            type: object
            properties:
              kind: { type: string }
              sequence: { type: integer }
        is_default: { type: boolean }
    InterviewProcessResponse:
      type: object
      properties:
        id: { type: string, format: uuid }
        application_id: { type: string, format: uuid }
        candidate_id: { type: string, format: uuid }
        intent_id: { type: string, format: uuid }
        status: { type: string }
        rounds:
          type: array
          items: { $ref: '#/components/schemas/InterviewRoundResponse' }
        created_at: { type: string, format: date-time }
        updated_at: { type: string, format: date-time }
    InterviewRoundResponse:
      type: object
      properties:
        id: { type: string, format: uuid }
        kind: { type: string }
        sequence: { type: integer }
        status: { type: string }
        questions:
          type: array
          items: { $ref: '#/components/schemas/Question' }
        attempt_count: { type: integer }
        last_error: { type: string }
        feedback_summary: { $ref: '#/components/schemas/FeedbackSummary' }
        created_at: { type: string, format: date-time }
        updated_at: { type: string, format: date-time }
    Question:
      type: object
      required: [prompt, skill_probed, why, expected_signals, model_answer, red_flags, follow_ups]
      properties:
        prompt: { type: string }
        skill_probed: { type: string }
        why: { type: string }
        expected_signals: { type: array, items: { type: string }, minItems: 3 }
        model_answer: { type: string }
        red_flags: { type: array, items: { type: string }, minItems: 2 }
        follow_ups: { type: array, items: { type: string }, minItems: 1 }
    FeedbackSummary:
      type: object
      properties:
        strong_yes: { type: integer }
        yes: { type: integer }
        mixed: { type: integer }
        no: { type: integer }
        strong_no: { type: integer }
        total: { type: integer }
        latest_decision: { type: string }
    ListProcessesResponse:
      type: object
      properties:
        processes:
          type: array
          items: { $ref: '#/components/schemas/InterviewProcessResponse' }
    RecordFeedbackRequest:
      type: object
      required: [interviewer_name, decision]
      properties:
        interviewer_name: { type: string, minLength: 1 }
        interviewer_email: { type: string, format: email }
        decision:
          type: string
          enum: [strong_yes, yes, mixed, no, strong_no]
        notes: { type: string }
    RegenerateRoundRequest:
      type: object
      properties:
        steering: { type: string }
    ErrorResponse:
      type: object
      properties:
        code: { type: string }
        message: { type: string }
```

- [ ] **Step 3: Verify + commit**

```
make build
git add README.md docs/modules/interview/ docs/api/v1/interview.openapi.yaml
git commit -m "docs(interview): module README + OpenAPI v1 for slice 1"
```

---

## Wrap-up

After all 17 tasks complete:

- [ ] `make test-unit` clean.
- [ ] `INTEGRATION_TESTS=1 make test-integration` — slice-1, slice-2, slice-3, slice-4 sourcing AND slice-1 interview e2e tests all pass against live Postgres.
- [ ] `go vet ./...` and `gofmt -l -s .` both clean.
- [ ] Smoke run `bin/api` with `ANTHROPIC_API_KEY` + `VOYAGE_API_KEY` + `SOURCING_PII_DEK` + `JWT_ACCESS_SECRET` set; confirm the interview dispatcher + worker pool start, no fatal log lines.

**What slice 1 ships:**

- New `internal/interview/` bounded context with five tables, three aggregates (LoopTemplate, InterviewProcess, append-only Feedback rows), and three domain events.
- Subscribes to `sourcing.ApplicationShortlisted`; creates an `InterviewProcess` with rounds from a per-intent template (`screen → technical → bar_raiser` default).
- LLM-backed `AnthropicQuestionGenerator` producing structured probes with `prompt`, `skill_probed`, `why`, `expected_signals`, `model_answer`, `red_flags`, `follow_ups`. Eager async generation via a worker pool mirroring the sourcing match + judge pools.
- 10 HTTP endpoints for template management, process listing/detail, feedback, and round/process lifecycle. Audit-log integration on every PII read and lifecycle transition.
- e2e integration test covering shortlist → process → generation → feedback → round-completion → process-completion.

**What slice 1 does NOT ship (deferred):**

- Voice / video AI interviewer (Slices B, C, D, E from the original decomposition).
- Per-answer LLM evaluation endpoint (the natural next slice — captured in spec Out of scope §).
- Magic-link external-interviewer feedback paths.
- Scheduling / calendar integration.
- Candidate-facing UX.
- LoopTemplate versioning (existing processes are not retroactively mutated).
- Per-tenant prompt customization.
- Aggregation rules / automatic Hire/Reject loop-back to sourcing.

After slice 1, the interview module is feature-complete for tenants who run their interviews themselves — using the generated questions and the `model_answer` / `red_flags` scaffolding as their real-time reference. The AI-conducted interview (Slice B) is the next step.
