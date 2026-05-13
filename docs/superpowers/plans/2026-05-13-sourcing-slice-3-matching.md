# Sourcing Slice 3 — Match Scoring + `Application` Aggregate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build end-to-end Candidate × Intent scoring. When an intent is confirmed or a candidate is parsed, fan out to create `Application` rows, embed (Voyage), apply rule criteria, compute cosine similarity, and LLM-judge the top-K via Claude tool-use. Recruiter sees ranked applications with rule chips + LLM evidence via `GET /api/v1/intents/{id}/applications`.

**Architecture:** Three new ports — `Embedder` (Voyage `voyage-3`), `MatchScorer` (in-process rule engine + cosine via pgvector), `LLMJudge` (Claude tool-use against `judge_match` schema). Two new aggregates — `Application` (per `(Candidate, Intent)` pair) and a small `JudgeJob` queue row. Two new event consumers — `IntentConfirmedConsumer` (subscribes to `hiringintent` outbox) and `CandidateParsedConsumer` (subscribes to sourcing's own outbox). Two new worker pools — match worker (embedding + rule + cosine) and judge worker (top-K LLM judging).

**Tech Stack:** Same as slices 1+2. New deps: `pgvector/pgvector-go` (Postgres vector type binding for pgx), plus the `pgvector` extension in Postgres (added via migration). Voyage AI HTTP API via `net/http` (no Go SDK exists; thin custom client).

**Spec reference:** `docs/superpowers/specs/2026-05-12-sourcing-design.md` — implements the third slice from §Rollout. Detailed scoring reference at `docs/modules/sourcing/scoring.md` (read this before T11 for the algorithm).

**Locked decisions from the 2026-05-13 brainstorm:**

| # | Decision |
|---|---|
| S3-D1 | Scoring fan-out: `CandidateParsed` creates Application rows per open intent; `IntentConfirmed` creates rows per candidate in tenant |
| S3-D2 | Two scores per Application: `embedding_score` (always populated unless rule-failed) and `overall_score` (only for judged top-K) |
| S3-D3 | LLM judge fires after `IntentConfirmed` fan-out completes, against top-K (default 20) sorted by coarse score |
| S3-D4 | Match worker (separate from upload worker) handles per-Application embedding + rule + cosine |
| S3-D5 | Judge worker (third pool) handles top-K judging; queue is a `judge_jobs` table |
| S3-D6 | Application statuses: `New`, `Scored`, `Excluded`, `EmbedFailed`, `JudgeFailed`, `Stale` (slice 4 adds `Shortlisted`/`Rejected`/`Interviewing`/`Hired`) |
| S3-D7 | Slice 3 ships only `GET /intents/{id}/applications`; rescore + lifecycle actions are slice 4 |
| S3-D8 | Caches keyed by `(spec_version, schema_version, prompt_version)`; re-confirm of unchanged intent reuses everything |
| S3-D9 | Out of scope: rescore endpoint, lifecycle actions, SSE, cross-tenant matching, per-tenant scoring config |

---

## File structure

### Files created

```
migrations/sourcing/
    000005_enable_pgvector.up.sql
    000005_enable_pgvector.down.sql
    000006_create_applications.up.sql
    000006_create_applications.down.sql
    000007_create_judge_jobs.up.sql
    000007_create_judge_jobs.down.sql

internal/sourcing/
    domain/
        valueobjects/
            match_score.go              MatchScore + ScoreBand + thresholds
            match_score_test.go
            rule_match.go               RuleCriterion + RuleMatchReport + RuleResult
            rule_match_test.go
            llm_judgment.go             LLMJudgment + Evidence + Concern
            llm_judgment_test.go
            application_status.go       ApplicationStatus enum + transitions
            application_status_test.go
            role_embedding.go           RoleEmbedding (just a 1024-float wrapper)
        entities/
            application.go              Application aggregate
            application_test.go
            judge_job.go                JudgeJob (very small aggregate — claim/complete/fail)
            judge_job_test.go
        events/
            application_events.go       ApplicationScored, ApplicationExcluded, ApplicationEmbedFailed
            application_events_test.go
            intent_embedding_events.go  IntentEmbedded (internal — informational)
        repositories/
            application_repository.go   Port
            intent_embedding_repository.go
            judge_job_repository.go
        services/
            embedder.go                 Embedder port
            match_scorer.go             MatchScorer port (returns RuleMatchReport, embedding_score)
            llm_judge.go                LLMJudge port
            intent_reader.go            Anti-corruption port for hiringintent (already exists in jobposting — mirror that)
    application/
        commands/
            score_candidate.go          Per-CandidateParsed fan-out: upsert Applications, embed candidate, rule+cosine each
            score_candidate_test.go
            score_intent.go             Per-IntentConfirmed fan-out: embed intent, upsert Applications for all tenant candidates, rule+cosine each, enqueue top-K judge jobs
            score_intent_test.go
            judge_application.go        Judge worker entry point: pulls JudgeJob, calls LLMJudge, writes Application.llm_judgment
            judge_application_test.go
        queries/
            list_applications.go        GET /intents/{id}/applications query handler
            list_applications_test.go
        dto/
            application_dto.go          ApplicationListItem + filter/sort DTOs
    infrastructure/
        embedding/
            voyage.go                   Voyage AI HTTP client + embedder adapter
            voyage_test.go              Fake HTTP transport
            stub.go                     Deterministic stub embedder (random-but-stable per content_hash) — for tests
        scoring/
            inproc_match_scorer.go      In-process MatchScorer impl
            inproc_match_scorer_test.go
            serializer.go               Profile-to-embedding-text + RoleSpec-to-embedding-text projections
            serializer_test.go
        judging/
            anthropic_judge.go          Claude tool-use adapter
            anthropic_judge_test.go
            prompts/
                judge_match.tmpl
        persistence/
            postgres_application_repository.go
            postgres_application_repository_test.go    integration-tagged
            application_serializer.go
            postgres_intent_embedding_repository.go
            postgres_intent_embedding_repository_test.go integration-tagged
            postgres_judge_job_repository.go
            postgres_judge_job_repository_test.go      integration-tagged
        clients/
            intent_reader.go            Reads HiringIntent via shared eventbus + a thin Postgres query (no domain coupling)
            intent_reader_test.go       integration-tagged
        subscribers/
            intent_confirmed.go         Consumer wiring: bus subscribes "hiringintent.IntentConfirmed" → application.commands.ScoreIntent
            intent_confirmed_test.go
            candidate_parsed.go         Consumer wiring: bus subscribes "sourcing.CandidateParsed" → application.commands.ScoreCandidate
            candidate_parsed_test.go
        worker/
            match_pool.go               Worker that polls applications WHERE status=New for embedding+rule+cosine
            match_pool_test.go
            judge_pool.go               Worker that polls judge_jobs for LLM judging
            judge_pool_test.go
    delivery/
        http/v1/
            (handlers.go modified — add ListApplications)
            (dto.go modified — add ApplicationListResponse, ApplicationItem, ScoreBlock, etc.)
            (routes.go modified — mount /intents/{intent_id}/applications)
            applications_handler_test.go

tests/
    sourcing_slice3_e2e_test.go         Full flow: upload → scan → extract → parse → CandidateParsed fan-out → IntentConfirmed fan-out → Applications scored → GET returns ranked list
```

### Files modified

- `cmd/api/main.go` — wire Embedder, MatchScorer, LLMJudge, ApplicationRepo, IntentEmbeddingRepo, JudgeJobRepo, match worker, judge worker, two subscribers
- `internal/sourcing/domain/valueobjects/parsed_profile.go` — add `EmbeddingText() string` projection method
- `compose.yml` — add `pgvector` to the postgres image OR document the manual extension install (decided in T1)
- `developer.md` — new env vars (`VOYAGE_API_KEY`, `VOYAGE_MODEL`, `SOURCING_JUDGE_TOP_K`, etc.) and bring-up notes for the pgvector extension
- `docs/api/v1/sourcing.openapi.yaml` — add `/intents/{id}/applications` path + schemas
- `README.md` — flip the sourcing row to "Live (matched scoring)"
- `docs/modules/sourcing/README.md` — pipeline diagram + capability list update

---

## Conventions baked into every task

- **Working branch:** continue on `feat/sourcing-slice-1`. Slice 3 builds on slices 1+2.
- **Module:** `github.com/hustle/hireflow`.
- **Tests:** unit `_test.go`; integration `//go:build integration`.
- **Commit cadence:** one commit per task. **No `Co-Authored-By: Claude` trailers.**
- **Anthropic ZDR + Voyage equivalent:** both providers require zero-data-retention enrollment in prod. Tests use fake HTTP transports.
- **pgvector extension** is required from T1. The CI environment must have `pgvector` installed alongside Postgres 14 (the docker image `postgres:14-alpine` does NOT include pgvector by default — T1 switches the image).

---

## Task 1: pgvector extension + base migration

**Files:**
- Modify: `compose.yml`
- Modify: `developer.md`
- Create: `migrations/sourcing/000005_enable_pgvector.up.sql`
- Create: `migrations/sourcing/000005_enable_pgvector.down.sql`

The default `postgres:14-alpine` image doesn't include pgvector. The `pgvector/pgvector:pg14` image does — same Postgres, same port, plus the extension binaries. Swap the image, then add `CREATE EXTENSION vector` in a migration.

- [ ] **Step 1: Swap the Postgres image**

In `compose.yml`, change the `postgres` service's image:

```yaml
  postgres:
    image: pgvector/pgvector:pg14
    container_name: hireflow-postgres
    ...
```

All other settings stay the same. The image is a drop-in replacement that adds the pgvector extension binaries.

- [ ] **Step 2: Write the up migration**

Create `migrations/sourcing/000005_enable_pgvector.up.sql`:

```sql
CREATE EXTENSION IF NOT EXISTS vector;
```

- [ ] **Step 3: Write the down migration**

Create `migrations/sourcing/000005_enable_pgvector.down.sql`:

```sql
-- We deliberately do NOT drop the extension on down — other contexts may
-- start using it. Migration 000006's down drops the only consumer (applications
-- + hiring_intent_embeddings), which is sufficient for a clean rollback.
```

- [ ] **Step 4: Verification**

If containers are running:
```
docker compose down
docker compose up -d --wait
make migrate-up
psql "$DATABASE_URL" -c "SELECT extname FROM pg_extension WHERE extname='vector';"
```
Should print one row: `vector`.

If containers aren't running, skip — T17 (e2e) will catch any DDL issues.

- [ ] **Step 5: Commit**

```bash
git add compose.yml migrations/sourcing/000005_enable_pgvector.up.sql \
        migrations/sourcing/000005_enable_pgvector.down.sql developer.md
git commit -m "feat(sourcing): enable pgvector extension"
```

---

## Task 2: Application + intent embedding tables

**Files:**
- Create: `migrations/sourcing/000006_create_applications.up.sql`
- Create: `migrations/sourcing/000006_create_applications.down.sql`

Adds:
1. `vector(1024)` column on `candidates` (was deliberately omitted in slice 2 — added now that pgvector is enabled).
2. `hiring_intent_embeddings` table — un-partitioned, primary key `(intent_id, spec_version)`.
3. `applications` table — partition-ready (`PARTITION BY LIST (tenant_id)` with default partition, per spec D10).

- [ ] **Step 1: Up migration**

Create `migrations/sourcing/000006_create_applications.up.sql`:

```sql
-- Profile embedding column on candidates. Slice 2 introduced the candidates
-- table without this column because pgvector wasn't yet enabled.
ALTER TABLE candidates
    ADD COLUMN profile_embedding vector(1024);

-- ivfflat ANN index. Lists=100 is a reasonable default for up to ~1M rows;
-- can be REINDEX-ed with higher lists once we scale.
CREATE INDEX candidates_profile_embedding_idx
    ON candidates USING ivfflat (profile_embedding vector_cosine_ops)
    WITH (lists = 100);

-- hiring_intent_embeddings: cached embedding for each (intent, spec_version).
-- Re-confirming an intent with a changed RoleSpec bumps spec_version and
-- triggers a fresh embedding compute.
CREATE TABLE hiring_intent_embeddings (
    intent_id      UUID         NOT NULL,
    tenant_id      UUID         NOT NULL,
    spec_version   INT          NOT NULL,
    role_embedding vector(1024) NOT NULL,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (intent_id, spec_version)
);

CREATE INDEX hiring_intent_embeddings_tenant_idx
    ON hiring_intent_embeddings (tenant_id);

-- applications: per (Candidate, Intent) pair. Partition-ready by tenant.
CREATE TABLE applications (
    id                     UUID         NOT NULL,
    tenant_id              UUID         NOT NULL,
    candidate_id           UUID         NOT NULL,
    intent_id              UUID         NOT NULL,
    intent_spec_version    INT          NOT NULL,
    profile_schema_version INT          NOT NULL,

    status                 TEXT         NOT NULL,
    overall_score          NUMERIC(5,2),                   -- 0..100, populated after LLM judge
    score_band             TEXT,                            -- 'strong' | 'moderate' | 'weak' | NULL
    rule_match             JSONB        NOT NULL,           -- structured per-criterion report
    embedding_score        NUMERIC(5,4),                   -- cosine sim, null if rule-failed or embed-failed
    llm_judgment           JSONB,                           -- {score, evidence[], summary, concerns[], prompt_version}
    last_error             TEXT,
    attempt_count          INT          NOT NULL DEFAULT 0,
    next_attempt_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),

    scored_at              TIMESTAMPTZ,
    created_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, id),

    CONSTRAINT applications_status_check
        CHECK (status IN (
            'New','Scored','Excluded','EmbedFailed','JudgeFailed','Stale',
            'Shortlisted','Rejected','Interviewing','Hired'   -- slice 4 statuses, declared now for forward-compat
        )),
    CONSTRAINT applications_score_band_check
        CHECK (score_band IS NULL OR score_band IN ('strong','moderate','weak'))
) PARTITION BY LIST (tenant_id);

CREATE TABLE applications_default PARTITION OF applications DEFAULT;

-- Unique constraint: at most one Application per (Candidate, Intent) per tenant.
-- Partitioned-table rule: must include the partition key.
CREATE UNIQUE INDEX applications_uniq_idx
    ON applications (tenant_id, candidate_id, intent_id);

-- Recruiter list view: per-intent, sorted by overall_score desc (judged first).
CREATE INDEX applications_intent_score_idx
    ON applications (tenant_id, intent_id, overall_score DESC NULLS LAST);

-- Match worker poll index.
CREATE INDEX applications_match_pending_idx
    ON applications (tenant_id, next_attempt_at)
    WHERE status IN ('New');

-- Stale detection (slice 4 background reconciler will use this).
CREATE INDEX applications_stale_idx
    ON applications (tenant_id, intent_id)
    WHERE status = 'Stale';
```

- [ ] **Step 2: Down migration**

Create `migrations/sourcing/000006_create_applications.down.sql`:

```sql
DROP TABLE IF EXISTS applications_default;
DROP TABLE IF EXISTS applications;
DROP TABLE IF EXISTS hiring_intent_embeddings;
ALTER TABLE candidates DROP COLUMN IF EXISTS profile_embedding;
```

- [ ] **Step 3: Apply + verify (DB-available only)**

```
make migrate-up
psql "$DATABASE_URL" -c "\d applications" | head -25
psql "$DATABASE_URL" -c "\d hiring_intent_embeddings"
```

If no DB available, skip — T17 will catch DDL issues.

- [ ] **Step 4: Commit**

```bash
git add migrations/sourcing/000006_create_applications.up.sql \
        migrations/sourcing/000006_create_applications.down.sql
git commit -m "feat(sourcing): applications + hiring_intent_embeddings tables"
```

---

## Task 3: judge_jobs queue table

**Files:**
- Create: `migrations/sourcing/000007_create_judge_jobs.up.sql`
- Create: `migrations/sourcing/000007_create_judge_jobs.down.sql`

A small queue table the judge worker pulls from. Populated by `ScoreIntent` (after rule+embedding pass) with up to top-K applications.

- [ ] **Step 1: Up migration**

Create `migrations/sourcing/000007_create_judge_jobs.up.sql`:

```sql
CREATE TABLE judge_jobs (
    id              UUID         PRIMARY KEY,
    tenant_id       UUID         NOT NULL,
    application_id  UUID         NOT NULL,
    intent_id       UUID         NOT NULL,           -- denormalized for poll efficiency
    coarse_score    NUMERIC(7,4) NOT NULL,           -- the sort key (descending)
    status          TEXT         NOT NULL DEFAULT 'Pending',
    attempt_count   INT          NOT NULL DEFAULT 0,
    last_error      TEXT,
    next_attempt_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    enqueued_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ,

    CONSTRAINT judge_jobs_status_check
        CHECK (status IN ('Pending','Running','Done','Failed'))
);

CREATE INDEX judge_jobs_pending_idx
    ON judge_jobs (next_attempt_at)
    WHERE status IN ('Pending','Running');

CREATE INDEX judge_jobs_intent_idx ON judge_jobs (intent_id);
```

- [ ] **Step 2: Down migration**

Create `migrations/sourcing/000007_create_judge_jobs.down.sql`:

```sql
DROP TABLE IF EXISTS judge_jobs;
```

- [ ] **Step 3: Apply + verify (DB-available only)**

- [ ] **Step 4: Commit**

```bash
git add migrations/sourcing/000007_create_judge_jobs.up.sql \
        migrations/sourcing/000007_create_judge_jobs.down.sql
git commit -m "feat(sourcing): judge_jobs queue table"
```

---

## Task 4: Application + JudgeJob value objects + status enum

**Files:**
- Create: `internal/sourcing/domain/valueobjects/application_status.go` + `_test.go`
- Create: `internal/sourcing/domain/valueobjects/match_score.go` + `_test.go`
- Create: `internal/sourcing/domain/valueobjects/rule_match.go` + `_test.go`
- Create: `internal/sourcing/domain/valueobjects/llm_judgment.go` + `_test.go`
- Create: `internal/sourcing/domain/valueobjects/role_embedding.go`

Pure-Go value objects, no I/O. Mirror the slice-1/2 patterns:
- `ApplicationStatus` — enum with `ParseApplicationStatus`, `IsTerminal()`, `CanTransitionTo()`.
- `MatchScore` — struct holding `Overall float64`, `EmbeddingScore float64`, `Band ScoreBand`, plus a `DeriveBand(overall float64) ScoreBand` helper for the thresholds (≥80, 60–79, <60).
- `RuleCriterion`, `RuleResult`, `RuleMatchReport` — the structured pass/fail data structure.
- `LLMJudgment`, `JudgmentEvidence`, `JudgmentConcern` — the structured judgment data.
- `RoleEmbedding` — thin wrapper around `[]float32` with `Dim()` validator (must equal 1024).

TDD: write tests first, run, implement, run again.

**Implementation guidance:**
- `ApplicationStatus` permitted transitions (slice 3 only): `New → Scored | Excluded | EmbedFailed`, `Scored → JudgeFailed | Stale`, terminals `Excluded`/`EmbedFailed`/`JudgeFailed`/`Stale` can all be reset to `New` via the explicit rescore path (slice 4).
- `MatchScore.DeriveBand` thresholds match the scoring doc (`docs/modules/sourcing/scoring.md` §4).
- `RuleMatchReport` exposes `RequiredPassRate() float64`, `PassedRequired() bool`, `Marshal()/UnmarshalRuleMatch()`.
- `LLMJudgment` exposes `Marshal()/UnmarshalLLMJudgment()` for pgx jsonb round-trip.

Tests cover: parse-known-values, transitions (allowed + disallowed), terminal checks, band thresholds (test 79.99, 80, 60, 59.99), rule-match required-pass-rate math, jsonb round-trip for all.

- [ ] **Step 1–N: TDD per VO file**

Follow the slice-1/2 pattern (write test → run → implement → run).

- [ ] **Verification + commit**

```
go test ./internal/sourcing/domain/valueobjects/... -v -count=1
go vet ./...
git add internal/sourcing/domain/valueobjects/
git commit -m "feat(sourcing): application-side value objects"
```

---

## Task 5: Application + JudgeJob entities

**Files:**
- Create: `internal/sourcing/domain/entities/application.go` + `_test.go`
- Create: `internal/sourcing/domain/entities/judge_job.go` + `_test.go`

### `Application` aggregate

Per `(Candidate, Intent, intent_spec_version)`. State + lifecycle methods:

```go
type NewApplicationInput struct {
    TenantID             shared.TenantID
    CandidateID          uuid.UUID
    IntentID             uuid.UUID
    IntentSpecVersion    int
    ProfileSchemaVersion int
    Now func() time.Time
    ID  uuid.UUID
}

func NewApplication(in NewApplicationInput) (*Application, error)

// Accessors: ID/TenantID/CandidateID/IntentID/IntentSpecVersion/
//             ProfileSchemaVersion/Status/OverallScore/ScoreBand/
//             RuleMatch/EmbeddingScore/LLMJudgment/LastError/
//             AttemptCount/NextAttemptAt/ScoredAt/CreatedAt/UpdatedAt

// Lifecycle:
func (a *Application) RecordRuleMatch(report vo.RuleMatchReport) error  // status=New only
func (a *Application) Exclude(reason string) error                       // status=New only → Excluded
func (a *Application) RecordEmbeddingScore(score float64) error          // status=New, only after RecordRuleMatch
func (a *Application) MarkEmbedFailed(reason string) error               // status=New → EmbedFailed
func (a *Application) MarkScored(overallScore *float64) error            // status=New → Scored (overallScore nil if not judged)
func (a *Application) RecordLLMJudgment(j vo.LLMJudgment) error          // status=Scored, sets overall_score + band
func (a *Application) MarkJudgeFailed(reason string) error               // status=Scored → JudgeFailed
func (a *Application) MarkStale() error                                  // any non-terminal → Stale
func (a *Application) ScheduleRetry(reason string, now time.Time, schedule []time.Duration)

func (a *Application) PullEvents() []events.Event
func RehydrateApplication(in RehydrateApplicationInput) *Application
```

Emits:
- `ApplicationScored` after `MarkScored`
- `ApplicationExcluded` after `Exclude`
- `ApplicationEmbedFailed` after `MarkEmbedFailed`
- `ApplicationJudgeFailed` after `MarkJudgeFailed`

### `JudgeJob` entity

Tiny — just a row that says "judge this application." Methods: `BeginRunning`, `Complete`, `Fail`. Doesn't emit events (it's an internal queue artifact).

