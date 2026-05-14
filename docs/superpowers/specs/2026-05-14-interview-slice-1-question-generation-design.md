# Interview Module — Slice 1: Question Generation Design

> **Status:** approved 2026-05-14. Foundational slice of a multi-slice interview-module product line. Establishes the bounded context, the question-generation engine, and the recruiter-driven feedback loop.

## Why this slice

After slice 4, sourcing is feature-complete for the pre-seed pilot: recruiters upload resumes, the system scores and ranks candidates, recruiters shortlist. **What happens after a shortlist is undefined.** Today, recruiters drop into Notion / Google Docs / their head to figure out what to ask each candidate, run interviews ad-hoc, and capture notes wherever. There's no tenant-wide consistency, no per-role tailoring, and nothing the platform can build on.

This slice introduces a new bounded context — `internal/interview/` — that:

1. **Subscribes to `ApplicationShortlisted`** and creates a structured `InterviewProcess` per shortlisted candidate.
2. **Generates tailored interview questions** for each round using the existing Anthropic LLM client, conditioned on the intent's role spec and the candidate's parsed profile.
3. **Captures structured feedback** from the recruiter after they conduct the interview.

The bigger vision — AI-conducted voice and video interviews, multimodal analysis, automated Hire/Reject loop-back — is real, large, and explicitly **out of scope** for this slice. It is decomposed into subsequent slices (see *Future scope* below). Slice 1 ships the smallest meaningful surface and the foundation every later slice reuses (question generation is reused by the AI interviewer; the process/round/feedback aggregates are reused by every downstream slice).

## Bounded-context architecture

A new peer of `sourcing`, `hiringintent`, `jobposting`, `auth`:

```
internal/interview/
    domain/
        entities/        InterviewProcess, InterviewRound, LoopTemplate
        valueobjects/    RoundKind, ProcessStatus, RoundStatus, Feedback, Question, RetryDecision
        events/          InterviewProcessCreated, InterviewQuestionsGenerated, InterviewFeedbackRecorded
        repositories/    (ports)
        services/        (ports: QuestionGenerator, IntentReader, CandidateReader)
    application/
        commands/        StartInterviewProcess, GenerateRoundQuestions, RegenerateRoundQuestions,
                         RecordFeedback, MarkRoundCompleted, CompleteProcess, CancelProcess,
                         UpsertLoopTemplate
        queries/         GetInterviewProcess, ListInterviewProcesses, GetLoopTemplate
        dto/             input + output DTOs for commands/queries
    infrastructure/
        persistence/     PostgresProcessRepository, PostgresLoopTemplateRepository,
                         PostgresFeedbackRepository
        generation/      AnthropicQuestionGenerator
        clients/         IntentReader (wraps hiringintent.Reader), CandidateReader (wraps sourcing
                         repository — profile only, no PII)
        messaging/       OutboxDispatcher (own table, own dispatcher), BusPublisher
        worker/          QuestionGenerationPool (mirrors sourcing.MatchPool / JudgePool)
        subscribers/     ApplicationShortlistedConsumer
    delivery/
        http/v1/         handlers, dto, routes
migrations/interview/
    000001_create_interview_tables.up.sql
    000001_create_interview_tables.down.sql
```

**Hard rules:**

- `internal/interview/` does **not** import `internal/sourcing/...`. All cross-context reads go through the `IntentReader` / `CandidateReader` ports (anti-corruption layer). Adapters live in `internal/interview/infrastructure/clients/` and may call sourcing's existing read paths.
- All tables are tenant-scoped. Every row carries `tenant_id` and every query filters on it.
- No FK constraints across context boundaries. Cross-context identifiers (`application_id`, `candidate_id`, `intent_id`) are plain UUID columns, same convention slices 1-4 use.

**Inbound event:** `sourcing.ApplicationShortlisted` (already emitted by slice 4). A subscriber registered in `cmd/api/main.go` translates this into a `StartInterviewProcess` command.

**Outbound events (to `interview_outbox`):**

