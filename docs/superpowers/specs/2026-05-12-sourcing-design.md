# Sourcing — Resume Parsing & Job Matchmaking Design

**Status:** approved (brainstorm session 2026-05-12)
**Date:** 2026-05-12
**Author:** brainstorm session, Claude Opus 4.7
**Bounded context:** `sourcing` (new — the pending stub in the README context table)
**Upstream:** `hiringintent` (consumes `IntentConfirmed`)
**Downstream:** future `interview` context (consumes `ApplicationShortlisted`)

---

## Summary

Build the `sourcing` bounded context — the resume ingestion and matchmaking layer
of hireflow. Recruiters upload one or more resumes against a confirmed
`HiringIntent`. The system streams uploads through a virus-scan → text-extract →
LLM-parse → embed → rule-match → top-K LLM-judge pipeline, and exposes a
ranked, explainable list of `Application`s per intent for the recruiter to
shortlist. Pipeline runs async with live progress to the FE; matches are
recomputed on intent re-confirmation or explicit rescore.

This is the data and decision surface that feeds the downstream AI interview
flow described in the Tulifo pre-seed deck.

## Decisions

| #   | Decision                                                                                                                                                                                                                | Rationale |
|-----|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|---|
| D1  | **Identity scope: tenant-scoped `Candidate` + `Application` join.** Same person re-uploaded across intents reuses one `Candidate`; per-intent state lives on `Application`.                                              | Gives real recruiter value ("we've seen this person") with simple, tenant-isolated semantics. Lifts cleanly to a global pool later for the Tulifo Capability Card. |
| D2  | **Parsing: hybrid text-extract + LLM parse, with OCR fallback.** Deterministic `TextExtractor` (UniDoc/Tika) pulls text; Claude tool-use produces structured `ParsedProfile`. Image-only PDFs fall through to Claude vision. | Two narrow ports test independently. PDF extraction is a commodity — paying LLM rates for it is waste. Keeps cost at the Tulifo unit-economics target (~$0.02–0.05 per resume). |
| D3  | **Matching: two-stage funnel.** Stage 1 (every resume): rule-based filters from RoleSpec + embedding cosine similarity. Stage 2 (top-K only, K=20): Claude LLM-as-judge produces score + per-skill evidence + summary.   | Recruiter UI needs explainability (rule chips + evidence). Naive "judge every pair" is $30K/intent at 1M candidates. Two-stage caps LLM cost at `top_k × intents`, not `resumes × intents`. |
| D4  | **Match recomputation: on `IntentConfirmed` and on explicit recruiter "rescore" action only.** Drafted-intent edits do not re-score.                                                                                     | Bounds LLM spend predictably. Drafted intents change frequently; scoring against an unstable spec wastes money. |
| D5  | **Ingestion: async batch with SSE live updates + polling fallback.** FE posts N files in one multipart batch; server returns `202` per file; in-process goroutine pool drains `resume_uploads` rows via `FOR UPDATE SKIP LOCKED`. | Matches the existing outbox pattern (no Redis/SQS yet). Recruiter UX is a live-filling pipeline, not a frozen spinner. |
| D6  | **File storage: behind a `ResumeStorage` port.** Adapters: `localfs` (dev default), `s3`, `supabase`. Files keyed by `sha256(content)`.                                                                                  | Content-hash key gives free dedup and idempotent re-uploads. Adapter swap (localfs → S3) is a config change. |
| D7  | **Virus scanning: ClamAV via clamd sidecar, before extraction.** `FileScanner` port; `clamd` adapter for prod, `noop` for tests. Positive scan moves file to quarantine prefix; row is `Failed(virus_detected)`.         | Resumes are external PII — VirusTotal-class third-party scanning is a non-starter. Self-hosted clamd is free, signature-updated, and runs as a compose sidecar today. |
| D8  | **Vector store: pgvector inside the existing Postgres.** Embedding dimension 1024. `ivfflat` for v1, `hnsw` swap once total vectors exceed ~1M.                                                                          | "One Postgres" matches the codebase. pgvector + hnsw scales to 10M comfortably. Separate vector DB is YAGNI at pre-seed. |
| D9  | **Embedding provider: Voyage AI `voyage-3` (1024d).** Behind an `Embedder` port; `random` adapter for tests.                                                                                                             | Best quality/$ at 1024d, no Anthropic embeddings product, port keeps provider swappable. |
| D10 | **Partition-ready DDL from v1.** `applications` declared `PARTITION BY LIST (tenant_id)` with default partition; `resume_uploads` declared `PARTITION BY RANGE (created_at)` monthly with default. `candidates` un-partitioned. | Cutting over to multi-partition later is metadata-only, no data rewrite. Pays a small PK-shape cost now in exchange for zero-downtime scale-out. |
| D11 | **Per-stage artifacts persisted on the upload row** (`stage_artifacts` jsonb). Crash mid-pipeline resumes from the last successful stage; re-running parse/embed/score is never lost work.                              | Bounds LLM cost on failure paths. Big win for the most expensive stages. |
| D12 | **Adapter classifies its own retryability** via `RetryDecision{Retryable, Reason, Detail, BackoffHint}`. Worker applies exponential backoff (1m/5m/15m/1h/4h, cap 5 attempts) for retryable; fatal terminates immediately. | Only the adapter knows whether a 5xx is transient or a `parse_resume: not_a_resume` is terminal. Centralized policy would either over- or under-retry. |
| D13 | **PII to LLM is unavoidable for parsing; mitigate via Anthropic ZDR enrollment on the API key.** `LLMJudge` receives `ParsedProfile`, not raw resume text. Application-layer envelope encryption on `parsed_profile.personal.*` with tenant-scoped DEKs. | Pitch deck commits to DPDP/GDPR/EU AI Act. ZDR + envelope encryption is the realistic operating posture. |
| D14 | **Prompts version-controlled in code; prompt-version stored on `applications.llm_judgment`.** Schema-versioned `ParsedProfile` (`schema_version: 1`).                                                                    | EU AI Act auditability — must be able to point to the exact prompt that produced any past score. |

