# Recruiter UI — Resume Upload + Candidate List + Shortlist Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the recruiter dashboard surface on the posting detail page — drop resumes (singles or ZIPs), watch them score, shortlist/reject/hire candidates inline. Backend gets per-intent dedup + ZIP support + ODT support; frontend replaces dead Source Distribution scaffolding with a real workbench.

**Architecture:** Backend changes are small and well-bounded (one new migration, one new helper package for ZIP, one repo method, one upload-handler rewrite, one list-response enrichment). Frontend adds a new `features/sourcing/` directory wired into the existing `features/posting/PostingDetailPage.tsx`. SSE for upload-batch progress is already exposed by the backend (slice 4); UI consumes it via a new EventSource hook.

**Tech Stack:** Same as slices 1-5. Backend: Go, pgx, archive/zip (stdlib). Frontend: React 18, TanStack Query (React Query) v5, react-router-dom v6, Tailwind, lucide-react. No new dependencies.

**Spec reference:** `docs/superpowers/specs/2026-05-17-recruiter-ui-resume-upload-design.md`. Locked decisions S-1 through S-10.

---

## Conventions baked into every task

- **Working branch:** start a fresh branch off the just-merged `main`: `feat/recruiter-ui-slice`.
- **Module path:** `github.com/hustle/hireflow`.
- **Backend tests:** unit `_test.go`; integration `//go:build integration`-gated. `make test-integration` runs against live Postgres on `localhost:5433`.
- **Frontend tests deferred:** the web app has no Vitest / MSW setup today (no `test` script in `web/package.json`). Frontend verification in this slice is `npm run typecheck` + `npm run lint` + the manual smoke run from the spec's testing section. Adding Vitest/MSW is filed as a follow-up after this slice ships.
- **Commit cadence:** one commit per task. **No `Co-Authored-By: Claude` trailers.**
- **`make test-integration`** runs with `-p 1` so per-test TRUNCATE doesn't race across packages.

---

## File structure

### Files created

```
migrations/sourcing/
    000010_dedup_per_intent.up.sql
    000010_dedup_per_intent.down.sql

internal/sourcing/infrastructure/text/
    zip_extractor.go                 ExtractZip + ZipLimits + Zip* errors
    zip_extractor_test.go

tests/
    sourcing_zip_upload_e2e_test.go  full ZIP+dedup e2e

web/src/api/
    sourcing.ts                      typed client for sourcing endpoints

web/src/features/sourcing/
    UploadCard.tsx                   drag-drop + outcomes list + Clear button
    UploadOutcomeRow.tsx             one outcome row (queued / dedup / rejected / zip-parent / zip-child)
    UploadOutcomesList.tsx           groups ZIP parents with their children
    useUploadBatch.ts                useMutation wrapper around POST /resumes:batch (multipart)
    useBatchSSE.ts                   EventSource hook on /resumes/batches/{id}/events
    CandidateListSection.tsx         sticky toolbar + bulk-action bar + paginated list container
    CandidateListToolbar.tsx         status filter chips + search + sort + density toggle
    CandidateCard.tsx                compact card row (default density)
    CandidateDenseRow.tsx            ultra-dense row (alt density)
    BulkActionBar.tsx                floating "N selected — Shortlist N / Reject N / Clear"
    ApplicationActions.tsx           primary Shortlist button + ⋮ overflow menu, conditional per lifecycle
    useApplicationsList.ts           useInfiniteQuery wrapper around GET /intents/{id}/applications
    useShortlist.ts                  optimistic mutation
    useReject.ts                     optimistic mutation
    useHire.ts                       optimistic mutation
    useBulkAction.ts                 bulk Shortlist / Reject mutation (sequential per id)
    useRetryUpload.ts                mutation against POST /resumes/{upload_id}:retry

web/src/components/ui/
    DropZone.tsx                     generic drag-drop primitive
    Menu.tsx                         generic dropdown (used by ⋮)
    Pill.tsx                         band pill (Strong/Moderate/Weak) + variants
```

### Files modified

- `internal/jobposting/domain/entities/posting.go` — `Publish` accepts empty channel list; `ErrPublishNeedsChannels` removed.
- `internal/jobposting/domain/entities/posting_test.go` — drop the "rejects empty channels" test, add "accepts empty channels".
- `internal/jobposting/application/commands/publish_posting.go` — pass through unchanged when channels empty.
- `internal/sourcing/domain/valueobjects/mime_type.go` — add `application/vnd.oasis.opendocument.text` + `application/zip` to `Allowed`.
- `internal/sourcing/domain/valueobjects/mime_type_test.go` — add cases for the new types.
- `internal/sourcing/domain/repositories/resume_upload_repository.go` — add `FindByContentHashAndIntent` to the port.
- `internal/sourcing/infrastructure/persistence/postgres_resume_upload_repository.go` — implement `FindByContentHashAndIntent`; update Save error mapping for the new dedup key.
- `internal/sourcing/infrastructure/persistence/postgres_resume_upload_repository_test.go` — extend TRUNCATE list if needed; add `FindByContentHashAndIntent` integration tests.
- `internal/sourcing/application/commands/upload_resume_batch.go` — major rewrite: ZIP fan-out + per-intent dedup + new outcome statuses.
- `internal/sourcing/application/commands/upload_resume_batch_test.go` — extensive new test cases.
- `internal/sourcing/application/dto/batch_upload.go` — add `duplicate_in_intent` and `extracted_from_zip` statuses; add `ParentFilename` + `ParentItemID` fields.
- `internal/sourcing/application/queries/list_applications.go` — extend response with `top_skills` + `judge_summary`.
- `internal/sourcing/application/queries/list_applications_test.go` — add coverage for the enriched fields.
- `internal/sourcing/delivery/http/v1/dto.go` — extend the HTTP response DTO mirroring the application-layer shape change.
- `docs/api/v1/sourcing.openapi.yaml` — bump `info.version` to `1.0.0-slice5`; document new statuses, ZIP behaviour, response extensions.
- `Makefile` — extend `test-integration` to include the new `tests/sourcing_zip_upload_e2e_test.go` (already in `tests/`, picked up automatically).
- `web/src/api/types.ts` — add `BatchUploadOutcome`, `BatchUploadResponse`, `Application`, `CandidateDetail`, `BatchStatusResponse` shapes.
- `web/src/features/posting/PostingDetailPage.tsx` — remove Source Distribution card, mount `<UploadCard/>` + `<CandidateListSection/>`.

---

## Task 1: Relax `posting.Publish` invariant (zero channels allowed)

**Files:**
- Modify: `internal/jobposting/domain/entities/posting.go`
- Modify: `internal/jobposting/domain/entities/posting_test.go`
- Modify: `internal/jobposting/application/commands/publish_posting.go` (likely no body change — just verify)

- [ ] **Step 1: Inspect the existing test for the old invariant**

```bash
cd /Users/manojkumar.m1thoughtworks.com/hustle/code/theo/hireflow
grep -n "ErrPublishNeedsChannels" internal/jobposting/
```

Expected: test `TestPublish_RejectsEmptyChannelList` in `posting_test.go` and entity error in `posting.go`. Note the line ranges.

- [ ] **Step 2: Write the new test (replacing the old)**

In `internal/jobposting/domain/entities/posting_test.go`, replace `TestPublish_RejectsEmptyChannelList` with:

```go
func TestPublish_AcceptsEmptyChannelList(t *testing.T) {
	p := newPosting(t)
	_ = p.PullEvents()

	err := p.Publish(nil)
	require.NoError(t, err)

	assert.Equal(t, valueobjects.StatusPublished, p.Status())
	assert.NotNil(t, p.PublishedAt())
	assert.Empty(t, p.Sources())

	evs := p.PullEvents()
	require.Len(t, evs, 1)
	assert.Equal(t, "jobposting.JobPostingPublished", evs[0].EventName())
}
```

- [ ] **Step 3: Run the new test — it should fail**

```bash
go test ./internal/jobposting/domain/entities/... -run TestPublish_AcceptsEmptyChannelList -v
```

Expected: FAIL with `ErrPublishNeedsChannels`.

- [ ] **Step 4: Update `posting.go`**

In `internal/jobposting/domain/entities/posting.go`:

1. Remove the line that defines `ErrPublishNeedsChannels = errors.New("publish requires at least one source channel")`.
2. In `Publish`, remove the early return `if len(channels) == 0 { return ErrPublishNeedsChannels }`.

If `ErrPublishNeedsChannels` is referenced anywhere else (`grep -rn ErrPublishNeedsChannels`), remove or update those references too.

- [ ] **Step 5: Run test to verify it passes**

```bash
go test ./internal/jobposting/... -count=1 -race
```

Expected: PASS.

- [ ] **Step 6: Run `go vet` and `make build`**

```bash
go vet ./...
make build
```

Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add internal/jobposting/
git commit -m "refactor(jobposting): Publish accepts empty channel list"
```

---

## Task 2: Extend MimeType allowlist with ODT + ZIP

**Files:**
- Modify: `internal/sourcing/domain/valueobjects/mime_type.go`
- Modify: `internal/sourcing/domain/valueobjects/mime_type_test.go`

- [ ] **Step 1: Write failing test cases**

In `internal/sourcing/domain/valueobjects/mime_type_test.go`, add to the existing happy-path table:

```go
func TestParseMimeType_AcceptsODT(t *testing.T) {
	m, err := vo.ParseMimeType("application/vnd.oasis.opendocument.text")
	require.NoError(t, err)
	assert.Equal(t, "application/vnd.oasis.opendocument.text", m.String())
}

func TestParseMimeType_AcceptsZIP(t *testing.T) {
	m, err := vo.ParseMimeType("application/zip")
	require.NoError(t, err)
	assert.Equal(t, "application/zip", m.String())
}
```

(Use whatever import alias the existing file uses for `valueobjects`.)

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/sourcing/domain/valueobjects/... -run "TestParseMimeType_AcceptsODT|TestParseMimeType_AcceptsZIP" -v
```

Expected: FAIL with `ErrUnsupportedMime`.

- [ ] **Step 3: Update `mime_type.go`**

In `internal/sourcing/domain/valueobjects/mime_type.go`, extend the `Allowed` map:

```go
var Allowed = map[string]struct{}{
	"application/pdf": {},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": {},
	"application/msword": {},
	"application/vnd.oasis.opendocument.text": {},
	"application/zip": {},
}
```

Update the type doc comment to read `// MimeType is an accepted resume MIME type (PDF, DOC, DOCX, ODT, or ZIP).`

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/sourcing/domain/valueobjects/... -count=1 -race
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sourcing/domain/valueobjects/mime_type.go internal/sourcing/domain/valueobjects/mime_type_test.go
git commit -m "feat(sourcing): MimeType allowlist includes ODT and ZIP"
```

---

## Task 3: ZIP extractor utility with anti-zip-bomb rails

**Files:**
- Create: `internal/sourcing/infrastructure/text/zip_extractor.go`
- Create: `internal/sourcing/infrastructure/text/zip_extractor_test.go`

- [ ] **Step 1: Write the extractor module**

Create `internal/sourcing/infrastructure/text/zip_extractor.go`:

```go
// Package text holds text-extraction helpers used by the upload pipeline.
// zip_extractor.go fans a multipart-uploaded ZIP file into per-entry byte
// blobs that the upload command can route through the dedup-and-persist flow,
// one entry at a time. Anti-zip-bomb rails are enforced here (entry count,
// uncompressed total size, per-entry size, nested-ZIP, path-traversal,
// encryption).
package text

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gabriel-vasile/mimetype"
)

// ZipLimits caps what ExtractZip will accept. Zero / negative values fall
// back to the defaults from DefaultZipLimits.
type ZipLimits struct {
	MaxEntries           int
	MaxUncompressedBytes int64
	MaxEntrySizeBytes    int64
}

// DefaultZipLimits matches the spec's anti-abuse rails.
var DefaultZipLimits = ZipLimits{
	MaxEntries:           100,
	MaxUncompressedBytes: 50 * 1024 * 1024, // 50 MiB
	MaxEntrySizeBytes:    10 * 1024 * 1024, // 10 MiB
}

// ExtractedEntry is one file pulled out of a ZIP.
type ExtractedEntry struct {
	Filename string
	Bytes    []byte
}

// Error sentinels returned by ExtractZip. Upload command maps these to
// specific outcome error codes for the HTTP response.
var (
	ErrZipEncrypted            = errors.New("zip: encrypted entries not supported")
	ErrZipNested               = errors.New("zip: nested zips not supported")
	ErrZipPathTraversal        = errors.New("zip: path traversal not allowed")
	ErrZipTooManyEntries       = errors.New("zip: too many entries")
	ErrZipUncompressedTooLarge = errors.New("zip: uncompressed total too large")
	ErrZipEntryTooLarge        = errors.New("zip: entry too large")
)