```go
type JudgeJob struct {
    id, tenantID, applicationID, intentID  // ids
    coarseScore float64
    status vo.JudgeJobStatus  // 'Pending','Running','Done','Failed'
    attemptCount int
    lastError string
    nextAttemptAt time.Time
    enqueuedAt time.Time
    completedAt *time.Time
}

func NewJudgeJob(in NewJudgeJobInput) *JudgeJob
func (j *JudgeJob) BeginRunning() error
func (j *JudgeJob) Complete()
func (j *JudgeJob) Fail(reason string, now time.Time, schedule []time.Duration)
```

Tests cover all valid + invalid transitions, the rule-match-without-passing-required path (sets status to Excluded), the embedding-score-only-after-rule-match guard, retry math.

- [ ] **Steps + verification + commit** — follow slice-1/2 pattern.

```
git add internal/sourcing/domain/entities/application.go \
        internal/sourcing/domain/entities/application_test.go \
        internal/sourcing/domain/entities/judge_job.go \
        internal/sourcing/domain/entities/judge_job_test.go
git commit -m "feat(sourcing): Application aggregate and JudgeJob entity"
```

---

## Task 6: Application + JudgeJob events

**Files:**
- Create: `internal/sourcing/domain/events/application_events.go` + `_test.go`

Events emitted from the Application aggregate:

| Event | When | Fields |
|---|---|---|
| `ApplicationScored` | After `MarkScored` (includes both judged and not-yet-judged) | application_id, candidate_id, intent_id, tenant_id, overall_score *float64, score_band string, embedding_score float64 |
| `ApplicationExcluded` | After `Exclude` | application_id, candidate_id, intent_id, tenant_id, reason string |
| `ApplicationEmbedFailed` | After `MarkEmbedFailed` | application_id, tenant_id, reason string |
| `ApplicationJudgeFailed` | After `MarkJudgeFailed` | application_id, tenant_id, reason string |

All implement the `events.Event` interface (slice 1 pattern).

Tests cover event-name strings, accessor methods, and JSON round-trip.

Also extend `outbox_dispatcher.go`'s `decodeEvent` switch (touched in slice 2's fix commit) to know about the four new event names.

- [ ] **Steps + verification + commit**

```
git add internal/sourcing/domain/events/application_events.go \
        internal/sourcing/domain/events/application_events_test.go \
        internal/sourcing/infrastructure/messaging/outbox_dispatcher.go
git commit -m "feat(sourcing): application-side events"
```

---

## Task 7: Domain ports — `Embedder`, `MatchScorer`, `LLMJudge`, repositories

**Files:**
- Create: `internal/sourcing/domain/services/embedder.go`
- Create: `internal/sourcing/domain/services/match_scorer.go`
- Create: `internal/sourcing/domain/services/llm_judge.go`
- Create: `internal/sourcing/domain/services/intent_reader.go`
- Create: `internal/sourcing/domain/repositories/application_repository.go`
- Create: `internal/sourcing/domain/repositories/intent_embedding_repository.go`
- Create: `internal/sourcing/domain/repositories/judge_job_repository.go`

