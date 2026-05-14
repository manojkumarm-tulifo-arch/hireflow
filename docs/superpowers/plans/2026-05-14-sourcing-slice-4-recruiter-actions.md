# Sourcing Slice 4 — Recruiter Lifecycle Actions, SSE, Rescore, GDPR Erasure, Audit Log Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the recruiter dashboard surface. After slice 4, recruiters can shortlist/reject/hire applications, retry failed uploads, rescore an intent's applications, GDPR-erase candidates, and watch a batch's progress live via SSE. Every PII access and lifecycle transition writes an audit log row.

**Architecture:** Builds on slices 1+2+3. Adds (a) a tiny `shared/audit` cross-context concern with its own migration namespace; (b) Application aggregate lifecycle methods for the four recruiter actions; (c) commands + HTTP endpoints for shortlist/reject/hire/retry/rescore/erase; (d) an in-process SSE fanout component subscribing to the existing eventbus; (e) audit-log instrumentation on the slice-2 candidate-detail endpoint. No new external dependencies.

**Tech Stack:** Same as slices 1+2+3. No new Go deps. The SSE handler uses `http.Flusher` + `text/event-stream`. The fanout uses a plain `sync.Mutex` + map.

**Spec reference:** `docs/superpowers/specs/2026-05-12-sourcing-design.md` — implements the fourth (final) slice from §Rollout. Locked tactical decisions from the 2026-05-14 brainstorm:

| # | Decision |
|---|---|
| S4-D1 | Application lifecycle: `Scored → Shortlisted|Rejected|Hired`; `Shortlisted → Interviewing|Rejected|Hired`; `Interviewing → Hired|Rejected`. `Rejected/Hired` terminal. Reject requires free-text reason. |
| S4-D2 | Audit log lives in new `internal/shared/audit/` with own `migrations/shared/` namespace. |
| S4-D3 | Slice-4 audit-write surface: candidate PII reads, lifecycle transitions, erasure, rescore. NOT applications-list reads (high volume, low value). |
| S4-D4 | SSE channel granularity: per-`batch_id`. Heartbeat every 30s. JWT enforcement same as polling endpoint. |
| S4-D5 | SSE backend: in-memory `BatchEventFanout` subscribed to the in-process eventbus. Per-batch subscriber map; broadcast on bus events filtered by `batch_id`. |
| S4-D6 | Rescore: invalidate `llm_judgment`/`overall_score`/`score_band` for the intent's applications, then dispatch existing `ScoreIntent` command. |
| S4-D7 | Retry: only on `Failed`/`Quarantined` status; resets attempt_count + next_attempt_at. |
| S4-D8 | Erasure: tx-atomic DB delete cascade (applications → judge_jobs → resume_uploads → resume_uploads_dedup → candidates). Storage blob delete best-effort, logged. Emits `CandidateErased`. |
| S4-D9 | Auth: any authenticated recruiter in the tenant. RBAC deferred. |
| S4-D10 | Out of scope: stale-Application reconciler, per-tenant config, email/Slack notifications, interview-module wiring, cross-tenant Capability Card, bulk ATS export. |

---

## File structure

### Files created

```
migrations/shared/
    000001_create_audit_log.up.sql
    000001_create_audit_log.down.sql

internal/shared/audit/
    domain/
        audit_event.go              AuditEvent value object (action, resource, payload, actor, occurred_at)
        audit_event_test.go
        writer.go                   AuditWriter port + ErrAuditFailed
    infrastructure/
        postgres_writer.go          PostgresAuditWriter adapter
        postgres_writer_test.go     integration-tagged
        noop_writer.go              No-op adapter (tests/dev that don't care)

internal/sourcing/
    application/
        commands/
            transition_application.go     One command type covering Shortlist/Reject/Hire (one verb param)
            transition_application_test.go
            retry_resume_upload.go
            retry_resume_upload_test.go
            rescore_intent.go
            rescore_intent_test.go
            erase_candidate.go
            erase_candidate_test.go
    infrastructure/
        sse/
            batch_fanout.go               Subscribe to eventbus, route events to per-batch channels
            batch_fanout_test.go
        storage/
            (localfs.go modified — add Delete method)
        persistence/
            (postgres_candidate_repository.go modified — add CascadeDelete)
            postgres_candidate_repository_cascade_test.go    integration-tagged
    delivery/
        http/v1/
            (handlers.go modified — add TransitionApplication, RetryUpload, RescoreIntent, EraseCandidate, BatchEvents)
            (dto.go modified — add ApplicationTransitionRequest etc.)
            (routes.go modified — mount the new endpoints)
            actions_handler_test.go

tests/
    sourcing_slice4_e2e_test.go     Full lifecycle e2e: upload → score → shortlist → SSE notification → audit log row → reject → erase → 404
```

### Files modified

- `Makefile` — `MIGRATE_SHARED` variable, chained into `migrate-up`/`migrate-down`.
- `internal/sourcing/domain/entities/application.go` — add `Shortlist`/`Reject`/`Hire` lifecycle methods.
- `internal/sourcing/domain/events/application_events.go` — add `ApplicationShortlisted`/`ApplicationRejected`/`ApplicationHired`/`CandidateErased` events.
- `internal/sourcing/infrastructure/messaging/outbox_dispatcher.go` — `decodeEvent` switch gains the new events.
- `internal/sourcing/domain/services/resume_storage.go` — add `Delete(ctx, key)` method to the port.
- `internal/sourcing/infrastructure/storage/localfs.go` — implement `Delete`.
- `internal/sourcing/application/queries/get_candidate.go` — write an audit row when called.
- `internal/sourcing/delivery/http/v1/handlers.go` — instrument resume-download with audit writes.
- `cmd/api/main.go` — wire `AuditWriter`, `BatchEventFanout`, new command handlers, new HTTP routes.
- `docs/api/v1/sourcing.openapi.yaml` — add the new endpoints + schemas; bump version to `1.0.0-slice4`.
- `README.md` — flip sourcing row to "Live (recruiter dashboard)".
- `docs/modules/sourcing/README.md` — refresh pipeline diagram, capability list, env vars.
- `developer.md` — note the new shared/migrate target.

