# Recruiter UI — Resume Upload, Candidate List, Shortlist Actions

> **Status:** approved 2026-05-17. First UI slice for sourcing — the React surface that has been API-only since slice 1.

## Why this slice

Slices 1-4 of sourcing + slice 1 of interview shipped a complete backend: recruiters can upload resumes, the pipeline scans/extracts/parses/scores them, and the recruiter can shortlist/reject/hire candidates and have interview processes spin up automatically. None of this has a UI yet — every recruiter flow today requires curl or Postman against `http://localhost:8080`.

The web app (`web/`) only has pages for intent + posting + BGV. The posting page even still has a **Source Distribution** section showing four channels (LinkedIn, Career Page, Email, Internal DB) with a Publish button — but those channels don't exist as integrations. They're aspirational scaffolding (issue #8).

This slice does two things at once:

1. **Removes the dead Source Distribution scaffolding** from the posting page (and relaxes the backend `Publish` invariant that requires non-empty channels).
2. **Adds the recruiter dashboard surface** — resume upload, candidate list, shortlist/reject/hire actions — wired against the existing sourcing API on the same posting detail page.

It also extends the upload contract: ZIP support (the recruiter dumps a folder of resumes at once), ODT support, and per-intent dedup with a visible warning when the recruiter accidentally re-uploads a resume already in the same intent.

## Scope

Locked decisions from the 2026-05-17 brainstorm:

| # | Decision |
|---|---|
| S-1 | Source Distribution card REMOVED from posting page. Backend `posting.Publish` accepts zero channels. |
| S-2 | Posting detail page sections, stacked: header → JD → **Upload card** → **Candidate list**. No tabs, no new routes. |
| S-3 | Accepted formats: PDF, DOC, DOCX, ODT, ZIP. ZIP entries must be one of the other 4. |
| S-4 | Dedup re-keyed: `(tenant, intent, content_hash)`. Same-intent dupes warn + skip. Different-intent uploads process normally, reusing the same Candidate row. |
| S-5 | ZIP extracted synchronously at upload time. Anti-abuse rails: ≤100 entries, ≤50 MB uncompressed total, reject nested ZIPs, reject encrypted ZIPs, reject path-traversal entries. |
| S-6 | Card layout for candidate rows, with band pill (Strong / Moderate / Weak). |
| S-7 | Hybrid action affordance: primary Shortlist button + ⋮ overflow menu. |
| S-8 | Sticky toolbar above candidate list: status filter chips with counts, search by name, sort, density toggle (compact-card / dense-row). |
| S-9 | Bulk-select checkboxes; bulk-action bar ("Shortlist N" / "Reject N") appears when any selected. Hire is per-candidate only — not bulkable. |
| S-10 | Pagination via "Load more" (20 per page). Candidate detail page deferred to a follow-up slice; ⋮ menu's "View detail" is a placeholder link. |

## Architecture overview

**Single posting detail page** at `/postings/:id` becomes the recruiter's workbench:

```
┌──────────────────────────────────────────────────────────┐
│  ← Back to postings                                       │
│                                                           │
│  Senior Backend Engineer  [PUBLISHED]                     │
│  Intent: <id> · Created: 2 days ago                       │
├──────────────────────────────────────────────────────────┤
│  Job Description                                          │
│  (existing — unchanged)                                   │
├──────────────────────────────────────────────────────────┤
│  Upload resumes                          (REPLACES        │
│  ┌──────────────────────────────────┐    Source           │
│  │ Drop resumes here                │    Distribution)    │
│  │ or browse files · PDF DOC DOCX   │                     │
│  │ ODT ZIP · up to 10 MB each       │                     │
│  └──────────────────────────────────┘                     │
│  [per-file outcome rows: queued / dedup / failed / ZIP]   │
├──────────────────────────────────────────────────────────┤
│  Candidates  (14)              [filters][search][sort][≡▦]│
│  (sticky toolbar)                                         │
│                                                           │
│  □ ◐ Alice Singh · Sr Backend · Bangalore   92  STRONG ⭐ │
│      [Shortlist] [⋮]                                      │
│  □ ◐ Bharat Kumar · Staff Eng · Pune        88  STRONG ⭐ │
│      [Shortlist] [⋮]                                      │
│  □ ◐ Chitra Rao · Backend · Hyderabad       71  MODERATE  │
│      [Shortlist] [⋮]                                      │
│  ...                                                      │
│  Showing 20 of 32 Scored  ·  [Load more]                  │
└──────────────────────────────────────────────────────────┘
```

When the recruiter starts an upload, a batch SSE connection opens. Per-file outcomes (queued, deduplicated-in-intent, rejected, ZIP-extracted children) render immediately from the HTTP response. As the pipeline progresses, SSE events update each row's status badge; when an application gets scored, the React Query cache for `['applications', intentID]` invalidates and the candidate appears in the list below.

Selection state (checkboxes) is local to the page; reloading the page clears it. Filter and sort state are URL search params so the recruiter can share a deep link to a filtered view. Density mode is a personal preference and persists per-browser in `localStorage`.

## Frontend implementation

### New & modified files in `web/src/`

```
api/
  sourcing.ts                          NEW — typed client for sourcing endpoints
  types.ts                             MODIFY — add Candidate, Application, BatchUploadOutcome types

features/sourcing/                     NEW feature directory
  UploadCard.tsx                       drag-drop zone + file picker + outcomes list
  UploadOutcomeRow.tsx                 one row per file (plain / ZIP-parent / ZIP-child / dedup warning / rejection)
  UploadOutcomesList.tsx               container; groups ZIP parents with children
  useUploadBatch.ts                    useMutation wrapper around POST /resumes:batch (multipart)
  useBatchSSE.ts                       EventSource hook subscribing to /resumes/batches/{id}/events
  CandidateListSection.tsx             sticky toolbar + bulk-action bar + paginated list
  CandidateListToolbar.tsx             status filter chips + search + sort + density toggle
  CandidateCard.tsx                    compact card (single-line layout) — density=compact
  CandidateDenseRow.tsx                ultra-dense row — density=dense
  BulkActionBar.tsx                    floating bar shown when selectedIds.size > 0
  ApplicationActions.tsx               primary Shortlist + ⋮ overflow menu, conditional per lifecycle
  useApplicationsList.ts               useInfiniteQuery wrapper around GET /intents/{id}/applications
  useShortlist.ts                      optimistic-update mutation
  useReject.ts                         optimistic-update mutation
  useHire.ts                           optimistic-update mutation
  useRetryUpload.ts                    mutation against POST /resumes/{upload_id}:retry

features/posting/
  PostingDetailPage.tsx                MODIFY — remove Source Distribution card; mount <UploadCard/> + <CandidateListSection/>

components/ui/
  DropZone.tsx                         NEW — generic drag-drop primitive
  Menu.tsx                             NEW — generic dropdown (used by ⋮ overflow)
  Pill.tsx                             NEW — band pill (Strong/Moderate/Weak)
```

### API client surface (`api/sourcing.ts`)

```ts
export const sourcingApi = {
  uploadBatch(intentId: string, files: File[]): Promise<BatchUploadResponse>,
  getBatchStatus(batchId: string): Promise<BatchStatusResponse>,
  subscribeBatchEvents(batchId: string): EventSource,
  listApplications(intentId: string, filter: AppListFilter): Promise<ApplicationListResponse>,
  getCandidate(candidateId: string): Promise<CandidateDetail>,
  shortlist(applicationId: string): Promise<void>,
  reject(applicationId: string, reason: string): Promise<void>,
  hire(applicationId: string): Promise<void>,
  retryUpload(uploadId: string): Promise<void>,
};

export interface BatchUploadOutcome {
  filename: string;
  status: 'queued' | 'deduplicated' | 'duplicate_in_intent' | 'extracted_from_zip' | 'rejected';
  upload_id?: string;
  candidate_id?: string;
  parent_filename?: string;   // when this is a ZIP child
  parent_item_id?: string;    // ditto
  error?: { code: string; message: string; detail?: Record<string, unknown> };
}

export interface BatchUploadResponse {
  batch_id: string;
  items: BatchUploadOutcome[];
}

export interface Application {
  id: string;
  candidate_id: string;
  intent_id: string;
  status: 'New' | 'Scored' | 'Excluded' | 'EmbedFailed' | 'JudgeFailed' | 'Stale'
        | 'Shortlisted' | 'Interviewing' | 'Rejected' | 'Hired';
  overall_score: number | null;
  score_band: 'strong' | 'moderate' | 'weak' | null;
  candidate: {
    full_name: string;
    headline: string;
    location: string;
    top_skills: Array<{ name: string; years?: number }>;
    judge_summary?: string;  // first sentence of llm_judgment.summary, for the card preview
  };
  created_at: string;
  updated_at: string;
}
```

The `Application` shape is enriched server-side by the existing `GetApplicationsHandler` join — no new backend query is needed, but a few profile fields (top_skills, judge_summary) are added to the response. The list endpoint already loads candidate detail per row; this is a shape extension, not a new query.

### State management

| State | Where it lives | Why |
|---|---|---|
| Server state (applications, batch outcomes, candidates) | React Query | Already used in the app; query keys `['applications', intentId, filter]`, `['batch', batchId]`. |
| Active upload batch ids | local component state in `<UploadCard/>` | Per-session; no need to persist. |
| Selected application ids | local state in `<CandidateListSection/>` (`Set<string>`) | Clears on page change. |
| Filter / search / sort | URL search params (`useSearchParams`) | Shareable, survives refresh. |
| Density preference | `localStorage` | Per-user preference, per browser. |

### SSE wiring

`useBatchSSE(batchId)` is a hook:

1. On mount: open `EventSource` to `/api/v1/resumes/batches/{batchId}/events`.
2. On each event:
   - `item_accepted` / `item_failed` / `item_extracted` / `item_parsed` → update the corresponding outcome row's badge in-place.
   - On `item_parsed`, invalidate `['applications', intentId]` so the candidate appears in the list.
3. On all items reaching terminal state, close the EventSource.
4. On reconnect failure (3 retries with backoff 1s/5s/15s), fall back to polling `getBatchStatus` every 5s for up to 2 minutes, then give up with a toast.

Polling for the applications list otherwise only fires while an upload batch is active — no continuous polling.

### Routing

No route changes. `/postings/:id` continues to serve the posting detail page; everything new is rendered inline. URL search params drive filter + sort (shareable):

```
/postings/abc123?status=Scored&sort=score_desc
```

Density mode is intentionally NOT in the URL — it's a personal preference (localStorage), not a view to share.

## Backend implementation

### Files modified / created

| File | Change |
|---|---|
| `internal/jobposting/domain/entities/posting.go` | `Publish` no longer rejects empty channel list. `ErrPublishNeedsChannels` removed; existing test updated. |
| `internal/jobposting/application/commands/publish_posting.go` | Accept empty channels in input; pass through unchanged. |
| `internal/jobposting/domain/repositories/posting_repository_test.go` | Update tests that asserted on the old invariant. |
| `internal/sourcing/domain/valueobjects/mime_type.go` | Add `application/vnd.oasis.opendocument.text` (ODT) and `application/zip` to `Allowed`. |
| `internal/sourcing/domain/repositories/resume_upload_repository.go` | New method on the port: `FindByContentHashAndIntent(ctx, tenant, intentID, hash)`. Old `FindByContentHash` retained for non-upload callers. |
| `internal/sourcing/infrastructure/persistence/postgres_resume_upload_repository.go` | Implement `FindByContentHashAndIntent`. Update `Save` to catch the per-(intent, hash) dedup violation. |
| `internal/sourcing/application/commands/upload_resume_batch.go` | Major: detect ZIP, extract, recurse per entry through dedup-and-persist flow. New outcome statuses `extracted_from_zip` (parent marker) and `duplicate_in_intent` (warning). |
| `internal/sourcing/infrastructure/text/zip_extractor.go` (NEW) | Helper: `ExtractZip(body []byte, limits ZipLimits) ([]ExtractedEntry, error)`. |
| `internal/sourcing/application/dto/batch_upload.go` | Add `Status` enum values; add `ParentFilename`, `ParentItemID` to `ItemOutcome` so the UI can group ZIP children. |
| `internal/sourcing/application/queries/list_applications.go` | Extend response shape with `top_skills` + `judge_summary` per candidate (decode existing `parsed_profile` JSONB; first-sentence-extract from `llm_judgment.summary`). |
| `migrations/sourcing/000010_dedup_per_intent.up.sql` (NEW) | Drop `UNIQUE (tenant_id, content_hash)` on `resume_uploads_dedup`. Add `intent_id UUID NOT NULL`. Backfill from `resume_uploads`. Add `UNIQUE (tenant_id, intent_id, content_hash)`. |
| `migrations/sourcing/000010_dedup_per_intent.down.sql` (NEW) | Reverse. |
| `docs/api/v1/sourcing.openapi.yaml` | Bump to `1.0.0-slice5`. Document the new outcome statuses, ZIP behaviour, response shape extension. |

No new domain events, no new outbox plumbing, no new audit actions. The existing pipeline (scan → extract → parse → score) keeps running unchanged per upload — ZIP just fans one upload into N before the pipeline starts.

### ZIP extraction details

`internal/sourcing/infrastructure/text/zip_extractor.go`:

```go
type ZipLimits struct {
    MaxEntries           int   // default 100
    MaxUncompressedBytes int64 // default 50 MiB
    MaxEntrySizeBytes    int64 // default 10 MiB (matches MaxFileBytes)
}

type ExtractedEntry struct {
    Filename string
    Bytes    []byte
}

var (
    ErrZipEncrypted        = errors.New("zip: encrypted entries not supported")
    ErrZipNested           = errors.New("zip: nested zips not supported")
    ErrZipPathTraversal    = errors.New("zip: path traversal not allowed")
    ErrZipTooManyEntries   = errors.New("zip: too many entries")
    ErrZipUncompressedTooLarge = errors.New("zip: uncompressed total too large")
)

func ExtractZip(body []byte, limits ZipLimits) ([]ExtractedEntry, error)
```

Implementation uses `archive/zip` from the standard library. Each entry:
- Reject if `entry.IsDir()` (directories skipped silently).
- Reject filename containing `..`, leading `/`, or empty.
- Reject if `entry.UncompressedSize64 > MaxEntrySizeBytes`.
- Reject if entry's MIME (sniffed from the first 512 bytes) is `application/zip` — no nested ZIPs.
- Encryption: `archive/zip` returns an error opening the entry reader when it's encrypted; treat as `ErrZipEncrypted`.

Total uncompressed size is checked as bytes are streamed out, not from the declared header (zip-bomb resistance). If the running total exceeds `MaxUncompressedBytes`, abort with `ErrZipUncompressedTooLarge`.

### Upload command changes

In `UploadResumeBatchHandler.Handle`, before the existing per-item processing:

```go
mime, err := vo.SniffMimeType(body)
if err != nil { /* rejected outcome */ return }

if mime.String() == "application/zip" {
    entries, err := text.ExtractZip(body, text.DefaultZipLimits)
    if err != nil {
        return rejected(item.Filename, zipErrCode(err), err.Error(), nil)
    }
    // Emit parent outcome (informational, no upload_id)
    parentID := uuid.New().String()
    out := []dto.ItemOutcome{{
        Filename: item.Filename,
        Status:   "extracted_from_zip",
        // parent_item_id is the ID children reference
    }}
    out[0].ParentItemID = &parentID
    for _, entry := range entries {
        child := h.processOneFile(ctx, tenant, intentID, batchID, dto.BatchItem{
            Filename: entry.Filename,
            Body:     bytes.NewReader(entry.Bytes),
            Size:     int64(len(entry.Bytes)),
        })
        child.ParentFilename = &item.Filename
        child.ParentItemID = &parentID
        out = append(out, child)
    }
    return out
}

// non-ZIP — existing flow unchanged.
```

Per-intent dedup change inside `processOneFile`:

```go
// before:
existing, err := h.repo.FindByContentHash(ctx, tenant, hashStr)

// after:
existing, err := h.repo.FindByContentHashAndIntent(ctx, tenant, intentID, hashStr)
if err == nil {
    uid := existing.ID()
    return dto.ItemOutcome{
        Filename: item.Filename, UploadID: &uid, Status: "duplicate_in_intent",
    }
}
```

The `FindByContentHash` method (without intent scoping) stays on the repository for non-upload callers (e.g., the cascade-delete query in slice 4 still keys candidates by content hash).

### Migration `000010_dedup_per_intent.up.sql`

```sql
BEGIN;

-- Add intent_id column, nullable for now while we backfill.
ALTER TABLE resume_uploads_dedup
    ADD COLUMN intent_id UUID;

-- Backfill from resume_uploads. Each dedup row maps to the intent of the
-- ORIGINAL upload that won the dedup; that's the only intent_id the row
-- ever knew about. Once backfilled, the column can be NOT NULL.
UPDATE resume_uploads_dedup d
SET intent_id = (
    SELECT u.intent_id
    FROM resume_uploads u
    WHERE u.id = d.upload_id
);

-- Any rows with NULL intent_id are orphans (their upload row was deleted —
-- shouldn't happen, but defend). Delete them.
DELETE FROM resume_uploads_dedup WHERE intent_id IS NULL;

ALTER TABLE resume_uploads_dedup
    ALTER COLUMN intent_id SET NOT NULL;

-- Drop the old global-per-tenant unique constraint.
ALTER TABLE resume_uploads_dedup
    DROP CONSTRAINT resume_uploads_dedup_tenant_id_content_hash_key;

-- Add the new per-intent unique constraint.
ALTER TABLE resume_uploads_dedup
    ADD CONSTRAINT resume_uploads_dedup_tenant_intent_hash_key
    UNIQUE (tenant_id, intent_id, content_hash);

COMMIT;
```

Reverse migration drops the new constraint, drops the column, restores the old constraint. Data loss risk if reversed after new cross-intent uploads have happened — documented in the migration header.

## Data flow

### Happy path — recruiter drops a ZIP

```
[browser] recruiter drops batch.zip (3 PDFs)
   ↓
POST /api/v1/intents/{intent_id}/resumes:batch  (multipart)
   ↓
[backend: UploadResumeBatchHandler]
  • multipartSource yields the single zip file
  • SniffMimeType → application/zip
  • ExtractZip → 3 entries
  • for each entry:
      SniffMimeType → application/pdf  (else → rejected outcome)
      FindByContentHashAndIntent(tenant, intent, hash)
        hit  → outcome {status: "duplicate_in_intent", parent_filename: "batch.zip"}
        miss → storage.Put, repo.Save → outcome {status: "queued", parent_filename: "batch.zip"}
   ↓
HTTP 200 — body lists 1 parent (extracted_from_zip) + 3 child outcomes
   ↓
[browser] UploadCard renders parent row with 3 indented child rows
          opens EventSource on /api/v1/resumes/batches/{batch_id}/events
   ↓
[backend pipeline runs async per upload]
   scanner → extractor → parser → Candidate → Application(intent)
   each stage emits to outbox → dispatched → SSE
   ↓
[SSE → browser]
  resume_extracted   → child badge "extracting"
  resume_parsed      → child badge "parsed"
  application_scored → invalidate ['applications', intentID] → list refetches → card appears
```

### Same-intent dedup path

Recruiter re-drops `alice.pdf` (already in this intent). `FindByContentHashAndIntent` hits, outcome is `duplicate_in_intent`, amber row renders, no SSE subscription for this row.

### Different-intent path

Same recruiter drops `alice.pdf` onto a different intent's posting. `FindByContentHashAndIntent` misses (different `intent_id` key), new dedup row created, `storage.Put` is idempotent (bytes already at the content-hash key), new `ResumeUpload` row created. The slice-2 `CandidateRepository.Save(byContentHash)` returns the existing `Candidate` row → reused. `ScoreCandidateHandler` creates a new `Application(existing candidate, new intent)`. Outcome is `queued`. No backend changes needed for candidate reuse — existing behaviour.

### Shortlist flow (optimistic)

```
[browser] click Shortlist on Alice's card
   ↓
useShortlist mutation:
  onMutate: optimistically update ['applications', intentID] cache — Alice's row → Shortlisted instantly
  POST /api/v1/applications/{appID}:shortlist  →  204
  onSuccess: confirm (no-op, cache already updated)
  onError: rollback + toast
   ↓
[backend] TransitionApplicationHandler runs, emits ApplicationShortlisted to outbox
   ↓
Outbox dispatcher publishes the event → interview ApplicationShortlistedConsumer creates InterviewProcess (slice 1, already wired).
```

## Error handling

### Frontend

1. **Pre-submit validation:** dropzone's `accept` attribute filters to PDF/DOC/DOCX/ODT/ZIP, but users can still drop other types. Client-side MIME sniff via the `File.type` + extension check rejects with an inline outcome row (`Unsupported format`); no HTTP call.
2. **File >10 MB:** client-side rejection with `File exceeds 10 MB`.
3. **400 from backend** (intent not found, malformed multipart): toast at top of upload card.
4. **`duplicate_in_intent` is NOT an error:** it's a successful 200 with that status; renders as the amber warning row.
5. **5xx / network on upload:** toast with message; outcome rows show `failed` with a Retry link per file.
6. **Retry link:** calls `POST /resumes/{upload_id}:retry` only for outcomes that have an `upload_id` (persisted but worker later failed). For client-side rejections, Retry is hidden — user must re-pick.
7. **Action mutations failing:** roll back optimistic update + toast.
8. **SSE connection drop:** exponential-backoff reconnect (1s, 5s, 15s); after 3 fails, switch to polling `getBatchStatus` every 5s for up to 2 minutes; after that, toast and stop.

### ZIP-specific

- All ZIP-level rejections (encrypted, nested, path traversal, >100 entries, >50 MB uncompressed) come back as a single rejected outcome on the ZIP parent. UI shows the parent-only row with the rejection reason; no child rows render.
- Per-entry-within-ZIP rejections (bad MIME, too large) render as a child row with the same rejection treatment as a top-level rejection.

### Transaction safety

ZIP extraction is in-memory; per-entry persistence happens in separate transactions. If a later entry fails after earlier ones succeed, the UI sees mixed outcomes. The recruiter can use the Retry action on individual failed entries.

## Testing strategy

### Backend (Go)

| Layer | Test |
|---|---|
| Domain | `posting.Publish` accepts empty channel list (one new test, one removed). |
| Domain | `MimeType.Allowed` extended; `ParseMimeType` covers ODT + ZIP. |
| Unit | `text/zip_extractor.go` — happy path (3 entries), encrypted ZIP, nested ZIP, path-traversal entry, entry-count limit, uncompressed-size limit. Use `archive/zip` to construct test fixtures inline. |
| Unit | `UploadResumeBatchHandler` — ZIP fan-out produces N+1 outcomes (parent + entries); each entry goes through dedup + persist; per-(intent, hash) collision returns `duplicate_in_intent`. |
| Integration | `PostgresResumeUploadRepository.FindByContentHashAndIntent` — same hash on different intents both return their own row; same hash on same intent returns the existing. |
| Integration | Migration test: insert pre-migration rows → run migration → assert backfilled `intent_id` matches the source `resume_uploads` row. |
| E2e | `tests/sourcing_zip_upload_e2e_test.go` — upload a ZIP with 3 PDFs to one intent; assert 3 candidates land. Re-upload the same ZIP to the same intent; assert 3 `duplicate_in_intent` outcomes and no new candidates. Upload the same ZIP to a *different* intent; assert 3 new Applications referencing the same 3 Candidate rows. |

### Frontend (React)

| Layer | Test |
|---|---|
| Unit (Vitest) | Upload outcome rendering: each `status` value → right badge + colour. ZIP grouping: outcomes with same `parent_filename` render under one parent. |
| Unit | `useApplicationsList` filter + sort + density toggle state machine. |
| Unit | Optimistic-update mutations (shortlist/reject/hire) — onMutate updates cache, onError rolls back. |
| Integration (MSW) | Full `PostingDetailPage` with mocked `/api/v1/*` endpoints: upload a file → see queued outcome → mock SSE pushes parsed event → candidate appears in list → click Shortlist → optimistic update → backend confirms. |
| Smoke (manual) | After code lands: run `STUB_LLMS=true ./bin/api` + `npm run dev`, walk the recruiter flow end-to-end. |

### Excluded

- The Anthropic adapter for ODT is not tested separately — covered by the existing parser contract; ODT is just another MIME the extractor receives.
- No pixel-accurate Storybook stories; cards + Tailwind classes from existing `components/ui` primitives are used directly.

## Out of scope

Deferred to follow-up slices:

- **Candidate detail page** (`/candidates/:id`) — shows decrypted PII, full parsed profile, attached interview process with generated questions, full LLM judgment text. The ⋮ menu's "View detail" is a placeholder in this slice.
- **Original-resume download endpoint** — `GET /candidates/{id}/resume` streaming the stored bytes, with audit. Needed for the candidate detail page.
- **Async ZIP processing** — for ZIPs over the inline rails. Persist the ZIP as `ResumeUpload(kind=zip)`, a worker extracts entries. Defer until a recruiter hits the rails.
- **Interview module UI** — loop template editor on intent page, per-round feedback UI on interview process page.
- **Notifications** — email / Slack on candidate scored or process state changes.
- **Recruiter actions on multiple intents at once** — bulk candidate management across an entire tenant.
- **Real source-distribution integrations** — once we wire one (e.g., Career Page hosted listing), the page can re-introduce a section. Issue #8 tracks.

## What this slice ships

- Posting detail page becomes the recruiter's workbench: drop resumes, watch them score, shortlist the strong ones, reject the weak ones — all without leaving the page.
- ZIP upload lets a recruiter ingest a folder of resumes in one drop.
- Per-intent dedup with a visible warning prevents accidental double-processing of the same candidate for the same role.
- Bulk-select makes it possible to act on 10+ candidates at a time.
- Source Distribution scaffolding (which never worked) is removed.

After this slice, the only end-to-end recruiter flows that still require curl are:
- Candidate detail / resume download (next slice).
- Interview-process management (interview UI slice — separate context, separate UI work).
