# sourcing — bounded context

Recruiters upload resumes against confirmed hiring intents. The context
parses, dedups, scores, exposes a recruiter-facing pipeline of applications,
and supports full lifecycle actions (shortlist/reject/hire), live SSE progress
updates, on-demand rescore, and GDPR erasure.

Slices 1+2+3 ship the full ingestion → parsing → matching pipeline.
Slice 4 adds the recruiter dashboard actions, SSE, rescore, and erasure.

## Ubiquitous language

| Term | Meaning |
|---|---|
| **Resume Upload** | The lifecycle of one uploaded file: status, attempts, errors, per-stage artifacts. Tracks scan + extract + parse stages. |
| **Batch** | A grouping of uploads submitted together. UI-level concept; `batch_id` is on every upload row. |
| **Candidate** | Tenant-level identity for a person. One per `(tenant_id, content_hash)`. PII fields encrypted at the application layer. |
| **ParsedProfile** | Canonical structured form of a resume (schema_version=1). Holds personal info, skills, experiences, education, certifications, languages, plus parse-time warnings. |
| **Application** | Match between a `Candidate` and a `HiringIntent`. Holds rule_match, embedding_score, optional LLM judgment, and lifecycle. |
| **JudgeJob** | Internal queue row driving LLM judging of top-K applications per intent. |
| **Stage Artifacts** | Per-stage outputs persisted on the upload row so crashes resume from the last successful stage. |

## Ubiquitous language (additions in slice 4)

The table in the previous section now also covers:

| Term | Meaning |
|---|---|
| **AuditEvent** | Immutable cross-context record in `audit_log` (shared schema). Written whenever PII is read or an Application lifecycle transition fires. Fields: `id`, `tenant_id`, `actor_id`, `action`, `entity_type`, `entity_id`, `ip_address`, `user_agent`, `occurred_at`. |
| **Shortlisted** | Application status indicating the recruiter has moved it forward from `Scored`. |
| **Interviewing** | Application status indicating an interview has been scheduled (set externally or via downstream event). |
| **Rejected** | Terminal negative lifecycle state. Requires a non-empty `reason`. |
| **Hired** | Terminal positive lifecycle state. |

## Pipeline (slices 1+2)

```
Pending → Scanning → Extracting → Extracted → Parsing → Parsed
                          ↘            ↘          ↘
                         Failed   Quarantined   Failed (Candidate created)
```

Each stage is a port; each port has a dev and prod adapter. The upload worker
(`infrastructure/worker.Pool`) polls the `resume_uploads` table via the
repository's `ClaimNextPending` and hands each row to `ProcessUploadHandler`.

Parsing fans out into:
- **Text path** (default): use the slice-1 extracted text as the parser input.
- **OCR fallback** (image-only PDFs): when extracted text is under 50 chars after `TrimSpace`, the worker reopens the original bytes and sends them to `OCRExtractor` (Claude vision) for transcription.

After parsing succeeds, the worker:
1. Encrypts the three PII fields (`full_name`, `email`, `phone`) via `PIIEncryptor`.
2. Creates or attaches a `Candidate` via `CandidateRepo.Save` (idempotent on `(tenant_id, content_hash)`).
3. Records the parsed profile JSON on the upload's `StageArtifacts`.
4. Links the upload to the candidate (`candidate_id` FK).
5. Transitions the upload to `Parsed` and emits `ResumeParsed` + `CandidateParsed` on the outbox.

## Pipeline (slice 3 — matching)

```
CandidateParsed ─► ScoreCandidateHandler ─► creates Application(New) per confirmed intent
IntentConfirmed ─► ScoreIntentHandler ─► creates Application(New) per tenant candidate
                                       └► enqueues JudgeJobs for top-K scored apps

Application(New) ─► MatchPool ─► ScoreApplicationHandler
                                  ├─► embeds candidate if missing
                                  ├─► embeds intent if missing
                                  ├─► rule_match + cosine
                                  └─► transitions to Scored / Excluded / EmbedFailed

JudgeJob(Pending) ─► JudgePool ─► JudgeApplicationHandler
                                  ├─► Claude tool-use against judge_match schema
                                  └─► records LLMJudgment + overall_score on Application
```