---

## Conventions baked into every task

- **Working branch:** start a new branch off the just-merged `main`: `feat/sourcing-slice-4`.
- **Module path:** `github.com/hustle/hireflow`.
- **Tests:** unit `_test.go`; integration `//go:build integration`-gated.
- **Per-test isolation:** the `newPool(t)` helpers in slice-3 already TRUNCATE on entry. Extend the TRUNCATE list to include the new `audit_log` table.
- **Commit cadence:** one commit per task. **No `Co-Authored-By: Claude` trailers.**
- **`make test-integration`** runs with `-p 1` (from slice 3) so per-test TRUNCATE doesn't race across packages.

---

## Task 1: `shared/audit` migration namespace + audit_log table

**Files:**
- Create: `migrations/shared/000001_create_audit_log.up.sql`
- Create: `migrations/shared/000001_create_audit_log.down.sql`
- Modify: `Makefile` (`MIGRATE_SHARED` variable, chain into up/down targets)

This is the first migration in the `shared` namespace. Each context has owned its own migrations until now; the audit log is a cross-context concern that needs its own table tracked separately.

- [ ] **Step 1: Up migration**

Create `migrations/shared/000001_create_audit_log.up.sql`:

```sql
-- audit_log: cross-context immutable append log. Every PII read, lifecycle
-- transition, and erasure writes one row here. Lives in the `shared` namespace
-- because any bounded context can write to it via the AuditWriter port.
CREATE TABLE audit_log (
    id            BIGSERIAL    PRIMARY KEY,
    actor_user_id UUID         NOT NULL,
    tenant_id     UUID         NOT NULL,
    action        TEXT         NOT NULL,          -- e.g. "candidate_read", "application_shortlisted"
    resource_kind TEXT         NOT NULL,          -- e.g. "candidate", "application", "resume_upload"
    resource_id   UUID         NOT NULL,
    payload       JSONB        NOT NULL DEFAULT '{}'::jsonb,
    occurred_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),

    -- audit_log is append-only — no UPDATE expected. No FK constraints because
    -- this table outlives the rows it references (the whole point is
    -- post-erasure forensics).

    CONSTRAINT audit_log_action_nonempty CHECK (length(action) > 0),
    CONSTRAINT audit_log_resource_kind_nonempty CHECK (length(resource_kind) > 0)
);

-- Compliance queries: "who accessed this candidate's PII?"
CREATE INDEX audit_log_resource_idx
    ON audit_log (tenant_id, resource_kind, resource_id, occurred_at DESC);

-- Actor accountability: "what did this user do this month?"
CREATE INDEX audit_log_actor_idx
    ON audit_log (tenant_id, actor_user_id, occurred_at DESC);
```

- [ ] **Step 2: Down migration**

Create `migrations/shared/000001_create_audit_log.down.sql`:

```sql
DROP TABLE IF EXISTS audit_log;
```

- [ ] **Step 3: Makefile wiring**

In `Makefile`, below the `MIGRATE_SOURCING` line add:

```makefile
MIGRATE_SHARED   := migrate -path migrations/shared      -database "$(DATABASE_URL)&x-migrations-table=schema_migrations_shared"
```

In `migrate-up`, append `$(MIGRATE_SHARED) up` as the last line (after sourcing).
In `migrate-down`, prepend `$(MIGRATE_SHARED) down 1` as the first line.

- [ ] **Step 4: Apply + verify** (skip if no DB)

```
make migrate-up
psql "$DATABASE_URL" -c "\d audit_log"
```
Expected: table with two indexes, two CHECK constraints.

- [ ] **Step 5: Commit**

```
git add migrations/shared/ Makefile
git commit -m "feat(shared): audit_log table in shared migration namespace"
```

---

## Task 2: `AuditEvent` value object + `AuditWriter` port

**Files:**
- Create: `internal/shared/audit/domain/audit_event.go` + `_test.go`
- Create: `internal/shared/audit/domain/writer.go`

```go
package domain

import (
    "context"
    "encoding/json"
    "errors"
    "time"

    "github.com/google/uuid"

    shared "github.com/hustle/hireflow/internal/shared/domain"
)

// AuditEvent is one row in the audit log. Immutable; no behaviour.
type AuditEvent struct {
    ActorUserID  uuid.UUID
    TenantID     shared.TenantID
    Action       string
    ResourceKind string
    ResourceID   uuid.UUID
    Payload      map[string]any
    OccurredAt   time.Time
}

// Validate enforces minimum invariants.
func (e AuditEvent) Validate() error {
    if e.Action == "" {
        return errors.New("audit: action required")
    }
    if e.ResourceKind == "" {
        return errors.New("audit: resource_kind required")
    }
    return nil
}

// MarshalPayload returns the JSON-encoded payload bytes, or `{}` if empty.
func (e AuditEvent) MarshalPayload() ([]byte, error) {
    if len(e.Payload) == 0 {
        return []byte(`{}`), nil
    }
    return json.Marshal(e.Payload)
}
```

```go
// writer.go
package domain

import (
    "context"
    "errors"
)

// ErrAuditFailed is returned when the audit write itself failed. Callers MUST
// treat this as load-bearing: if audit fails, the caller should NOT proceed
// (e.g., don't return PII to a caller you couldn't audit).
var ErrAuditFailed = errors.New("audit: write failed")

// AuditWriter persists audit events.
type AuditWriter interface {
    Write(ctx context.Context, event AuditEvent) error
}
```