- `interview.InterviewProcessCreated` — fired on process creation. Carries `process_id, tenant_id, application_id, candidate_id, intent_id, occurred_at`.
- `interview.InterviewQuestionsGenerated` — fired per round when generation succeeds. Carries `round_id, process_id, kind, question_count, tenant_id, occurred_at`.
- `interview.InterviewFeedbackRecorded` — fired per feedback row. Carries `feedback_id, round_id, decision, tenant_id, occurred_at`.

No downstream consumer in slice 1. The events exist for future slices (AI interviewer subscribes to `QuestionsGenerated`; Hire/Reject loop-back to sourcing subscribes to a future `InterviewProcessConcluded`).

## Data model

Five tables in the `interview` migration namespace.

```sql
-- 1. Loop template — one per intent. Defines the shape of the interview process.
CREATE TABLE intent_loops (
    id          UUID         PRIMARY KEY,
    tenant_id   UUID         NOT NULL,
    intent_id   UUID         NOT NULL,
    rounds      JSONB        NOT NULL,   -- [{"kind":"screen","sequence":1}, ...]
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, intent_id),
    CONSTRAINT intent_loops_rounds_nonempty CHECK (jsonb_array_length(rounds) > 0)
);

-- 2. Process — one per shortlisted application.
CREATE TABLE interview_processes (
    id              UUID         PRIMARY KEY,
    tenant_id       UUID         NOT NULL,
    application_id  UUID         NOT NULL,
    candidate_id    UUID         NOT NULL,
    intent_id       UUID         NOT NULL,
    status          TEXT         NOT NULL,   -- New | InProgress | Completed | Cancelled
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, application_id),
    CONSTRAINT interview_processes_status_valid
        CHECK (status IN ('New','InProgress','Completed','Cancelled'))
);

CREATE INDEX interview_processes_intent_idx
    ON interview_processes (tenant_id, intent_id, status, created_at DESC);

-- 3. Round — one per round per process.
CREATE TABLE interview_rounds (
    id               UUID         PRIMARY KEY,
    tenant_id        UUID         NOT NULL,
    process_id       UUID         NOT NULL,
    kind             TEXT         NOT NULL,   -- screen | technical | system_design | behavioral | bar_raiser
    sequence         INT          NOT NULL,
    status           TEXT         NOT NULL,   -- Pending | QuestionsReady | InProgress | Completed | Skipped | GenerationFailed
    questions        JSONB,                   -- NULL until generation succeeds
    attempt_count    INT          NOT NULL DEFAULT 0,
    last_error       TEXT         NOT NULL DEFAULT '',
    next_attempt_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT interview_rounds_kind_valid
        CHECK (kind IN ('screen','technical','system_design','behavioral','bar_raiser')),
    CONSTRAINT interview_rounds_status_valid
        CHECK (status IN ('Pending','QuestionsReady','InProgress','Completed','Skipped','GenerationFailed')),
    CONSTRAINT interview_rounds_sequence_positive CHECK (sequence > 0),
    UNIQUE (tenant_id, process_id, sequence)
);

-- Worker poll index — claim next Pending round whose backoff has elapsed.
CREATE INDEX interview_rounds_pending_idx
    ON interview_rounds (next_attempt_at)
    WHERE status = 'Pending';

-- 4. Feedback — append-only. Multiple rows per round allowed (panel interviews).
CREATE TABLE interview_feedback (
    id                 UUID         PRIMARY KEY,
    tenant_id          UUID         NOT NULL,
    round_id           UUID         NOT NULL,
    interviewer_name   TEXT         NOT NULL,
    interviewer_email  TEXT         NOT NULL DEFAULT '',
    decision           TEXT         NOT NULL,   -- strong_yes | yes | mixed | no | strong_no
    notes              TEXT         NOT NULL DEFAULT '',
    submitted_by       UUID         NOT NULL,   -- recruiter who entered it
    submitted_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT interview_feedback_decision_valid
        CHECK (decision IN ('strong_yes','yes','mixed','no','strong_no')),
    CONSTRAINT interview_feedback_interviewer_name_nonempty CHECK (length(interviewer_name) > 0)
);

CREATE INDEX interview_feedback_round_idx
    ON interview_feedback (tenant_id, round_id, submitted_at DESC);

-- 5. Outbox — same shape as sourcing_outbox.
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

### Question shape

Stored as JSONB in `interview_rounds.questions`:

```json
[
  {
    "prompt": "Walk me through how you'd design a write-heavy event ingestion service handling 50k events/sec.",
    "skill_probed": "System Design",
    "why": "Candidate's resume claims 4y at Razorpay on payment ingestion — probe depth.",
    "expected_signals": [
      "Asks about durability vs latency trade-offs",
      "Reaches for Kafka or similar log-structured store",
      "Considers backpressure and partitioning"
    ],
    "follow_ups": [
      "How would you handle a downstream consumer being slow?",
      "What if a partition becomes hot?"
    ]
  }
]
```

The Anthropic adapter validates the shape (JSON-schema-style check inside the parser) before persisting. Malformed LLM output triggers a single retry with a "your last output was not valid JSON" suffix, then transitions the round to `GenerationFailed`.

### Default loop fallback

If a recruiter shortlists a candidate before defining `intent_loops` for the intent, the new process gets a hardcoded default loop: `screen → technical → bar_raiser`. The recruiter can override later by `PUT`ing a template; **existing processes do not retroactively change** (this is explicitly out of scope — see *Out of scope* below).

### Round kinds — fixed enum

For slice 1 the supported `RoundKind` values are:

| Value | Meaning |
|---|---|
| `screen` | Initial recruiter / hiring-manager screen. Probes role fit + interest. |
| `technical` | Coding / craft round. Probes hands-on ability. |
| `system_design` | Architecture / scaling round. Probes design judgment. |
| `behavioral` | STAR-style past-experience probes. |
| `bar_raiser` | Broader judgment / leadership / culture round. |

Each value has a corresponding prompt template the question generator uses. Adding new values is a migration + a new prompt template + tests.

### Feedback decision enum

| Value | Meaning |
|---|---|
| `strong_yes` | Definitely hire at this level. |
| `yes` | Lean hire. |
| `mixed` | Could be persuaded either way. |
| `no` | Lean no-hire. |
| `strong_no` | Definitely do not hire. |

Recruiter sees aggregated counts per round (no weighted scoring in slice 1). The hiring decision is made manually by the recruiter; slice 1 does **not** trigger sourcing's `:hire` / `:reject` endpoints automatically.

## Domain entities and invariants

### `InterviewProcess` (aggregate root)

```go
type InterviewProcess struct {
    id            uuid.UUID
    tenantID      shared.TenantID
    applicationID uuid.UUID
    candidateID   uuid.UUID
    intentID      uuid.UUID
    status        vo.ProcessStatus    // New | InProgress | Completed | Cancelled
    rounds        []*InterviewRound
    createdAt     time.Time
    updatedAt     time.Time
    pendingEvents []events.Event
}
```

**Invariants:**

- A process must have at least one round.
- Rounds' `sequence` field is contiguous, 1-indexed.
- A process cannot be `Completed` while any round is in `Pending`, `QuestionsReady`, `InProgress`, or `GenerationFailed` status.
- A process cannot transition out of a terminal state (`Completed`, `Cancelled`).
- `Cancel` is always allowed from non-terminal states.

### `InterviewRound`

```go
type InterviewRound struct {
    id              uuid.UUID
    kind            vo.RoundKind
    sequence        int
    status          vo.RoundStatus
    questions       []vo.Question   // nil until QuestionsReady
    attemptCount    int
    lastError       string
    nextAttemptAt   time.Time
}
```

**State machine:**

```
Pending ──generate ok────► QuestionsReady ──interview started─► InProgress ──done─► Completed
   │                            │                                  │
   │                            └─────────recruiter regenerates────┤
   └──N attempts failed─► GenerationFailed ◄────────────────────────┘
                                │
                                └──recruiter regenerates──► Pending