## Non-goals (v1)

- **Cross-intent candidate search** (`GET /candidates?q=`). Comes with the Capability Card / network-effect phase, not core to "HR uploaded resumes to one intent."
- **Bulk ATS export (Greenhouse, Lever)**. The pitch deck schedules ATS integrations in Phase 2. Keep `parsed_profile` ATS-friendly but ship no export endpoints in v1.
- **Auto-matching old candidates to new intents**. `IntentConfirmed` does not retroactively fan out to every existing `Candidate`. Recruiter explicitly retargets if they want it.
- **Resumable/chunked uploads** (tus.io, S3 multipart). Typical resume is 100–500 KB; whole-file retry is sufficient until file sizes routinely exceed 50 MB.
- **Distributed tracing (OpenTelemetry)**. Codebase has no OTel yet; adding it for one context is premature.
- **External queue (NATS / SQS) and separate worker binary**. In-process goroutine pool against `resume_uploads` polling is sufficient through ~10K resumes/min sustained — well past pre-seed scale.

## Architecture

### Bounded-context placement

```
internal/sourcing/
    domain/
        entities/              Candidate, ResumeUpload, Application
        valueobjects/          ParsedProfile, RoleEmbedding, MatchScore,
                               RuleMatchReport, LLMJudgment, RetryDecision
        events/                ResumeUploadAccepted, ResumeUploadFailed,
                               CandidateParsed, ApplicationScored,
                               ApplicationShortlisted, ApplicationRejected,
                               ApplicationHired, CandidateErased
        repositories/          CandidateRepository, ResumeUploadRepository,
                               ApplicationRepository, IntentEmbeddingRepository
        services/              FileScanner, TextExtractor, OCRExtractor,
                               ResumeParser, Embedder, MatchScorer, LLMJudge,
                               ResumeStorage  (all ports)
    application/
        commands/              UploadResumeBatch, ProcessResumeUpload (worker entry),
                               ShortlistApplication, RejectApplication, HireApplication,
                               RescoreIntent, RetryUpload, EraseCandidate
        queries/               GetBatchStatus, ListApplications,
                               GetCandidate, ListBatchEvents (SSE)
        dto/                   BatchUploadRequest, BatchStatusDTO,
                               ApplicationListItemDTO, CandidateDetailDTO
    infrastructure/
        clients/               IntentReader (anti-corruption against hiringintent)
        scanning/              clamd adapter, noop adapter
        text/                  unidoc adapter, tika adapter (fallback)
        ocr/                   claude_vision adapter
        parsing/
            anthropic_parser.go
            prompts/
                parse_resume.tmpl
                judge_match.tmpl
            schemas/
                parse_resume.schema.json
                judge_match.schema.json
        embedding/             voyage adapter, random adapter (test only)
        scoring/               rule_matcher.go (in-process Go)
        judging/               anthropic_judge adapter
        storage/               localfs adapter, s3 adapter, supabase adapter
        persistence/           Postgres repos (one per aggregate)
        messaging/             event_publisher.go (outbox dispatcher)
        worker/                pipeline.go (the stage machine)
        encryption/            envelope.go (KMS-backed DEK manager for PII fields)
    delivery/
        http/v1/
            handlers.go        BatchUpload, BatchStatus, BatchEvents (SSE),
                               ListApplications, GetCandidate, DownloadResume,
                               Shortlist, Reject, Hire, RetryUpload, Rescore,
                               EraseCandidate
            routes.go
```