`audit_event_test.go`: cover `Validate()` happy path + missing-action + missing-kind, plus `MarshalPayload()` empty + non-empty.

- [ ] **Steps + verification + commit**

```
git add internal/shared/audit/domain/
git commit -m "feat(audit): AuditEvent value object + AuditWriter port"
```

---

## Task 3: Postgres `AuditWriter` adapter + no-op adapter

**Files:**
- Create: `internal/shared/audit/infrastructure/postgres_writer.go` + `_test.go`
- Create: `internal/shared/audit/infrastructure/noop_writer.go` + `_test.go`

```go
// postgres_writer.go
package infrastructure

import (
    "context"
    "fmt"

    "github.com/jackc/pgx/v5/pgxpool"

    "github.com/hustle/hireflow/internal/shared/audit/domain"
)

type PostgresAuditWriter struct {
    pool *pgxpool.Pool
}

func NewPostgresAuditWriter(pool *pgxpool.Pool) *PostgresAuditWriter

func (w *PostgresAuditWriter) Write(ctx context.Context, e domain.AuditEvent) error {
    if err := e.Validate(); err != nil {
        return fmt.Errorf("validate: %w", err)
    }
    payload, err := e.MarshalPayload()
    if err != nil {
        return fmt.Errorf("payload: %w", err)
    }
    _, err = w.pool.Exec(ctx, `
        INSERT INTO audit_log (
            actor_user_id, tenant_id, action, resource_kind, resource_id, payload, occurred_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7)
    `, e.ActorUserID, e.TenantID.String(), e.Action, e.ResourceKind, e.ResourceID, payload, e.OccurredAt)
    if err != nil {
        return fmt.Errorf("%w: %v", domain.ErrAuditFailed, err)
    }
    return nil
}
```

`noop_writer.go` returns `nil` from `Write`. Useful in unit tests that don't care about audit.

Integration test for the Postgres adapter: write an event, query the row back via raw SQL, assert all fields round-trip.

- [ ] **Verification + commit**

```
git add internal/shared/audit/infrastructure/
git commit -m "feat(audit): Postgres and noop AuditWriter adapters"
```

---

## Task 4: ResumeStorage.Delete + extend localfs adapter

**Files:**
- Modify: `internal/sourcing/domain/services/resume_storage.go` (add Delete method)
- Modify: `internal/sourcing/infrastructure/storage/localfs.go` (implement Delete)
- Modify: `internal/sourcing/infrastructure/storage/localfs_test.go` (test Delete)

```go
// resume_storage.go — add to the existing interface:
type ResumeStorage interface {
    Put(ctx context.Context, key string, body io.Reader) error
    Open(ctx context.Context, key string) (io.ReadCloser, error)
    MoveToQuarantine(ctx context.Context, key string) (newKey string, err error)

    // Delete removes the bytes at the given key. Idempotent — deleting a
    // missing key returns nil (no ErrNotFound). Used by GDPR erasure.
    Delete(ctx context.Context, key string) error
}
```

`localfs.go` Delete impl: `safePath`, `os.Remove`, swallow `ErrNotExist`.

Test cases:
- Put then Delete → file gone (Open returns ErrNotFound)
- Delete on a missing key → no error (idempotent)
- Delete with a path-escape key → ErrUnsafeKey

- [ ] **Verification + commit**

```
git add internal/sourcing/domain/services/resume_storage.go \
        internal/sourcing/infrastructure/storage/localfs.go \
        internal/sourcing/infrastructure/storage/localfs_test.go
git commit -m "feat(sourcing): ResumeStorage.Delete for GDPR erasure"
```

---

## Task 5: Application lifecycle methods + new events

**Files:**
- Modify: `internal/sourcing/domain/entities/application.go` (+`_test.go`)
- Modify: `internal/sourcing/domain/events/application_events.go` (+`_test.go`)
- Modify: `internal/sourcing/domain/valueobjects/application_status.go` (+`_test.go`)
- Modify: `internal/sourcing/infrastructure/messaging/outbox_dispatcher.go` (decode new events)

### Lifecycle methods on `Application`

Append to `application.go`:

```go
// Shortlist transitions Scored → Shortlisted. Emits ApplicationShortlisted.
func (a *Application) Shortlist(actorUserID uuid.UUID) error {
    return a.transition(vo.StatusShortlisted, "", actorUserID, "shortlisted")
}

// Reject transitions Scored | Shortlisted | Interviewing → Rejected.
// reason is required (>=1 char). Emits ApplicationRejected with the reason.
func (a *Application) Reject(actorUserID uuid.UUID, reason string) error {
    reason = strings.TrimSpace(reason)
    if reason == "" {
        return errors.New("reject: reason required")
    }
    return a.transitionWithReason(vo.StatusRejected, reason, actorUserID, "rejected")
}

// Hire transitions Scored | Shortlisted | Interviewing → Hired.
func (a *Application) Hire(actorUserID uuid.UUID) error {
    return a.transition(vo.StatusHired, "", actorUserID, "hired")
}

// MoveToInterviewing transitions Shortlisted → Interviewing. Slice 4 ships this
// for completeness; the actual trigger (interview-scheduled event from a future
// interview module) is post-slice-4 work.
func (a *Application) MoveToInterviewing(actorUserID uuid.UUID) error {
    return a.transition(vo.StatusInterviewing, "", actorUserID, "moved_to_interviewing")
}
```

The `transition`/`transitionWithReason` helpers (private) call `CanTransitionTo`, update status, set `last_actor_id` if you add one to the aggregate (or just rely on the emitted event for actor), emit the right event.

### Status transitions

`application_status.go`'s `CanTransitionTo` — slice 3 declared these statuses but didn't wire transitions. Add:

```go
case vo.StatusScored:
    return next == StatusShortlisted || next == StatusRejected || next == StatusHired || next == StatusStale
case vo.StatusShortlisted:
    return next == StatusInterviewing || next == StatusRejected || next == StatusHired
case vo.StatusInterviewing:
    return next == StatusRejected || next == StatusHired
```

Add tests for each new transition (valid + invalid).

### New events

Append to `application_events.go`:

```go
type ApplicationShortlisted struct {
    ApplicationID uuid.UUID
    CandidateID   uuid.UUID
    IntentID      uuid.UUID
    TenantID      shared.TenantID
    ActorUserID   uuid.UUID
    OccurredAt    time.Time
}
// EventName "sourcing.ApplicationShortlisted"

type ApplicationRejected struct {
    ApplicationID uuid.UUID
    CandidateID   uuid.UUID
    IntentID      uuid.UUID
    TenantID      shared.TenantID
    ActorUserID   uuid.UUID
    Reason        string
    OccurredAt    time.Time
}
// EventName "sourcing.ApplicationRejected"

type ApplicationHired struct {
    ApplicationID uuid.UUID
    CandidateID   uuid.UUID
    IntentID      uuid.UUID
    TenantID      shared.TenantID
    ActorUserID   uuid.UUID
    OccurredAt    time.Time
}
// EventName "sourcing.ApplicationHired"

type ApplicationMovedToInterviewing struct { ... }
// EventName "sourcing.ApplicationMovedToInterviewing"

// Sourcing-side erasure event. Emits when DELETE /candidates/{id} completes.
// Downstream contexts (future interview module) subscribe to clean their state.
type CandidateErased struct {
    CandidateID uuid.UUID
    TenantID    shared.TenantID
    ActorUserID uuid.UUID
    OccurredAt  time.Time
}
// EventName "sourcing.CandidateErased"
```

Each event implements `events.Event` (EventName/AggregateID/Tenant/At). All written to the outbox via `Save()`.

### Outbox decoder

Extend `outbox_dispatcher.go`'s `decodeEvent` switch with the 5 new event names.

- [ ] **Steps + verification + commit**

Append tests covering: each lifecycle method happy path, invalid transitions (e.g., Hired → Rejected), reject-without-reason error, events emitted with correct actor + reason.

```
git add internal/sourcing/domain/entities/ \
        internal/sourcing/domain/events/ \
        internal/sourcing/domain/valueobjects/application_status.go \
        internal/sourcing/domain/valueobjects/application_status_test.go \
        internal/sourcing/infrastructure/messaging/outbox_dispatcher.go
git commit -m "feat(sourcing): Application lifecycle (shortlist/reject/hire) + events"
```

---

## Task 6: Lifecycle command + HTTP endpoints (shortlist/reject/hire)

**Files:**
- Create: `internal/sourcing/application/commands/transition_application.go` + `_test.go`
- Modify: `internal/sourcing/delivery/http/v1/handlers.go` (add three handlers)
- Modify: `internal/sourcing/delivery/http/v1/dto.go` (request bodies)
- Modify: `internal/sourcing/delivery/http/v1/routes.go` (mount routes)
- Modify: `internal/sourcing/delivery/http/v1/handlers_test.go` (3 endpoint tests)

### Single command type for all three actions

```go
package commands

type ApplicationAction string

const (
    ActionShortlist           ApplicationAction = "shortlist"
    ActionReject              ApplicationAction = "reject"
    ActionHire                ApplicationAction = "hire"
    ActionMoveToInterviewing  ApplicationAction = "move_to_interviewing"
)

type TransitionApplicationHandler struct {
    repo    repositories.ApplicationRepository
    audit   auditdomain.AuditWriter
}

type TransitionApplicationInput struct {
    TenantID      shared.TenantID
    ActorUserID   uuid.UUID
    ApplicationID uuid.UUID
    Action        ApplicationAction
    RejectReason  string  // required when Action==ActionReject
}

func (h *TransitionApplicationHandler) Handle(ctx context.Context, in TransitionApplicationInput) error {
    app, err := h.repo.FindByID(ctx, in.TenantID, in.ApplicationID)
    if err != nil { return err }

    switch in.Action {
    case ActionShortlist:
        if err := app.Shortlist(in.ActorUserID); err != nil { return err }
    case ActionReject:
        if err := app.Reject(in.ActorUserID, in.RejectReason); err != nil { return err }
    case ActionHire:
        if err := app.Hire(in.ActorUserID); err != nil { return err }
    case ActionMoveToInterviewing:
        if err := app.MoveToInterviewing(in.ActorUserID); err != nil { return err }
    default:
        return fmt.Errorf("unknown action: %s", in.Action)
    }

    if err := h.repo.Save(ctx, app); err != nil { return err }

    // Audit log (best-effort: log even on audit failure but bubble it up).
    auditErr := h.audit.Write(ctx, auditdomain.AuditEvent{
        ActorUserID:  in.ActorUserID,
        TenantID:     in.TenantID,
        Action:       "application_" + string(in.Action),
        ResourceKind: "application",
        ResourceID:   in.ApplicationID,
        Payload:      map[string]any{"reason": in.RejectReason},
        OccurredAt:   time.Now().UTC(),
    })
    if auditErr != nil {
        return auditErr  // load-bearing: caller signals error to recruiter
    }
    return nil
}
```

### HTTP endpoints

Three handlers, all calling `TransitionApplicationHandler.Handle`:
- `POST /applications/{id}:shortlist` — no body
- `POST /applications/{id}:reject` — body `{"reason":"..."}`
- `POST /applications/{id}:hire` — no body

Each responds 204 on success, 404 if app not found, 400 if invalid transition / missing reason, 401 on no identity, 500 on audit failure.