```

Plus: any non-terminal round can be `Skipped` (recruiter decision).

### `LoopTemplate`

```go
type LoopTemplate struct {
    id         uuid.UUID
    tenantID   shared.TenantID
    intentID   uuid.UUID
    rounds     []TemplateRound  // {Kind, Sequence}
}
```

**Invariants:** at least one round; sequences are contiguous and start at 1; no duplicate sequences. Validation in the constructor.

### `Feedback` (value object — append-only)

```go
type Feedback struct {
    InterviewerName  string
    InterviewerEmail string
    Decision         vo.FeedbackDecision
    Notes            string
    SubmittedBy      uuid.UUID
    SubmittedAt      time.Time
}
```

`InterviewerName` is required (length > 0); email is optional but if present must parse via `net/mail.ParseAddress`.

## Cross-context reads (ACL)

Two ports defined in `internal/interview/domain/services/`:

```go
type IntentReader interface {
    GetRoleSpec(ctx context.Context, tenant shared.TenantID, intentID uuid.UUID) (RoleSpec, error)
}

type CandidateReader interface {
    // GetProfileForQuestions returns the resume-derived profile fields used
    // by the question generator. Excludes encrypted PII (name/email/phone)
    // — questions don't need them, and we don't want to audit-write a PII
    // read for every generation event.
    GetProfileForQuestions(ctx context.Context, tenant shared.TenantID, candidateID uuid.UUID) (CandidateProfile, error)
}
```

Adapters:

- **`PostgresIntentReader`** (in `internal/interview/infrastructure/clients/`) — reads `hiring_intents.role` JSONB directly. Tenant-scoped. Wraps the existing slice-3 `sourcing.PostgresIntentReader` pattern.
- **`SourcingCandidateReader`** — reads `candidates.parsed_profile` JSONB. Skips the encrypted PII columns. Tenant-scoped.

`CandidateProfile` is an interview-context-local DTO. The `parsed_profile` JSONB is unmarshalled into it directly; if the schema_version is one the interview context doesn't recognize, the reader returns a typed error and the round goes to `GenerationFailed`.

## Question generation

### Port

```go
type QuestionGenerator interface {
    Generate(ctx context.Context, in GenerationInput) ([]Question, error)
}