### Aggregates

| Aggregate       | Identity                          | Responsibility |
|-----------------|-----------------------------------|---|
| `Candidate`     | `(tenant_id, id)`                 | Person identity within a tenant. Owns parsed profile, PII (encrypted), source, embedding. Immutable name/email after creation in v1 (re-parse creates a new candidate; deduplicates by `(tenant_id, content_hash)`). |
| `ResumeUpload`  | `id` (per-file)                   | Lifecycle of one uploaded file. Owns status, attempts, last error, per-stage artifacts. References `Candidate` after successful parse. |
| `Application`   | `(tenant_id, candidate_id, intent_id)` unique | The match. Owns rule-match report, embedding score, LLM judgment, recruiter lifecycle (`New → Shortlisted → Rejected | Interviewing → Hired`). |

### Anti-corruption with `hiringintent`

`sourcing` reads intents through `infrastructure/clients/IntentReader`, which
projects the upstream aggregate into a local `IntentSnapshot` value object
holding only what scoring needs (`RoleSpec`, `Priority`, `TrustSignals`,
`SpecVersion`). Same pattern as `jobposting/infrastructure/clients/IntentReader`.

### Events

**Emitted:**
- `ResumeUploadAccepted{upload_id, tenant_id, batch_id, intent_id, content_hash}` — after byte-write.
- `ResumeUploadFailed{upload_id, reason, detail}` — fatal failures (virus, unreadable, parse refused).
- `CandidateParsed{candidate_id, tenant_id, schema_version}` — triggers scoring.
- `ApplicationScored{application_id, candidate_id, intent_id, overall_score, score_band}` — FE notification, audit.
- `ApplicationShortlisted{application_id, ...}` — downstream interview module trigger.
- `ApplicationRejected{application_id, reason}` / `ApplicationHired{application_id}`
- `CandidateErased{candidate_id, tenant_id}` — GDPR erasure cascade.

**Consumed:**
- `IntentConfirmed` (from `hiringintent`) — embeds the role spec into `hiring_intent_embeddings`, opens scoring eligibility. No retroactive fan-out to old candidates in v1.

## Data model

All tables live in `migrations/sourcing/` with their own tracking table, per
context invariant. DDL highlights below; complete migrations live in
implementation.

### `candidates`