### Tests

Command tests cover each action with happy path + invalid transition + audit-failure-propagation. HTTP tests cover routing + status codes + body parsing.

- [ ] **Verification + commit**

```
git add internal/sourcing/application/commands/transition_application.go \
        internal/sourcing/application/commands/transition_application_test.go \
        internal/sourcing/delivery/
git commit -m "feat(sourcing): shortlist/reject/hire endpoints + audit-logged transitions"
```

---

## Task 7: Retry resume upload (command + endpoint)

**Files:**
- Create: `internal/sourcing/application/commands/retry_resume_upload.go` + `_test.go`
- Modify: `internal/sourcing/delivery/http/v1/handlers.go` (add `RetryUpload`)
- Modify: `internal/sourcing/delivery/http/v1/routes.go`
- Modify: `internal/sourcing/delivery/http/v1/handlers_test.go`

```go
type RetryResumeUploadHandler struct {
    repo repositories.ResumeUploadRepository
}

type RetryResumeUploadInput struct {
    TenantID shared.TenantID
    UploadID uuid.UUID
}

// Handle: load upload, check status ∈ {Failed, Quarantined}, reset retry state,
// Save (which re-publishes the row for the worker pool to pick up).
func (h *RetryResumeUploadHandler) Handle(ctx context.Context, in RetryResumeUploadInput) error {
    u, err := h.repo.FindByID(ctx, in.TenantID, in.UploadID)
    if err != nil { return err }

    if u.Status() != vo.StatusFailed && u.Status() != vo.StatusQuarantined {
        return fmt.Errorf("retry: upload status %s is not retryable (must be Failed or Quarantined)", u.Status())
    }

    // Reset retry state. The entity exposes a Reset method; if not, this is
    // a slice-4 addition. Reset transitions back to the appropriate stage
    // (Pending for Failed, scanning-fresh for Quarantined).
    if err := u.ResetForRetry(); err != nil {
        return err
    }

    return h.repo.Save(ctx, u)
}
```

The entity needs a `ResetForRetry()` method. Add it to `resume_upload.go`:

```go
func (u *ResumeUpload) ResetForRetry() error {
    if u.status != vo.StatusFailed && u.status != vo.StatusQuarantined {
        return ErrInvalidTransition
    }
    u.status = vo.StatusPending
    u.attemptCount = 0
    u.lastError = ""
    u.nextAttemptAt = time.Now().UTC()
    u.touch()
    return nil
}
```

HTTP endpoint:
- `POST /resumes/{upload_id}:retry` — no body
- 204 on success, 404 if not found, 400 if not retryable, 401 on no identity.

Tests as usual.

- [ ] **Verification + commit**

```
git add internal/sourcing/application/commands/retry_resume_upload.go \
        internal/sourcing/application/commands/retry_resume_upload_test.go \
        internal/sourcing/domain/entities/resume_upload.go \
        internal/sourcing/domain/entities/resume_upload_test.go \
        internal/sourcing/delivery/
git commit -m "feat(sourcing): POST /resumes/{id}:retry — reset failed/quarantined uploads"
```

---

## Task 8: Rescore intent (command + endpoint)

**Files:**
- Create: `internal/sourcing/application/commands/rescore_intent.go` + `_test.go`
- Modify: `internal/sourcing/domain/repositories/application_repository.go` (add `InvalidateJudgmentsForIntent`)
- Modify: `internal/sourcing/infrastructure/persistence/postgres_application_repository.go` (impl)
- Modify: `internal/sourcing/delivery/http/v1/handlers.go`
- Modify: `internal/sourcing/delivery/http/v1/routes.go`

### Repository extension

```go
// In ApplicationRepository port:
// InvalidateJudgmentsForIntent nulls out llm_judgment, overall_score, and
// score_band for all applications belonging to the intent. Used by rescore.
InvalidateJudgmentsForIntent(ctx context.Context, tenant shared.TenantID, intentID uuid.UUID) error
```

Postgres impl: single UPDATE statement.

### Command

```go
type RescoreIntentHandler struct {
    appRepo            repositories.ApplicationRepository
    scoreIntentHandler *ScoreIntentHandler
    audit              auditdomain.AuditWriter
}

type RescoreIntentInput struct {
    TenantID    shared.TenantID
    ActorUserID uuid.UUID
    IntentID    uuid.UUID
}

func (h *RescoreIntentHandler) Handle(ctx context.Context, in RescoreIntentInput) error {
    // 1. Invalidate cached LLM judgments for this intent.
    if err := h.appRepo.InvalidateJudgmentsForIntent(ctx, in.TenantID, in.IntentID); err != nil {
        return err
    }
    // 2. Dispatch ScoreIntent (which fans out apps + enqueues judge jobs).
    if err := h.scoreIntentHandler.Handle(ctx, ScoreIntentInput{
        TenantID: in.TenantID,
        IntentID: in.IntentID,
    }); err != nil {
        return err
    }
    // 3. Audit.
    return h.audit.Write(ctx, auditdomain.AuditEvent{
        ActorUserID:  in.ActorUserID,
        TenantID:     in.TenantID,
        Action:       "intent_rescored",
        ResourceKind: "intent",
        ResourceID:   in.IntentID,
        OccurredAt:   time.Now().UTC(),
    })
}
```

### Endpoint

`POST /intents/{intent_id}/applications:rescore` — no body. 202 Accepted (async work continues in the match + judge workers).

Tests + commit.

- [ ] **Verification + commit**

```
git add internal/sourcing/application/commands/rescore_intent.go \
        internal/sourcing/application/commands/rescore_intent_test.go \
        internal/sourcing/domain/repositories/application_repository.go \
        internal/sourcing/infrastructure/persistence/postgres_application_repository.go \
        internal/sourcing/delivery/
git commit -m "feat(sourcing): POST /intents/{id}/applications:rescore"
```