type GenerationInput struct {
    RoundKind        vo.RoundKind
    RoleSpec         RoleSpec
    CandidateProfile CandidateProfile
    Steering         string  // optional recruiter steering for regenerations
}
```

### Anthropic adapter

`AnthropicQuestionGenerator` in `internal/interview/infrastructure/generation/`:

- Reuses the existing `internal/shared/infrastructure/llm/anthropic.Client`.
- Per-`RoundKind` prompt template. Each template:
  1. Names the round type and what it should probe.
  2. Embeds the role spec (formatted as a brief).
  3. Embeds the candidate profile (skills, experiences, education).
  4. Specifies the output JSON schema (matches the `Question` struct).
  5. Includes a fixed number of `n` questions to generate (default 6 for technical/system_design, 4 for screen/behavioral/bar_raiser; configurable via env).
  6. Includes steering text if provided.
- Uses Anthropic's structured output (tool-use forced response) to get clean JSON.
- Validates: parses JSON, asserts each question has the required fields, asserts count is in range, asserts `skill_probed` references something plausible from the role spec.

### Generation worker pool

`QuestionGenerationPool` in `internal/interview/infrastructure/worker/`. Identical pattern to `sourcing.MatchPool` and `sourcing.JudgePool`:

- Polls `interview_rounds WHERE status='Pending' AND next_attempt_at <= now()`.
- Claims one row at a time (slice-1 uses the same load-then-save pattern; FOR UPDATE SKIP LOCKED is a future hardening).
- Dispatches `GenerateRoundQuestions` command. The command:
  1. Loads the round + its parent process.
  2. Reads the intent's role spec and the candidate's profile (via the ACL readers).
  3. Calls the `QuestionGenerator`.
  4. On success: stores `questions`, transitions to `QuestionsReady`, emits `InterviewQuestionsGenerated`, saves.
  5. On failure: increments `attempt_count`, sets `last_error`, schedules retry via the retry decision table below.

Config: pool size (default 2), poll interval (default 1s) — env vars `INTERVIEW_QGEN_POOL` and `INTERVIEW_QGEN_POLL`.

### Retry decisions

| Failure | Decision | Backoff schedule |
|---|---|---|
| transient (5xx, 429, timeout) | retry | `[1m, 5m, 15m, 1h, 4h]` |
| LLM auth / permission (401/403) | abort → `GenerationFailed` | none |
| invalid JSON output (parser failure) | retry once with "your previous output was not valid JSON" suffix | `[30s]`, then abort |
| any other unknown | retry up to 3x | `[1m, 5m, 15m]` |

After the backoff schedule is exhausted: round → `GenerationFailed`. Recruiter can hit `:regenerate` to reset.

## Commands

| Command | Trigger | Effect |
|---|---|---|
| `StartInterviewProcess` | `ApplicationShortlistedConsumer` | Loads loop template (or default), creates process + N rounds in `Pending`, saves, emits `InterviewProcessCreated`. Idempotent on `(tenant_id, application_id)`. |
| `GenerateRoundQuestions` | `QuestionGenerationPool` | Generates questions for one round; see retry table. |
| `RegenerateRoundQuestions` | `POST /interview/rounds/{id}:regenerate` | Resets `attempt_count`, sets steering text, transitions any `GenerationFailed` or `QuestionsReady` round back to `Pending`. |
| `RecordFeedback` | `POST /interview/rounds/{id}/feedback` | Appends a feedback row, writes audit log, emits `InterviewFeedbackRecorded`. Audit-write failure propagates as 500 (same load-bearing semantics as slice 4). |
| `MarkRoundCompleted` | `POST /interview/rounds/{id}:mark-done` | Transitions round to `Completed` (only from `QuestionsReady` or `InProgress`). |
| `CompleteProcess` | `POST /interview/processes/{id}:complete` | Asserts all rounds are terminal; transitions process to `Completed`. |
| `CancelProcess` | `POST /interview/processes/{id}:cancel` | Transitions process to `Cancelled` from any non-terminal state. |
| `UpsertLoopTemplate` | `PUT /intents/{intent_id}/loop-template` | Creates or replaces the template. Does not retroactively mutate existing processes. |

## Queries

- `GetInterviewProcess(tenant, processID)` — returns process + rounds + per-round feedback summary (count by decision, latest decision). Writes an audit row (`interview_process_read`).
- `ListInterviewProcesses(tenant, filter)` — by `intent_id`, by `status`, paginated. No audit (high-volume).
- `GetLoopTemplate(tenant, intentID)` — returns the template, or a sentinel indicating the default loop applies.

## HTTP API

All routes require authenticated identity (the existing JWT middleware in `internal/shared/infrastructure/auth`). Tenant scoping is enforced from `identity.TenantID` everywhere.

```
PUT    /intents/{intent_id}/loop-template          UpsertLoopTemplate            204
GET    /intents/{intent_id}/loop-template          GetLoopTemplate               200 / 404
GET    /intents/{intent_id}/interview-processes    ListInterviewProcesses        200
GET    /interview/processes/{process_id}           GetInterviewProcess           200 / 404
POST   /interview/processes/{process_id}:complete  CompleteProcess               204 / 409
POST   /interview/processes/{process_id}:cancel    CancelProcess                 204 / 409
POST   /interview/rounds/{round_id}/feedback       RecordFeedback                201
POST   /interview/rounds/{round_id}:regenerate     RegenerateRoundQuestions      202
POST   /interview/rounds/{round_id}:mark-done      MarkRoundCompleted            204 / 409
```

Status codes follow slice-4 conventions: `401` on missing identity, `400` on malformed UUIDs / bodies, `404` on missing tenant-scoped row, `409` on invalid state transitions, `500` on audit-write failure or unhandled errors.

DTOs live in `internal/interview/delivery/http/v1/dto.go`. The `Question` shape on the wire mirrors the JSONB shape stored in DB (no transformation).

## Audit-log integration

Consistent with slice 4's load-bearing semantics. The shared `audit.AuditWriter` (from `internal/shared/audit/`) is reused. Audit rows are written for:

- `interview_process_read` — every successful `GetInterviewProcess` call (returns candidate-derived data).
- `interview_round_feedback_recorded` — every `RecordFeedback` call.
- `interview_round_completed` / `interview_round_skipped` — round state changes.
- `interview_process_completed` / `interview_process_cancelled` — process state changes.
- `interview_loop_template_upserted` — template changes.

Audit failure on write returns 500 to the caller, after the underlying state change has already been committed. This is the same trade-off slice 4 made and is documented as a known follow-up across both contexts.

## Wiring (`cmd/api/main.go`)

Adds the following blocks:

1. **Repositories** — `NewPostgresProcessRepository(pool)`, `NewPostgresLoopTemplateRepository(pool)`, `NewPostgresFeedbackRepository(pool)`.
2. **Cross-context readers** — `NewPostgresIntentReader(pool)` (interview-context version), `NewSourcingCandidateReader(candidateRepo, piiEncryptor)`. The candidate reader is constructed with a reference to the sourcing `CandidateRepository`; this is the only place where wiring crosses contexts. Internally, both ports stay isolated.
3. **Generator** — `NewAnthropicQuestionGenerator(anthropicClient, cfg)` with per-kind prompt templates loaded from `internal/interview/infrastructure/generation/prompts/`.
4. **Audit writer** — the existing `auditWriter` from slice 4.
5. **Command handlers** — one per command.
6. **HTTP handler** — `interview.NewHandler(...Deps)`. Mount via a new `interview/delivery/http/v1.Mount(router, handler)` call.
7. **Outbox + dispatcher** — `interview.NewBusPublisher(bus)`, `interview.NewOutboxDispatcher(pool, pub, logger, cfg)`.
8. **Worker pool** — `interview.NewQuestionGenerationPool(...)`. Started under the same lifecycle pattern as `match.Pool` / `judge.Pool`.
9. **Subscriber** — `interview.NewApplicationShortlistedConsumer(startInterviewProcessHandler, logger)`; `bus.Subscribe("sourcing.ApplicationShortlisted", consumer.Handle)`.

The Makefile gains `MIGRATE_INTERVIEW` (mirrors `MIGRATE_SHARED`), chained into `migrate-up` after sourcing.

## Testing strategy

- **Unit tests** in each layer:
  - Domain: state-machine transitions, invariant checks (e.g., "completing a process with pending rounds fails"), feedback validation.
  - Question prompt builder: snapshot test for each round kind with a fixed role spec + candidate profile.
  - Retry decision logic: same shape as slice-3 retry tests.
- **Integration tests** (`//go:build integration`):
  - Repository round-trips for all four aggregates (process, round, template, feedback).
  - Outbox event dispatch — assert each of the three new event names decodes correctly via the dispatcher.
  - `BatchExistsForTenant`-style scoped lookup tests for cross-tenant isolation.