// ExtractZip pulls entries out of body. Directories are skipped silently.
// Returns the first error encountered; partial extracts are not surfaced.
func ExtractZip(body []byte, limits ZipLimits) ([]ExtractedEntry, error) {
	if limits.MaxEntries <= 0 {
		limits.MaxEntries = DefaultZipLimits.MaxEntries
	}
	if limits.MaxUncompressedBytes <= 0 {
		limits.MaxUncompressedBytes = DefaultZipLimits.MaxUncompressedBytes
	}
	if limits.MaxEntrySizeBytes <= 0 {
		limits.MaxEntrySizeBytes = DefaultZipLimits.MaxEntrySizeBytes
	}

	r, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("zip: read: %w", err)
	}

	// Filter to non-directory entries first so we can count against MaxEntries
	// using the same number the recruiter expects to see.
	type pending struct{ entry *zip.File }
	var queue []pending
	for _, e := range r.File {
		if e.FileInfo().IsDir() {
			continue
		}
		queue = append(queue, pending{entry: e})
	}
	if len(queue) > limits.MaxEntries {
		return nil, fmt.Errorf("%w: %d > %d", ErrZipTooManyEntries, len(queue), limits.MaxEntries)
	}

	out := make([]ExtractedEntry, 0, len(queue))
	var totalBytes int64
	for _, p := range queue {
		name := p.entry.Name

		// Path traversal: reject "..", absolute paths, leading slash.
		if strings.Contains(name, "..") || strings.HasPrefix(name, "/") || name == "" {
			return nil, fmt.Errorf("%w: %q", ErrZipPathTraversal, name)
		}
		// Encryption: archive/zip exposes flag bit 0 as IsEncrypted.
		if p.entry.IsEncrypted() {
			return nil, fmt.Errorf("%w: %q", ErrZipEncrypted, name)
		}
		// Per-entry size: trust the declared size only as a sanity gate;
		// the running total below is the real bomb-resistance.
		if int64(p.entry.UncompressedSize64) > limits.MaxEntrySizeBytes {
			return nil, fmt.Errorf("%w: %q (%d > %d)", ErrZipEntryTooLarge, name, p.entry.UncompressedSize64, limits.MaxEntrySizeBytes)
		}

		rc, err := p.entry.Open()
		if err != nil {
			return nil, fmt.Errorf("zip: open entry %q: %w", name, err)
		}

		// LimitReader caps the read so a lying header can't blow past
		// MaxEntrySizeBytes during stream extraction.
		buf := make([]byte, 0, p.entry.UncompressedSize64)
		w := bytes.NewBuffer(buf)
		n, err := io.Copy(w, io.LimitReader(rc, limits.MaxEntrySizeBytes+1))
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("zip: read entry %q: %w", name, err)
		}
		if n > limits.MaxEntrySizeBytes {
			return nil, fmt.Errorf("%w: %q (stream exceeded %d)", ErrZipEntryTooLarge, name, limits.MaxEntrySizeBytes)
		}
		totalBytes += n
		if totalBytes > limits.MaxUncompressedBytes {
			return nil, fmt.Errorf("%w: %d > %d", ErrZipUncompressedTooLarge, totalBytes, limits.MaxUncompressedBytes)
		}

		// Reject nested ZIPs by content sniff (extension lies).
		entryBytes := w.Bytes()
		if mt := mimetype.Detect(entryBytes); mt.Is("application/zip") {
			return nil, fmt.Errorf("%w: %q", ErrZipNested, name)
		}

		out = append(out, ExtractedEntry{Filename: name, Bytes: entryBytes})
	}
	return out, nil
}
```

- [ ] **Step 2: Write the test file**

Create `internal/sourcing/infrastructure/text/zip_extractor_test.go`:

```go
package text_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/infrastructure/text"
)