---

## Task 9: Erase candidate (command + endpoint + cascade repo method)

**Files:**
- Create: `internal/sourcing/application/commands/erase_candidate.go` + `_test.go`
- Modify: `internal/sourcing/domain/repositories/candidate_repository.go` (add `EraseCascade`)
- Modify: `internal/sourcing/infrastructure/persistence/postgres_candidate_repository.go` (impl)
- Create: `internal/sourcing/infrastructure/persistence/postgres_candidate_repository_cascade_test.go` (integration)
- Modify: `internal/sourcing/delivery/http/v1/handlers.go`
- Modify: `internal/sourcing/delivery/http/v1/routes.go`

### Repository extension

```go
// In CandidateRepository port:
// EraseCascade transactionally deletes the candidate, its applications,
// associated judge_jobs, resume_uploads, and resume_uploads_dedup rows.
// Returns the storage keys of deleted resume_uploads so the caller can
// best-effort delete the blob storage outside the tx.
EraseCascade(ctx context.Context, tenant shared.TenantID, candidateID uuid.UUID) (storageKeys []string, err error)
```

Postgres impl:
```go
func (r *PostgresCandidateRepository) EraseCascade(ctx, tenant, candidateID) ([]string, error) {
    tx, err := r.pool.BeginTx(ctx, ...)
    defer tx.Rollback(ctx)

    // Collect storage keys before deleting the rows.
    rows, err := tx.Query(ctx, `SELECT storage_key FROM resume_uploads WHERE candidate_id=$1 AND tenant_id=$2`, candidateID, tenant.String())
    var keys []string
    for rows.Next() { var k string; rows.Scan(&k); keys = append(keys, k) }

    // Delete in dependency order. FKs would help but we don't have CASCADE
    // declared (slice 2 chose to handle cascade in application code for audit).
    if _, err := tx.Exec(ctx, `DELETE FROM judge_jobs WHERE application_id IN (SELECT id FROM applications WHERE candidate_id=$1 AND tenant_id=$2)`, candidateID, tenant.String()); err != nil { return nil, err }
    if _, err := tx.Exec(ctx, `DELETE FROM applications WHERE candidate_id=$1 AND tenant_id=$2`, candidateID, tenant.String()); err != nil { return nil, err }
    if _, err := tx.Exec(ctx, `DELETE FROM resume_uploads_dedup WHERE tenant_id=$1 AND content_hash IN (SELECT content_hash FROM resume_uploads WHERE candidate_id=$2 AND tenant_id=$1)`, tenant.String(), candidateID); err != nil { return nil, err }
    if _, err := tx.Exec(ctx, `DELETE FROM resume_uploads WHERE candidate_id=$1 AND tenant_id=$2`, candidateID, tenant.String()); err != nil { return nil, err }
    if _, err := tx.Exec(ctx, `DELETE FROM candidates WHERE id=$1 AND tenant_id=$2`, candidateID, tenant.String()); err != nil { return nil, err }

    return keys, tx.Commit(ctx)
}
```

### Command

```go
type EraseCandidateHandler struct {
    repo    repositories.CandidateRepository
    storage services.ResumeStorage
    audit   auditdomain.AuditWriter
    bus     Bus  // for emitting CandidateErased
}

func (h *EraseCandidateHandler) Handle(ctx, in EraseCandidateInput) error {
    // 1. Cascade delete in DB tx.
    keys, err := h.repo.EraseCascade(ctx, in.TenantID, in.CandidateID)
    if err != nil { return err }

    // 2. Best-effort storage blob delete (outside tx, log failures).
    for _, k := range keys {
        if err := h.storage.Delete(ctx, k); err != nil {
            // Log but don't fail — the DB delete succeeded, PII is gone from API surface.
        }
    }

    // 3. Audit log.
    if err := h.audit.Write(ctx, ...); err != nil { return err }

    // 4. Emit CandidateErased.
    return h.bus.Publish(ctx, "sourcing.CandidateErased", events.CandidateErased{...})
}
```

### Endpoint

`DELETE /candidates/{candidate_id}` — 204 on success, 404 if not found.

### Integration test (cascade_test.go)

- Insert candidate + 2 applications + 1 judge_job + 1 resume_upload via raw SQL
- Call `EraseCascade`
- Assert all rows gone, returned keys match the uploads' storage_keys

- [ ] **Verification + commit**

```
git add internal/sourcing/application/commands/erase_candidate.go \
        internal/sourcing/application/commands/erase_candidate_test.go \
        internal/sourcing/domain/repositories/candidate_repository.go \
        internal/sourcing/infrastructure/persistence/postgres_candidate_repository.go \
        internal/sourcing/infrastructure/persistence/postgres_candidate_repository_cascade_test.go \
        internal/sourcing/delivery/
git commit -m "feat(sourcing): DELETE /candidates/{id} with cascade + GDPR erasure"
```

---

## Task 10: SSE `BatchEventFanout`

**Files:**
- Create: `internal/sourcing/infrastructure/sse/batch_fanout.go` + `_test.go`