Port interfaces only, no impls (impls land in tasks 8–13). Reference scoring.md for exact contracts.

```go
// embedder.go
type EmbeddingError struct {
    Retryable bool
    Reason    string
    Detail    string
}
func (e EmbeddingError) Error() string { ... }

type Embedder interface {
    // EmbedDocument embeds a single text into a 1024-dim vector. Errors should
    // be EmbeddingError when classified; raw errors are treated retryable.
    EmbedDocument(ctx context.Context, text string) ([]float32, error)
}

// match_scorer.go
type MatchInput struct {
    Profile         vo.ParsedProfile
    Role            services.RoleSpec       // anti-corruption type, see intent_reader.go
    CandidateVec    []float32
    RoleVec         []float32
}

type MatchOutput struct {
    Rules           vo.RuleMatchReport
    EmbeddingScore  *float64                 // nil if rule-failed
    CoarseScore     *float64                 // nil if rule-failed
}

type MatchScorer interface {
    Score(ctx context.Context, in MatchInput) (MatchOutput, error)
}

// llm_judge.go
type JudgeError struct { Retryable bool; Reason, Detail string }
func (e JudgeError) Error() string { ... }

type LLMJudge interface {
    Judge(ctx context.Context, profile vo.ParsedProfile, role services.RoleSpec,
          rules vo.RuleMatchReport) (vo.LLMJudgment, error)
}

// intent_reader.go — anti-corruption (mirrors jobposting/infrastructure/clients/IntentReader)
type RoleSpec struct {
    Title           string
    RequiredSkills  []SkillSpec
    OptionalSkills  []SkillSpec
    MinYears        int
    MaxYears        int
    Locations       []string
    WorkMode        string  // 'remote'|'hybrid'|'onsite'
    Degree          string
    Languages       []string
}
type SkillSpec struct { Name string; MinYears float64 }

type IntentSnapshot struct {
    ID          uuid.UUID
    TenantID    shared.TenantID
    Status      string  // we only score Confirmed
    SpecVersion int
    Role        RoleSpec
}

type IntentReader interface {
    // FindByID — tenant-scoped lookup of an intent snapshot.
    FindByID(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (IntentSnapshot, error)
    // ListConfirmedIntents — tenant-scoped list of all currently-Confirmed intents.
    ListConfirmedIntents(ctx context.Context, tenant shared.TenantID) ([]IntentSnapshot, error)
}
```

Repositories:

```go
// application_repository.go
type ApplicationRepository interface {
    // Save upserts on (tenant_id, candidate_id, intent_id). Atomically writes
    // pending events to sourcing_outbox.
    Save(ctx context.Context, a *entities.Application) error

    // FindByID — tenant-scoped.
    FindByID(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (*entities.Application, error)

    // FindByCandidateAndIntent — tenant-scoped lookup of the unique row.
    FindByCandidateAndIntent(ctx context.Context, tenant shared.TenantID,
        candidateID, intentID uuid.UUID) (*entities.Application, error)

    // ListByIntent — for the GET endpoint.
    ListByIntent(ctx context.Context, tenant shared.TenantID, intentID uuid.UUID,
        filter ApplicationListFilter) ([]*entities.Application, error)

    // ClaimNextNew — match worker entry point. Returns ErrApplicationNotFound
    // when nothing's ready.
    ClaimNextNew(ctx context.Context) (*entities.Application, error)

    // ListCandidatesScoredForIntent — used by ScoreIntent to pick top-K.
    // Returns ordered by coarse_score (= 100*required_pass_rate + 20*embedding_score) desc.
    TopByCoarseScoreForIntent(ctx context.Context, tenant shared.TenantID,
        intentID uuid.UUID, limit int) ([]*entities.Application, error)
}

// intent_embedding_repository.go
type IntentEmbeddingRepository interface {
    Save(ctx context.Context, intentID uuid.UUID, tenant shared.TenantID,
        specVersion int, vector []float32) error
    Find(ctx context.Context, intentID uuid.UUID, specVersion int) ([]float32, error)
}

// judge_job_repository.go
type JudgeJobRepository interface {
    Save(ctx context.Context, j *entities.JudgeJob) error
    ClaimNextPending(ctx context.Context) (*entities.JudgeJob, error)
    FindByID(ctx context.Context, id uuid.UUID) (*entities.JudgeJob, error)
}
```

- [ ] **Verification + commit**

```
go build ./...
go vet ./...
git add internal/sourcing/domain/services/ internal/sourcing/domain/repositories/
git commit -m "feat(sourcing): scoring/judging/intent ports"
```

---

## Task 8: Voyage embedder adapter + stub

**Files:**
- Create: `internal/sourcing/infrastructure/embedding/voyage.go` + `_test.go`
- Create: `internal/sourcing/infrastructure/embedding/stub.go` + `_test.go`

### `Voyage` adapter

Voyage AI's API is documented at `https://docs.voyageai.com/`. The relevant endpoint is `POST https://api.voyageai.com/v1/embeddings` with body `{ "input": ["text..."], "model": "voyage-3" }` and returns `{"data":[{"embedding":[1024 floats]}]}`. Auth via `Authorization: Bearer $VOYAGE_API_KEY` header.

No Go SDK exists; write a thin client:

```go
type VoyageClient struct {
    apiKey string
    model  string
    http   *http.Client
}

type Voyage struct {
    client *VoyageClient
}

func NewVoyage(client *VoyageClient) *Voyage
func NewVoyageClient(apiKey, model string) *VoyageClient

func (v *Voyage) EmbedDocument(ctx context.Context, text string) ([]float32, error)
```

Error classification:
- HTTP 429/5xx → `services.EmbeddingError{Retryable: true}`
- HTTP 400/401/422 → `services.EmbeddingError{Retryable: false}`
- Bad response shape (wrong dim, missing field) → `services.EmbeddingError{Retryable: false}`

Test pattern: fake HTTP transport returning canned response bodies (mirror `anthropic_extractor_test.go`). Cover happy path, 429 → retryable, 401 → non-retryable, wrong dimension → non-retryable.

### `Stub` adapter (test-only)

Deterministic embedder for tests that need a real embedding shape but don't want to call Voyage:

```go
type Stub struct{}

func (Stub) EmbedDocument(_ context.Context, text string) ([]float32, error) {
    // Use sha256(text) to seed a Rand, generate a stable 1024-dim normalized vector.
    h := sha256.Sum256([]byte(text))
    src := rand.NewSource(int64(binary.BigEndian.Uint64(h[:8])))
    r := rand.New(src)
    out := make([]float32, 1024)
    var norm float64
    for i := range out {
        out[i] = float32(r.NormFloat64())
        norm += float64(out[i]) * float64(out[i])
    }
    norm = math.Sqrt(norm)
    for i := range out {
        out[i] /= float32(norm) // L2-normalize → cosine sim = dot product
    }
    return out, nil
}
```

Why deterministic: tests need stable cosine similarities for assertions.

- [ ] **TDD + verification + commit**

```
git add internal/sourcing/infrastructure/embedding/
git commit -m "feat(sourcing): Voyage embedder adapter + deterministic stub"
```

---

## Task 9: Profile/Role embedding-text serializer

**Files:**
- Create: `internal/sourcing/infrastructure/scoring/serializer.go` + `_test.go`

Implements the projections described in `scoring.md` §2:

```go
// SerializeProfile produces the embedding-input text for a Candidate.
// Excludes PII; includes headline, summary, skills, experiences.
func SerializeProfile(p vo.ParsedProfile) string

// SerializeRole produces the embedding-input text for an Intent's RoleSpec.
// Symmetric with SerializeProfile so the resulting vectors live in the same space.
func SerializeRole(r services.RoleSpec) string
```

Tests assert specific output shapes for various inputs (golden-test style with literal expected strings — small, focused).

- [ ] **TDD + verification + commit**

```
git add internal/sourcing/infrastructure/scoring/serializer.go \
        internal/sourcing/infrastructure/scoring/serializer_test.go
git commit -m "feat(sourcing): profile and role embedding-text serializers"
```

---

## Task 10: In-process `MatchScorer`

**Files:**
- Create: `internal/sourcing/infrastructure/scoring/inproc_match_scorer.go` + `_test.go`

Pure-Go impl. No I/O. Inputs: `ParsedProfile`, `RoleSpec`, `candidate_vector`, `role_vector`. Output: `RuleMatchReport` + `embedding_score` + `coarse_score`.

```go
type InProcMatchScorer struct{}

func NewInProcMatchScorer() *InProcMatchScorer

func (s *InProcMatchScorer) Score(ctx context.Context, in services.MatchInput) (services.MatchOutput, error) {
    // 1. Build rule_match: for each criterion in role.RequiredSkills + role.OptionalSkills
    //    + experience range + location/work_mode + degree, produce a pass/fail.
    rules := buildRuleMatch(in.Profile, in.Role)

    // 2. If required-pass-rate < 1.0, return MatchOutput{Rules: rules} (embedding_score nil → caller excludes).
    if !rules.PassedRequired() {
        return services.MatchOutput{Rules: rules}, nil
    }

    // 3. Cosine similarity from the two vectors.
    sim := cosineSimilarity(in.CandidateVec, in.RoleVec)

    // 4. Coarse score: rules.RequiredPassRate()*100 + sim*20.
    coarse := rules.RequiredPassRate()*100 + sim*20

    return services.MatchOutput{
        Rules:          rules,
        EmbeddingScore: &sim,
        CoarseScore:    &coarse,
    }, nil
}

// cosineSimilarity expects L2-normalized vectors (Voyage returns these) — so
// cos(a,b) = dot(a,b). For safety we still divide by norms.
func cosineSimilarity(a, b []float32) float64 { ... }
```

Tests cover:
- Required-pass-rate math (3/3, 2/3, 1/3, 0/3)
- All-required-pass + cosine 0.8 → coarse score ~116
- Missing required skill → output has nil EmbeddingScore, no CoarseScore
- Experience range: under, in-range, over
- Location: exact match, csv match, "remote" work_mode bypass
- Cosine of identical vectors → 1.0
- Cosine of orthogonal vectors → 0.0