Application states: `New`, `Scored`, `Excluded`, `EmbedFailed`, `JudgeFailed`,
`Stale`, `Shortlisted`, `Rejected`, `Interviewing`, `Hired`.

Allowed transitions (slice 4):
- `Scored` → `Shortlisted` (recruiter shortlists)
- `Scored | Shortlisted | Interviewing` → `Rejected` (requires non-empty reason)
- `Scored | Shortlisted | Interviewing` → `Hired`

The full scoring algorithm — embedding semantics, cosine similarity, coarse-score
formula, top-K judging, caching, cost shape — is documented in
[`scoring.md`](./scoring.md).

## Configuration

| Var | Default | Purpose |
|---|---|---|
| `SOURCING_STORAGE_PATH` | `/tmp/hireflow-resumes` | Localfs storage root |
| `SOURCING_SCANNER_BACKEND` | `noop` | `noop` or `clamd` |
| `SOURCING_SCANNER_ADDR` | `tcp://localhost:3310` | Honored when backend=clamd |
| `SOURCING_MAX_FILE_BYTES` | `10485760` | Per-file cap |
| `SOURCING_WORKER_POOL` | `4` | Upload worker goroutines |
| `SOURCING_PII_DEK` | *(required)* | 64-hex AES-256 key for PII envelope encryption. `openssl rand -hex 32`. |
| `SOURCING_OCR_THRESHOLD` | `50` | Char threshold below which OCR fallback runs |
| `SOURCING_PARSER_BACKEND` | `claude` | Only "claude" supported |
| `SOURCING_OCR_BACKEND` | `claude` | Only "claude" supported |
| `VOYAGE_API_KEY` | *(required)* | Voyage AI key for the `Embedder` adapter |
| `VOYAGE_MODEL` | `voyage-3` | Voyage embedding model (1024-dim) |
| `SOURCING_JUDGE_TOP_K` | `20` | Top-K Applications per intent that get LLM-judged |
| `SOURCING_MATCH_POOL` | `4` | Match worker goroutine count |
| `SOURCING_JUDGE_POOL` | `2` | Judge worker goroutine count |

## API (slices 1+2+3)

```
POST /api/v1/intents/{intent_id}/resumes:batch     multipart/form-data
GET  /api/v1/resumes/batches/{batch_id}            batch status
GET  /api/v1/candidates/{candidate_id}             candidate detail (PII decrypted)
GET  /api/v1/intents/{intent_id}/applications      ranked Applications + rule chips + LLM evidence
```

See `docs/api/v1/sourcing.openapi.yaml`.

## Recruiter actions (slice 4)

The following 7 endpoints extend the sourcing API. All require `bearerAuth` (recruiter JWT).

| Endpoint | Method | Status | Semantics |
|---|---|---|---|
| `/applications/{application_id}:shortlist` | POST | 204 | Transitions `Scored` → `Shortlisted`. Writes audit log. |
| `/applications/{application_id}:reject` | POST | 204 | Body: `{"reason":"..."}` (required, non-empty after trim). Transitions `Scored\|Shortlisted\|Interviewing` → `Rejected`. Writes audit log. |
| `/applications/{application_id}:hire` | POST | 204 | Transitions `Scored\|Shortlisted\|Interviewing` → `Hired`. Writes audit log. |
| `/resumes/{upload_id}:retry` | POST | 204 | Resets `Failed\|Quarantined` → `Pending` so the upload worker re-claims the row. |
| `/intents/{intent_id}/applications:rescore` | POST | 202 | Nulls `llm_judgment`/`overall_score`/`score_band` for all the intent's applications, then re-enqueues judge jobs from the coarse-score list. Does not reset status; does not re-embed; does not change `rule_match`. |
| `/candidates/{candidate_id}` | DELETE | 204 | GDPR erasure: cascade-deletes candidate + applications + judge_jobs + resume_uploads + dedup rows; best-effort blob storage delete; emits `sourcing.CandidateErased` on the outbox. |
| `/resumes/batches/{batch_id}/events` | GET | 200 (SSE) | `text/event-stream`. `:ping` heartbeat every 30 s. Event types: `item_accepted`, `item_failed`, `item_extracted`, `item_parsed`. Each event carries a JSON payload with `upload_id`, `batch_id`, and event-specific fields. |