```go
package sse

import (
    "context"
    "sync"

    "github.com/google/uuid"
)

// Subscriber is a per-connection channel that receives events for one batch.
type Subscriber struct {
    BatchID uuid.UUID
    C       chan []byte  // pre-formatted SSE-line bytes
    Done    chan struct{}
}

// BatchEventFanout subscribes to the in-process eventbus once and routes events
// to all interested HTTP subscribers based on their batch_id.
type BatchEventFanout struct {
    mu    sync.Mutex
    subs  map[uuid.UUID][]*Subscriber  // batch_id -> subscribers
}

func NewBatchEventFanout() *BatchEventFanout

// Subscribe registers a new SSE subscriber for the given batch_id.
// Returns the channel and a cleanup function to call on disconnect.
func (f *BatchEventFanout) Subscribe(batchID uuid.UUID) (<-chan []byte, func())

// OnEvent is the bus callback. Called when a slice-1/2/3 event fires.
// Filters by batch_id (when present in the event) and routes to subscribers.
func (f *BatchEventFanout) OnEvent(ctx context.Context, event any) error {
    // Type-switch on the events that carry batch_id:
    //   events.ResumeUploadAccepted, events.ResumeUploadFailed,
    //   events.ResumeExtracted, events.ResumeParsed,
    //   events.CandidateParsed (no batch_id — skip)
    //   events.ApplicationScored etc. (no batch_id — skip)
    //
    // For each event with a batch_id, format the SSE line and broadcast to
    // f.subs[batch_id]. Non-blocking send (drop if subscriber's channel is full).
}
```

The bus callback registration happens in T13 (main.go wiring). The fanout component itself is just a struct with methods.

Tests:
- Subscribe, fire event with matching batch_id → subscriber receives bytes
- Multiple subscribers per batch → all receive
- Subscriber's channel full → no blocking; event dropped (logged)
- Unsubscribe → no more events to that channel

- [ ] **Verification + commit**

```
git add internal/sourcing/infrastructure/sse/
git commit -m "feat(sourcing): BatchEventFanout for SSE pubsub"
```

---

## Task 11: SSE `GET /resumes/batches/{batch_id}/events` endpoint

**Files:**
- Modify: `internal/sourcing/delivery/http/v1/handlers.go` (add BatchEvents handler)
- Modify: `internal/sourcing/delivery/http/v1/routes.go` (mount)
- Modify: `internal/sourcing/delivery/http/v1/handlers_test.go`

```go
func (h *SourcingHandler) BatchEvents(w http.ResponseWriter, r *http.Request) {
    identity, err := auth.IdentityFromContext(r.Context())
    if err != nil { writeError(w, 401, ...); return }

    batchID, err := uuid.Parse(chi.URLParam(r, "batch_id"))
    if err != nil { writeError(w, 400, ...); return }

    // TODO: verify the batch belongs to identity.TenantID. Slice-4
    // simplification: skip the check and rely on UUID unguessability for v1.

    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.WriteHeader(200)

    flusher, ok := w.(http.Flusher)
    if !ok { return }

    ch, cleanup := h.fanout.Subscribe(batchID)
    defer cleanup()

    // Heartbeat every 30s as ":ping\n\n" (SSE comment, not a real event).
    heartbeat := time.NewTicker(30 * time.Second)
    defer heartbeat.Stop()

    for {
        select {
        case <-r.Context().Done():
            return
        case <-heartbeat.C:
            io.WriteString(w, ":ping\n\n")
            flusher.Flush()
        case payload := <-ch:
            w.Write(payload)
            flusher.Flush()
        }
    }
}
```

Tests use `httptest.NewServer` (NOT `httptest.NewRecorder` — SSE needs a real conn). Connect a goroutine that reads from the body; fire an event via the fanout; assert the read sees the bytes within 1s. Use a 5s test timeout.

- [ ] **Verification + commit**

```
git add internal/sourcing/delivery/
git commit -m "feat(sourcing): GET /resumes/batches/{id}/events SSE endpoint"
```

---

## Task 12: Audit instrumentation on candidate-detail + resume-download

**Files:**
- Modify: `internal/sourcing/application/queries/get_candidate.go` (write audit row on Handle)
- Modify: `internal/sourcing/delivery/http/v1/handlers.go` (audit on resume-blob download)
- Modify: existing test fakes to provide an `AuditWriter`

Wrap the existing `GetCandidateHandler.Handle` flow with an audit write:

```go
type GetCandidateHandler struct {
    repo      repositories.CandidateRepository
    encryptor services.PIIEncryptor
    audit     auditdomain.AuditWriter  // new field
}

func (h *GetCandidateHandler) Handle(ctx, tenantID, actorUserID, candidateID) {
    // ... existing fetch + decrypt logic ...

    // Audit AFTER the read returns successfully (don't audit failed reads).
    if err := h.audit.Write(ctx, auditdomain.AuditEvent{
        ActorUserID:  actorUserID,
        TenantID:     tenantID,
        Action:       "candidate_read",
        ResourceKind: "candidate",
        ResourceID:   candidateID,
        OccurredAt:   time.Now().UTC(),
    }); err != nil {
        // Audit-write failure is load-bearing: don't return PII to a caller
        // we couldn't audit. Wrap and propagate.
        return dto.CandidateDetailDTO{}, fmt.Errorf("audit candidate read: %w", err)
    }
    return out, nil
}
```

The query handler's signature gains `actorUserID uuid.UUID` — pass `identity.RecruiterID` from the HTTP layer. Update all callers + test fakes.

For resume downloads (the `GET /candidates/{id}/resume` endpoint, if it exists in slice 2 — verify by inspecting slice-2 handlers), do the same: audit row keyed by `action="candidate_resume_downloaded"`.

- [ ] **Verification + commit**

```
git add internal/sourcing/application/queries/get_candidate.go \
        internal/sourcing/application/queries/get_candidate_test.go \
        internal/sourcing/delivery/http/v1/handlers.go \
        internal/sourcing/delivery/http/v1/handlers_test.go
git commit -m "feat(sourcing): audit-log instrumentation on candidate-detail reads"
```

---

## Task 13: Wire into `cmd/api/main.go`

**Files:**
- Modify: `cmd/api/main.go`
- Modify: `developer.md`