- [ ] **TDD + verification + commit**

```
git add internal/sourcing/infrastructure/scoring/inproc_match_scorer.go \
        internal/sourcing/infrastructure/scoring/inproc_match_scorer_test.go
git commit -m "feat(sourcing): in-process MatchScorer with rule engine and cosine"
```

---

## Task 11: Anthropic LLMJudge adapter + prompt + schema

**Files:**
- Create: `internal/sourcing/infrastructure/judging/anthropic_judge.go` + `_test.go`
- Create: `internal/sourcing/infrastructure/judging/prompts/judge_match.tmpl`

Same pattern as slice 2's `AnthropicParser` (T9 of slice 2). Forced tool-use against `judge_match` schema. Returns `vo.LLMJudgment`.

Schema fields:
```json
{
  "score":    "integer 0-100",
  "evidence": [{"skill": "string", "claim": "string", "support": "string"}, ...],
  "summary":  "string (2 sentences)",
  "concerns": ["string", ...]
}
```

Prompt (embedded via `//go:embed`): instructs the judge to ground each claim in evidence from experience prose, flag career gaps, flag unsupported skill claims, return a structured score.

Adapter exposes `PromptVersion = "v1"`. Stamped onto `LLMJudgment.PromptVersion` so future bumps don't invalidate historical scores.

Error classification: same as slice 2 parser — `services.JudgeError{Retryable bool, Reason, Detail string}`.

- [ ] **TDD + verification + commit**

```
git add internal/sourcing/infrastructure/judging/
git commit -m "feat(sourcing): Anthropic LLMJudge adapter with forced tool-use"
```

---

## Task 12: Postgres repositories (Application, IntentEmbedding, JudgeJob)

**Files:**
- Create: `internal/sourcing/infrastructure/persistence/application_serializer.go`
- Create: `internal/sourcing/infrastructure/persistence/postgres_application_repository.go` + `_test.go`
- Create: `internal/sourcing/infrastructure/persistence/postgres_intent_embedding_repository.go` + `_test.go`
- Create: `internal/sourcing/infrastructure/persistence/postgres_judge_job_repository.go` + `_test.go`

All three follow the slice-1 `PostgresResumeUploadRepository` pattern. Notes:

### Application repo

- `Save` uses upsert on `(tenant_id, candidate_id, intent_id)` unique index. Writes outbox events atomically (just like ResumeUpload).
- `TopByCoarseScoreForIntent` SQL:
  ```sql
  SELECT * FROM applications
  WHERE tenant_id=$1 AND intent_id=$2 AND status='New' AND embedding_score IS NOT NULL
  ORDER BY (
    (CASE WHEN (rule_match->'required_pass_rate')::numeric > 0 THEN (rule_match->'required_pass_rate')::numeric ELSE 0 END) * 100
    + COALESCE(embedding_score, 0) * 20
  ) DESC
  LIMIT $3
  ```
  Stores `required_pass_rate` in `rule_match` jsonb for this query.

- `ClaimNextNew` matches the slice-1 simple-polling pattern: select `WHERE status='New' AND next_attempt_at <= now() ORDER BY next_attempt_at LIMIT 1`. (Slice 4 hardens this with `FOR UPDATE SKIP LOCKED` inside a tx.)

### IntentEmbedding repo

- `Save` is upsert on `(intent_id, spec_version)` — re-confirming the same spec_version is a no-op.
- pgx handles `vector(1024)` columns by registering a type via `pgvector/pgvector-go`'s `Vector` wrapper. Add the registration in `cmd/api/main.go`.

### JudgeJob repo

- Standard CRUD. `ClaimNextPending` picks Pending or Running-but-stale jobs.

Integration tests for all three, build-tag `integration`. Standard test pattern from slice 1/2.

- [ ] **TDD + verification + commit**

```
git add internal/sourcing/infrastructure/persistence/
git commit -m "feat(sourcing): postgres repos for Application, IntentEmbedding, JudgeJob"
```

---

## Task 13: IntentReader adapter

**Files:**
- Create: `internal/sourcing/infrastructure/clients/intent_reader.go` + `_test.go`

Mirrors `internal/jobposting/infrastructure/clients/IntentReader`. Queries `hiring_intents` table directly via pgx — no domain coupling. Translates the persisted shape into the `services.IntentSnapshot` anti-corruption type defined in T7.

```go
type PostgresIntentReader struct {
    pool *pgxpool.Pool
}

func NewPostgresIntentReader(pool *pgxpool.Pool) *PostgresIntentReader

func (r *PostgresIntentReader) FindByID(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (services.IntentSnapshot, error)
func (r *PostgresIntentReader) ListConfirmedIntents(ctx context.Context, tenant shared.TenantID) ([]services.IntentSnapshot, error)
```

Look at `internal/jobposting/infrastructure/clients/intent_reader.go` for the exact SQL + jsonb deserialization shape — copy that, adapt the output type.

**spec_version note:** `hiring_intents` doesn't currently have a `spec_version` column. Slice 3 introduces it implicitly via a small migration in T1 (TODO: add `ALTER TABLE hiring_intents ADD COLUMN spec_version INT NOT NULL DEFAULT 1` to migration 000005 or 000006). The first confirm initializes it at 1; re-confirming bumps via the `hiringintent` aggregate (a separate migration that team will own).

Actually — to avoid coupling slice 3 to a `hiringintent` schema change, simpler: the IntentReader fakes `spec_version` as the count of `confirmed_at` updates (initially 1; if `hiringintent` later supports re-confirm, that team adds it). For now, return `1` always. Document this in the adapter doc-comment.

Integration test against real Postgres: insert a row into `hiring_intents` via raw SQL, call `FindByID`, assert the shape.

- [ ] **TDD + verification + commit**

```
git add internal/sourcing/infrastructure/clients/
git commit -m "feat(sourcing): IntentReader anti-corruption adapter"
```

---

## Task 14: Application commands — `ScoreCandidate`, `ScoreIntent`, `JudgeApplication`

**Files:**
- Create: `internal/sourcing/application/commands/score_candidate.go` + `_test.go`
- Create: `internal/sourcing/application/commands/score_intent.go` + `_test.go`
- Create: `internal/sourcing/application/commands/judge_application.go` + `_test.go`

### `ScoreCandidate`

Triggered by `CandidateParsed`. Flow:
1. Fetch candidate.
2. If `candidate.profile_embedding` is unset, call `Embedder.EmbedDocument(SerializeProfile(candidate.profile))` and persist on `candidates.profile_embedding`.
3. List all `Confirmed` intents in the tenant via `IntentReader.ListConfirmedIntents`.
4. For each intent:
   a. Find or create `Application(candidate, intent, intent.spec_version, profile.schema_version)`.
   b. Fetch role embedding via `IntentEmbeddingRepository.Find(intent.id, intent.spec_version)`. If missing, embed and save.
   c. Call `MatchScorer.Score`. Apply result to Application (`RecordRuleMatch`, then `Exclude` OR `RecordEmbeddingScore` + `MarkScored(nil)`).
   d. Persist Application.

Does NOT enqueue judge jobs — that happens on `IntentConfirmed` fan-out (slice-3 semantics: judging is intent-driven, not candidate-driven).