- **End-to-end test** (`tests/interview_slice1_e2e_test.go`):
  1. Seed an intent + candidate + a Scored Application (reuse slice-3 helpers).
  2. POST `:shortlist` (sourcing endpoint from slice 4).
  3. Wait for the consumer to create an `InterviewProcess` with default rounds.
  4. Stub the `QuestionGenerator` with a canned 3-question response (mirrors the slice-3 `stubJudge` pattern); worker fires, populates round 1, transitions to `QuestionsReady`.
  5. GET the process → assert structure.
  6. POST feedback for round 1 → assert row + audit log.
  7. Run regenerate → assert questions are replaced and a new `InterviewQuestionsGenerated` event lands on the outbox.
  8. Cancel the process → assert state.

The Anthropic adapter has its own unit test using a recorded fixture (matching how slice-2 tests the resume parser). The e2e uses a stub generator — the goal of the e2e is to exercise the worker / dispatcher / persistence / HTTP wiring, not the LLM itself.

## Out of scope for slice 1

Deferred to future slices in the interview-module roadmap:

- **Voice / video AI interviewer.** Browser-based real-time conversation agent (STT + LLM + TTS loop). Telephony (Twilio Voice). Video capture + multimodal analysis (body language, tone). Real-time adaptive question selection. These are slices B, C, D, E in the original decomposition; each is a meaningful surface in its own right.
- **External-interviewer feedback paths.** Magic-link feedback URLs, authenticated tenant-member-as-interviewer flows. Slice 1 only accepts recruiter-entered feedback.
- **Aggregation rules.** Bar-raiser veto, weighted decisions, automatic Hire/Reject loop-back to sourcing's `:hire` / `:reject` endpoints. Slice 1 is "recruiter decides".
- **Calendar / scheduling.** Interviewer assignment, slot suggestions, Google / Outlook / iCal integration, candidate self-scheduling.
- **Candidate-facing UX.** No notification to the candidate, no self-scheduling, no progress visibility.
- **LoopTemplate versioning.** Changing the template after shortlists exist does NOT mutate existing processes — they keep the rounds they were created with. Versioned templates with migration paths are deferred.
- **Per-tenant prompt customization / question-bank seeding.** All tenants get the same per-kind prompt templates in slice 1; tenant-specific overrides or seeded canned-question libraries are deferred.