```sql
CREATE TABLE candidates (
    id                 uuid        PRIMARY KEY,
    tenant_id          uuid        NOT NULL,
    content_hash       text        NOT NULL,         -- sha256 of source file
    full_name          text,                         -- envelope-encrypted at app layer
    email              citext,                       -- envelope-encrypted at app layer
    phone              text,                         -- envelope-encrypted at app layer
    location           text,
    headline           text,
    parsed_profile     jsonb       NOT NULL,         -- canonical structured profile
    profile_embedding  vector(1024),                 -- pgvector
    source             text        NOT NULL DEFAULT 'manual_upload',
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),

    UNIQUE (tenant_id, content_hash)
);
CREATE INDEX candidates_tenant_email_idx
    ON candidates (tenant_id, email) WHERE email IS NOT NULL;
CREATE INDEX candidates_profile_embedding_idx
    ON candidates USING ivfflat (profile_embedding vector_cosine_ops);
```

### `resume_uploads` — partitioned by month

```sql
CREATE TABLE resume_uploads (
    id              uuid        NOT NULL,
    tenant_id       uuid        NOT NULL,
    intent_id       uuid        NOT NULL,
    batch_id        uuid        NOT NULL,
    candidate_id    uuid,
    storage_key     text        NOT NULL,
    original_name   text        NOT NULL,
    mime_type       text        NOT NULL,
    size_bytes      bigint      NOT NULL,
    content_hash    text        NOT NULL,
    status          text        NOT NULL,           -- enum (see Pipeline §)
    stage_artifacts jsonb       NOT NULL DEFAULT '{}',
    attempt_count   int         NOT NULL DEFAULT 0,
    last_error      text,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (created_at, id)
) PARTITION BY RANGE (created_at);

CREATE TABLE resume_uploads_default PARTITION OF resume_uploads DEFAULT;

CREATE INDEX resume_uploads_pending_idx
    ON resume_uploads (next_attempt_at)
    WHERE status NOT IN ('Scored','Failed','Quarantined');
CREATE INDEX resume_uploads_batch_idx ON resume_uploads (batch_id);
CREATE INDEX resume_uploads_tenant_intent_idx ON resume_uploads (tenant_id, intent_id);
```

A daily cron in the API binary pre-creates the next month's partition.
Archival of `Scored`/`Failed` rows older than 90 days = `DETACH PARTITION` to a
cold table.

### `applications` — partitioned by tenant

```sql
CREATE TABLE applications (
    id                 uuid        NOT NULL,
    tenant_id          uuid        NOT NULL,
    candidate_id       uuid        NOT NULL,
    intent_id          uuid        NOT NULL,
    status             text        NOT NULL,         -- New|Shortlisted|Rejected|Interviewing|Hired
    overall_score      numeric(5,2),                 -- 0–100 after stage 2
    rule_match         jsonb       NOT NULL,         -- structured per-criterion report
    embedding_score    numeric(5,4),                 -- raw cosine sim
    llm_judgment       jsonb,                        -- {score, evidence[], summary, prompt_version}
    scored_at          timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id)
) PARTITION BY LIST (tenant_id);

CREATE TABLE applications_default PARTITION OF applications DEFAULT;

CREATE UNIQUE INDEX applications_uniq_idx
    ON applications (tenant_id, candidate_id, intent_id);
CREATE INDEX applications_intent_score_idx
    ON applications (tenant_id, intent_id, overall_score DESC NULLS LAST);
```

All repository queries MUST include `tenant_id` in the WHERE clause for
partition pruning. Documented on the repository port interface.

### `hiring_intent_embeddings`

```sql
CREATE TABLE hiring_intent_embeddings (
    intent_id        uuid        PRIMARY KEY,
    tenant_id        uuid        NOT NULL,
    spec_version     int         NOT NULL,
    role_embedding   vector(1024) NOT NULL,
    created_at       timestamptz  NOT NULL DEFAULT now()
);
```

Populated by the `IntentConfirmed` consumer. Re-confirmation bumps
`spec_version` and recomputes; cached row invalidates the cached
`LLMJudgment` keyed by `(candidate_id, intent_id, intent_version)`.