### `ScoreIntent`

Triggered by `IntentConfirmed`. Flow:
1. Fetch intent snapshot.
2. Compute or reuse role embedding for `(intent.id, intent.spec_version)`.
3. List all parsed candidates in the tenant.
4. For each candidate:
   a. Find or create Application.
   b. Compute rule + embedding score via `MatchScorer.Score`.
   c. Persist Application.
5. After the loop, fetch top-K via `ApplicationRepository.TopByCoarseScoreForIntent`.
6. For each top-K Application, insert a `JudgeJob` (idempotent — if a Pending job exists for this `application_id`, skip).

### `JudgeApplication`

Triggered by the judge worker pool. Handles one `JudgeJob`:
1. Load the Application + Candidate + Intent.
2. Call `LLMJudge.Judge(profile, role, rule_match)`.
3. On success: `application.RecordLLMJudgment(judgment)`, persist. Mark `judge_job.Complete()`.
4. On retryable error: `job.Fail(reason, schedule)` — worker re-picks it later.
5. On fatal error or max retries: `application.MarkJudgeFailed(reason)`, persist. Mark `job.Fail` terminal.

All three commands use in-memory fakes in tests: `fakeApplicationRepo`, `fakeEmbedder`, `fakeMatchScorer`, `fakeLLMJudge`, `fakeIntentReader`. Cover happy paths, embedding-failed, rule-failed, judge-failed-retryable, judge-failed-fatal.

- [ ] **TDD per command + verification + commit**

```
git add internal/sourcing/application/commands/
git commit -m "feat(sourcing): scoring commands — ScoreCandidate, ScoreIntent, JudgeApplication"
```

---

## Task 15: `ListApplications` query + DTOs

**Files:**
- Create: `internal/sourcing/application/queries/list_applications.go` + `_test.go`
- Modify: `internal/sourcing/application/dto/batch_dto.go` (append application DTOs)

```go
type ListApplicationsHandler struct {
    repo repositories.ApplicationRepository
}

type ListApplicationsInput struct {
    TenantID  shared.TenantID
    IntentID  uuid.UUID
    Filter    repositories.ApplicationListFilter  // status, min_score, sort, limit, offset
}

type ApplicationListItemDTO struct {
    ApplicationID   uuid.UUID
    CandidateID     uuid.UUID
    CandidateName   string   // masked: "A***"  (PII not decrypted in list view)
    Headline        string
    Location        string
    Status          string
    OverallScore    *float64
    ScoreBand       *string
    EmbeddingScore  *float64
    RuleMatch       json.RawMessage
    LLMJudgment     json.RawMessage   // populated only for judged rows
    ScoredAt        *time.Time
    UpdatedAt       time.Time
}

type ApplicationListResponse struct {
    Items  []ApplicationListItemDTO
    Total  int
    Facets ApplicationListFacets   // counts per score band
}

type ApplicationListFacets struct {
    Strong   int
    Moderate int
    Weak     int
}
```

Tests cover: empty list, mixed-status list, filter by `status`, filter by `min_score`, sort by `score_desc` vs `recent`, masking applied.

- [ ] **TDD + verification + commit**

```
git add internal/sourcing/application/queries/list_applications.go \
        internal/sourcing/application/queries/list_applications_test.go \
        internal/sourcing/application/dto/batch_dto.go
git commit -m "feat(sourcing): ListApplications query"
```

---

## Task 16: Event subscribers — `IntentConfirmedConsumer`, `CandidateParsedConsumer`

**Files:**
- Create: `internal/sourcing/infrastructure/subscribers/intent_confirmed.go` + `_test.go`
- Create: `internal/sourcing/infrastructure/subscribers/candidate_parsed.go` + `_test.go`

Mirror `internal/jobposting/infrastructure/subscribers/intent_confirmed.go` for the wiring shape.

Each subscriber:
1. Registers with the in-process bus (`eventbus.InMemory`) for the specific event name.
2. On event, unmarshals payload, calls the corresponding command handler.
3. Returns error from command → bus surfaces it → outbox dispatcher leaves the row undispatched for retry.

```go
type IntentConfirmedConsumer struct {
    cmd *commands.ScoreIntentHandler
    logger zerolog.Logger
}

func (c *IntentConfirmedConsumer) Handle(ctx context.Context, event any) error {
    // event is the deserialized intentevents.IntentConfirmed (from hiringintent)
    ...
    return c.cmd.Handle(ctx, in)
}
```

The `intentevents.IntentConfirmed` event type needs to be importable from `hiringintent/domain/events`. It already is — slice 1's `jobposting` subscribes to it.

Tests: instantiate a bus, register the consumer, publish a fake event, assert the command was called.

- [ ] **TDD + verification + commit**

```
git add internal/sourcing/infrastructure/subscribers/
git commit -m "feat(sourcing): IntentConfirmed and CandidateParsed subscribers"
```

---

## Task 17: Match worker + judge worker pools

**Files:**
- Create: `internal/sourcing/infrastructure/worker/match_pool.go` + `_test.go`
- Create: `internal/sourcing/infrastructure/worker/judge_pool.go` + `_test.go`

Two new pools, same shape as the slice-1 `worker.Pool`:

```go
type MatchPool struct {
    repo    repositories.ApplicationRepository
    handler *commands.ScoreApplicationHandler  // per-Application embedding+rule+cosine
    cfg     Config
    logger  zerolog.Logger
}

type JudgePool struct {
    repo    repositories.JudgeJobRepository
    handler *commands.JudgeApplicationHandler
    cfg     Config
    logger  zerolog.Logger
}
```

`Run(ctx)` fans out N goroutines, each polling its repo's `ClaimNextNew` / `ClaimNextPending` and dispatching to the handler.

Wait — there's a wrinkle. The plan above has `ScoreCandidate` and `ScoreIntent` as "fan-out" commands that do the per-Application work inline. That conflicts with having a "match worker" that picks up Applications individually.

**Resolution:** ScoreCandidate / ScoreIntent are the **fan-out** commands invoked by the subscribers — they create Applications in `New` status without doing the embedding+rule work. The match worker (`ScoreApplicationHandler`) picks up `New` Applications one at a time and does the actual scoring work. This decouples fan-out (sync, fast) from scoring (async, expensive embedding API calls).

So:
- Subscriber gets `CandidateParsed` → `ScoreCandidate.Handle()` → creates N Application rows in New status → done.
- Match worker pulls each New Application → `ScoreApplicationHandler.Handle(app)` → embeds candidate (if not yet), embeds intent (if not yet), computes match, sets to Scored/Excluded/EmbedFailed → done.

Add a `ScoreApplicationHandler` (per-Application worker entry) to T14. Update T14 to clarify the split.

- [ ] **TDD + verification + commit**

```
git add internal/sourcing/infrastructure/worker/match_pool.go \
        internal/sourcing/infrastructure/worker/match_pool_test.go \
        internal/sourcing/infrastructure/worker/judge_pool.go \
        internal/sourcing/infrastructure/worker/judge_pool_test.go
git commit -m "feat(sourcing): match worker and judge worker pools"
```

---

## Task 18: HTTP `GET /intents/{id}/applications` + OpenAPI