## Future scope — broader sourcing roadmap

Captured here from the slice-4 follow-up brainstorm, so they don't get lost:

### Additional ingestion paths (alternatives to today's manual upload)

- **Public application page.** Per-intent shareable URL; candidate self-uploads; rate-limited; spam-resistant.
- **Email forwarding.** Per-tenant inbox; recruiter forwards candidate emails; system parses attachments and pre-parses against the existing pipeline; unassigned uploads land in a "pending assignment" inbox; recruiter assigns to an intent (nullable `intent_id` on `resume_uploads`; new `PendingAssignment` status). Inbound provider TBD (Postmark / SES / Mailgun / SendGrid).
- **ATS / job-board import.** Adapters for Greenhouse / Lever / LinkedIn / Naukri.
- **Cloud storage scan.** Daemon polling a Google Drive folder or S3 bucket for new resumes.

### Subsequent interview-module slices

- **Slice B — AI voice interviewer (browser-based).** Magic-link interview URL; WebRTC mic capture; STT → LLM orchestration → TTS loop. Transcript + LLM scorecard.
- **Slice C — Telephony.** Outbound PSTN via Twilio Voice or inbound DID.
- **Slice D — Video + multimodal post-call analysis.** Video capture; sampled frames + audio to multimodal LLM for body-language + engagement signals. Post-processing, not real-time.
- **Slice E — Real-time multimodal.** During-call tone and sentiment streaming; adaptive question selection.

Each of D and E carries meaningful regulatory exposure (NYC AEDT, EU AI Act, Illinois AIVI). A separate compliance design pass is required before any video / emotion-analysis slice ships.

## What slice 1 ships

End-to-end product surface:

- Recruiter shortlists a candidate via the slice-4 endpoint.
- An `InterviewProcess` is created automatically with three default rounds (or whatever the intent template defines).
- Within seconds (worker pool latency + LLM round-trip), each round has tailored, structured questions ready for the recruiter to see.
- The recruiter conducts the interview, types interviewer name + email + decision + notes into a feedback form per round.
- The recruiter marks each round done as it completes.
- The recruiter completes or cancels the process when the loop is done.
- Every PII read and lifecycle transition writes an audit row.
- Three new domain events (`InterviewProcessCreated`, `InterviewQuestionsGenerated`, `InterviewFeedbackRecorded`) are published to the outbox for downstream slices to consume.

After slice 1, the interview module is feature-complete enough for a tenant to run their entire interview process inside the platform — provided they conduct the interview themselves. The AI-conducted interview is the next slice (B).