### `sourcing_outbox`

Standard outbox row shape — mirrors `hiringintent_outbox`. Dispatcher in
`infrastructure/messaging/event_publisher.go` polls and forwards to the shared
in-memory `eventbus`.

### `ParsedProfile` JSON schema (schema_version 1)

```json
{
  "schema_version": 1,
  "personal": {
    "full_name": "...",
    "email": "...",
    "phone": "...",
    "location": "...",
    "links": [{ "kind": "linkedin|github|portfolio", "url": "..." }]
  },
  "headline": "Senior Backend Engineer with 7y Go",
  "summary": "...",
  "skills": [{ "name": "Go", "years": 5.5, "evidence_ref": "exp_0" }],
  "experiences": [{
    "id": "exp_0",
    "company": "Razorpay",
    "title": "Senior Backend Engineer",
    "start": "2020-04",
    "end": "2025-01",
    "current": false,
    "description": "...",
    "skills_used": ["Go", "Postgres"]
  }],
  "education": [{ "institution": "...", "degree": "...", "field": "...", "start": "...", "end": "..." }],
  "certifications": [{ "name": "...", "issuer": "...", "issued": "...", "expires": "..." }],
  "languages": [{ "name": "...", "proficiency": "native|fluent|professional|basic" }],
  "warnings": ["phone_unparseable", "date_format_ambiguous_exp_2"]
}
```

## Pipeline

Single in-process worker loop per pod processes `resume_uploads` rows through
a linear stage machine. Each stage is a port; each port has a dev and prod
adapter. Stages persist their artifact on the row before transitioning state.

```
Pending → Scanning → Extracting → Parsing → Embedding → Scoring → Scored
                                                                     ↓
                                                          Application rows
                                                          written; top-K
                                                          queued for LLMJudge
```

Failure: any stage may transition `→ Failed(reason)` (fatal) or schedule
itself for retry via `next_attempt_at` + `attempt_count++`.

### Stage → port mapping

| Stage      | Port               | Prod adapter                          | Dev/test adapter           |
|------------|--------------------|---------------------------------------|----------------------------|
| Scanning   | `FileScanner`      | `clamd` (TCP, `go-clamd`)             | `noop` (always-clean)      |
| Extracting | `TextExtractor`    | `unidoc` (PDF/DOCX native Go) → Tika fallback | same |
| (fallback) | `OCRExtractor`     | Claude vision via shared SDK          | stub                       |
| Parsing    | `ResumeParser`     | Claude tool-use, `parse_resume` schema | fixture parser            |
| Embedding  | `Embedder`         | Voyage `voyage-3` (1024d)             | `random` vector            |
| Scoring    | `MatchScorer`      | in-process Go (deterministic rules + cosine) | same             |
| (top-K)    | `LLMJudge`         | Claude tool-use, `judge_match` schema | stub                       |
| (storage)  | `ResumeStorage`    | `s3` / `supabase` (prod), `localfs` (dev) | `memfs` (tests)         |

### Defense in depth at upload

- MIME sniff with `gabriel-vasile/mimetype` on byte stream; reject anything
  not `application/pdf`, `application/vnd.openxmlformats-officedocument.wordprocessingml.document`,
  or `application/msword`.
- Per-file cap `SOURCING_MAX_FILE_BYTES=10MB`; per-batch cap `SOURCING_MAX_BATCH_BYTES=100MB`.
- DOCX macro stripping at parser layer.
- Quarantine prefix on scanner-positive files; never delete.
- Worker container is the sandbox boundary for any parser RCE.

### Retry policy

`RetryDecision` returned by every stage:
```go
type RetryDecision struct {
    Retryable   bool
    Reason      string         // e.g. "anthropic_429", "virus_detected", "ocr_empty"
    Detail      string         // human-readable, lands in last_error
    BackoffHint time.Duration  // optional override of default schedule
}
```