// buildZip writes a deterministic test zip. Each entry pair is (name, content).
// content == nil creates a directory entry.
func buildZip(t *testing.T, entries [][2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		name, content := e[0], e[1]
		if content == "" && strings.HasSuffix(name, "/") {
			_, err := w.Create(name)
			require.NoError(t, err)
			continue
		}
		f, err := w.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func pdfBytes(unique string) string {
	// Minimal but valid-enough PDF header so mimetype.Detect returns application/pdf.
	return "%PDF-1.4\n%" + unique + "\n%%EOF\n"
}

func TestExtractZip_HappyPath(t *testing.T) {
	z := buildZip(t, [][2]string{
		{"alice.pdf", pdfBytes("alice")},
		{"bharat.pdf", pdfBytes("bharat")},
		{"chitra.pdf", pdfBytes("chitra")},
	})

	out, err := text.ExtractZip(z, text.DefaultZipLimits)
	require.NoError(t, err)
	require.Len(t, out, 3)
	assert.Equal(t, "alice.pdf", out[0].Filename)
	assert.Contains(t, string(out[0].Bytes), "%PDF-1.4")
}

func TestExtractZip_SkipsDirectoryEntries(t *testing.T) {
	z := buildZip(t, [][2]string{
		{"folder/", ""}, // directory entry
		{"folder/alice.pdf", pdfBytes("alice")},
	})

	out, err := text.ExtractZip(z, text.DefaultZipLimits)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "folder/alice.pdf", out[0].Filename)
}

func TestExtractZip_RejectsPathTraversal(t *testing.T) {
	z := buildZip(t, [][2]string{
		{"../etc/passwd", "evil"},
	})

	_, err := text.ExtractZip(z, text.DefaultZipLimits)
	require.Error(t, err)
	assert.True(t, errors.Is(err, text.ErrZipPathTraversal))
}

func TestExtractZip_RejectsAbsolutePaths(t *testing.T) {
	z := buildZip(t, [][2]string{
		{"/etc/passwd", "evil"},
	})

	_, err := text.ExtractZip(z, text.DefaultZipLimits)
	require.Error(t, err)
	assert.True(t, errors.Is(err, text.ErrZipPathTraversal))
}

func TestExtractZip_RejectsTooManyEntries(t *testing.T) {
	entries := make([][2]string, 0, 101)
	for i := 0; i < 101; i++ {
		entries = append(entries, [2]string{
			"r" + string(rune('a'+i%26)) + ".pdf",
			pdfBytes(string(rune('a' + i%26))),
		})
	}
	// dedupe filenames so the zip writer doesn't choke
	for i := range entries {
		entries[i][0] = "f" + strings.Repeat("a", i) + ".pdf"
	}
	z := buildZip(t, entries)

	_, err := text.ExtractZip(z, text.ZipLimits{MaxEntries: 100})
	require.Error(t, err)
	assert.True(t, errors.Is(err, text.ErrZipTooManyEntries))
}

func TestExtractZip_RejectsNestedZip(t *testing.T) {
	inner := buildZip(t, [][2]string{{"x.pdf", pdfBytes("x")}})
	outer := buildZip(t, [][2]string{{"nested.zip", string(inner)}})

	_, err := text.ExtractZip(outer, text.DefaultZipLimits)
	require.Error(t, err)
	assert.True(t, errors.Is(err, text.ErrZipNested))
}

func TestExtractZip_RejectsEntryTooLarge(t *testing.T) {
	big := strings.Repeat("A", 11*1024*1024) // 11 MiB
	z := buildZip(t, [][2]string{{"big.pdf", "%PDF-1.4\n" + big}})

	_, err := text.ExtractZip(z, text.ZipLimits{MaxEntrySizeBytes: 10 * 1024 * 1024})
	require.Error(t, err)
	assert.True(t, errors.Is(err, text.ErrZipEntryTooLarge))
}

func TestExtractZip_RejectsUncompressedTotalTooLarge(t *testing.T) {
	chunk := strings.Repeat("A", 4*1024*1024) // 4 MiB
	// 3 entries × 4 MiB = 12 MiB total; limit 10 MiB → rejected.
	z := buildZip(t, [][2]string{
		{"a.pdf", "%PDF-1.4\n" + chunk},
		{"b.pdf", "%PDF-1.4\n" + chunk},
		{"c.pdf", "%PDF-1.4\n" + chunk},
	})

	_, err := text.ExtractZip(z, text.ZipLimits{
		MaxEntries:           10,
		MaxUncompressedBytes: 10 * 1024 * 1024,
		MaxEntrySizeBytes:    5 * 1024 * 1024,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, text.ErrZipUncompressedTooLarge))
}
```

- [ ] **Step 3: Run the tests**

```bash
go test ./internal/sourcing/infrastructure/text/... -count=1 -race -v
```

Expected: all 8 tests PASS.

- [ ] **Step 4: Verify build + vet**

```bash
make build
go vet ./...
```

Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/sourcing/infrastructure/text/zip_extractor.go internal/sourcing/infrastructure/text/zip_extractor_test.go
git commit -m "feat(sourcing): zip extractor with anti-zip-bomb rails"
```

---

## Task 4: Migration — re-key `resume_uploads_dedup` to per-intent

**Files:**
- Create: `migrations/sourcing/000010_dedup_per_intent.up.sql`
- Create: `migrations/sourcing/000010_dedup_per_intent.down.sql`

- [ ] **Step 1: Inspect the current dedup table schema**

```bash
docker exec hireflow-postgres psql -U hireflow -d hireflow -c '\d resume_uploads_dedup'
```

Note the existing UNIQUE constraint's exact name (probably `resume_uploads_dedup_tenant_id_content_hash_key` — confirm).

- [ ] **Step 2: Write `up.sql`**

Create `migrations/sourcing/000010_dedup_per_intent.up.sql`:

```sql
BEGIN;

-- Add intent_id column, nullable while we backfill.
ALTER TABLE resume_uploads_dedup
    ADD COLUMN intent_id UUID;

-- Backfill from resume_uploads. Each dedup row maps to the intent of the
-- ORIGINAL upload that won the dedup; that's the only intent_id the row
-- ever knew about. Once backfilled, the column becomes NOT NULL.
UPDATE resume_uploads_dedup d
SET intent_id = (
    SELECT u.intent_id
    FROM resume_uploads u
    WHERE u.id = d.upload_id
);

-- Defensive: drop any orphan rows whose upload row no longer exists.
DELETE FROM resume_uploads_dedup WHERE intent_id IS NULL;

ALTER TABLE resume_uploads_dedup
    ALTER COLUMN intent_id SET NOT NULL;

-- Drop the old global-per-tenant unique constraint. Replace with per-intent.
ALTER TABLE resume_uploads_dedup
    DROP CONSTRAINT resume_uploads_dedup_tenant_id_content_hash_key;

ALTER TABLE resume_uploads_dedup
    ADD CONSTRAINT resume_uploads_dedup_tenant_intent_hash_key
    UNIQUE (tenant_id, intent_id, content_hash);

COMMIT;
```

If `\d` in Step 1 showed a different constraint name, substitute it in the `DROP CONSTRAINT` line.

- [ ] **Step 3: Write `down.sql`**

Create `migrations/sourcing/000010_dedup_per_intent.down.sql`:

```sql
BEGIN;

-- WARNING: Reversing after new cross-intent uploads have happened will
-- violate the old uniqueness. We DELETE the offending rows (keeping the
-- earliest upload per (tenant, hash)) before re-adding the constraint.
DELETE FROM resume_uploads_dedup
WHERE id IN (
    SELECT id FROM (
        SELECT id, ROW_NUMBER() OVER (
            PARTITION BY tenant_id, content_hash
            ORDER BY created_at
        ) AS rn
        FROM resume_uploads_dedup
    ) ranked
    WHERE rn > 1
);

ALTER TABLE resume_uploads_dedup
    DROP CONSTRAINT resume_uploads_dedup_tenant_intent_hash_key;

ALTER TABLE resume_uploads_dedup
    ADD CONSTRAINT resume_uploads_dedup_tenant_id_content_hash_key
    UNIQUE (tenant_id, content_hash);

ALTER TABLE resume_uploads_dedup
    DROP COLUMN intent_id;

COMMIT;
```

- [ ] **Step 4: Apply and verify**

```bash
export DATABASE_URL="postgres://hireflow:hireflow@localhost:5433/hireflow?sslmode=disable"
make migrate-up
docker exec hireflow-postgres psql -U hireflow -d hireflow -c '\d resume_uploads_dedup'
```

Expected: table now has `intent_id UUID NOT NULL` and the new `resume_uploads_dedup_tenant_intent_hash_key` UNIQUE constraint.

- [ ] **Step 5: Verify the build still passes**

```bash
make build
```

Expected: clean (no Go code change yet — the repo still uses the old key shape until Task 5).

- [ ] **Step 6: Commit**

```bash
git add migrations/sourcing/000010_dedup_per_intent.up.sql migrations/sourcing/000010_dedup_per_intent.down.sql
git commit -m "feat(sourcing): migration 000010 — re-key resume_uploads_dedup per intent"
```

---

## Task 5: `FindByContentHashAndIntent` repo method + Postgres impl

**Files:**
- Modify: `internal/sourcing/domain/repositories/resume_upload_repository.go`
- Modify: `internal/sourcing/infrastructure/persistence/postgres_resume_upload_repository.go`
- Modify: `internal/sourcing/infrastructure/persistence/postgres_resume_upload_repository_test.go`

- [ ] **Step 1: Add the new port method**

In `internal/sourcing/domain/repositories/resume_upload_repository.go`, add to the `ResumeUploadRepository` interface (keeping `FindByContentHash` for non-upload callers):

```go
// FindByContentHashAndIntent looks up an existing upload by content hash
// scoped to a specific intent. Used by the upload command to detect
// same-intent duplicates. Returns ErrNotFound if no row matches.
FindByContentHashAndIntent(ctx context.Context, tenant shared.TenantID, intentID uuid.UUID, hash string) (*entities.ResumeUpload, error)
```

- [ ] **Step 2: Write the failing integration test**

In `internal/sourcing/infrastructure/persistence/postgres_resume_upload_repository_test.go`, append:

```go
func TestFindByContentHashAndIntent_HitForSameIntent(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresResumeUploadRepository(pool)
	tenant := shared.NewTenantID()
	intentID := uuid.New()
	upload := newUploadForIntent(t, tenant, intentID)
	require.NoError(t, repo.Save(context.Background(), upload))

	got, err := repo.FindByContentHashAndIntent(
		context.Background(), tenant, intentID, upload.ContentHash().String(),
	)
	require.NoError(t, err)
	assert.Equal(t, upload.ID(), got.ID())
}

func TestFindByContentHashAndIntent_MissForDifferentIntent(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresResumeUploadRepository(pool)
	tenant := shared.NewTenantID()
	intentA, intentB := uuid.New(), uuid.New()
	upload := newUploadForIntent(t, tenant, intentA)
	require.NoError(t, repo.Save(context.Background(), upload))

	_, err := repo.FindByContentHashAndIntent(
		context.Background(), tenant, intentB, upload.ContentHash().String(),
	)
	require.ErrorIs(t, err, repositories.ErrNotFound)
}

func TestFindByContentHashAndIntent_MissForDifferentTenant(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresResumeUploadRepository(pool)
	tenantA, tenantB := shared.NewTenantID(), shared.NewTenantID()
	intentID := uuid.New()
	upload := newUploadForIntent(t, tenantA, intentID)
	require.NoError(t, repo.Save(context.Background(), upload))

	_, err := repo.FindByContentHashAndIntent(
		context.Background(), tenantB, intentID, upload.ContentHash().String(),
	)
	require.ErrorIs(t, err, repositories.ErrNotFound)
}
```

If `newUploadForIntent` doesn't exist in the test file, add a helper that mirrors the existing `newUpload` (one is probably already defined for slice 1 — `grep -n "func newUpload" internal/sourcing/infrastructure/persistence/postgres_resume_upload_repository_test.go`). Helper signature:

```go
func newUploadForIntent(t *testing.T, tenant shared.TenantID, intentID uuid.UUID) *entities.ResumeUpload {
	t.Helper()
	h, err := vo.NewContentHash(uuidHex(t))
	require.NoError(t, err)
	mime, err := vo.ParseMimeType("application/pdf")
	require.NoError(t, err)
	u, err := entities.NewResumeUpload(entities.UploadInput{
		TenantID:     tenant,
		IntentID:     intentID,
		BatchID:      uuid.New(),
		StorageKey:   "k/" + uuid.New().String(),
		OriginalName: "alice.pdf",
		MimeType:     mime,
		SizeBytes:    1000,
		ContentHash:  h,
	})
	require.NoError(t, err)
	return u
}
```

- [ ] **Step 3: Run tests — expected to fail (method not implemented)**

```bash
export DATABASE_URL="postgres://hireflow:hireflow@localhost:5433/hireflow?sslmode=disable"
go test -tags=integration ./internal/sourcing/infrastructure/persistence/... -run TestFindByContentHashAndIntent -v
```

Expected: compile error — method missing on `PostgresResumeUploadRepository`.

- [ ] **Step 4: Implement the Postgres method**

In `internal/sourcing/infrastructure/persistence/postgres_resume_upload_repository.go`, add a method on `PostgresResumeUploadRepository`:

```go
// FindByContentHashAndIntent — tenant + intent + content_hash lookup.
// Used by the upload command for per-intent dedup detection.
func (r *PostgresResumeUploadRepository) FindByContentHashAndIntent(
	ctx context.Context, tenant shared.TenantID, intentID uuid.UUID, hash string,
) (*entities.ResumeUpload, error) {
	row := r.pool.QueryRow(ctx,
		selectSQL+" WHERE tenant_id=$1 AND intent_id=$2 AND content_hash=$3",
		tenant.String(), intentID, hash,
	)
	return scanRow(row)
}
```

`scanRow` already maps `pgx.ErrNoRows` to `repositories.ErrNotFound` — see the existing `FindByID` for the pattern.

- [ ] **Step 5: Verify tests pass**

```bash
go test -tags=integration ./internal/sourcing/infrastructure/persistence/... -count=1 -race -v
```

Expected: PASS (incl. the 3 new tests + all existing ones).

- [ ] **Step 6: Update any test fakes that implement the port**

```bash
grep -rln "FindByContentHash\b" --include="*_test.go" | head -10
```

For each fake `ResumeUploadRepository` (in tests outside the persistence package), add a `FindByContentHashAndIntent` method. Most can return `(nil, repositories.ErrNotFound)` since the tests don't exercise this path; tests that DO care will be updated in Task 6.

Likely fakes:
- `internal/sourcing/application/queries/get_batch_status_test.go` (`fakeListRepo`)
- `internal/sourcing/application/commands/retry_resume_upload_test.go` (`retryUploadRepo`)
- `internal/sourcing/application/commands/upload_resume_batch_test.go` (`fakeRepo`)
- `internal/sourcing/delivery/http/v1/handlers_test.go` (`memRepo`, `retryRepo`)
- `internal/sourcing/infrastructure/worker/pool_test.go` (`oneShotRepo`)

For each, add:

```go
func (r *<FakeName>) FindByContentHashAndIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID, _ string) (*entities.ResumeUpload, error) {
	return nil, repositories.ErrNotFound
}
```

- [ ] **Step 7: Verify the full suite still compiles + passes**

```bash
go test ./... -count=1 -race 2>&1 | tail -5
make build
```

Expected: green.

- [ ] **Step 8: Commit**

```bash
git add internal/sourcing/domain/repositories/resume_upload_repository.go \
        internal/sourcing/infrastructure/persistence/postgres_resume_upload_repository.go \
        internal/sourcing/infrastructure/persistence/postgres_resume_upload_repository_test.go \
        internal/sourcing/application/queries/get_batch_status_test.go \
        internal/sourcing/application/commands/retry_resume_upload_test.go \
        internal/sourcing/application/commands/upload_resume_batch_test.go \
        internal/sourcing/delivery/http/v1/handlers_test.go \
        internal/sourcing/infrastructure/worker/pool_test.go
git commit -m "feat(sourcing): FindByContentHashAndIntent repo method + per-intent integration tests"
```

---

## Task 6: Upload command — per-intent dedup + ZIP fan-out + new statuses

**Files:**
- Modify: `internal/sourcing/application/dto/batch_upload.go`
- Modify: `internal/sourcing/application/commands/upload_resume_batch.go`
- Modify: `internal/sourcing/application/commands/upload_resume_batch_test.go`

- [ ] **Step 1: Extend the DTO**

In `internal/sourcing/application/dto/batch_upload.go`, extend `ItemOutcome`:

```go
// ItemOutcome is one row in the batch-upload response. Status values:
//   "queued"               — accepted; the worker will process it.
//   "deduplicated"         — DEPRECATED in slice 5; kept for old callers. Use duplicate_in_intent.
//   "duplicate_in_intent"  — content already uploaded to this intent; skipped.
//   "extracted_from_zip"   — parent marker for a ZIP file; child entries follow.
//   ""                     — rejection (see Error).
type ItemOutcome struct {
	Filename       string                 `json:"filename"`
	UploadID       *uuid.UUID             `json:"upload_id,omitempty"`
	Status         string                 `json:"status,omitempty"`
	CandidateID    *uuid.UUID             `json:"candidate_id,omitempty"`
	Error          *ItemError             `json:"error,omitempty"`
	ParentFilename *string                `json:"parent_filename,omitempty"`
	ParentItemID   *string                `json:"parent_item_id,omitempty"`
}
```

- [ ] **Step 2: Write failing tests for the new outcome flows**

In `internal/sourcing/application/commands/upload_resume_batch_test.go`, append (using your test scaffolding patterns):

```go
func TestUploadBatch_ZipFanOut_QueuesEachEntry(t *testing.T) {
	ctx := context.Background()
	tenant := shared.NewTenantID()
	intentID := uuid.New()

	zipBody := buildTestZip(t, []zipFile{
		{Name: "alice.pdf", Body: pdfBytes("alice")},
		{Name: "bharat.pdf", Body: pdfBytes("bharat")},
	})

	h := newUploadHandlerWithStubRepo(t)
	out, err := h.Handle(ctx, dto.BatchUploadInput{
		TenantID: tenant,
		IntentID: intentID,
		Source: oneItemSource(t, "batch.zip", zipBody),
	})
	require.NoError(t, err)
	require.Len(t, out.Items, 3) // parent + 2 children
	assert.Equal(t, "extracted_from_zip", out.Items[0].Status)
	assert.Equal(t, "queued", out.Items[1].Status)
	assert.Equal(t, "alice.pdf", out.Items[1].Filename)
	require.NotNil(t, out.Items[1].ParentFilename)
	assert.Equal(t, "batch.zip", *out.Items[1].ParentFilename)
	assert.Equal(t, "queued", out.Items[2].Status)
}

func TestUploadBatch_DuplicateInIntent(t *testing.T) {
	ctx := context.Background()
	tenant := shared.NewTenantID()
	intentID := uuid.New()

	// Repo pre-seeded with alice.pdf already uploaded to this intent.
	h := newUploadHandlerWithRepoSeeded(t, tenant, intentID, "alice.pdf", pdfBytes("alice"))

	out, err := h.Handle(ctx, dto.BatchUploadInput{
		TenantID: tenant,
		IntentID: intentID,
		Source: oneItemSource(t, "alice.pdf", pdfBytes("alice")),
	})
	require.NoError(t, err)
	require.Len(t, out.Items, 1)
	assert.Equal(t, "duplicate_in_intent", out.Items[0].Status)
}

func TestUploadBatch_SameHashDifferentIntent_Queued(t *testing.T) {
	ctx := context.Background()
	tenant := shared.NewTenantID()
	intentA, intentB := uuid.New(), uuid.New()

	h := newUploadHandlerWithRepoSeeded(t, tenant, intentA, "alice.pdf", pdfBytes("alice"))

	// Upload the same bytes for a different intent — should NOT dedup.
	out, err := h.Handle(ctx, dto.BatchUploadInput{
		TenantID: tenant,
		IntentID: intentB,
		Source: oneItemSource(t, "alice.pdf", pdfBytes("alice")),
	})
	require.NoError(t, err)
	require.Len(t, out.Items, 1)
	assert.Equal(t, "queued", out.Items[0].Status)
}

func TestUploadBatch_ZipWithDuplicate_PartialOutcomes(t *testing.T) {
	ctx := context.Background()
	tenant := shared.NewTenantID()
	intentID := uuid.New()

	// alice.pdf is pre-seeded for this intent.
	h := newUploadHandlerWithRepoSeeded(t, tenant, intentID, "alice.pdf", pdfBytes("alice"))

	zipBody := buildTestZip(t, []zipFile{
		{Name: "alice.pdf", Body: pdfBytes("alice")},   // duplicate
		{Name: "bharat.pdf", Body: pdfBytes("bharat")}, // new
	})

	out, err := h.Handle(ctx, dto.BatchUploadInput{
		TenantID: tenant,
		IntentID: intentID,
		Source: oneItemSource(t, "batch.zip", zipBody),
	})
	require.NoError(t, err)
	require.Len(t, out.Items, 3)
	assert.Equal(t, "extracted_from_zip", out.Items[0].Status)
	assert.Equal(t, "duplicate_in_intent", out.Items[1].Status)
	assert.Equal(t, "queued", out.Items[2].Status)
}

func TestUploadBatch_ZipRejection_NoChildren(t *testing.T) {
	ctx := context.Background()
	tenant := shared.NewTenantID()
	intentID := uuid.New()

	// Build a ZIP with > 100 entries to trigger ErrZipTooManyEntries.
	entries := make([]zipFile, 0, 101)
	for i := 0; i < 101; i++ {
		entries = append(entries, zipFile{
			Name: fmt.Sprintf("f%03d.pdf", i),
			Body: pdfBytes(fmt.Sprintf("f%03d", i)),
		})
	}
	zipBody := buildTestZip(t, entries)

	h := newUploadHandlerWithStubRepo(t)
	out, err := h.Handle(ctx, dto.BatchUploadInput{
		TenantID: tenant,
		IntentID: intentID,
		Source: oneItemSource(t, "huge.zip", zipBody),
	})
	require.NoError(t, err)
	require.Len(t, out.Items, 1)
	assert.Equal(t, "huge.zip", out.Items[0].Filename)
	require.NotNil(t, out.Items[0].Error)
	assert.Equal(t, "zip_too_many_entries", out.Items[0].Error.Code)
}
```

Test helpers (`buildTestZip`, `pdfBytes`, `oneItemSource`, `newUploadHandlerWithStubRepo`, `newUploadHandlerWithRepoSeeded`) need to be in the same test file (or shared with the existing test file). Patterns:

```go
type zipFile struct{ Name, Body string }

func buildTestZip(t *testing.T, entries []zipFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		f, err := w.Create(e.Name)
		require.NoError(t, err)
		_, err = f.Write([]byte(e.Body))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func pdfBytes(unique string) string {
	return "%PDF-1.4\n%" + unique + "\n%%EOF\n"
}

func oneItemSource(t *testing.T, filename string, body string) dto.BatchItemSource {
	t.Helper()
	return &singleItemSource{filename: filename, body: []byte(body)}
}

type singleItemSource struct {
	filename string
	body     []byte
	served   bool
}

func (s *singleItemSource) Next() (dto.BatchItem, error) {
	if s.served {
		return dto.BatchItem{}, io.EOF
	}
	s.served = true
	return dto.BatchItem{
		Filename: s.filename,
		Body:     bytes.NewReader(s.body),
		Size:     int64(len(s.body)),
	}, nil
}
```

`newUploadHandlerWithStubRepo` / `newUploadHandlerWithRepoSeeded` use the existing in-memory `fakeRepo` from this test file. Extend `fakeRepo` to support `FindByContentHashAndIntent` properly (return a stored upload when the (intent, hash) matches).

- [ ] **Step 3: Run the new tests — expected to fail**

```bash
go test ./internal/sourcing/application/commands/... -run TestUploadBatch -v
```

Expected: tests compile (after helper additions) but fail because the handler doesn't yet implement ZIP fan-out or per-intent dedup.

- [ ] **Step 4: Rewrite `upload_resume_batch.go`**

In `internal/sourcing/application/commands/upload_resume_batch.go`:

1. Add the import: `"github.com/hustle/hireflow/internal/sourcing/infrastructure/text"`.
2. Add the import: `"github.com/google/uuid"` (probably already there).
3. Restructure `Handle` so the per-item logic is in a separate `processOneFile` method (refactor — extract the body that runs per item into a function returning a single `ItemOutcome`).
4. In the outer loop, sniff the MIME of each top-level item. If `application/zip`, call `text.ExtractZip` and fan out:

```go
// Inside Handle, replacing the per-item processing:
for {
	item, err := in.Source.Next()
	if errors.Is(err, io.EOF) {
		break
	}
	if err != nil {
		return dto.BatchUploadResponse{}, fmt.Errorf("source: %w", err)
	}

	body, err := io.ReadAll(io.LimitReader(item.Body, h.cfg.MaxFileBytes+1))
	if err != nil {
		out.Items = append(out.Items, rejected(item.Filename, "read_failed", err.Error(), nil))
		continue
	}
	if int64(len(body)) > h.cfg.MaxFileBytes {
		out.Items = append(out.Items, rejected(item.Filename, "too_large", fmt.Sprintf("file exceeds %d bytes", h.cfg.MaxFileBytes), nil))
		continue
	}

	sniff, err := vo.SniffMimeType(body)
	if err != nil {
		out.Items = append(out.Items, rejected(item.Filename, "unsupported_format", err.Error(), nil))
		continue
	}

	if sniff.String() == "application/zip" {
		out.Items = append(out.Items, h.processZip(ctx, tenant, intentID, batchID, item.Filename, body)...)
		continue
	}

	out.Items = append(out.Items, h.processOneFile(ctx, tenant, intentID, batchID, item.Filename, body, sniff, nil, nil))
}
```

Add the `processZip` method:

```go
func (h *UploadResumeBatchHandler) processZip(
	ctx context.Context,
	tenant shared.TenantID,
	intentID uuid.UUID,
	batchID uuid.UUID,
	filename string,
	body []byte,
) []dto.ItemOutcome {
	entries, err := text.ExtractZip(body, text.DefaultZipLimits)
	if err != nil {
		code := mapZipError(err)
		return []dto.ItemOutcome{
			rejected(filename, code, err.Error(), nil),
		}
	}
	parentID := uuid.New().String()
	out := []dto.ItemOutcome{{
		Filename:     filename,
		Status:       "extracted_from_zip",
		ParentItemID: &parentID,
	}}
	for _, entry := range entries {
		sniff, sniffErr := vo.SniffMimeType(entry.Bytes)
		var child dto.ItemOutcome
		if sniffErr != nil {
			child = rejected(entry.Filename, "unsupported_format", sniffErr.Error(), nil)
		} else {
			child = h.processOneFile(ctx, tenant, intentID, batchID, entry.Filename, entry.Bytes, sniff, &filename, &parentID)
		}
		child.ParentFilename = &filename
		child.ParentItemID = &parentID
		out = append(out, child)
	}
	return out
}

func mapZipError(err error) string {
	switch {
	case errors.Is(err, text.ErrZipEncrypted):
		return "zip_encrypted"
	case errors.Is(err, text.ErrZipNested):
		return "zip_nested"
	case errors.Is(err, text.ErrZipPathTraversal):
		return "zip_path_traversal"
	case errors.Is(err, text.ErrZipTooManyEntries):
		return "zip_too_many_entries"
	case errors.Is(err, text.ErrZipUncompressedTooLarge):
		return "zip_uncompressed_too_large"
	case errors.Is(err, text.ErrZipEntryTooLarge):
		return "zip_entry_too_large"
	default:
		return "zip_extraction_failed"
	}
}
```

Add `processOneFile`:

```go
func (h *UploadResumeBatchHandler) processOneFile(
	ctx context.Context,
	tenant shared.TenantID,
	intentID uuid.UUID,
	batchID uuid.UUID,
	filename string,
	body []byte,
	mime vo.MimeType,
	parentFilename *string,
	parentItemID *string,
) dto.ItemOutcome {
	hash := vo.ComputeContentHash(body)
	hashStr := hash.String()

	// Per-intent dedup.
	if existing, err := h.repo.FindByContentHashAndIntent(ctx, tenant, intentID, hashStr); err == nil {
		uid := existing.ID()
		return dto.ItemOutcome{
			Filename:       filename,
			UploadID:       &uid,
			Status:         "duplicate_in_intent",
			ParentFilename: parentFilename,
			ParentItemID:   parentItemID,
		}
	} else if !errors.Is(err, repositories.ErrNotFound) {
		return rejected(filename, "lookup_failed", err.Error(), nil)
	}

	// Persist bytes to storage keyed by hash.
	key := hashStr[:2] + "/" + hashStr[2:4] + "/" + hashStr
	if err := h.storage.Put(ctx, key, bytes.NewReader(body)); err != nil {
		return rejected(filename, "storage_write_failed", err.Error(), nil)
	}

	upload, err := entities.NewResumeUpload(entities.UploadInput{
		TenantID:     tenant,
		IntentID:     intentID,
		BatchID:      batchID,
		StorageKey:   key,
		OriginalName: filename,
		MimeType:     mime,
		SizeBytes:    int64(len(body)),
		ContentHash:  hash,
	})
	if err != nil {
		return rejected(filename, "construct_failed", err.Error(), nil)
	}

	if err := h.repo.Save(ctx, upload); err != nil {
		if errors.Is(err, repositories.ErrDuplicate) {
			// Race: someone else inserted between FindBy... and Save.
			if dup, derr := h.repo.FindByContentHashAndIntent(ctx, tenant, intentID, hashStr); derr == nil {
				uid := dup.ID()
				return dto.ItemOutcome{
					Filename:       filename,
					UploadID:       &uid,
					Status:         "duplicate_in_intent",
					ParentFilename: parentFilename,
					ParentItemID:   parentItemID,
				}
			}
		}
		return rejected(filename, "persist_failed", err.Error(), nil)
	}

	uid := upload.ID()
	return dto.ItemOutcome{
		Filename: filename,
		UploadID: &uid,
		Status:   "queued",
	}
}
```

The old inline body in `Handle` can now be deleted — `processOneFile` covers the path. Remove the unused old `FindByContentHash` call inside this command (the method stays on the repo for non-upload callers).

- [ ] **Step 5: Run unit tests**

```bash
go test ./internal/sourcing/application/commands/... -count=1 -race
```

Expected: PASS (incl. the 5 new test cases).

- [ ] **Step 6: Run vet + build**

```bash
go vet ./...
make build
```

Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add internal/sourcing/application/commands/upload_resume_batch.go \
        internal/sourcing/application/commands/upload_resume_batch_test.go \
        internal/sourcing/application/dto/batch_upload.go
git commit -m "feat(sourcing): UploadResumeBatch — ZIP fan-out + per-intent dedup + new statuses"
```

---

## Task 7: Enrich `ListApplications` response with `top_skills` + `judge_summary`

**Files:**
- Modify: `internal/sourcing/application/queries/list_applications.go`
- Modify: `internal/sourcing/application/queries/list_applications_test.go`
- Modify: `internal/sourcing/application/dto/list_applications.go` (or wherever the existing list DTO lives — grep first)
- Modify: `internal/sourcing/delivery/http/v1/dto.go` (HTTP response shape)
- Modify: `internal/sourcing/delivery/http/v1/handlers.go` (response mapping)

- [ ] **Step 1: Locate the existing DTO and handler**

```bash
grep -rn "type.*Application.*DTO\|ListApplicationsResponse\|type.*Application.*struct" internal/sourcing/application/ internal/sourcing/delivery/ | head -10
```

Note the exact file paths and current shape.

- [ ] **Step 2: Extend the application-layer DTO**

Find the existing DTO (e.g., `internal/sourcing/application/dto/list_applications.go`). Add two fields to the candidate shape carried inside each list item:

```go
type CandidateSummary struct {
	FullName      string         `json:"full_name"`
	Headline      string         `json:"headline"`
	Location      string         `json:"location"`
	TopSkills     []SkillSummary `json:"top_skills"`     // NEW
	JudgeSummary  string         `json:"judge_summary"`  // NEW — first sentence of llm_judgment.summary
}

type SkillSummary struct {
	Name  string  `json:"name"`
	Years float64 `json:"years,omitempty"`
}
```

If the existing shape is flat (no nested `CandidateSummary`), add the same two fields directly on `ApplicationDTO`. Don't break the field naming the HTTP layer already uses.

- [ ] **Step 3: Write failing tests**

In `internal/sourcing/application/queries/list_applications_test.go`, append:

```go
func TestListApplications_PopulatesTopSkillsFromParsedProfile(t *testing.T) {
	// Seed a candidate with parsed_profile containing 5 skills.
	// Build the query handler with the seeded repo.
	// Assert the first 3 skills appear in TopSkills with their years populated.
	// ...
}

func TestListApplications_PopulatesJudgeSummaryFirstSentence(t *testing.T) {
	// Seed an Application with llm_judgment.summary = "Strong match. Built X. Owns Y."
	// Assert CandidateSummary.JudgeSummary == "Strong match."
	// ...
}
```

The exact test scaffolding depends on the existing test helpers in that file — follow the pattern already there.

- [ ] **Step 4: Run tests — expected to fail**

```bash
go test ./internal/sourcing/application/queries/... -run TestListApplications -v
```

- [ ] **Step 5: Implement the enrichment**

In `list_applications.go`'s `Handle`, when building each `ApplicationDTO`:

```go
// Extract top 3 skills from candidate.parsed_profile.skills, ordered by years desc.
top := []dto.SkillSummary{}
if profile, err := candidate.DecodedProfile(); err == nil {
	skills := profile.Skills
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Years > skills[j].Years
	})
	for i, s := range skills {
		if i >= 3 {
			break
		}
		top = append(top, dto.SkillSummary{Name: s.Name, Years: s.Years})
	}
}

