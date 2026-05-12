# sourcing — bounded context

Recruiters upload resumes against confirmed hiring intents. The context
parses, dedups, scores, and exposes a recruiter-facing pipeline of applications.

Slice 1 (this milestone) ships ingestion only: upload → scan → text-extract.
Parsing, matching, candidate aggregate, lifecycle actions land in slices 2–4.

## Ubiquitous language

| Term | Meaning |
|---|---|
| **Resume Upload** | The lifecycle of one uploaded file: status, attempts, errors, per-stage artifacts. Owns nothing about the person yet. |
| **Batch** | A grouping of uploads submitted together. UI-level concept; `batch_id` is on every upload row. |
| **Candidate** *(slice 2+)* | Tenant-level identity for a person. One per `(tenant_id, content_hash)`. |
| **Application** *(slice 3+)* | Match between a `Candidate` and a `HiringIntent`. Holds match score and lifecycle. |
| **Stage Artifacts** | Per-stage outputs persisted on the upload row so crashes resume from the last successful stage. |

## Pipeline (slice 1)

```
Pending → Scanning → Extracting → Extracted
                          ↘            ↘
                         Failed     Quarantined
```

Each stage is a port (`FileScanner`, `TextExtractor`), each port has a dev and
prod adapter. The worker (`infrastructure/worker.Pool`) polls the
`resume_uploads` table via the repository's `ClaimNextPending` and hands each
row to `ProcessUploadHandler`.

## Configuration

| Var | Default | Purpose |
|---|---|---|
| `SOURCING_STORAGE_PATH` | `/tmp/hireflow-resumes` | Localfs storage root |
| `SOURCING_SCANNER_BACKEND` | `noop` | `noop` or `clamd` |
| `SOURCING_SCANNER_ADDR` | `tcp://localhost:3310` | Honored when backend=clamd |
| `SOURCING_MAX_FILE_BYTES` | `10485760` | Per-file cap |
| `SOURCING_WORKER_POOL` | `4` | Worker goroutines |

## API (slice 1)

```
POST /api/v1/intents/{intent_id}/resumes:batch   multipart/form-data
GET  /api/v1/resumes/batches/{batch_id}          batch status
```

See `docs/api/v1/sourcing.openapi.yaml`.

## Architecture invariants

- **Each upload is per-file independent.** A batch is a UI grouping; the
  pipeline doesn't have batch-level transactions.
- **Stages persist before transition** via `StageArtifacts`. Crash mid-extracting
  resumes by re-running extract (idempotent on bytes).
- **Adapter classifies retryability** via `RetryDecision`. The worker applies
  backoff and attempt-count caps; only the adapter decides Retryable vs Fatal.
- **Tenant-scoped from line one.** Every read includes `tenant_id`.
- **Outbox + in-process bus.** Same pattern as `hiringintent` and `jobposting`.