Default backoff schedule: 1m, 5m, 15m, 1h, 4h — cap 5 attempts, then
`Failed(max_retries_exceeded)`. Recruiter has explicit "Retry" UI which resets
`attempt_count=0`.

Concrete classifications:

| Error                                            | Decision         |
|--------------------------------------------------|------------------|
| Anthropic / Voyage 429 / 5xx                     | retryable        |
| clamd unreachable                                | retryable        |
| DB serialization conflict                        | retryable        |
| Empty extract + empty OCR                        | fatal `unreadable` |
| MIME mismatch on re-check after extract          | fatal `mime_mismatch` |
| `parse_resume` returns malformed JSON twice      | fatal `parse_failed` |
| Virus detected                                   | fatal `virus_detected` |
| File size > cap (re-checked post-store)          | fatal `size_exceeded` |

### Crash safety

- `FOR UPDATE SKIP LOCKED` claim — another worker picks up a row if its claimer dies.
- Each stage idempotent on its `(upload_id, stage)` key (Anthropic/Voyage idempotency keys).
- Per-stage artifacts persisted on the row before transition — crash mid-parsing does not lose the extracted text; crash mid-embedding does not lose the parsed profile.

### Match scoring

**Stage 1 (every resume — runs in `MatchScorer`):**
- `RuleMatchReport` — for each `RoleSpec` criterion, produce `{criterion, required, passed, evidence_ref|actual}`. Criteria: required skills, optional skills, experience-years band, location/work-mode compatibility, education hard-floor (if present).
- `EmbeddingScore` — cosine similarity between `profile_embedding` and `role_embedding`.
- Coarse rank for shortlist = required-pass-rate + cosine sim. Fails any required criterion ⇒ excluded from stage 2.

**Stage 2 (top-K only, default K=20):**
- Claude tool-use against `judge_match.schema.json`:
  ```json
  {
    "score": 0–100,
    "evidence": [{ "skill|experience|fit": "...", "claim": "...", "support": "..." }],
    "summary": "2-sentence rationale",
    "concerns": ["..."]
  }
  ```
- Result written to `applications.llm_judgment` with `prompt_version`.
- `overall_score = llm.score` for judged applications; coarse score otherwise.

**Cache key:** `(candidate_id, intent_id, intent.spec_version, profile.schema_version, prompt_version)` — re-confirming an unchanged intent reuses prior judgments at zero LLM cost.

**`score_band` thresholds** (computed at write time, indexed for the list-view facet):

| Band       | Threshold                |
|------------|--------------------------|
| `strong`   | `overall_score >= 80`    |
| `moderate` | `60 <= overall_score < 80` |
| `weak`     | `overall_score < 60`     |