// First sentence of llm_judgment.summary
judgeSummary := ""
if app.LLMJudgment() != nil {
	full := app.LLMJudgment().Summary
	if idx := strings.Index(full, ". "); idx > 0 {
		judgeSummary = full[:idx+1]
	} else {
		judgeSummary = full
	}
}

result.Candidate.TopSkills = top
result.Candidate.JudgeSummary = judgeSummary
```

If `candidate.DecodedProfile()` doesn't exist, use the slice-2 pattern for unmarshaling `parsed_profile` JSONB — read `internal/sourcing/domain/entities/candidate.go` for the accessor.

- [ ] **Step 6: Update the HTTP DTO mirror**

In `internal/sourcing/delivery/http/v1/dto.go`, mirror the application-layer change (HTTP DTOs in this repo mirror application DTOs 1:1). Then in `handlers.go`, copy the fields into the HTTP response in the `ListApplications` handler.

- [ ] **Step 7: Run tests + build**

```bash
go test ./internal/sourcing/... -count=1 -race
go vet ./...
make build
```

Expected: green.

- [ ] **Step 8: Commit**

```bash
git add internal/sourcing/application/queries/ internal/sourcing/application/dto/ internal/sourcing/delivery/
git commit -m "feat(sourcing): list-applications response carries top_skills + judge_summary"
```

---

## Task 8: Slice-5 e2e — ZIP + per-intent dedup against live Postgres

**Files:**
- Create: `tests/sourcing_zip_upload_e2e_test.go`

- [ ] **Step 1: Create the e2e test**

Create `tests/sourcing_zip_upload_e2e_test.go` with `//go:build integration` tag. Reuse the helpers already defined in `sourcing_slice3_e2e_test.go` and `sourcing_slice4_e2e_test.go` (`newPgvectorPool`, `stubParser`, `stubOCR`, `stubJudge`, `writeMultipart`, `insertHiringIntentForSlice3`). Do NOT redeclare them.

```go
//go:build integration

package tests

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/shared/infrastructure/auth"
	sourcingcommands "github.com/hustle/hireflow/internal/sourcing/application/commands"
	sourcingqueries "github.com/hustle/hireflow/internal/sourcing/application/queries"
	v1 "github.com/hustle/hireflow/internal/sourcing/delivery/http/v1"
	sourcingstorage "github.com/hustle/hireflow/internal/sourcing/infrastructure/storage"
	auditinfra "github.com/hustle/hireflow/internal/shared/audit/infrastructure"
	sourcingpersist "github.com/hustle/hireflow/internal/sourcing/infrastructure/persistence"
	sourcingenc "github.com/hustle/hireflow/internal/sourcing/infrastructure/encryption"
)

func TestSourcingZipUploadE2E_QueuesEachEntryAcrossIntents(t *testing.T) {
	pool := newPgvectorPool(t)
	logger := zerolog.New(zerolog.Nop().NopCloser{}).Level(zerolog.Disabled)
	_ = logger

	tenant := shared.NewTenantID()
	tenantUUID, err := uuid.Parse(tenant.String())
	require.NoError(t, err)
	intentA := uuid.New()
	intentB := uuid.New()
	insertHiringIntentForSlice3(t, pool, intentA, tenantUUID)
	insertHiringIntentForSlice3(t, pool, intentB, tenantUUID)

	// Wire only the bits we need for the test — sourcing upload + list.
	storageDir := t.TempDir()
	store, err := sourcingstorage.NewLocalFS(storageDir)
	require.NoError(t, err)
	piiEnc, err := sourcingenc.NewLocalDevDEK("0000000000000000000000000000000000000000000000000000000000000000")
	require.NoError(t, err)
	uploadRepo := sourcingpersist.NewPostgresResumeUploadRepository(pool)

	uploadH := sourcingcommands.NewUploadResumeBatchHandler(
		uploadRepo, store,
		sourcingcommands.UploadConfig{MaxFileBytes: 10 * 1024 * 1024},
	)
	statusH := sourcingqueries.NewGetBatchStatusHandler(uploadRepo)

	// We're not running scoring in this test — just upload + list applications.
	// The Applications list will be empty until the worker pipeline runs.
	auditWriter := auditinfra.NewNoopAuditWriter()
	candRepo := sourcingpersist.NewPostgresCandidateRepository(pool)
	candidateH := sourcingqueries.NewGetCandidateHandler(candRepo, piiEnc, auditWriter)

	sourcingH := v1.NewSourcingHandler(v1.SourcingHandlerDeps{
		Upload:    uploadH,
		Status:    statusH,
		Candidate: candidateH,
		Logger:    zerolog.Nop(),
	})

	router := chi.NewRouter()
	identity := auth.Identity{TenantID: tenant, RecruiterID: shared.NewRecruiterID()}
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), identity)))
		})
	})
	v1.Mount(router, sourcingH)

	// Build a 3-PDF zip.
	pdfA := "%PDF-1.4\n%alice\n%%EOF\n"
	pdfB := "%PDF-1.4\n%bharat\n%%EOF\n"
	pdfC := "%PDF-1.4\n%chitra\n%%EOF\n"
	zipBody := buildE2EZip(t, []zipEntry{
		{"alice.pdf", pdfA}, {"bharat.pdf", pdfB}, {"chitra.pdf", pdfC},
	})

	// Upload the ZIP to intentA.
	respA := postZip(t, router, intentA, "batch.zip", zipBody)
	require.Len(t, respA.Items, 4) // parent + 3 children
	assert.Equal(t, "extracted_from_zip", respA.Items[0].Status)
	assert.Equal(t, "queued", respA.Items[1].Status)
	assert.Equal(t, "queued", respA.Items[2].Status)
	assert.Equal(t, "queued", respA.Items[3].Status)

	// Re-upload the same ZIP to intentA — all 3 should now be duplicate_in_intent.
	respDup := postZip(t, router, intentA, "batch.zip", zipBody)
	require.Len(t, respDup.Items, 4)
	assert.Equal(t, "extracted_from_zip", respDup.Items[0].Status)
	assert.Equal(t, "duplicate_in_intent", respDup.Items[1].Status)
	assert.Equal(t, "duplicate_in_intent", respDup.Items[2].Status)
	assert.Equal(t, "duplicate_in_intent", respDup.Items[3].Status)

	// Upload the same ZIP to intentB — all 3 should be queued (different intent key).
	respB := postZip(t, router, intentB, "batch.zip", zipBody)
	require.Len(t, respB.Items, 4)
	assert.Equal(t, "queued", respB.Items[1].Status)
	assert.Equal(t, "queued", respB.Items[2].Status)
	assert.Equal(t, "queued", respB.Items[3].Status)

	// Sanity: resume_uploads now has 6 rows (3 for intentA + 3 for intentB).
	var count int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM resume_uploads WHERE tenant_id=$1`, tenant.String(),
	).Scan(&count))
	assert.Equal(t, 6, count)

	// And resume_uploads_dedup has 6 rows too (3 per intent).
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM resume_uploads_dedup WHERE tenant_id=$1`, tenant.String(),
	).Scan(&count))
	assert.Equal(t, 6, count)
}

// --- helpers local to this file ---

type zipEntry struct{ Name, Body string }

func buildE2EZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		f, err := w.Create(e.Name)
		require.NoError(t, err)
		_, err = f.Write([]byte(e.Body))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func postZip(t *testing.T, router *chi.Mux, intentID uuid.UUID, filename string, body []byte) v1.BatchUploadResponse {
	t.Helper()
	mpBody, ct := writeMultipart(t, map[string][]byte{filename: body})
	req := httptest.NewRequest(http.MethodPost,
		"/intents/"+intentID.String()+"/resumes:batch", mpBody)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out v1.BatchUploadResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	return out
}

// Suppress unused warnings in case the helpers aren't used in some paths.
var _ = time.Second
```