**Files:**
- Modify: `internal/sourcing/delivery/http/v1/handlers.go` (add `ListApplications`)
- Modify: `internal/sourcing/delivery/http/v1/dto.go` (add HTTP-side DTOs)
- Modify: `internal/sourcing/delivery/http/v1/routes.go` (mount the route)
- Modify: `internal/sourcing/delivery/http/v1/handlers_test.go` (3+ new tests)
- Modify: `docs/api/v1/sourcing.openapi.yaml` (add path + schemas)

Endpoint:
```
GET /api/v1/intents/{intent_id}/applications
    ?status=New|Scored|Excluded|...
    &min_score=70
    &sort=score_desc|recent
    &limit=50
    &offset=0
```

Response shape per `scoring.md` §1 / spec API §4:

```json
{
  "items": [
    {
      "application_id": "uuid",
      "candidate": {
        "id": "uuid",
        "full_name_masked": "A***",
        "headline": "Senior Backend Engineer with 7y Go",
        "location": "Bangalore"
      },
      "score": {
        "overall": 87.2,
        "band": "strong",
        "embedding_score": 0.81,
        "rule_match": [...],
        "llm": {
          "summary": "...",
          "evidence": [...],
          "concerns": [...]
        }
      },
      "status": "Scored",
      "scored_at": "..."
    }
  ],
  "total": 50,
  "facets": { "strong": 12, "moderate": 24, "weak": 14 }
}
```

Tests: happy path, filter by status, filter by min_score, sort variants, empty result, no-auth → 401, bad intent_id → 400.

- [ ] **TDD + verification + commit**

```
git add internal/sourcing/delivery/ docs/api/v1/sourcing.openapi.yaml
git commit -m "feat(sourcing): GET /intents/{id}/applications endpoint"
```

---

## Task 19: Wire into `cmd/api/main.go`

**Files:**
- Modify: `cmd/api/main.go`
- Modify: `developer.md` (env vars)

New components to wire:
- `Voyage` embedder client + adapter (`VOYAGE_API_KEY`, `VOYAGE_MODEL` env vars)
- `InProcMatchScorer`
- `AnthropicJudge`
- `PostgresApplicationRepository`, `PostgresIntentEmbeddingRepository`, `PostgresJudgeJobRepository`
- `PostgresIntentReader`
- `pgvector.RegisterTypes(pool)` — register the `vector` type with pgx so it can serialize `[]float32`
- `ScoreCandidateHandler`, `ScoreIntentHandler`, `ScoreApplicationHandler`, `JudgeApplicationHandler`
- `IntentConfirmedConsumer` subscribed to `bus.Subscribe("hiringintent.IntentConfirmed", ...)`
- `CandidateParsedConsumer` subscribed to `bus.Subscribe("sourcing.CandidateParsed", ...)`
- `MatchPool` + `JudgePool` (new `go` statements alongside existing dispatcher launches)
- HTTP handler updated to include `ListApplicationsHandler`

New env vars:
```
VOYAGE_API_KEY        (required for Voyage)
VOYAGE_MODEL          default: voyage-3
SOURCING_JUDGE_TOP_K  default: 20
SOURCING_MATCH_POOL   default: 4
SOURCING_JUDGE_POOL   default: 2
```

Fail-fast at startup if `VOYAGE_API_KEY` is missing.

- [ ] **Verification + commit**

```
go build ./...
go test ./... -count=1
git add cmd/api/main.go developer.md
git commit -m "feat(sourcing): wire scoring pipeline into api binary"
```

---

## Task 20: Slice 3 e2e integration test

**Files:**
- Create: `tests/sourcing_slice3_e2e_test.go`

Full flow against real Postgres:
1. Insert a `hiring_intents` row directly (status Confirmed, spec_version 1).
2. Upload a resume via HTTP.
3. Wait for `ResumeUpload.Status = Parsed`.
4. The `CandidateParsed` subscriber fires → `ScoreCandidate` creates Application.
5. Match worker picks up Application → embeds (stub embedder), rule + cosine.
6. Application transitions to `Scored`.
7. *Separately* fire `IntentConfirmed` → `ScoreIntent` enqueues JudgeJob for top-K.
8. Judge worker picks up JudgeJob → stub judge returns canned LLMJudgment.
9. Application has `overall_score`, `score_band`.
10. `GET /intents/{id}/applications` returns the ranked list.

Use stub embedder + stub judge (deterministic outputs). Real Postgres, real pipeline glue.

Skip if `DATABASE_URL` is unset (per slice-1/2 pattern).

- [ ] **TDD + verification + commit**

```
git add tests/sourcing_slice3_e2e_test.go
git commit -m "test(sourcing): slice-3 e2e — score Candidate × Intent and list applications"
```

---

## Task 21: README + module README refresh

**Files:**
- Modify: `README.md` (context table row)
- Modify: `docs/modules/sourcing/README.md` (pipeline diagram, capabilities)

Update the root README sourcing row:

```
| `sourcing` | Resume ingestion + parsing + LLM-driven Candidate × Intent scoring with rule chips + Claude judge for top-K (slices 1+2+3). Recruiter dashboard actions coming in slice 4. | **Live (matched scoring)** |
```

Update the module README — add an "Applications & scoring" section pointing at `scoring.md`, refresh the pipeline diagram with the new statuses, list the new env vars.

- [ ] **Verification + commit**

```
git add README.md docs/modules/sourcing/README.md
git commit -m "docs(sourcing): refresh for slice 3 (scoring + Applications)"
```

---

## Wrap-up

After all 21 tasks complete:

- [ ] `make test-unit` clean.
- [ ] `INTEGRATION_TESTS=1 make test-integration` — slice-1, slice-2, and slice-3 e2e tests all pass against live Postgres + pgvector.
- [ ] `go vet ./...` and `gofmt -l -s .` both clean.
- [ ] Smoke run `bin/api` with `VOYAGE_API_KEY=<key>`, watch the logs — match pool + judge pool both start without panic, dispatcher subscriptions register correctly.

**What slice 3 ships:**
- Voyage AI `voyage-3` embedding integration.
- pgvector-backed cosine similarity at scale (ivfflat index on 1024-dim).
- In-process rule-based scoring (skills, experience, location, work_mode, language).
- Claude tool-use LLM judging for top-K applications per intent.
- Two new event consumers wiring the cross-context fan-out (`hiringintent.IntentConfirmed`, `sourcing.CandidateParsed`).
- Two new worker pools (match + judge) running alongside the existing upload worker.
- `applications` table partition-ready by tenant, `hiring_intent_embeddings` cached by (intent, spec_version), `judge_jobs` queue.
- `GET /api/v1/intents/{id}/applications` returning ranked Applications with rule chips + cosine + LLM evidence.
- All five Application failure states (`Excluded`, `EmbedFailed`, `JudgeFailed`, `Stale`) plumbed end-to-end.

**What slice 3 does NOT ship (deferred to slice 4):**
- `POST .../rescore` endpoint.
- Application lifecycle actions (`shortlist`/`reject`/`hire`).
- SSE for live score updates.
- Per-tenant scoring config (top-K, weights, threshold tuning).
- Stale-reconciler background job.
- Cross-tenant Capability Card matching.