### Audit log

Every Application lifecycle transition (shortlist/reject/hire) writes a row to
the cross-context `audit_log` table (in `migrations/shared/`). Fields include
`actor_id`, `action`, `entity_type`, `entity_id`, `ip_address`, `user_agent`,
`occurred_at`. PII-read events (candidate detail) are also audited. The
`AuditWriter` port has a Postgres adapter (production) and a noop adapter
(tests).

### SSE live updates

`GET /api/v1/resumes/batches/{batch_id}/events` opens a long-lived
`text/event-stream` connection. The server-side `BatchEventFanout` component
maintains a per-batch subscriber list. Upload worker stages emit events which
fan out to all open SSE connections for that batch. The connection stays alive
via `:ping` heartbeats every 30 seconds. Clients reconnect on disconnect using
the standard `EventSource` `retry` field.

## Architecture invariants

- **Each upload is per-file independent.** A batch is a UI grouping; the pipeline doesn't have batch-level transactions.
- **Stages persist before transition** via `StageArtifacts`. Crash mid-parsing resumes by re-running parse (idempotent on the extracted text + dedup-on-collision for the candidate).
- **Adapter classifies retryability** via `RetryDecision` / `ResumeParseError` / `EmbeddingError` / `JudgeError`. The worker applies backoff and attempt-count caps; only the adapter decides Retryable vs Fatal.
- **Tenant-scoped from line one.** Every read includes `tenant_id`.
- **Outbox + in-process bus.** Same pattern as `hiringintent` and `jobposting`.

### Slice 2 specifics

- **PII at rest is encrypted at the application layer.** The full parsed profile (`parsed_profile` JSONB column) carries cleartext non-PII data only; PII lives in dedicated `full_name_enc`/`email_enc`/`phone_enc` columns produced by the `PIIEncryptor` port.
- **Candidate creation is dedup-on-collision.** `CandidateRepo.Save` returns the existing aggregate on `(tenant_id, content_hash)` match. The upload still links to that candidate. The would-have-emitted `CandidateParsed` event is dropped on the attach path.
- **OCR fallback runs only when text extraction produces < `SOURCING_OCR_THRESHOLD` chars.** Image-only PDFs hit Claude vision; everything else short-circuits.
- **Prompt + schema versioning.** `parsing.PromptVersion` is stamped into the audit trail. Bump it whenever `parse_resume.tmpl` meaningfully changes.

### Slice 3 specifics

- **Two-stage scoring funnel.** Stage 1 (every Application): rule_match gate + embedding cosine. Stage 2 (top-K only): Claude tool-use judging. The funnel caps LLM cost at `top_k × intents`, not `candidates × intents`. See [`scoring.md`](./scoring.md) §3 for the algorithm.
- **Rule-fail short-circuits.** If a candidate misses any *required* criterion, the Application is `Excluded` immediately — no embedding cost, no LLM cost.
- **Embeddings cached with version keys.** `(intent_id, spec_version)` for role embeddings, `(candidate_id, profile_schema_version)` implicit for candidate embeddings. Re-confirming an unchanged intent reuses everything.
- **LLM judgments cached** by `(candidate_id, intent_id, intent_spec_version, profile_schema_version, prompt_version)`. Re-confirm with no change → zero LLM cost.
- **pgvector backs the cosine search.** `candidates.profile_embedding` and `hiring_intent_embeddings.role_embedding` are `vector(1024)`; `ivfflat` index supports ANN at scale.
- **Two new worker pools.** `MatchPool` drains `applications WHERE status=New`; `JudgePool` drains `judge_jobs WHERE status IN (Pending, Running)`. Both run alongside the slice-1 upload worker.
- **Symmetric serialization** of candidate profile and role spec into embedding text — keeps both vectors in the same semantic space (see `scoring.md` §2).
- **On-demand rescore available.** Recruiter can trigger `POST /intents/{id}/applications:rescore` to re-judge without waiting for a new `IntentConfirmed` event (see §"Recruiter actions (slice 4)" above).