- [ ] **Step 2: Run the test**

```bash
export DATABASE_URL="postgres://hireflow:hireflow@localhost:5433/hireflow?sslmode=disable"
INTEGRATION_TESTS=1 go test -tags=integration -count=1 -race ./tests/... -run TestSourcingZipUploadE2E -v
```

Expected: PASS (3 ZIP uploads, 6 final rows per dedup + uploads, correct status sequences).

- [ ] **Step 3: Run the full integration suite**

```bash
make test-integration 2>&1 | tail -10
```

Expected: all packages PASS.

- [ ] **Step 4: Commit**

```bash
git add tests/sourcing_zip_upload_e2e_test.go
git commit -m "test(sourcing): slice-5 e2e — ZIP upload + per-intent dedup"
```

---

## Task 9: OpenAPI bump + documented behaviour

**Files:**
- Modify: `docs/api/v1/sourcing.openapi.yaml`

- [ ] **Step 1: Bump version + add status values**

In `docs/api/v1/sourcing.openapi.yaml`:

1. Change `info.version` to `"1.0.0-slice5"`.
2. Update `info.description` to mention ZIP fan-out + per-intent dedup.
3. In the `BatchItemResponse` schema's `status` enum, add `"duplicate_in_intent"` and `"extracted_from_zip"`.
4. Add `parent_filename: { type: string }` and `parent_item_id: { type: string }` to `BatchItemResponse`.
5. In `BatchUploadResponse.description`, note: "ZIP files are extracted server-side; each entry surfaces as a separate item in the response with `parent_filename` + `parent_item_id` linking it to the ZIP's `extracted_from_zip` marker row."
6. In the `application/vnd.openxmlformats...` or wherever MIME is documented for the upload endpoint, add ODT + ZIP.
7. In the applications list response schema, add `top_skills` (array of `{name, years}`) and `judge_summary` (string) fields under the candidate object.

- [ ] **Step 2: Verify no Go build/test impact (docs-only change)**

```bash
make build
```

- [ ] **Step 3: Commit**

```bash
git add docs/api/v1/sourcing.openapi.yaml
git commit -m "docs(sourcing): OpenAPI 1.0.0-slice5 — ZIP, per-intent dedup, list-app enrichment"
```

---

## Task 10: Frontend — `api/sourcing.ts` + type extensions

**Files:**
- Create: `web/src/api/sourcing.ts`
- Modify: `web/src/api/types.ts`

- [ ] **Step 1: Extend `web/src/api/types.ts`**

Append:

```typescript
// ============================================================================
// Sourcing context — slice 5
// ============================================================================

export type BatchUploadOutcomeStatus =
  | 'queued'
  | 'deduplicated'           // legacy; kept for old callers
  | 'duplicate_in_intent'    // new in slice 5
  | 'extracted_from_zip'     // new in slice 5 — ZIP parent marker
  | ''                       // empty when item rejected (see `error`)

export interface BatchUploadOutcome {
  filename: string
  status: BatchUploadOutcomeStatus
  upload_id?: string
  candidate_id?: string
  parent_filename?: string
  parent_item_id?: string
  error?: { code: string; message: string; detail?: Record<string, unknown> }
}

export interface BatchUploadResponse {
  batch_id: string
  items: BatchUploadOutcome[]
}

export interface BatchStatusItem {
  upload_id: string
  filename: string
  status: 'Pending' | 'Scanning' | 'Extracting' | 'Extracted' | 'Parsing' | 'Parsed' | 'Failed' | 'Quarantined'
  attempt: number
  last_error: string
}

export interface BatchStatusResponse {
  batch_id: string
  intent_id: string
  summary: {
    total: number
    in_flight: number
    extracted: number
    failed: number
    quarantined: number
  }
  items: BatchStatusItem[]
}

export type ApplicationStatus =
  | 'New'
  | 'Scored'
  | 'Excluded'
  | 'EmbedFailed'
  | 'JudgeFailed'
  | 'Stale'
  | 'Shortlisted'
  | 'Interviewing'
  | 'Rejected'
  | 'Hired'

export interface SkillSummary {
  name: string
  years?: number
}

export interface CandidateSummary {
  full_name: string
  headline: string
  location: string
  top_skills: SkillSummary[]
  judge_summary: string
}

export interface Application {
  id: string
  candidate_id: string
  intent_id: string
  status: ApplicationStatus
  overall_score: number | null
  score_band: 'strong' | 'moderate' | 'weak' | null
  candidate: CandidateSummary
  created_at: string
  updated_at: string
}

export interface ApplicationListResponse {
  applications: Application[]
  total: number
}

export interface CandidateDetail {
  id: string
  content_hash: string
  personal: { full_name: string; email: string; phone: string }
  location: string
  headline: string
  profile: Record<string, unknown>   // raw parsed_profile JSONB
  source: string
  created_at: string
}
```

- [ ] **Step 2: Create `web/src/api/sourcing.ts`**

```typescript
import { request } from './client'
import type {
  Application,
  ApplicationListResponse,
  BatchUploadResponse,
  BatchStatusResponse,
  CandidateDetail,
} from './types'

export interface AppListFilter {
  status?: string
  limit?: number
  offset?: number
}

export const sourcingApi = {
  async uploadBatch(intentId: string, files: File[]): Promise<BatchUploadResponse> {
    const form = new FormData()
    for (const f of files) {
      form.append('resume', f, f.name)
    }
    const resp = await fetch(`/api/v1/intents/${encodeURIComponent(intentId)}/resumes:batch`, {
      method: 'POST',
      body: form,
      credentials: 'include',
    })
    if (!resp.ok) {
      throw new Error(`upload failed: ${resp.status}`)
    }
    return resp.json() as Promise<BatchUploadResponse>
  },

  getBatchStatus(batchId: string): Promise<BatchStatusResponse> {
    return request<BatchStatusResponse>(`/api/v1/resumes/batches/${batchId}`)
  },

  /** Open an EventSource on the batch's SSE stream. Caller closes on unmount. */
  subscribeBatchEvents(batchId: string): EventSource {
    return new EventSource(`/api/v1/resumes/batches/${batchId}/events`, {
      withCredentials: true,
    })
  },

  listApplications(intentId: string, filter: AppListFilter = {}): Promise<ApplicationListResponse> {
    return request<ApplicationListResponse>(
      `/api/v1/intents/${encodeURIComponent(intentId)}/applications`,
      { query: filter as Record<string, string | number> },
    )
  },

  getCandidate(candidateId: string): Promise<CandidateDetail> {
    return request<CandidateDetail>(`/api/v1/candidates/${encodeURIComponent(candidateId)}`)
  },

  shortlist(applicationId: string): Promise<void> {
    return request<void>(`/api/v1/applications/${encodeURIComponent(applicationId)}:shortlist`, {
      method: 'POST',
    })
  },

  reject(applicationId: string, reason: string): Promise<void> {
    return request<void>(`/api/v1/applications/${encodeURIComponent(applicationId)}:reject`, {
      method: 'POST',
      body: { reason },
    })
  },

  hire(applicationId: string): Promise<void> {
    return request<void>(`/api/v1/applications/${encodeURIComponent(applicationId)}:hire`, {
      method: 'POST',
    })
  },

  retryUpload(uploadId: string): Promise<void> {
    return request<void>(`/api/v1/resumes/${encodeURIComponent(uploadId)}:retry`, {
      method: 'POST',
    })
  },
}
```

- [ ] **Step 3: Verify typecheck**

```bash
cd web && npm run typecheck
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add web/src/api/sourcing.ts web/src/api/types.ts
git commit -m "feat(web): sourcing api client + type extensions for slice 5"
```

---

## Task 11: Frontend UI primitives — DropZone, Menu, Pill

**Files:**
- Create: `web/src/components/ui/DropZone.tsx`
- Create: `web/src/components/ui/Menu.tsx`
- Create: `web/src/components/ui/Pill.tsx`

- [ ] **Step 1: `Pill.tsx` — band pill primitive**

```tsx
import { ReactNode } from 'react'

type PillVariant = 'strong' | 'moderate' | 'weak' | 'info' | 'warn' | 'error' | 'neutral'

const variantClasses: Record<PillVariant, string> = {
  strong:   'bg-green-100 text-green-800',
  moderate: 'bg-amber-100 text-amber-800',
  weak:     'bg-red-100 text-red-800',
  info:     'bg-blue-100 text-blue-800',
  warn:     'bg-amber-100 text-amber-800',
  error:    'bg-red-100 text-red-800',
  neutral:  'bg-stone-100 text-stone-700',
}

export function Pill({ variant = 'neutral', children }: { variant?: PillVariant; children: ReactNode }) {
  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider ${variantClasses[variant]}`}>
      {children}
    </span>
  )
}

export function BandPill({ band }: { band: 'strong' | 'moderate' | 'weak' | null }) {
  if (!band) return null
  return <Pill variant={band}>{band}</Pill>
}
```

- [ ] **Step 2: `DropZone.tsx` — generic drag-drop primitive**

```tsx
import { ReactNode, useCallback, useRef, useState } from 'react'

interface DropZoneProps {
  accept: string         // MIME comma list, e.g. "application/pdf,application/zip"
  multiple?: boolean
  onFiles: (files: File[]) => void
  children?: ReactNode
}

export function DropZone({ accept, multiple = true, onFiles, children }: DropZoneProps) {
  const [dragging, setDragging] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  const onDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    setDragging(true)
  }, [])
  const onDragLeave = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    setDragging(false)
  }, [])
  const onDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    setDragging(false)
    const files = Array.from(e.dataTransfer.files)
    if (files.length) onFiles(files)
  }, [onFiles])

  const onChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files ?? [])
    if (files.length) onFiles(files)
    if (inputRef.current) inputRef.current.value = ''
  }, [onFiles])

  return (
    <div
      onDragOver={onDragOver}
      onDragLeave={onDragLeave}
      onDrop={onDrop}
      className={`border-2 border-dashed rounded-lg p-10 text-center transition-colors ${
        dragging ? 'border-orange-500 bg-orange-50' : 'border-stone-400 bg-white'
      }`}
    >
      <input
        ref={inputRef}
        type="file"
        accept={accept}
        multiple={multiple}
        onChange={onChange}
        className="hidden"
      />
      {children}
      <button
        type="button"
        onClick={() => inputRef.current?.click()}
        className="mt-2 text-orange-600 font-semibold hover:text-orange-700 underline-offset-2 hover:underline"
      >
        browse files
      </button>
    </div>
  )
}
```

- [ ] **Step 3: `Menu.tsx` — generic dropdown**

```tsx
import { ReactNode, useEffect, useRef, useState } from 'react'

interface MenuProps {
  trigger: ReactNode
  children: ReactNode  // items
}

export function Menu({ trigger, children }: MenuProps) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function onDocClick(e: MouseEvent) {
      if (!ref.current?.contains(e.target as Node)) setOpen(false)
    }
    if (open) document.addEventListener('mousedown', onDocClick)
    return () => document.removeEventListener('mousedown', onDocClick)
  }, [open])

  return (
    <div ref={ref} className="relative inline-block">
      <button
        type="button"
        onClick={() => setOpen(v => !v)}
        className="px-2 py-1 border border-stone-300 rounded text-sm font-semibold hover:bg-stone-50"
      >
        {trigger}
      </button>
      {open && (
        <div className="absolute right-0 top-full mt-1 bg-white border border-stone-200 rounded shadow-lg min-w-[160px] z-10">
          <div onClick={() => setOpen(false)}>{children}</div>
        </div>
      )}
    </div>
  )
}

export function MenuItem({ onClick, children, danger = false }: { onClick: () => void; children: ReactNode; danger?: boolean }) {
  return (
    <button
      onClick={onClick}
      className={`w-full text-left px-3 py-2 text-sm hover:bg-stone-50 ${danger ? 'text-red-600' : 'text-stone-800'}`}
    >
      {children}
    </button>
  )
}
```

- [ ] **Step 4: Verify typecheck**

```bash
cd web && npm run typecheck
```

Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/ui/
git commit -m "feat(web): UI primitives — DropZone, Menu, Pill"
```

---

## Task 12: Frontend — Upload card + outcomes list + hooks

**Files:**
- Create: `web/src/features/sourcing/UploadCard.tsx`
- Create: `web/src/features/sourcing/UploadOutcomeRow.tsx`
- Create: `web/src/features/sourcing/UploadOutcomesList.tsx`
- Create: `web/src/features/sourcing/useUploadBatch.ts`
- Create: `web/src/features/sourcing/useBatchSSE.ts`
- Create: `web/src/features/sourcing/useRetryUpload.ts`

- [ ] **Step 1: `useUploadBatch.ts`**