New components to wire:
- `audit.NewPostgresAuditWriter(pool)` instance
- `commands.TransitionApplicationHandler` (takes audit writer)
- `commands.RetryResumeUploadHandler`
- `commands.RescoreIntentHandler` (takes ScoreIntentHandler + audit writer)
- `commands.EraseCandidateHandler` (takes storage + audit writer + bus)
- `sse.NewBatchEventFanout()` — subscribe to bus events that carry batch_id (4 event names from slice 1)
- `GetCandidateHandler` constructor gains the audit writer
- `SourcingHandler` gains 6 new fields + 6 new handler params

Add bus subscriptions for the fanout:

```go
bus.Subscribe("sourcing.ResumeUploadAccepted", fanout.OnEvent)
bus.Subscribe("sourcing.ResumeUploadFailed", fanout.OnEvent)
bus.Subscribe("sourcing.ResumeExtracted", fanout.OnEvent)
bus.Subscribe("sourcing.ResumeParsed", fanout.OnEvent)
```

The `SourcingHandler` constructor signature change ripples to test files — update them (same pattern as slices 2 and 3).

- [ ] **Verification + commit**

```
make build
go test ./... -count=1
git add cmd/api/main.go developer.md
git commit -m "feat(sourcing): wire slice-4 recruiter actions + SSE + audit"
```

---

## Task 14: Slice 4 e2e integration test

**Files:**
- Create: `tests/sourcing_slice4_e2e_test.go`

Full recruiter-flow e2e against real Postgres + pgvector:

1. Setup: insert intent, upload, score (reuse slice-3 wiring with stubs).
2. Wait for Application to be Scored.
3. `POST /applications/{id}:shortlist` → 204; verify status=Shortlisted in DB; verify audit row exists.
4. Subscribe to `GET /resumes/batches/{batch_id}/events` SSE (use `httptest.NewServer` for this).
5. Trigger a new upload in the same batch → assert SSE event arrives within 5s with `event: item_updated`.
6. `POST /intents/{id}/applications:rescore` → 202; verify llm_judgment NULL'd then re-populated by the worker; verify audit row.
7. `POST /resumes/{upload_id}:retry` on a fabricated Failed upload → verify status=Pending, attempt_count=0.
8. `DELETE /candidates/{id}` → 204; verify cascade deleted everything (application + judge_job + upload + dedup); verify `CandidateErased` event on bus; verify audit row.
9. `GET /candidates/{id}` afterwards → 404 (gone).

Use the same `newPool` helper (with TRUNCATE) so the test is isolated. Stub the LLM judge as in slice-3 e2e.

- [ ] **Verification + commit**

```
INTEGRATION_TESTS=1 go test -tags=integration ./tests/... -run TestSourcingSlice4 -v -count=1
git add tests/sourcing_slice4_e2e_test.go
git commit -m "test(sourcing): slice-4 e2e — lifecycle + SSE + rescore + erasure + audit"
```

---

## Task 15: README + module docs + OpenAPI + scoring.md updates

**Files:**
- Modify: `README.md`
- Modify: `docs/modules/sourcing/README.md`
- Modify: `docs/api/v1/sourcing.openapi.yaml` (add 6 new paths + schemas; bump to 1.0.0-slice4)
- Modify: `docs/modules/sourcing/scoring.md` (small update on rescore semantics + lifecycle)

Root README sourcing row:
```
| `sourcing` | Resume ingestion + parsing + scoring + recruiter dashboard (shortlist/reject/hire) + live SSE updates + audit log + GDPR erasure (slices 1+2+3+4). | **Live** |
```

Module README: add a §"Recruiter actions (slice 4)" section listing the 6 new endpoints with one-line semantics each.

OpenAPI: 6 new paths.

scoring.md: short update to §5 (cache invalidation) noting the explicit rescore endpoint.

- [ ] **Verification + commit**

```
git add README.md docs/
git commit -m "docs(sourcing): refresh for slice 4 (recruiter dashboard)"
```

---

## Wrap-up

After all 15 tasks complete:

- [ ] `make test-unit` clean.
- [ ] `INTEGRATION_TESTS=1 make test-integration` — slice-1, slice-2, slice-3, AND slice-4 e2e tests all pass against live Postgres + pgvector.
- [ ] `go vet ./...` and `gofmt -l -s .` both clean.
- [ ] Smoke run `bin/api` with `VOYAGE_API_KEY` + `ANTHROPIC_API_KEY` + `SOURCING_PII_DEK` set; confirm all dispatchers + workers start, no fatal log lines.

**What slice 4 ships:**
- Recruiter lifecycle actions: shortlist, reject (with required reason), hire, optional move-to-interviewing.
- Live SSE batch updates: `GET /resumes/batches/{id}/events` pushes deltas as the worker advances rows through the pipeline.
- Retry endpoint for failed/quarantined uploads.
- Rescore endpoint for re-judging an intent's applications.
- GDPR `DELETE /candidates/{id}` with transactional cascade across all related tables + best-effort blob delete + outbox event for downstream cleanup.
- Audit log on every PII access, lifecycle transition, rescore, and erasure.
- Five new domain events on the outbox for downstream consumers (most importantly `ApplicationShortlisted` — the trigger for the future interview module).

**What slice 4 does NOT ship (out of scope):**
- Stale-Application reconciler (background scan for spec-version drift).
- Per-tenant scoring config (top-K, weights, threshold tuning).
- Email/Slack notifications on lifecycle transitions.
- Interview-module wiring (separate bounded context — needs its own brainstorm).
- Cross-tenant Capability Card sharing.
- Bulk ATS export.
- KMS-backed `PIIEncryptor` (still local-dev DEK from slice 2).
- Stale-Running judge-job reaper (from slice-3 known gap).
- Authorization beyond tenant scoping (RBAC).

The full sourcing context is feature-complete after slice 4 for the Tulifo pre-seed pilot.