Unscored applications (stage 1 only, didn't reach top-K) get band derived from
the coarse score with the same thresholds. Thresholds are codified in
`domain/valueobjects/match_score.go` and not configurable per tenant in v1 —
tenant-tunable thresholds is a Phase 2 customization point.

## API surface

All endpoints under `/api/v1/`, bearer JWT, tenant-scoped. OpenAPI in
`docs/api/v1/sourcing.openapi.yaml`.

### Upload + lifecycle

```
POST /intents/{intent_id}/resumes:batch     multipart/form-data
GET  /resumes/batches/{batch_id}            polling status
GET  /resumes/batches/{batch_id}/events     SSE live updates
POST /resumes/{upload_id}:retry             reset failed → Pending
POST /intents/{intent_id}/applications:rescore   re-judge all applications for this intent
```

Batch upload response includes per-file outcome (queued / deduplicated /
mime_unsupported / size_exceeded). Partial success is the norm; HTTP 200 even
with rejected files.

### Applications + candidate

```
GET  /intents/{intent_id}/applications      ?status&min_score&sort&limit&offset
GET  /candidates/{candidate_id}             full profile, all applications (tenant-scoped)
GET  /candidates/{candidate_id}/resume      302 to signed storage URL (15-min TTL)
POST /applications/{id}:shortlist
POST /applications/{id}:reject              body: { reason }
POST /applications/{id}:hire
DELETE /candidates/{id}                     GDPR erasure
```

### Response highlights

- List view returns `email_masked`; full email only via `GET /candidates/{id}` (writes audit row).
- `rule_match` is structured (criterion + passed + evidence_ref) — FE renders chips without parsing strings.
- `score.band` is a coarse categorical (`strong | moderate | weak`) for UI fast-path; `score.overall` is the numeric.

## Cross-cutting

### Testing

| Layer            | Tooling                                   | What it proves |
|------------------|-------------------------------------------|---|
| Domain           | Pure Go table tests                       | Aggregate invariants, value-object validation, rule-match scoring against canonical RoleSpec inputs |
| Application      | Fake adapters + in-memory repos / testcontainers Postgres | Command orchestration, port wiring, outbox emission, tenant scoping on queries |
| Infrastructure   | Per-adapter tests against real upstream, gated by `INTEGRATION_TESTS=1` | Adapter contract holds against the real ClamAV / Voyage / Claude / UniDoc |
| Delivery         | `httptest` server + table-driven cases    | HTTP contract — multipart streaming, partial-success shape, SSE event stream, JWT enforcement |
| Cross-context E2E | `tests/sourcing_e2e_test.go` against compose Postgres + clamd + stub LLM adapters | Full lifecycle confirm → upload → parse → score → shortlist → event consumed downstream |

**Fixture corpus** lives at `testdata/resumes/`:
- one-column ATS-friendly PDF
- two-column designer PDF
- image-scanned PDF (forces OCR path)
- DOCX with macros (forces sanitization)
- oversize / corrupted / empty PDFs
- multi-language resumes

Doubles as benchmark input: `go test -bench` runs the pipeline against the
corpus, asserts parse accuracy against hand-labeled `testdata/resumes/expected/*.json`.

### Observability

**Structured logs (zerolog):** every stage transition logs `{upload_id,
tenant_id, stage, duration_ms, attempt, outcome}`. Tenant ID always present.

**Prometheus metrics** at `/metrics`:
```
sourcing_pipeline_stage_duration_seconds{stage, outcome}   histogram
sourcing_pipeline_stage_total{stage, outcome}              counter
sourcing_upload_status{tenant_id, status}                  gauge
sourcing_llm_tokens_total{model, kind="parse|judge|ocr"}   counter
sourcing_embedder_requests_total{outcome}                  counter
sourcing_application_score_distribution                    histogram
sourcing_worker_pool_active                                gauge
sourcing_outbox_lag_seconds                                gauge
```

`sourcing_outbox_lag_seconds` is the critical SLO; pageable past N seconds.
`sourcing_llm_tokens_total` × $/Mtoken → live unit-economics dashboard.

### PII, security, compliance

| Concern                   | Implementation |
|---------------------------|---|
| Encryption at rest        | Postgres TDE (managed). Storage: SSE-S3/SSE-KMS. `parsed_profile.personal.*` AES-encrypted at app layer with tenant-scoped DEKs (KEK in KMS). DB dump leak ≠ PII leak. |
| Transport                 | TLS-only API, TLS to Voyage / Anthropic / clamd. |
| PII to LLM                | `ResumeParser` necessarily sends raw resume text. Mitigated by **Anthropic ZDR enrollment on the API key** (documented in `developer.md`). `LLMJudge` sees `ParsedProfile` only. |
| Right to erasure          | `DELETE /candidates/{id}` cascades to `applications`, `resume_uploads`, `ResumeStorage` blobs; emits `CandidateErased` for downstream cleanup. Logs retain only IDs. |
| Audit log                 | Every unmasked PII read (candidate detail, resume download, export) writes an `audit_log` row keyed by `(actor_user_id, tenant_id, action, candidate_id, at)`. Lives in `shared`. |
| Algorithmic auditability  | Prompts version-controlled in `infrastructure/parsing/prompts/`. `ParsedProfile.schema_version` and `applications.llm_judgment.prompt_version` traceable to git. |
| Scoring policy            | Text-only parsing. No facial / vocal / sentiment scoring (pitch deck commitment). |

### Configuration

New env vars (read at startup via existing config loader):

```
SOURCING_STORAGE_BACKEND        localfs | s3 | supabase     default: localfs
SOURCING_STORAGE_PATH           /var/hireflow/resumes       (localfs)
SOURCING_STORAGE_BUCKET         hireflow-resumes-prod       (s3/supabase)
SOURCING_SCANNER_BACKEND        noop | clamd                default: noop
SOURCING_SCANNER_ADDR           tcp://clamav:3310
SOURCING_PARSER_BACKEND         claude                      default: claude
SOURCING_EMBEDDER_BACKEND       voyage | random             default: voyage
VOYAGE_API_KEY                  -                           required if voyage
VOYAGE_MODEL                    voyage-3
SOURCING_JUDGE_BACKEND          claude | stub               default: claude
SOURCING_JUDGE_TOP_K            20                          default
SOURCING_WORKER_POOL            4                           default
SOURCING_MAX_FILE_BYTES         10485760
SOURCING_MAX_BATCH_BYTES        104857600
SOURCING_MAX_RETRIES            5
SOURCING_RETRY_BACKOFF          1m,5m,15m,1h,4h
```

Defaults make `make run` work end-to-end without external services
(`noop` scanner, `localfs` storage, `random` embedder, real Claude parsing
since the key is already required by `hiringintent`).

## Scalability buckets

| Scale tier                          | What works as-is                                       | What changes |
|-------------------------------------|--------------------------------------------------------|---|
| **Pre-seed (≤10K resumes/mo)**      | Everything in v1                                       | — |
| **Seed (~100K resumes/mo)**         | Same shape                                             | Flip `localfs → s3`. Add monitoring dashboards. |
| **~1M total resumes**               | Same shape                                             | `ivfflat → hnsw` index swap (one-line). |
| **~10M lifetime upload rows**       | Same shape                                             | Pre-create monthly partitions; nightly archive `Scored`/`Failed` rows older than 90 days. |
| **~50M applications**               | Same shape                                             | List-partition `applications` per hot tenant (`ATTACH/DETACH`, no data rewrite). |
| **>16 sustained workers**           | Same code; new binary target                           | Lift workers into `cmd/sourcing-worker/`; API binary stops worker loop. |
| **>10K resumes/min sustained**      | Same domain/application code                           | Replace `resume_uploads` polling with NATS/SQS dispatcher behind the outbox. |
| **Bulk re-scoring jobs**            | Same ports                                             | `LLMJudge.BatchJudge()` route through Anthropic Batch API (50% discount, 24h SLA). |

## Rollout — four merge-ready slices

1. **Scaffold + storage + scan + extract.** Bounded context skeleton,
   `resume_uploads` table (partition-ready), `POST :batch` endpoint, ClamAV
   scan adapter + compose sidecar, `TextExtractor` (UniDoc). Outcome: HR can
   upload; system stores + scans. No parsing, no scoring.

2. **Parsing + `Candidate` aggregate.** `ResumeParser` adapter (Claude
   tool-use), `candidates` table, OCR fallback, content-hash dedup. Outcome:
   resumes parsed into structured profiles; candidate detail endpoint live.

3. **Match scoring.** Voyage `Embedder`, `applications` table (partition-ready),
   `MatchScorer` rule engine, `LLMJudge` for top-K,
   `hiring_intent_embeddings`, `IntentConfirmed` consumer. Outcome: end-to-end
   scoring; recruiter sees ranked list.

4. **FE surface + SSE + retry + lifecycle.** Application listing,
   SSE batch events, retry / rescore endpoints, `shortlist/reject/hire`,
   audit-log integration, GDPR erasure. Outcome: recruiter dashboard live;
   downstream interview module can subscribe to `ApplicationShortlisted`.

Each slice is independently mergeable with its own integration test. Slice 1
is shippable to a design-partner customer for upload UX feedback while later
slices are built.