```typescript
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { sourcingApi } from '@/api/sourcing'
import type { BatchUploadResponse } from '@/api/types'

export function useUploadBatch(intentId: string) {
  const qc = useQueryClient()
  return useMutation<BatchUploadResponse, Error, File[]>({
    mutationFn: (files) => sourcingApi.uploadBatch(intentId, files),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['applications', intentId] })
    },
  })
}
```

- [ ] **Step 2: `useBatchSSE.ts`**

```typescript
import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { sourcingApi } from '@/api/sourcing'

interface SSEHandler {
  onItemEvent?: (eventName: string, data: Record<string, unknown>) => void
}

/** Opens an SSE connection on batch events. Closes on unmount. */
export function useBatchSSE(batchId: string | null, intentId: string, handler: SSEHandler = {}) {
  const qc = useQueryClient()

  useEffect(() => {
    if (!batchId) return

    const es = sourcingApi.subscribeBatchEvents(batchId)

    for (const eventName of ['item_accepted', 'item_failed', 'item_extracted', 'item_parsed']) {
      es.addEventListener(eventName, (raw) => {
        let data: Record<string, unknown> = {}
        try { data = JSON.parse((raw as MessageEvent).data) } catch { /* ignore */ }
        handler.onItemEvent?.(eventName, data)

        if (eventName === 'item_parsed') {
          qc.invalidateQueries({ queryKey: ['applications', intentId] })
        }
      })
    }

    es.onerror = () => {
      // Browser handles reconnect automatically with EventSource;
      // for production-grade backoff we'd close + reopen here.
    }

    return () => es.close()
  }, [batchId, intentId, qc, handler])
}
```

- [ ] **Step 3: `useRetryUpload.ts`**

```typescript
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { sourcingApi } from '@/api/sourcing'

export function useRetryUpload(intentId: string) {
  const qc = useQueryClient()
  return useMutation<void, Error, string>({
    mutationFn: (uploadId) => sourcingApi.retryUpload(uploadId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['applications', intentId] })
    },
  })
}
```

- [ ] **Step 4: `UploadOutcomeRow.tsx`**

```tsx
import { FileText, Archive, AlertCircle, CheckCircle, XCircle } from 'lucide-react'
import type { BatchUploadOutcome } from '@/api/types'
import { Pill } from '@/components/ui/Pill'
import { useRetryUpload } from './useRetryUpload'

interface Props {
  outcome: BatchUploadOutcome
  intentId: string
  indented?: boolean
}

export function UploadOutcomeRow({ outcome, intentId, indented = false }: Props) {
  const retry = useRetryUpload(intentId)
  const isRejected = !outcome.status && outcome.error
  const isDuplicate = outcome.status === 'duplicate_in_intent'
  const isZipParent = outcome.status === 'extracted_from_zip'
  const isQueued = outcome.status === 'queued' || outcome.status === 'deduplicated'

  const bg = isRejected
    ? 'bg-red-50'
    : isDuplicate
      ? 'bg-amber-50 border border-amber-200'
      : 'bg-stone-50'

  const icon = isZipParent
    ? <Archive className="w-4 h-4 text-stone-500" />
    : isRejected
      ? <FileText className="w-4 h-4 text-red-500" />
      : isDuplicate
        ? <AlertCircle className="w-4 h-4 text-amber-500" />
        : <FileText className="w-4 h-4 text-stone-500" />

  return (
    <div className={`flex items-center justify-between px-3 py-2 rounded ${bg} ${indented ? 'ml-8' : ''} mb-1`}>
      <div className="flex items-center gap-2 min-w-0">
        {icon}
        <div className="min-w-0">
          <span className="text-sm font-medium truncate">{outcome.filename}</span>
          {outcome.error && (
            <div className="text-xs text-red-600 mt-0.5">{outcome.error.message}</div>
          )}
          {isDuplicate && (
            <div className="text-xs text-amber-700 mt-0.5">Already sourced for this intent · not reprocessed</div>
          )}
        </div>
      </div>

      <div className="flex items-center gap-2 shrink-0">
        {isQueued && <Pill variant="strong"><CheckCircle className="w-3 h-3 mr-1 inline" />Queued</Pill>}
        {isDuplicate && <Pill variant="warn"><AlertCircle className="w-3 h-3 mr-1 inline" />Duplicate</Pill>}
        {isRejected && <Pill variant="error"><XCircle className="w-3 h-3 mr-1 inline" />Rejected</Pill>}
        {isZipParent && <Pill variant="neutral">Expanded</Pill>}
        {isRejected && outcome.upload_id && (
          <button
            onClick={() => retry.mutate(outcome.upload_id!)}
            className="text-xs text-orange-600 font-semibold hover:underline"
            disabled={retry.isPending}
          >
            Retry
          </button>
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 5: `UploadOutcomesList.tsx` — groups ZIP parents with children**

Per Task 6, both the ZIP parent (status `extracted_from_zip`) AND its children carry the same `parent_item_id`. To distinguish, identify the parent by status; everything else with a `parent_item_id` is a child.

```tsx
import type { BatchUploadOutcome } from '@/api/types'
import { UploadOutcomeRow } from './UploadOutcomeRow'

export function UploadOutcomesList({ outcomes, intentId }: { outcomes: BatchUploadOutcome[]; intentId: string }) {
  if (outcomes.length === 0) return null

  // Index children by parent_item_id. Children are outcomes that have a
  // parent_item_id AND are NOT the parent marker themselves.
  const childrenByParent = new Map<string, BatchUploadOutcome[]>()
  const topLevel: BatchUploadOutcome[] = []

  for (const o of outcomes) {
    if (o.parent_item_id && o.status !== 'extracted_from_zip') {
      const list = childrenByParent.get(o.parent_item_id) ?? []
      list.push(o)
      childrenByParent.set(o.parent_item_id, list)
    } else {
      // Top-level: standalone files AND ZIP-parent marker rows.
      topLevel.push(o)
    }
  }

  return (
    <div className="mt-4 space-y-1">
      {topLevel.map((o, i) => {
        const children = o.parent_item_id ? (childrenByParent.get(o.parent_item_id) ?? []) : []
        return (
          <div key={`${o.filename}-${i}`}>
            <UploadOutcomeRow outcome={o} intentId={intentId} />
            {children.map((child, j) => (
              <UploadOutcomeRow key={`${child.filename}-${j}`} outcome={child} intentId={intentId} indented />
            ))}
          </div>
        )
      })}
    </div>
  )
}
```

- [ ] **Step 6: `UploadCard.tsx`**

```tsx
import { useState } from 'react'
import { ArrowDownToLine } from 'lucide-react'
import { DropZone } from '@/components/ui/DropZone'
import { useUploadBatch } from './useUploadBatch'
import { useBatchSSE } from './useBatchSSE'
import { UploadOutcomesList } from './UploadOutcomesList'
import type { BatchUploadOutcome } from '@/api/types'

const ACCEPTED_MIME = [
  'application/pdf',
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  'application/msword',
  'application/vnd.oasis.opendocument.text',
  'application/zip',
].join(',')

export function UploadCard({ intentId }: { intentId: string }) {
  const upload = useUploadBatch(intentId)
  const [outcomes, setOutcomes] = useState<BatchUploadOutcome[]>([])
  const [batchId, setBatchId] = useState<string | null>(null)

  useBatchSSE(batchId, intentId, {
    onItemEvent: (eventName, data) => {
      // For simplicity, slice-5 just invalidates ['applications', intentId];
      // per-row badge updates from SSE are a polish slice.
      void eventName; void data
    },
  })

  return (
    <div className="bg-white border border-stone-200 rounded p-6 space-y-4">
      <h2 className="text-sm font-bold text-stone-800">Upload resumes</h2>

      <DropZone
        accept={ACCEPTED_MIME}
        multiple
        onFiles={async (files) => {
          const resp = await upload.mutateAsync(files)
          setOutcomes((prev) => [...resp.items, ...prev])
          setBatchId(resp.batch_id)
        }}
      >
        <ArrowDownToLine className="w-8 h-8 mx-auto text-stone-400 mb-2" />
        <p className="text-sm font-semibold text-stone-700">Drop resumes here</p>
        <p className="text-xs text-stone-500 mt-1">PDF, DOC, DOCX, ODT, ZIP · up to 10 MB each</p>
      </DropZone>

      {upload.isPending && <div className="text-xs text-stone-500">Uploading…</div>}
      {upload.isError && <div className="text-xs text-red-600">Upload failed: {upload.error.message}</div>}

      <UploadOutcomesList outcomes={outcomes} intentId={intentId} />

      {outcomes.length > 0 && (
        <button
          onClick={() => { setOutcomes([]); setBatchId(null) }}
          className="text-xs text-stone-500 hover:text-stone-800"
        >
          Clear
        </button>
      )}
    </div>
  )
}
```

- [ ] **Step 7: Verify typecheck + lint**

```bash
cd web && npm run typecheck && npm run lint
```

Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/sourcing/UploadCard.tsx \
        web/src/features/sourcing/UploadOutcomeRow.tsx \
        web/src/features/sourcing/UploadOutcomesList.tsx \
        web/src/features/sourcing/useUploadBatch.ts \
        web/src/features/sourcing/useBatchSSE.ts \
        web/src/features/sourcing/useRetryUpload.ts
git commit -m "feat(web): upload card + outcome rows + batch SSE hook"
```

---

## Task 13: Frontend — Candidate card + ApplicationActions

**Files:**
- Create: `web/src/features/sourcing/CandidateCard.tsx`
- Create: `web/src/features/sourcing/CandidateDenseRow.tsx`
- Create: `web/src/features/sourcing/ApplicationActions.tsx`

- [ ] **Step 1: `ApplicationActions.tsx` — hybrid primary + ⋮ menu, conditional per lifecycle**

```tsx
import { Link } from 'react-router-dom'
import { MoreVertical } from 'lucide-react'
import { Menu, MenuItem } from '@/components/ui/Menu'
import type { ApplicationStatus } from '@/api/types'

interface Props {
  applicationId: string
  candidateId: string
  status: ApplicationStatus
  onShortlist: () => void
  onReject: () => void
  onHire: () => void
}

export function ApplicationActions({
  applicationId, candidateId, status, onShortlist, onReject, onHire,
}: Props) {
  // Terminal lifecycle states — no actions, just placeholder.
  if (status === 'Hired' || status === 'Rejected') {
    return <div className="text-xs text-stone-400">—</div>
  }

  // Show appropriate primary action by status.
  const isShortlistable = status === 'Scored'
  const isHireable = status === 'Shortlisted' || status === 'Interviewing'

  return (
    <div className="flex items-center gap-1">
      {isShortlistable && (
        <button
          onClick={onShortlist}
          className="text-xs px-3 py-1.5 bg-orange-600 text-white font-semibold rounded hover:bg-orange-700"
        >
          Shortlist
        </button>
      )}
      {isHireable && (
        <button
          onClick={onHire}
          className="text-xs px-3 py-1.5 bg-orange-600 text-white font-semibold rounded hover:bg-orange-700"
        >
          Hire
        </button>
      )}
      <Menu trigger={<MoreVertical className="w-4 h-4" />}>
        {isShortlistable === false && status !== 'Hired' && status !== 'Rejected' && (
          <MenuItem onClick={onShortlist}>Re-shortlist</MenuItem>
        )}
        {!isHireable && status !== 'Hired' && <MenuItem onClick={onHire}>Hire</MenuItem>}
        <MenuItem onClick={onReject} danger>Reject</MenuItem>
        <Link to={`/candidates/${candidateId}`} className="block">
          <MenuItem onClick={() => {/* navigation handled by Link */}}>View detail</MenuItem>
        </Link>
      </Menu>
    </div>
  )
}
```

Note: `/candidates/:id` is a future-slice route; the Link will render a non-existent path until then. Acceptable for slice 5 — the placeholder lets us validate the menu UX now.

- [ ] **Step 2: `CandidateCard.tsx` — compact card (default density)**

```tsx
import type { Application } from '@/api/types'
import { BandPill, Pill } from '@/components/ui/Pill'
import { ApplicationActions } from './ApplicationActions'

interface Props {
  application: Application
  selected: boolean
  onToggleSelect: () => void
  onShortlist: () => void
  onReject: () => void
  onHire: () => void
}

const statusVariant = (s: string): 'info' | 'warn' | 'neutral' | 'strong' => {
  if (s === 'Shortlisted' || s === 'Interviewing') return 'warn'
  if (s === 'Hired') return 'strong'
  if (s === 'Rejected') return 'neutral'
  return 'neutral'
}

export function CandidateCard({ application, selected, onToggleSelect, onShortlist, onReject, onHire }: Props) {
  const c = application.candidate
  const initials = c.full_name.split(/\s+/).map(w => w[0]).join('').slice(0, 2).toUpperCase()
  const isTerminal = application.status === 'Rejected' || application.status === 'Hired'

  return (
    <div className={`flex items-center gap-3 px-4 py-3 bg-white border rounded mb-1.5 ${
      selected ? 'border-orange-500' : 'border-stone-200'
    } ${isTerminal ? 'opacity-60' : ''}`}>
      <input
        type="checkbox"
        checked={selected}
        onChange={onToggleSelect}
        className="w-4 h-4"
        disabled={isTerminal}
      />

      <div className="w-8 h-8 rounded-full bg-blue-700 text-white text-xs font-bold flex items-center justify-center shrink-0">
        {initials || '?'}
      </div>

      <div className="flex-1 min-w-0">
        <div className="text-sm font-semibold truncate">
          {c.full_name}
          <span className="font-normal text-stone-500"> · {c.headline} · {c.location}</span>
        </div>
        <div className="text-xs text-stone-500 mt-0.5 truncate">
          {c.top_skills.map(s => s.years ? `${s.name} · ${s.years}y` : s.name).join(' · ')}
          {c.judge_summary && <span className="text-stone-700"> · {c.judge_summary}</span>}
        </div>
      </div>

      <div className="text-right shrink-0">
        <div className="text-lg font-bold leading-tight">{application.overall_score ?? '—'}</div>
        <BandPill band={application.score_band} />
      </div>

      <Pill variant={statusVariant(application.status)}>{application.status}</Pill>

      <ApplicationActions
        applicationId={application.id}
        candidateId={application.candidate_id}
        status={application.status}
        onShortlist={onShortlist}
        onReject={onReject}
        onHire={onHire}
      />
    </div>
  )
}
```

- [ ] **Step 3: `CandidateDenseRow.tsx` — ultra-dense (alt density)**

```tsx
import type { Application } from '@/api/types'
import { BandPill, Pill } from '@/components/ui/Pill'
import { ApplicationActions } from './ApplicationActions'

interface Props {
  application: Application
  selected: boolean
  onToggleSelect: () => void
  onShortlist: () => void
  onReject: () => void
  onHire: () => void
}

export function CandidateDenseRow({
  application, selected, onToggleSelect, onShortlist, onReject, onHire,
}: Props) {
  const c = application.candidate
  const isTerminal = application.status === 'Rejected' || application.status === 'Hired'

  return (
    <tr className={`border-b border-stone-100 hover:bg-stone-50 ${isTerminal ? 'opacity-60' : ''}`}>
      <td className="px-3 py-2">
        <input type="checkbox" checked={selected} onChange={onToggleSelect} disabled={isTerminal} />
      </td>
      <td className="px-3 py-2 text-sm font-semibold">{c.full_name}</td>
      <td className="px-3 py-2 text-xs text-stone-600">{c.headline}</td>
      <td className="px-3 py-2 text-xs text-stone-600">{c.location}</td>
      <td className="px-3 py-2 text-right">
        <span className="text-sm font-bold">{application.overall_score ?? '—'}</span>
      </td>
      <td className="px-3 py-2"><BandPill band={application.score_band} /></td>
      <td className="px-3 py-2"><Pill variant="neutral">{application.status}</Pill></td>
      <td className="px-3 py-2">
        <ApplicationActions
          applicationId={application.id}
          candidateId={application.candidate_id}
          status={application.status}
          onShortlist={onShortlist}
          onReject={onReject}
          onHire={onHire}
        />
      </td>
    </tr>
  )
}
```

- [ ] **Step 4: Verify typecheck + lint**

```bash
cd web && npm run typecheck && npm run lint
```

Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/sourcing/CandidateCard.tsx \
        web/src/features/sourcing/CandidateDenseRow.tsx \
        web/src/features/sourcing/ApplicationActions.tsx
git commit -m "feat(web): candidate card + dense-row + action affordance"
```

---

## Task 14: Frontend — list toolbar + list section + applications hook

**Files:**
- Create: `web/src/features/sourcing/useApplicationsList.ts`
- Create: `web/src/features/sourcing/CandidateListToolbar.tsx`
- Create: `web/src/features/sourcing/CandidateListSection.tsx`

- [ ] **Step 1: `useApplicationsList.ts`**

```typescript
import { useInfiniteQuery } from '@tanstack/react-query'
import { sourcingApi, type AppListFilter } from '@/api/sourcing'
import type { Application } from '@/api/types'

const PAGE_SIZE = 20

export function useApplicationsList(intentId: string, status?: string) {
  return useInfiniteQuery({
    queryKey: ['applications', intentId, { status: status ?? '' }],
    initialPageParam: 0,
    queryFn: async ({ pageParam }) => {
      const filter: AppListFilter = { limit: PAGE_SIZE, offset: pageParam as number }
      if (status) filter.status = status
      return sourcingApi.listApplications(intentId, filter)
    },
    getNextPageParam: (lastPage, allPages) => {
      const loaded = allPages.reduce((acc, p) => acc + p.applications.length, 0)
      if (loaded >= lastPage.total) return undefined
      return loaded
    },
  })
}

// Flatten pages into one list — convenience for the section component.
export function flattenApplications(pages: Array<{ applications: Application[] }> | undefined): Application[] {
  return pages?.flatMap(p => p.applications) ?? []
}
```

- [ ] **Step 2: `CandidateListToolbar.tsx`**

```tsx
import { Search, Rows3, LayoutGrid } from 'lucide-react'

type Status = 'All' | 'Scored' | 'Shortlisted' | 'Interviewing' | 'Hired' | 'Rejected'
type Density = 'card' | 'dense'

interface Props {
  total: number
  countsByStatus: Record<Status, number>
  activeStatus: Status
  onStatusChange: (s: Status) => void
  search: string
  onSearchChange: (s: string) => void
  sort: 'score_desc' | 'recent' | 'name_asc'
  onSortChange: (s: 'score_desc' | 'recent' | 'name_asc') => void
  density: Density
  onDensityChange: (d: Density) => void
}

export function CandidateListToolbar({
  total, countsByStatus, activeStatus, onStatusChange,
  search, onSearchChange, sort, onSortChange, density, onDensityChange,
}: Props) {
  const chips: Status[] = ['All', 'Scored', 'Shortlisted', 'Interviewing', 'Hired', 'Rejected']

  return (
    <div className="sticky top-0 bg-white border-b border-stone-200 px-5 py-3 flex flex-wrap items-center gap-3 z-10">
      <div className="flex items-center gap-1.5">
        {chips.map(s => (
          <button
            key={s}
            onClick={() => onStatusChange(s)}
            className={`text-xs px-3 py-1.5 rounded font-semibold border ${
              activeStatus === s
                ? 'bg-orange-600 text-white border-orange-600'
                : 'bg-white text-stone-700 border-stone-300 hover:bg-stone-50'
            }`}
          >
            {s} · {s === 'All' ? total : (countsByStatus[s] ?? 0)}
          </button>
        ))}
      </div>

      <div className="flex items-center gap-2 ml-auto">
        <div className="relative">
          <Search className="w-3.5 h-3.5 absolute left-2.5 top-2 text-stone-400" />
          <input
            type="text"
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder="Search by name"
            className="text-xs pl-8 pr-3 py-1.5 border border-stone-300 rounded w-44"
          />
        </div>
        <select
          value={sort}
          onChange={(e) => onSortChange(e.target.value as 'score_desc' | 'recent' | 'name_asc')}
          className="text-xs px-2 py-1.5 border border-stone-300 rounded bg-white"
        >
          <option value="score_desc">Sort: Score ↓</option>
          <option value="recent">Sort: Recently added</option>
          <option value="name_asc">Sort: Name A→Z</option>
        </select>
        <div className="inline-flex border border-stone-300 rounded overflow-hidden">
          <button
            onClick={() => onDensityChange('dense')}
            className={`p-1.5 ${density === 'dense' ? 'bg-orange-600 text-white' : 'bg-white'}`}
            title="Dense rows"
          ><Rows3 className="w-3.5 h-3.5" /></button>
          <button
            onClick={() => onDensityChange('card')}
            className={`p-1.5 ${density === 'card' ? 'bg-orange-600 text-white' : 'bg-white'}`}
            title="Cards"
          ><LayoutGrid className="w-3.5 h-3.5" /></button>
        </div>
      </div>
    </div>
  )
}
```

- [ ] **Step 3: `CandidateListSection.tsx`**

```tsx
import { useState, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Spinner } from '@/components/ui/primitives'
import { useApplicationsList, flattenApplications } from './useApplicationsList'
import { CandidateCard } from './CandidateCard'
import { CandidateDenseRow } from './CandidateDenseRow'
import { CandidateListToolbar } from './CandidateListToolbar'
import { BulkActionBar } from './BulkActionBar'
import { useShortlist } from './useShortlist'
import { useReject } from './useReject'
import { useHire } from './useHire'

type Status = 'All' | 'Scored' | 'Shortlisted' | 'Interviewing' | 'Hired' | 'Rejected'
type Density = 'card' | 'dense'

const DENSITY_KEY = 'hireflow.candidateListDensity'

export function CandidateListSection({ intentId }: { intentId: string }) {
  const [searchParams, setSearchParams] = useSearchParams()
  const status = (searchParams.get('status') as Status) || 'All'
  const sort = (searchParams.get('sort') as 'score_desc' | 'recent' | 'name_asc') || 'score_desc'
  const [search, setSearch] = useState('')
  const [density, setDensity] = useState<Density>(
    (localStorage.getItem(DENSITY_KEY) as Density) || 'card',
  )
  const [selected, setSelected] = useState<Set<string>>(new Set())

  const apiStatus = status === 'All' ? undefined : status
  const query = useApplicationsList(intentId, apiStatus)
  const apps = flattenApplications(query.data?.pages)
  const total = query.data?.pages[0]?.total ?? 0

  const filtered = useMemo(() => {
    let result = apps
    if (search) {
      const q = search.toLowerCase()
      result = result.filter(a => a.candidate.full_name.toLowerCase().includes(q))
    }
    // sort happens client-side (server-side sort would need extra param plumbing)
    result = [...result].sort((a, b) => {
      if (sort === 'score_desc') return (b.overall_score ?? 0) - (a.overall_score ?? 0)
      if (sort === 'recent') return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
      return a.candidate.full_name.localeCompare(b.candidate.full_name)
    })
    return result
  }, [apps, search, sort])

  const countsByStatus = useMemo(() => {
    const counts: Record<Status, number> = {
      All: total, Scored: 0, Shortlisted: 0, Interviewing: 0, Hired: 0, Rejected: 0,
    }
    for (const a of apps) {
      if (a.status in counts) counts[a.status as Status]++
    }
    return counts
  }, [apps, total])

  const setStatus = (s: Status) => {
    if (s === 'All') searchParams.delete('status')
    else searchParams.set('status', s)
    setSearchParams(searchParams)
  }
  const setSort = (s: 'score_desc' | 'recent' | 'name_asc') => {
    searchParams.set('sort', s)
    setSearchParams(searchParams)
  }
  const setDensityPersisted = (d: Density) => {
    localStorage.setItem(DENSITY_KEY, d)
    setDensity(d)
  }

  const toggleSelect = (id: string) => {
    const next = new Set(selected)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setSelected(next)
  }
  const clearSelection = () => setSelected(new Set())

  const shortlist = useShortlist(intentId)
  const reject = useReject(intentId)
  const hire = useHire(intentId)

  if (query.isLoading) return <div className="p-8 flex justify-center"><Spinner /></div>

  return (
    <div className="bg-white border border-stone-200 rounded mt-6 overflow-hidden">
      <CandidateListToolbar
        total={total}
        countsByStatus={countsByStatus}
        activeStatus={status}
        onStatusChange={setStatus}
        search={search}
        onSearchChange={setSearch}
        sort={sort}
        onSortChange={setSort}
        density={density}
        onDensityChange={setDensityPersisted}
      />

      <div className="p-4 bg-stone-50 min-h-[200px]">
        {filtered.length === 0 && (
          <p className="text-sm text-stone-500 text-center py-8">
            {status === 'All' ? 'No candidates yet. Drop resumes above to get started.' : `No candidates with status ${status}.`}
          </p>
        )}

        {density === 'card' && filtered.map(a => (
          <CandidateCard
            key={a.id}
            application={a}
            selected={selected.has(a.id)}
            onToggleSelect={() => toggleSelect(a.id)}
            onShortlist={() => shortlist.mutate(a.id)}
            onReject={() => {
              const reason = window.prompt('Reject reason?') || ''
              if (reason) reject.mutate({ applicationId: a.id, reason })
            }}
            onHire={() => hire.mutate(a.id)}
          />
        ))}

        {density === 'dense' && (
          <table className="w-full bg-white border border-stone-200 rounded">
            <thead>
              <tr className="bg-stone-50 text-xs text-stone-500 uppercase tracking-wider">
                <th className="px-3 py-2 w-8"></th>
                <th className="px-3 py-2 text-left">Name</th>
                <th className="px-3 py-2 text-left">Headline</th>
                <th className="px-3 py-2 text-left">Location</th>
                <th className="px-3 py-2 text-right">Score</th>
                <th className="px-3 py-2 text-left">Band</th>
                <th className="px-3 py-2 text-left">Status</th>
                <th className="px-3 py-2 text-left">Actions</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map(a => (
                <CandidateDenseRow
                  key={a.id}
                  application={a}
                  selected={selected.has(a.id)}
                  onToggleSelect={() => toggleSelect(a.id)}
                  onShortlist={() => shortlist.mutate(a.id)}
                  onReject={() => {
                    const reason = window.prompt('Reject reason?') || ''
                    if (reason) reject.mutate({ applicationId: a.id, reason })
                  }}
                  onHire={() => hire.mutate(a.id)}
                />
              ))}
            </tbody>
          </table>
        )}

        {query.hasNextPage && (
          <div className="text-center mt-4">
            <button
              onClick={() => query.fetchNextPage()}
              disabled={query.isFetchingNextPage}
              className="text-xs text-orange-600 font-semibold hover:underline"
            >
              {query.isFetchingNextPage ? 'Loading…' : `Load more (${total - apps.length} remaining)`}
            </button>
          </div>
        )}
      </div>

      <BulkActionBar
        selectedIds={selected}
        intentId={intentId}
        onClear={clearSelection}
      />
    </div>
  )
}
```

- [ ] **Step 4: Verify typecheck**

```bash
cd web && npm run typecheck
```

Note: this depends on `useShortlist`, `useReject`, `useHire`, `BulkActionBar` from Tasks 15/16 — typecheck will fail until those land. Expected red until Task 16 lands; that's fine.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/sourcing/useApplicationsList.ts \
        web/src/features/sourcing/CandidateListToolbar.tsx \
        web/src/features/sourcing/CandidateListSection.tsx
git commit -m "feat(web): candidate list section + sticky toolbar + applications hook"
```

---

## Task 15: Frontend — optimistic mutations (shortlist / reject / hire)

**Files:**
- Create: `web/src/features/sourcing/useShortlist.ts`
- Create: `web/src/features/sourcing/useReject.ts`
- Create: `web/src/features/sourcing/useHire.ts`

- [ ] **Step 1: Shared optimistic-update helper inline in each hook**

Each hook follows the same pattern: optimistically mutate the cache to set the new status, do the API call, roll back on error.

`useShortlist.ts`:

```typescript
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { sourcingApi } from '@/api/sourcing'
import type { Application } from '@/api/types'

interface PagedResponse {
  applications: Application[]
  total: number
}

export function useShortlist(intentId: string) {
  const qc = useQueryClient()
  return useMutation<void, Error, string, { prevData: Array<[unknown, unknown]> }>({
    mutationFn: (applicationId) => sourcingApi.shortlist(applicationId),
    onMutate: async (applicationId) => {
      const queryKey = ['applications', intentId]
      await qc.cancelQueries({ queryKey })
      const prevData = qc.getQueriesData({ queryKey })

      qc.setQueriesData<{ pages: PagedResponse[]; pageParams: unknown[] }>(
        { queryKey },
        (old) => {
          if (!old) return old
          return {
            ...old,
            pages: old.pages.map(p => ({
              ...p,
              applications: p.applications.map(a =>
                a.id === applicationId ? { ...a, status: 'Shortlisted' as const } : a,
              ),
            })),
          }
        },
      )
      return { prevData }
    },
    onError: (_err, _vars, ctx) => {
      ctx?.prevData?.forEach(([key, data]) => qc.setQueryData(key as unknown[], data))
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ['applications', intentId] })
    },
  })
}
```

`useReject.ts`:

```typescript
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { sourcingApi } from '@/api/sourcing'
import type { Application } from '@/api/types'

interface PagedResponse {
  applications: Application[]
  total: number
}

interface RejectInput {
  applicationId: string
  reason: string
}

export function useReject(intentId: string) {
  const qc = useQueryClient()
  return useMutation<void, Error, RejectInput, { prevData: Array<[unknown, unknown]> }>({
    mutationFn: ({ applicationId, reason }) => sourcingApi.reject(applicationId, reason),
    onMutate: async ({ applicationId }) => {
      const queryKey = ['applications', intentId]
      await qc.cancelQueries({ queryKey })
      const prevData = qc.getQueriesData({ queryKey })
      qc.setQueriesData<{ pages: PagedResponse[]; pageParams: unknown[] }>(
        { queryKey },
        (old) => {
          if (!old) return old
          return {
            ...old,
            pages: old.pages.map(p => ({
              ...p,
              applications: p.applications.map(a =>
                a.id === applicationId ? { ...a, status: 'Rejected' as const } : a,
              ),
            })),
          }
        },
      )
      return { prevData }
    },
    onError: (_err, _vars, ctx) => {
      ctx?.prevData?.forEach(([key, data]) => qc.setQueryData(key as unknown[], data))
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ['applications', intentId] })
    },
  })
}
```

`useHire.ts` is identical to `useShortlist.ts` but the new status is `'Hired'` and the API call is `sourcingApi.hire`. Write it analogously.

- [ ] **Step 2: Verify typecheck**

```bash
cd web && npm run typecheck
```

Expected: clean (assuming Task 14's `CandidateListSection.tsx` is on disk; otherwise still red on the `BulkActionBar` import from Task 16).

- [ ] **Step 3: Commit**

```bash
git add web/src/features/sourcing/useShortlist.ts \
        web/src/features/sourcing/useReject.ts \
        web/src/features/sourcing/useHire.ts
git commit -m "feat(web): optimistic shortlist/reject/hire mutations"
```

---

## Task 16: Frontend — BulkActionBar + useBulkAction

**Files:**
- Create: `web/src/features/sourcing/BulkActionBar.tsx`
- Create: `web/src/features/sourcing/useBulkAction.ts`

- [ ] **Step 1: `useBulkAction.ts`**

```typescript
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { sourcingApi } from '@/api/sourcing'

type BulkActionKind = 'shortlist' | 'reject'

interface BulkActionInput {
  kind: BulkActionKind
  ids: string[]
  reason?: string
}

interface BulkActionResult {
  succeeded: string[]
  failed: Array<{ id: string; error: string }>
}

export function useBulkAction(intentId: string) {
  const qc = useQueryClient()
  return useMutation<BulkActionResult, Error, BulkActionInput>({
    mutationFn: async ({ kind, ids, reason }) => {
      const result: BulkActionResult = { succeeded: [], failed: [] }
      // Sequential — keeps backend transitions predictable and avoids flooding.
      for (const id of ids) {
        try {
          if (kind === 'shortlist') {
            await sourcingApi.shortlist(id)
          } else {
            await sourcingApi.reject(id, reason ?? 'Bulk reject (no reason)')
          }
          result.succeeded.push(id)
        } catch (e) {
          result.failed.push({ id, error: (e as Error).message })
        }
      }
      return result
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ['applications', intentId] })
    },
  })
}
```

- [ ] **Step 2: `BulkActionBar.tsx`**

```tsx
import { useBulkAction } from './useBulkAction'

interface Props {
  selectedIds: Set<string>
  intentId: string
  onClear: () => void
}

export function BulkActionBar({ selectedIds, intentId, onClear }: Props) {
  const bulk = useBulkAction(intentId)

  if (selectedIds.size === 0) return null

  const ids = Array.from(selectedIds)

  return (
    <div className="sticky bottom-0 bg-amber-50 border-t border-amber-200 px-5 py-3 flex items-center justify-between">
      <span className="text-sm font-semibold text-stone-800">{ids.length} selected</span>
      <div className="flex gap-2">
        <button
          onClick={async () => {
            await bulk.mutateAsync({ kind: 'shortlist', ids })
            onClear()
          }}
          disabled={bulk.isPending}
          className="text-xs px-3 py-1.5 bg-orange-600 text-white font-semibold rounded hover:bg-orange-700 disabled:opacity-60"
        >
          {bulk.isPending ? 'Working…' : `Shortlist ${ids.length}`}
        </button>
        <button
          onClick={async () => {
            const reason = window.prompt('Reject reason (applied to all)?')
            if (!reason) return
            await bulk.mutateAsync({ kind: 'reject', ids, reason })
            onClear()
          }}
          disabled={bulk.isPending}
          className="text-xs px-3 py-1.5 border border-stone-300 rounded bg-white font-semibold hover:bg-stone-50 disabled:opacity-60"
        >
          {bulk.isPending ? 'Working…' : `Reject ${ids.length}`}
        </button>
        <button
          onClick={onClear}
          className="text-xs px-3 py-1.5 text-stone-500 font-semibold hover:text-stone-700"
        >
          Clear
        </button>
      </div>
    </div>
  )
}
```

- [ ] **Step 3: Verify typecheck + lint**

```bash
cd web && npm run typecheck && npm run lint
```

Expected: clean (this is the last piece — Task 14's `CandidateListSection` can now resolve all imports).

- [ ] **Step 4: Commit**

```bash
git add web/src/features/sourcing/BulkActionBar.tsx \
        web/src/features/sourcing/useBulkAction.ts
git commit -m "feat(web): bulk-action bar + sequential bulk mutation"
```

---

## Task 17: Frontend — wire into PostingDetailPage (remove Source Distribution)

**Files:**
- Modify: `web/src/features/posting/PostingDetailPage.tsx`

- [ ] **Step 1: Open the file and find the Source Distribution section**

```bash
grep -n "Source Distribution\|CHANNELS\|sources\|publishMutation" web/src/features/posting/PostingDetailPage.tsx
```

Note the line range of the entire Source Distribution `<Card>` block and the channel-picker state (`selected`, `toggle`).

- [ ] **Step 2: Replace the Source Distribution card with the new sections**

Remove:
- The `CHANNELS` constant.
- The `selected` state and `toggle` callback.
- The entire `<Card>` for Source Distribution.
- The `publishMutation` (call it the new way — see below).
- Imports for `Linkedin, Globe, Mail, Database` from lucide (no longer used in this file).
- Import of `SourceChannel` from `@/api/types` (no longer used here).

Add at the top:

```tsx
import { UploadCard } from '@/features/sourcing/UploadCard'
import { CandidateListSection } from '@/features/sourcing/CandidateListSection'
```

The new flow:
- Posting still has a Publish button (publishes to zero channels — backend now accepts this since Task 1).
- Below the JD section, render `<UploadCard intentId={posting.intent_id} />`.
- Below the upload card, render `<CandidateListSection intentId={posting.intent_id} />`.

Updated publish mutation:

```tsx
const publishMutation = useMutation({
  mutationFn: () => postingApi.publish(id, []),   // empty channels — slice 5 backend accepts
  onSuccess: () => {
    qc.invalidateQueries({ queryKey: ['posting', id] })
    qc.invalidateQueries({ queryKey: ['postings'] })
  },
})
```

The JSX skeleton becomes (rough — adapt to the existing page header/structure):

```tsx
return (
  <div className="px-8 py-6 max-w-5xl space-y-6">
    {/* existing posting header + JD card unchanged */}

    {posting.status === 'DRAFT' && (
      <Card className="p-6">
        <div className="flex justify-between items-center">
          <div>
            <h2 className="text-sm font-bold">Ready to start sourcing?</h2>
            <p className="text-xs text-stone-500 mt-1">Publishing opens this posting for resume uploads.</p>
          </div>
          <Button onClick={() => publishMutation.mutate()} disabled={publishMutation.isPending}>
            {publishMutation.isPending ? 'Publishing…' : 'Publish'}
          </Button>
        </div>
      </Card>
    )}

    {posting.status !== 'DRAFT' && posting.status !== 'ARCHIVED' && (
      <>
        <UploadCard intentId={posting.intent_id} />
        <CandidateListSection intentId={posting.intent_id} />
      </>
    )}

    {/* existing Close button card unchanged */}
  </div>
)
```

The exact existing-page structure (header, JD card, close button) stays as-is — only the Source Distribution card is replaced.

- [ ] **Step 3: Verify typecheck + lint + build**

```bash
cd web && npm run typecheck && npm run lint && npm run build
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add web/src/features/posting/PostingDetailPage.tsx
git commit -m "refactor(web): replace Source Distribution with upload + candidate list"
```

---

## Task 18: Smoke run + manual acceptance walk

**Files:** none (manual checklist).

- [ ] **Step 1: Bring up the stack**

```bash
cd /Users/manojkumar.m1thoughtworks.com/hustle/code/theo/hireflow
docker compose up -d postgres
export DATABASE_URL="postgres://hireflow:hireflow@localhost:5433/hireflow?sslmode=disable"
make migrate-up
make build

DATABASE_URL="postgres://hireflow:hireflow@localhost:5433/hireflow?sslmode=disable" \
JWT_ACCESS_SECRET="devsecret" \
SOURCING_PII_DEK="0000000000000000000000000000000000000000000000000000000000000000" \
STUB_LLMS=true \
PORT=8080 \
./bin/api > /tmp/hireflow-api.log 2>&1 &

cd web && npm run dev > /tmp/hireflow-web.log 2>&1 &
```

Expected: api log shows STUB_LLMS WARN line + all dispatchers started; web log shows Vite serving on http://localhost:5173.

- [ ] **Step 2: Sign up + confirm OTP**

Open `http://localhost:5173`. Sign up with `demo@example.com` / `Demo` / tenant slug `demo`. Read the OTP from the API log (`grep log_otp_sender /tmp/hireflow-api.log | tail -1`). Submit it.

- [ ] **Step 3: Create an intent + confirm it**

In the UI: create a Senior Backend Engineer intent (Go, Kafka required). Confirm.

- [ ] **Step 4: Open the auto-created posting**

Navigate to `/postings` → click into the posting. The Source Distribution card should NOT be present. Status: DRAFT. There's a "Publish" button.

- [ ] **Step 5: Publish the posting**

Click Publish. Status should flip to PUBLISHED. The Upload card and (empty) Candidate list should now appear.

- [ ] **Step 6: Upload a single resume**

Drag any PDF onto the upload area. Expected:
- An outcome row appears immediately with `Queued` badge.
- Within ~10s, the candidate appears in the list below with a score (from stub).

- [ ] **Step 7: Upload the same resume again**

Drop the same PDF again. Expected: an amber `Duplicate` row with text "Already sourced for this intent · not reprocessed". No new candidate in the list.

- [ ] **Step 8: Upload a ZIP**

Create a ZIP with 3 PDFs. Drop it. Expected:
- ZIP parent row with `Expanded` badge.
- 3 indented child rows, each with `Queued`.
- 3 new candidates appear in the list.

- [ ] **Step 9: Shortlist + Reject + Bulk**

- Click Shortlist on the first candidate. Status changes immediately (optimistic).
- Click ⋮ on another candidate → Reject. Provide a reason. Row dims.
- Tick checkboxes on 2 remaining candidates. Bulk bar appears at the bottom. Click "Shortlist 2". Both flip to Shortlisted.

- [ ] **Step 10: Toolbar interactions**

- Click status chips (Scored / Shortlisted / Rejected) — list filters correctly.
- Type in the search box — list filters by name client-side.
- Toggle the density button — list switches between cards and dense table.
- Reload the page — density preference persists; filter+sort come from URL.

- [ ] **Step 11: Tear down**

```bash
pkill -INT -f "./bin/api"
pkill -INT -f "vite"
```

- [ ] **Step 12: Document any deviations in a final commit**

If the manual run surfaced anything (e.g., row spacing wrong, copy ambiguous, badge colour off), make targeted fixes and commit:

```bash
git add web/src/features/sourcing/
git commit -m "fix(web): polish from manual smoke walk"
```

Otherwise no commit needed.

---

## Wrap-up

After Tasks 1-18:

- `make test-unit` and `make test-integration` both green.
- `go vet ./...` and `gofmt -s -l .` clean.
- `cd web && npm run typecheck && npm run lint && npm run build` clean.
- Smoke walk in Task 18 completed end-to-end without surprises.

**What this slice ships:**

- Source Distribution card is gone. Posting page now has Upload + Candidate list.
- Resume upload supports PDF, DOC, DOCX, ODT, ZIP. ZIPs expand inline.
- Same-intent re-uploads are warned + skipped; cross-intent uploads of the same resume are processed (one Candidate, multiple Applications).
- Candidates appear in a sticky-toolbar list with status filter chips, search, sort, density toggle, bulk select.
- Hybrid action affordance (primary button + overflow menu) per lifecycle.
- All backend endpoints already existed (slices 1-4) except the relaxed `Publish` invariant and the new `FindByContentHashAndIntent` repo method.
- OpenAPI bumped to `1.0.0-slice5`.

**What slice 5 does NOT ship (deferred):**

- Candidate detail page (`/candidates/:id`).
- Original-resume download endpoint + UI.
- Async ZIP processing (for ZIPs over the inline rails).
- Interview module UI.
- Frontend test infrastructure (Vitest + MSW) — file as a follow-up.
- Per-row SSE badge updates as scanning/extraction/parsing progresses (UI listens to SSE for cache invalidation only in this slice; the per-row badge stays at "Queued" until the candidate appears).
- Real source-distribution integrations.

Per-row SSE badge updates and the candidate detail page are the natural next slice — both are small additions.
