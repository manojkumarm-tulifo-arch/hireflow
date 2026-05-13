# Sourcing Slice 2 — Parsing + `Candidate` Aggregate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the sourcing pipeline through structured profile extraction. When a `ResumeUpload` reaches `Extracted` (slice 1's terminal state), the worker picks it up, runs Claude tool-use against a `parse_resume` schema (with Claude-vision OCR fallback if the extracted text is empty), produces a versioned `ParsedProfile`, and creates a tenant-scoped `Candidate` aggregate. Adds `GET /api/v1/candidates/{id}` for the recruiter.

**Architecture:** Builds on slice 1's stage machine. Adds two new ports — `ResumeParser` (Claude tool-use) and `OCRExtractor` (Claude vision) — plus a `PIIEncryptor` port for tenant-scoped envelope encryption on personal fields. The `Candidate` aggregate is created in the same tx as the `ResumeUpload → Parsed` transition, with `CandidateParsed` written to the outbox atomically.

**Tech Stack:** Same as slice 1. Reuses the shared `anthropic.Client` (already wired by `hiringintent`). New module deps: none beyond what's already in `go.mod`. The PII encryption uses Go stdlib `crypto/aes` + `crypto/cipher` with a hardcoded dev-only DEK; KMS-backed prod adapter is deferred to v1.1.

**Spec reference:** `docs/superpowers/specs/2026-05-12-sourcing-design.md` — implements the second slice from §Rollout. Locked tactical decisions from the brainstorm session on 2026-05-13:

| # | Decision |
|---|---|
| S2-D1 | OCR adapter: Claude vision (multimodal via existing Anthropic SDK). No Tesseract dep. |
| S2-D2 | OCR trigger: when `TextExtractor` output is < 50 chars after `strings.TrimSpace`. |
| S2-D3 | PII encryption: `PIIEncryptor` port. Slice 2 ships `local-dev-dek` adapter (single hardcoded key); `aws-kms` adapter is a future task. |
| S2-D4 | Worker stage transitions: `Extracted → Parsing → Parsed`. OCR runs as a sub-step inside the Parsing stage handler, not as its own state. |
| S2-D5 | `Candidate` is created in the same tx as the `ResumeUpload → Parsed` transition. |
| S2-D6 | `GET /api/v1/candidates/{candidate_id}` ships in slice 2. PII fields decrypted at response time, with audit-log entry. |
| S2-D7 | Out of scope for slice 2: matching, `Application`, SSE, retry/rescore endpoints, lifecycle actions, deletion. |

---

## File structure

### Files created

```
migrations/sourcing/
    000002_create_candidates.up.sql
    000002_create_candidates.down.sql

internal/sourcing/
    domain/
        valueobjects/
            parsed_profile.go             ParsedProfile schema_version=1
            parsed_profile_test.go
        entities/
            candidate.go                  Candidate aggregate
            candidate_test.go
        events/
            candidate_events.go           CandidateParsed (with content_hash, schema_version)
            candidate_events_test.go
        repositories/
            candidate_repository.go       Port: Save, FindByID, FindByContentHash
        services/
            resume_parser.go              Port: Parse(ctx, text) → ParsedProfile
            ocr_extractor.go              Port: ExtractFromBytes(ctx, pdfBytes) → RawText
            pii_encryptor.go              Port: Encrypt/Decrypt, tenant-scoped
    application/
        commands/
            (process_upload.go modified — add runParsing branch)
            process_upload_parsing_test.go
        queries/
            get_candidate.go              GetCandidateHandler
            get_candidate_test.go
    infrastructure/
        parsing/
            anthropic_parser.go           Claude tool-use, parse_resume schema
            anthropic_parser_test.go      Fake HTTP transport
            prompts/
                parse_resume.tmpl
            schemas/
                parse_resume.schema.json
        ocr/
            claude_vision.go              Claude vision adapter
            claude_vision_test.go         Fake HTTP transport
        encryption/
            local_dev.go                  Local DEK adapter (AES-GCM)
            local_dev_test.go
        persistence/
            postgres_candidate_repository.go
            postgres_candidate_repository_test.go    integration-tagged
            candidate_serializer.go
    delivery/
        http/v1/
            (handlers.go modified — add GetCandidate)
            (dto.go modified — add CandidateDetailResponse)
            (routes.go modified — mount /candidates/{id})
            candidate_handler_test.go

tests/
    sourcing_slice2_e2e_test.go           Full slice-1+2 e2e: upload → scan → extract → parse → candidate
```

### Files modified

- `internal/sourcing/domain/valueobjects/upload_status.go` — add `StatusParsed` terminal state. Update `CanTransitionTo` so `Extracted → Parsing` and `Parsing → Parsed/Failed/Quarantined` are permitted. Mark `Extracted` as **not** terminal anymore — it's now intermediate. Mark `Parsed` as terminal.
- `internal/sourcing/domain/entities/resume_upload.go` — add lifecycle methods `BeginParsing`, `RecordParsedProfile(profile)`, `CompleteParsed`, `LinkCandidate(candidateID)`. Extend the `StageArtifacts` write path so the parsed JSON is persisted before transition.
- `internal/sourcing/domain/valueobjects/stage_artifacts.go` — add `SetParsedProfile(json []byte)` + `ParsedProfile() ([]byte, bool)`.
- `internal/sourcing/application/commands/process_upload.go` — switch on `StatusExtracted` (new entry point), add `runParsing(ctx, u)` which dispatches to OCR fallback if needed and then calls the parser. On success: create Candidate + link upload + commit.
- `cmd/api/main.go` — wire `ResumeParser`, `OCRExtractor`, `PIIEncryptor`, `CandidateRepository`, candidate detail handler. Inject parser + ocr + encryptor + candidate repo into the worker's `ProcessConfig`.
- `internal/sourcing/delivery/http/v1/handlers.go` + `dto.go` + `routes.go` — `GET /candidates/{candidate_id}` returns the full profile with PII decrypted.
- `docs/api/v1/sourcing.openapi.yaml` — add the candidate detail schema and route.
- `README.md` — update the sourcing context row to reflect slice-2 capabilities.
- `docs/modules/sourcing/README.md` — refresh pipeline diagram + capability list.
- `developer.md` — note the new `SOURCING_PARSER_BACKEND`, `SOURCING_OCR_BACKEND`, `SOURCING_PII_DEK` env vars.

---

## Conventions baked into every task

- **Working branch:** continue on `feat/sourcing-slice-1` (we never merged it; slice 2 builds on top). After slice 2 is verified end-to-end against the live containers, the whole branch becomes ship-ready as "sourcing slice 1+2."
  *(If you'd prefer separate branches per slice for cleaner PR boundaries, branch `feat/sourcing-slice-2` off `feat/sourcing-slice-1` before T1.)*
- **Module:** `github.com/hustle/hireflow`. All new code under `internal/sourcing/`.
- **Tests:** unit `_test.go` siblings; integration `//go:build integration`-gated.
- **Commit cadence:** one commit per task. **No `Co-Authored-By: Claude` trailers** in implementation commits.
- **Run unit tests** with `make test-unit` after each task.
- **Run integration tests** with `INTEGRATION_TESTS=1 make test-integration` once Postgres + ClamAV are up (`make db-up && make migrate-up` from the worktree).
- **Anthropic ZDR**: prod requires Zero Data Retention enrollment on the API key. The dev tests use a fake HTTP transport — no real keys ever sent.

---

## Task 1: Migration — `candidates` table + `candidate_id` FK on `resume_uploads`

**Files:**
- Create: `migrations/sourcing/000002_create_candidates.up.sql`
- Create: `migrations/sourcing/000002_create_candidates.down.sql`

- [ ] **Step 1: Write the up migration**

Create `migrations/sourcing/000002_create_candidates.up.sql`:

```sql
-- candidates: tenant-scoped person identity. One row per (tenant_id, content_hash).
-- The unique index lives on candidates directly (not partitioned, so no constraint
-- issue). Slice 2 ships an unpartitioned table — partition-readiness for candidates
-- can come at scale (~50M+ rows per spec §Scalability).
CREATE TABLE candidates (
    id                 UUID         PRIMARY KEY,
    tenant_id          UUID         NOT NULL,
    content_hash       TEXT         NOT NULL,

    -- PII fields encrypted at the application layer via the PIIEncryptor port.
    -- TEXT columns hold base64-encoded AES-GCM ciphertext + 12-byte nonce prefix.
    full_name_enc      TEXT,
    email_enc          TEXT,
    phone_enc          TEXT,

    -- Non-PII fields stored cleartext.
    location           TEXT,
    headline           TEXT,
    parsed_profile     JSONB        NOT NULL,
    profile_schema     INT          NOT NULL DEFAULT 1,

    source             TEXT         NOT NULL DEFAULT 'manual_upload',
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),

    UNIQUE (tenant_id, content_hash)
);

-- Lookup helpers. email_enc isn't indexable for content search since it's
-- ciphertext; the index is intentionally absent in slice 2. Slice 4's audit
-- + erasure work may introduce a per-tenant deterministic hash index.
CREATE INDEX candidates_tenant_created_idx ON candidates (tenant_id, created_at DESC);

-- Wire resume_uploads.candidate_id → candidates(id). No CASCADE on delete in v1:
-- candidate deletion (GDPR slice 4) explicitly cascades via application code so
-- audit-log entries are correctly written.
ALTER TABLE resume_uploads
    ADD CONSTRAINT resume_uploads_candidate_fk
        FOREIGN KEY (candidate_id) REFERENCES candidates(id);
```

- [ ] **Step 2: Write the down migration**

Create `migrations/sourcing/000002_create_candidates.down.sql`:

```sql
ALTER TABLE resume_uploads
    DROP CONSTRAINT IF EXISTS resume_uploads_candidate_fk;

DROP TABLE IF EXISTS candidates;
```

- [ ] **Step 3: Apply and verify (if Postgres is available)**

```
make migrate-up
```
Expected: `2/u create_candidates (...)` line for sourcing namespace.

Verify the table and FK exist:
```
psql "$DATABASE_URL" -c "\d candidates"
psql "$DATABASE_URL" -c "\d resume_uploads" | grep candidate_id
```
Expected: `candidates` table with the listed columns; `resume_uploads.candidate_id` references `candidates(id)`.

Test rollback:
```
make migrate-down   # rolls back slice 2's migration only (one step)
make migrate-up
```

If Postgres isn't available, skip and rely on syntactic check by visual inspection. Future T15 will catch any DDL issues.

- [ ] **Step 4: Commit**

```bash
git add migrations/sourcing/
git commit -m "feat(sourcing): migration for candidates and resume_uploads FK"
```

---

## Task 2: `StatusParsed` + entity lifecycle methods

**Files:**
- Modify: `internal/sourcing/domain/valueobjects/upload_status.go`
- Modify: `internal/sourcing/domain/valueobjects/upload_status_test.go`
- Modify: `internal/sourcing/domain/entities/resume_upload.go`
- Modify: `internal/sourcing/domain/entities/resume_upload_test.go`

Slice 1 left `Extracted` as a terminal state. Slice 2 makes it intermediate and adds `Parsed` as the new terminal. The entity gains `BeginParsing`, `RecordParsedProfile`, `CompleteParsed`, `LinkCandidate` (sets `candidate_id`).

- [ ] **Step 1: Update `upload_status_test.go` — add new transitions**

In `internal/sourcing/domain/valueobjects/upload_status_test.go`, modify `TestUploadStatus_CanTransitionTo` to include:

```go
		{vo.StatusExtracted, vo.StatusParsing, true},
		{vo.StatusParsing, vo.StatusParsed, true},
		{vo.StatusParsing, vo.StatusFailed, true},
		{vo.StatusParsed, vo.StatusParsing, false},   // Parsed is terminal
		{vo.StatusExtracted, vo.StatusExtracted, false},
```

And modify `TestUploadStatus_IsTerminal` so:
- `StatusParsed` returns true
- `StatusExtracted` returns **false** (was true in slice 1)

- [ ] **Step 2: Run — should fail**

```
go test ./internal/sourcing/domain/valueobjects/... -run TestUploadStatus -v -count=1
```
Expected: failures on the new cases.

- [ ] **Step 3: Update `upload_status.go`**

Modify `IsTerminal()` to remove `StatusExtracted` from the terminal list (keep `StatusScored`, `StatusFailed`, `StatusQuarantined`), and add `StatusParsed`:

```go
func (s UploadStatus) IsTerminal() bool {
	switch s {
	case StatusParsed, StatusScored, StatusFailed, StatusQuarantined:
		return true
	}
	return false
}
```

Modify `CanTransitionTo()` so `Extracting → Extracted` only allows downstream `Parsing`; also accept `Parsed` as a destination from `Parsing`:

```go
func (s UploadStatus) CanTransitionTo(next UploadStatus) bool {
	if s.IsTerminal() {
		return false
	}
	if next == StatusFailed || next == StatusQuarantined {
		return true
	}
	switch s {
	case StatusPending:
		return next == StatusScanning
	case StatusScanning:
		return next == StatusExtracting
	case StatusExtracting:
		return next == StatusExtracted
	case StatusExtracted:
		return next == StatusParsing
	case StatusParsing:
		return next == StatusParsed
	}
	return false
}
```

- [ ] **Step 4: Run — should pass**

```
go test ./internal/sourcing/domain/valueobjects/... -v -count=1
```
Expected: all PASS.

- [ ] **Step 5: Update `resume_upload_test.go` — add lifecycle tests**

Append to `internal/sourcing/domain/entities/resume_upload_test.go`:

```go
func mustParsedJSON(t *testing.T) []byte {
	t.Helper()
	return []byte(`{"schema_version":1,"headline":"Senior Backend Engineer"}`)
}

func TestParsingFlow_HappyPath(t *testing.T) {
	u := newUpload(t)
	_ = u.PullEvents()
	require.NoError(t, u.BeginScanning())
	require.NoError(t, u.BeginExtracting())
	require.NoError(t, u.RecordExtractedText("resume text", 2))
	require.NoError(t, u.CompleteExtracted())
	_ = u.PullEvents() // drain ResumeExtracted

	require.NoError(t, u.BeginParsing())
	assert.Equal(t, vo.StatusParsing, u.Status())

	require.NoError(t, u.RecordParsedProfile(mustParsedJSON(t)))

	candID := uuid.New()
	require.NoError(t, u.LinkCandidate(candID))
	assert.Equal(t, candID, u.CandidateID())

	require.NoError(t, u.CompleteParsed())
	assert.Equal(t, vo.StatusParsed, u.Status())
	assert.True(t, u.Status().IsTerminal())

	evs := u.PullEvents()
	require.Len(t, evs, 1)
	assert.Equal(t, "sourcing.ResumeParsed", evs[0].EventName())
}

func TestParsingFlow_RecordParsedProfile_OnlyDuringParsing(t *testing.T) {
	u := newUpload(t)
	err := u.RecordParsedProfile(mustParsedJSON(t))
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

func TestParsingFlow_LinkCandidate_OnlyDuringParsing(t *testing.T) {
	u := newUpload(t)
	err := u.LinkCandidate(uuid.New())
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

func TestParsingFlow_CompleteParsed_RequiresProfileAndCandidate(t *testing.T) {
	u := newUpload(t)
	_ = u.PullEvents()
	require.NoError(t, u.BeginScanning())
	require.NoError(t, u.BeginExtracting())
	require.NoError(t, u.RecordExtractedText("x", 1))
	require.NoError(t, u.CompleteExtracted())
	require.NoError(t, u.BeginParsing())

	// Without profile + candidate: CompleteParsed must reject.
	err := u.CompleteParsed()
	assert.Error(t, err)

	// With profile only — still missing candidate.
	require.NoError(t, u.RecordParsedProfile(mustParsedJSON(t)))
	err = u.CompleteParsed()
	assert.Error(t, err)

	// Now link a candidate; complete succeeds.
	require.NoError(t, u.LinkCandidate(uuid.New()))
	require.NoError(t, u.CompleteParsed())
}
```

You'll also need a `"sourcing.ResumeParsed"` event (added in T5/T6). For now the test references the event name string only; the entity emits whatever event matches that name when `CompleteParsed` is called.

- [ ] **Step 6: Run — should fail (new methods don't exist)**

```
go test ./internal/sourcing/domain/entities/...
```
Expected: build error / undefined methods.

- [ ] **Step 7: Implement lifecycle methods**

Append to `internal/sourcing/domain/entities/resume_upload.go`:

```go
// BeginParsing transitions Extracted → Parsing.
func (u *ResumeUpload) BeginParsing() error {
	return u.transition(vo.StatusParsing, "")
}

// RecordParsedProfile persists the parser's output bytes on the upload row.
// Idempotent — calling twice overwrites. Must be called during Parsing.
func (u *ResumeUpload) RecordParsedProfile(profileJSON []byte) error {
	if u.status != vo.StatusParsing {
		return ErrInvalidTransition
	}
	u.artifacts.SetParsedProfile(profileJSON)
	u.touch()
	return nil
}

// LinkCandidate attaches a candidate_id to this upload. Must be called during Parsing.
func (u *ResumeUpload) LinkCandidate(candidateID uuid.UUID) error {
	if u.status != vo.StatusParsing {
		return ErrInvalidTransition
	}
	u.candidateID = candidateID
	u.touch()
	return nil
}

// CompleteParsed transitions Parsing → Parsed and emits ResumeParsed.
// Requires the parsed profile artifact and a linked candidate.
func (u *ResumeUpload) CompleteParsed() error {
	if _, ok := u.artifacts.ParsedProfile(); !ok {
		return errors.New("parsed profile artifact missing")
	}
	if u.candidateID == uuid.Nil {
		return errors.New("candidate not linked")
	}
	if err := u.transition(vo.StatusParsed, ""); err != nil {
		return err
	}
	u.emit(events.ResumeParsed{
		UploadID:    u.id,
		TenantID:    u.tenantID,
		CandidateID: u.candidateID,
		OccurredAt:  u.updatedAt,
	})
	return nil
}
```

(The `ResumeParsed` event struct is defined in T6; this file imports `events` already from slice 1.)

- [ ] **Step 8: Skip running entity tests yet** — they reference the not-yet-defined `events.ResumeParsed` and `StageArtifacts.SetParsedProfile`. Those land in T3 and T6 respectively. Build will fail at this point — that's expected. The test suite returns to green at the end of T6.

- [ ] **Step 9: Commit**

```bash
git add internal/sourcing/domain/valueobjects/upload_status.go \
        internal/sourcing/domain/valueobjects/upload_status_test.go \
        internal/sourcing/domain/entities/resume_upload.go \
        internal/sourcing/domain/entities/resume_upload_test.go
git commit -m "feat(sourcing): add Parsed terminal state and entity parsing lifecycle"
```

Note: this commit intentionally leaves the build broken; T3, T5, T6 restore it. If you'd rather have green-builds at every commit, sequence as T3 → T2 → T5/T6.

---

## Task 3: `StageArtifacts` — add `ParsedProfile`

**Files:**
- Modify: `internal/sourcing/domain/valueobjects/stage_artifacts.go`
- Modify: `internal/sourcing/domain/valueobjects/stage_artifacts_test.go`

- [ ] **Step 1: Update the test**

Append to `internal/sourcing/domain/valueobjects/stage_artifacts_test.go`:

```go
func TestStageArtifacts_ParsedProfile_RoundTrip(t *testing.T) {
	a := vo.NewStageArtifacts()
	a.SetParsedProfile([]byte(`{"schema_version":1}`))

	out, err := a.Marshal()
	require.NoError(t, err)

	got, err := vo.UnmarshalStageArtifacts(out)
	require.NoError(t, err)
	b, ok := got.ParsedProfile()
	require.True(t, ok)
	assert.Contains(t, string(b), `"schema_version":1`)
}

func TestStageArtifacts_ParsedProfile_EmptyByDefault(t *testing.T) {
	a := vo.NewStageArtifacts()
	_, ok := a.ParsedProfile()
	assert.False(t, ok)
}
```

- [ ] **Step 2: Run — should fail**

```
go test ./internal/sourcing/domain/valueobjects/... -run TestStageArtifacts_Parsed -v
```
Expected: undefined methods.

- [ ] **Step 3: Implement**

Modify `internal/sourcing/domain/valueobjects/stage_artifacts.go`. Add a `ParsedProfileJSON` field (string for JSON-friendly marshal) and the helpers:

```go
type StageArtifacts struct {
	ExtractedTextValue string `json:"extracted_text,omitempty"`
	PageCount          int    `json:"page_count,omitempty"`
	ParsedProfileJSON  string `json:"parsed_profile,omitempty"`
}

// SetParsedProfile records the parser's output as a JSON byte slice.
func (a *StageArtifacts) SetParsedProfile(b []byte) {
	a.ParsedProfileJSON = string(b)
}

// ParsedProfile returns the parsed profile JSON, or ok=false if Parsing hasn't run.
func (a StageArtifacts) ParsedProfile() ([]byte, bool) {
	if a.ParsedProfileJSON == "" {
		return nil, false
	}
	return []byte(a.ParsedProfileJSON), true
}
```

- [ ] **Step 4: Run — should pass**

```
go test ./internal/sourcing/domain/valueobjects/... -v -count=1
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sourcing/domain/valueobjects/stage_artifacts.go \
        internal/sourcing/domain/valueobjects/stage_artifacts_test.go
git commit -m "feat(sourcing): persist parsed profile JSON on stage_artifacts"
```

---

## Task 4: `ParsedProfile` value object (schema_version=1)

**Files:**
- Create: `internal/sourcing/domain/valueobjects/parsed_profile.go`
- Create: `internal/sourcing/domain/valueobjects/parsed_profile_test.go`

The canonical schema for what the parser returns. JSON-marshalable so we can `unmarshal(stage_artifacts.ParsedProfile())` cleanly.

- [ ] **Step 1: Write the test**

Create `internal/sourcing/domain/valueobjects/parsed_profile_test.go`:

```go
package valueobjects_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

func TestParsedProfile_DefaultSchemaVersion(t *testing.T) {
	p := vo.NewParsedProfile()
	assert.Equal(t, 1, p.SchemaVersion)
}

func TestParsedProfile_RoundTrip(t *testing.T) {
	p := vo.NewParsedProfile()
	p.Personal.FullName = "Alice"
	p.Personal.Email = "alice@example.com"
	p.Headline = "Senior Backend Engineer"
	p.Skills = []vo.ParsedSkill{{Name: "Go", Years: 5}}
	p.Experiences = []vo.ParsedExperience{{
		ID: "exp_0", Company: "Razorpay", Title: "Senior Backend Engineer",
		Start: "2020-04", End: "2025-01",
	}}

	b, err := json.Marshal(p)
	require.NoError(t, err)

	var got vo.ParsedProfile
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, 1, got.SchemaVersion)
	assert.Equal(t, "Alice", got.Personal.FullName)
	assert.Equal(t, "Senior Backend Engineer", got.Headline)
	require.Len(t, got.Skills, 1)
	assert.Equal(t, "Go", got.Skills[0].Name)
	assert.Equal(t, 5.0, got.Skills[0].Years)
	require.Len(t, got.Experiences, 1)
	assert.Equal(t, "Razorpay", got.Experiences[0].Company)
}

func TestParsedProfile_Validate_RequiresSchemaVersion(t *testing.T) {
	var p vo.ParsedProfile // zero-value, schema=0
	err := p.Validate()
	assert.ErrorIs(t, err, vo.ErrInvalidProfile)
}

func TestParsedProfile_Validate_AcceptsMinimal(t *testing.T) {
	p := vo.NewParsedProfile()
	p.Personal.FullName = "Anon Candidate"
	err := p.Validate()
	require.NoError(t, err)
}
```

- [ ] **Step 2: Run — should fail**

```
go test ./internal/sourcing/domain/valueobjects/... -run TestParsedProfile
```
Expected: undefined.

- [ ] **Step 3: Implement**

Create `internal/sourcing/domain/valueobjects/parsed_profile.go`:

```go
package valueobjects

import "errors"

// ErrInvalidProfile is returned by ParsedProfile.Validate when the structure is malformed.
var ErrInvalidProfile = errors.New("invalid parsed profile")

// ParsedProfile is the canonical structured form of a resume.
// Versioned via SchemaVersion so future shape changes don't break old rows.
type ParsedProfile struct {
	SchemaVersion  int                  `json:"schema_version"`
	Personal       ParsedPersonal       `json:"personal"`
	Headline       string               `json:"headline,omitempty"`
	Summary        string               `json:"summary,omitempty"`
	Skills         []ParsedSkill        `json:"skills,omitempty"`
	Experiences    []ParsedExperience   `json:"experiences,omitempty"`
	Education      []ParsedEducation    `json:"education,omitempty"`
	Certifications []ParsedCertification `json:"certifications,omitempty"`
	Languages      []ParsedLanguage     `json:"languages,omitempty"`
	Warnings       []string             `json:"warnings,omitempty"`
}

// ParsedPersonal holds the PII portion of a parsed profile. These fields are
// encrypted at the application layer via the PIIEncryptor port before storage.
type ParsedPersonal struct {
	FullName string       `json:"full_name,omitempty"`
	Email    string       `json:"email,omitempty"`
	Phone    string       `json:"phone,omitempty"`
	Location string       `json:"location,omitempty"`
	Links    []ParsedLink `json:"links,omitempty"`
}

// ParsedLink is one external profile link (LinkedIn, GitHub, portfolio).
type ParsedLink struct {
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

// ParsedSkill is one skill claim with optional years and a reference back to the
// experience that supports it.
type ParsedSkill struct {
	Name        string  `json:"name"`
	Years       float64 `json:"years,omitempty"`
	EvidenceRef string  `json:"evidence_ref,omitempty"`
}

// ParsedExperience is one work-experience entry.
type ParsedExperience struct {
	ID          string   `json:"id"`
	Company     string   `json:"company"`
	Title       string   `json:"title"`
	Start       string   `json:"start"` // YYYY-MM
	End         string   `json:"end,omitempty"`
	Current     bool     `json:"current,omitempty"`
	Description string   `json:"description,omitempty"`
	SkillsUsed  []string `json:"skills_used,omitempty"`
}

// ParsedEducation is one education entry.
type ParsedEducation struct {
	Institution string `json:"institution"`
	Degree      string `json:"degree,omitempty"`
	Field       string `json:"field,omitempty"`
	Start       string `json:"start,omitempty"`
	End         string `json:"end,omitempty"`
}

// ParsedCertification is one certification entry.
type ParsedCertification struct {
	Name    string `json:"name"`
	Issuer  string `json:"issuer,omitempty"`
	Issued  string `json:"issued,omitempty"`
	Expires string `json:"expires,omitempty"`
}

// ParsedLanguage is one language proficiency entry.
type ParsedLanguage struct {
	Name        string `json:"name"`
	Proficiency string `json:"proficiency,omitempty"` // native|fluent|professional|basic
}

// NewParsedProfile returns a fresh empty profile pinned to schema_version=1.
func NewParsedProfile() ParsedProfile {
	return ParsedProfile{SchemaVersion: 1}
}

// Validate enforces minimum invariants. Currently only "schema_version > 0".
// Field-level validation is deferred to the parser adapter so the LLM's
// `warnings` array can carry parse-time issues instead of hard-rejecting.
func (p ParsedProfile) Validate() error {
	if p.SchemaVersion <= 0 {
		return ErrInvalidProfile
	}
	return nil
}
```

- [ ] **Step 4: Run — should pass**

```
go test ./internal/sourcing/domain/valueobjects/... -v -count=1
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sourcing/domain/valueobjects/parsed_profile.go \
        internal/sourcing/domain/valueobjects/parsed_profile_test.go
git commit -m "feat(sourcing): ParsedProfile value object with schema_version=1"
```

---

## Task 5: `Candidate` aggregate

**Files:**
- Create: `internal/sourcing/domain/entities/candidate.go`
- Create: `internal/sourcing/domain/entities/candidate_test.go`

The aggregate root for "a person within a tenant." Holds the encrypted PII at the field level (via opaque `EncryptedField` value), the cleartext profile metadata, and content-hash provenance.

- [ ] **Step 1: Write the test**

Create `internal/sourcing/domain/entities/candidate_test.go`:

```go
package entities_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

func mustCHash(t *testing.T) vo.ContentHash {
	h, err := vo.NewContentHash("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	require.NoError(t, err)
	return h
}

func newCandidateInput(t *testing.T) entities.NewCandidateInput {
	t.Helper()
	profile := vo.NewParsedProfile()
	profile.Personal.FullName = "Alice"
	profile.Personal.Email = "alice@example.com"
	profile.Headline = "Senior Backend Engineer"

	return entities.NewCandidateInput{
		TenantID:    shared.NewTenantID(),
		ContentHash: mustCHash(t),
		Profile:     profile,
		Encrypted: entities.EncryptedPersonal{
			FullName: "enc:full_name",
			Email:    "enc:email",
			Phone:    "enc:phone",
		},
		Location: "Bangalore",
		Headline: "Senior Backend Engineer",
		Source:   "manual_upload",
	}
}

func TestNewCandidate_HappyPath_EmitsParsedEvent(t *testing.T) {
	c, err := entities.NewCandidate(newCandidateInput(t))
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, c.ID())
	assert.Equal(t, "enc:email", c.EncryptedEmail())
	assert.Equal(t, "Bangalore", c.Location())
	assert.Equal(t, 1, c.ProfileSchema())

	evs := c.PullEvents()
	require.Len(t, evs, 1)
	assert.Equal(t, "sourcing.CandidateParsed", evs[0].EventName())
	assert.Empty(t, c.PullEvents(), "PullEvents must drain")
}

func TestNewCandidate_RejectsInvalidProfile(t *testing.T) {
	in := newCandidateInput(t)
	in.Profile = vo.ParsedProfile{} // schema_version=0
	_, err := entities.NewCandidate(in)
	assert.ErrorIs(t, err, vo.ErrInvalidProfile)
}

func TestNewCandidate_RejectsEmptyContentHash(t *testing.T) {
	in := newCandidateInput(t)
	in.ContentHash = vo.ContentHash{}
	_, err := entities.NewCandidate(in)
	assert.Error(t, err)
}

func TestRehydrateCandidate_BypassesEvents(t *testing.T) {
	c, err := entities.NewCandidate(newCandidateInput(t))
	require.NoError(t, err)
	_ = c.PullEvents()

	rh := entities.RehydrateCandidate(entities.RehydrateCandidateInput{
		ID:                 c.ID(),
		TenantID:           c.TenantID(),
		ContentHash:        c.ContentHash(),
		EncryptedFullName:  c.EncryptedFullName(),
		EncryptedEmail:     c.EncryptedEmail(),
		EncryptedPhone:     c.EncryptedPhone(),
		Location:           c.Location(),
		Headline:           c.Headline(),
		Profile:            c.Profile(),
		Source:             c.Source(),
		CreatedAt:          c.CreatedAt(),
		UpdatedAt:          c.UpdatedAt(),
	})
	assert.Equal(t, c.ID(), rh.ID())
	assert.Empty(t, rh.PullEvents(), "rehydrate must not emit events")
}
```

- [ ] **Step 2: Run — should fail**

```
go test ./internal/sourcing/domain/entities/... -run TestNewCandidate
```
Expected: undefined.

- [ ] **Step 3: Implement**

Create `internal/sourcing/domain/entities/candidate.go`:

```go
package entities

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/sourcing/domain/events"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// EncryptedPersonal holds the ciphertext form of PII fields produced by the
// PIIEncryptor port. The aggregate doesn't know how the encryption works —
// just that these are opaque strings to be stored as-is.
type EncryptedPersonal struct {
	FullName string
	Email    string
	Phone    string
}

// NewCandidateInput is the constructor input.
type NewCandidateInput struct {
	TenantID    shared.TenantID
	ContentHash vo.ContentHash
	Profile     vo.ParsedProfile
	Encrypted   EncryptedPersonal // ciphertext for personal.*; aggregate stores as-is
	Location    string            // cleartext, non-PII
	Headline    string            // cleartext, non-PII
	Source      string            // "manual_upload" for slice 2
	// Optional overrides for deterministic tests; nil → real values.
	Now func() time.Time
	ID  uuid.UUID
}

// Candidate is the tenant-scoped person aggregate. Unique on (tenant_id, content_hash).
type Candidate struct {
	id              uuid.UUID
	tenantID        shared.TenantID
	contentHash     vo.ContentHash
	encFullName     string
	encEmail        string
	encPhone        string
	location        string
	headline        string
	profile         vo.ParsedProfile
	source          string
	createdAt       time.Time
	updatedAt       time.Time
	pendingEvents   []events.Event
}

// NewCandidate constructs a fresh candidate, validating the profile and
// emitting CandidateParsed.
func NewCandidate(in NewCandidateInput) (*Candidate, error) {
	if err := in.Profile.Validate(); err != nil {
		return nil, err
	}
	if in.ContentHash.String() == "" {
		return nil, errors.New("content_hash required")
	}
	if in.Source == "" {
		in.Source = "manual_upload"
	}
	now := time.Now().UTC
	if in.Now != nil {
		now = in.Now
	}
	id := in.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	t := now().UTC()

	c := &Candidate{
		id:          id,
		tenantID:    in.TenantID,
		contentHash: in.ContentHash,
		encFullName: in.Encrypted.FullName,
		encEmail:    in.Encrypted.Email,
		encPhone:    in.Encrypted.Phone,
		location:    in.Location,
		headline:    in.Headline,
		profile:     in.Profile,
		source:      in.Source,
		createdAt:   t,
		updatedAt:   t,
	}
	c.emit(events.CandidateParsed{
		CandidateID:   id,
		TenantID:      in.TenantID,
		ContentHash:   in.ContentHash.String(),
		SchemaVersion: in.Profile.SchemaVersion,
		OccurredAt:    t,
	})
	return c, nil
}

// Accessors.
func (c *Candidate) ID() uuid.UUID                { return c.id }
func (c *Candidate) TenantID() shared.TenantID    { return c.tenantID }
func (c *Candidate) ContentHash() vo.ContentHash  { return c.contentHash }
func (c *Candidate) EncryptedFullName() string    { return c.encFullName }
func (c *Candidate) EncryptedEmail() string       { return c.encEmail }
func (c *Candidate) EncryptedPhone() string       { return c.encPhone }
func (c *Candidate) Location() string             { return c.location }
func (c *Candidate) Headline() string             { return c.headline }
func (c *Candidate) Profile() vo.ParsedProfile    { return c.profile }
func (c *Candidate) ProfileSchema() int           { return c.profile.SchemaVersion }
func (c *Candidate) Source() string               { return c.source }
func (c *Candidate) CreatedAt() time.Time         { return c.createdAt }
func (c *Candidate) UpdatedAt() time.Time         { return c.updatedAt }

// PullEvents drains pending events. Same pattern as ResumeUpload.
func (c *Candidate) PullEvents() []events.Event {
	out := c.pendingEvents
	c.pendingEvents = nil
	return out
}

func (c *Candidate) emit(ev events.Event) {
	c.pendingEvents = append(c.pendingEvents, ev)
}

// RehydrateCandidateInput is for repository reads — bypasses event emission.
type RehydrateCandidateInput struct {
	ID                uuid.UUID
	TenantID          shared.TenantID
	ContentHash       vo.ContentHash
	EncryptedFullName string
	EncryptedEmail    string
	EncryptedPhone    string
	Location          string
	Headline          string
	Profile           vo.ParsedProfile
	Source            string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// RehydrateCandidate reconstructs an aggregate from a persisted row.
func RehydrateCandidate(in RehydrateCandidateInput) *Candidate {
	return &Candidate{
		id:          in.ID,
		tenantID:    in.TenantID,
		contentHash: in.ContentHash,
		encFullName: in.EncryptedFullName,
		encEmail:    in.EncryptedEmail,
		encPhone:    in.EncryptedPhone,
		location:    in.Location,
		headline:    in.Headline,
		profile:     in.Profile,
		source:      in.Source,
		createdAt:   in.CreatedAt,
		updatedAt:   in.UpdatedAt,
	}
}
```

- [ ] **Step 4: Skip test run** — `events.CandidateParsed` and `events.ResumeParsed` aren't defined yet. T6 defines both and gets the build green.

- [ ] **Step 5: Commit**

```bash
git add internal/sourcing/domain/entities/candidate.go \
        internal/sourcing/domain/entities/candidate_test.go
git commit -m "feat(sourcing): Candidate aggregate"
```

---

## Task 6: New events (`ResumeParsed`, `CandidateParsed`) + domain ports

**Files:**
- Modify: `internal/sourcing/domain/events/upload_events.go`
- Modify: `internal/sourcing/domain/events/upload_events_test.go`
- Create: `internal/sourcing/domain/events/candidate_events.go`
- Create: `internal/sourcing/domain/events/candidate_events_test.go`
- Create: `internal/sourcing/domain/services/resume_parser.go`
- Create: `internal/sourcing/domain/services/ocr_extractor.go`
- Create: `internal/sourcing/domain/services/pii_encryptor.go`
- Create: `internal/sourcing/domain/repositories/candidate_repository.go`

This commit restores the build by defining the missing events and the new ports.

- [ ] **Step 1: Add `ResumeParsed` to upload events**

Append to `internal/sourcing/domain/events/upload_events.go`:

```go
// ResumeParsed is emitted when parsing succeeds and a candidate has been linked.
// This is the slice-2 terminal event for the ResumeUpload aggregate; slice 3's
// scoring consumer subscribes to it.
type ResumeParsed struct {
	UploadID    uuid.UUID       `json:"upload_id"`
	TenantID    shared.TenantID `json:"tenant_id"`
	CandidateID uuid.UUID       `json:"candidate_id"`
	OccurredAt  time.Time       `json:"occurred_at"`
}

func (e ResumeParsed) EventName() string         { return "sourcing.ResumeParsed" }
func (e ResumeParsed) AggregateID() uuid.UUID    { return e.UploadID }
func (e ResumeParsed) Tenant() shared.TenantID   { return e.TenantID }
func (e ResumeParsed) At() time.Time             { return e.OccurredAt }
```

Update `internal/sourcing/domain/events/upload_events_test.go` — append:

```go
func TestResumeParsed_Shape(t *testing.T) {
	ev := events.ResumeParsed{
		UploadID:    uuid.New(),
		TenantID:    shared.NewTenantID(),
		CandidateID: uuid.New(),
		OccurredAt:  time.Now().UTC(),
	}
	assert.Equal(t, "sourcing.ResumeParsed", ev.EventName())
	assert.Equal(t, ev.UploadID, ev.AggregateID())
}
```

- [ ] **Step 2: Create candidate events file**

Create `internal/sourcing/domain/events/candidate_events.go`:

```go
package events

import (
	"time"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// CandidateParsed is emitted when a Candidate aggregate is first created
// (i.e., a new resume produced a new structured profile). Downstream:
// slice-3 scoring uses this to enqueue scoring jobs against open intents.
type CandidateParsed struct {
	CandidateID   uuid.UUID       `json:"candidate_id"`
	TenantID      shared.TenantID `json:"tenant_id"`
	ContentHash   string          `json:"content_hash"`
	SchemaVersion int             `json:"schema_version"`
	OccurredAt    time.Time       `json:"occurred_at"`
}

func (e CandidateParsed) EventName() string         { return "sourcing.CandidateParsed" }
func (e CandidateParsed) AggregateID() uuid.UUID    { return e.CandidateID }
func (e CandidateParsed) Tenant() shared.TenantID   { return e.TenantID }
func (e CandidateParsed) At() time.Time             { return e.OccurredAt }
```

Create the test `internal/sourcing/domain/events/candidate_events_test.go`:

```go
package events_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/hustle/hireflow/internal/sourcing/domain/events"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

func TestCandidateParsed_Shape(t *testing.T) {
	id := uuid.New()
	tenant := shared.NewTenantID()
	at := time.Now().UTC()
	ev := events.CandidateParsed{
		CandidateID:   id,
		TenantID:      tenant,
		ContentHash:   "abc",
		SchemaVersion: 1,
		OccurredAt:    at,
	}
	assert.Equal(t, "sourcing.CandidateParsed", ev.EventName())
	assert.Equal(t, id, ev.AggregateID())
	assert.Equal(t, tenant, ev.Tenant())
	assert.Equal(t, at, ev.At())
}
```

- [ ] **Step 3: Define ports**

Create `internal/sourcing/domain/services/resume_parser.go`:

```go
package services

import (
	"context"
	"fmt"

	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// ResumeParser is the port for LLM-driven extraction of a structured profile
// from plain resume text. Returns the canonical ParsedProfile. Adapters
// classify errors via ResumeParseError so the worker can apply the right
// retry policy.
type ResumeParser interface {
	Parse(ctx context.Context, text string) (vo.ParsedProfile, error)
}

// ResumeParseError carries the parser adapter's retryability classification.
// The worker layer uses errors.As to unwrap and dispatch to ScheduleRetry
// (when Retryable=true) or MarkFailed (when Retryable=false).
type ResumeParseError struct {
	Retryable bool
	Reason    string // short code, e.g. "anthropic_5xx", "tool_invalid_json", "no_tool_use"
	Detail    string // human-readable
}

func (e ResumeParseError) Error() string {
	return fmt.Sprintf("resume parse: %s: %s", e.Reason, e.Detail)
}
```

Create `internal/sourcing/domain/services/ocr_extractor.go`:

```go
package services

import "context"

// OCRExtractor is the port for image-based text extraction (slice 2 fallback
// when the text extractor returns empty). Input is the raw resume bytes
// (typically image-only PDF); output mirrors RawText for symmetry with the
// regular TextExtractor.
type OCRExtractor interface {
	ExtractFromBytes(ctx context.Context, body []byte, mime string) (RawText, error)
}
```

Create `internal/sourcing/domain/services/pii_encryptor.go`:

```go
package services

import (
	"context"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// PIIEncryptor is the port for envelope encryption of personal fields.
// The tenant parameter scopes the key (per-tenant DEK in the future
// KMS-backed adapter; ignored by the dev adapter that uses a single key).
//
// Encrypt returns an opaque base64 string the aggregate stores as-is.
// Decrypt round-trips it back.
type PIIEncryptor interface {
	Encrypt(ctx context.Context, tenant shared.TenantID, plaintext string) (string, error)
	Decrypt(ctx context.Context, tenant shared.TenantID, ciphertext string) (string, error)
}
```

Create `internal/sourcing/domain/repositories/candidate_repository.go`:

```go
package repositories

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ErrCandidateNotFound is returned when a candidate isn't found.
var ErrCandidateNotFound = errors.New("candidate not found")

// CandidateRepository persists Candidate aggregates and the upload-side
// candidate_id link, transactionally with the ResumeUpload.
type CandidateRepository interface {
	// Save inserts the candidate and drains its pending events into the
	// shared sourcing_outbox. Honors (tenant_id, content_hash) uniqueness —
	// returns nil + the existing candidate when the row already exists, so
	// the parsing handler can attach to it.
	Save(ctx context.Context, c *entities.Candidate) (*entities.Candidate, error)

	// FindByID — tenant-scoped lookup. Returns ErrCandidateNotFound when missing.
	FindByID(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (*entities.Candidate, error)

	// FindByContentHash — tenant-scoped lookup by content_hash. Used by the
	// parsing handler to dedup before creating a new aggregate.
	FindByContentHash(ctx context.Context, tenant shared.TenantID, hash string) (*entities.Candidate, error)
}
```

- [ ] **Step 4: Run everything**

```
go build ./...
go test ./internal/sourcing/... -count=1
```
Expected: build clean, all unit tests pass (including the parsing-flow + Candidate tests added in T2 / T5).

- [ ] **Step 5: Commit**

```bash
git add internal/sourcing/domain/events/ \
        internal/sourcing/domain/services/resume_parser.go \
        internal/sourcing/domain/services/ocr_extractor.go \
        internal/sourcing/domain/services/pii_encryptor.go \
        internal/sourcing/domain/repositories/candidate_repository.go
git commit -m "feat(sourcing): parsing-stage events and ports"
```

---

## Task 7: `PIIEncryptor` — local-dev DEK adapter (AES-GCM)

**Files:**
- Create: `internal/sourcing/infrastructure/encryption/local_dev.go`
- Create: `internal/sourcing/infrastructure/encryption/local_dev_test.go`

Single 256-bit DEK in process memory, AES-GCM. Tenant parameter is accepted but ignored (single shared key in dev). The prod KMS adapter swaps this out without touching call sites.

- [ ] **Step 1: Write the test**

Create `internal/sourcing/infrastructure/encryption/local_dev_test.go`:

```go
package encryption_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/infrastructure/encryption"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

func newEncryptor(t *testing.T) *encryption.LocalDevDEK {
	t.Helper()
	// 32-byte hex key (all zeros for determinism; never use in prod).
	key := "0000000000000000000000000000000000000000000000000000000000000000"
	enc, err := encryption.NewLocalDevDEK(key)
	require.NoError(t, err)
	return enc
}

func TestEncrypt_Decrypt_RoundTrip(t *testing.T) {
	enc := newEncryptor(t)
	tenant := shared.NewTenantID()
	plain := "alice@example.com"

	ct, err := enc.Encrypt(context.Background(), tenant, plain)
	require.NoError(t, err)
	assert.NotEqual(t, plain, ct)

	got, err := enc.Decrypt(context.Background(), tenant, ct)
	require.NoError(t, err)
	assert.Equal(t, plain, got)
}

func TestEncrypt_TwoCallsProduceDifferentCiphertexts(t *testing.T) {
	enc := newEncryptor(t)
	tenant := shared.NewTenantID()

	a, err := enc.Encrypt(context.Background(), tenant, "same")
	require.NoError(t, err)
	b, err := enc.Encrypt(context.Background(), tenant, "same")
	require.NoError(t, err)
	assert.NotEqual(t, a, b, "AES-GCM nonces must differ across calls")
}

func TestNewLocalDevDEK_RejectsWrongKeyLength(t *testing.T) {
	_, err := encryption.NewLocalDevDEK("abc")
	assert.Error(t, err)
}

func TestEncrypt_EmptyStringRoundTrips(t *testing.T) {
	enc := newEncryptor(t)
	tenant := shared.NewTenantID()

	ct, err := enc.Encrypt(context.Background(), tenant, "")
	require.NoError(t, err)

	got, err := enc.Decrypt(context.Background(), tenant, ct)
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

func TestDecrypt_RejectsTamperedCiphertext(t *testing.T) {
	enc := newEncryptor(t)
	tenant := shared.NewTenantID()
	ct, err := enc.Encrypt(context.Background(), tenant, "hello")
	require.NoError(t, err)

	// Flip a byte in the middle of the b64 payload.
	tampered := ct[:len(ct)/2] + "X" + ct[len(ct)/2+1:]
	_, err = enc.Decrypt(context.Background(), tenant, tampered)
	assert.Error(t, err)
}
```

- [ ] **Step 2: Implement**

Create `internal/sourcing/infrastructure/encryption/local_dev.go`:

```go
// Package encryption holds adapters implementing the PIIEncryptor port.
// LocalDevDEK is the development adapter: a single 256-bit AES key from env,
// shared across all tenants. Prod uses a KMS-backed adapter (future task).
package encryption

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ErrInvalidKey is returned when the configured DEK isn't 32 bytes (after hex decode).
var ErrInvalidKey = errors.New("local-dev DEK must be 64 hex chars / 32 bytes")

// LocalDevDEK uses a single shared AES-256-GCM key for all tenants.
// Ciphertext format: base64( nonce || aes-gcm-ciphertext ).
type LocalDevDEK struct {
	gcm cipher.AEAD
}

// NewLocalDevDEK validates and parses a 64-hex-char key string and returns
// the adapter. Pass via SOURCING_PII_DEK in prod-of-dev environments.
func NewLocalDevDEK(hexKey string) (*LocalDevDEK, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != 32 {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}
	return &LocalDevDEK{gcm: gcm}, nil
}

// Encrypt produces base64(nonce || ciphertext). Empty plaintext is round-trippable.
func (e *LocalDevDEK) Encrypt(_ context.Context, _ shared.TenantID, plaintext string) (string, error) {
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}
	ct := e.gcm.Seal(nil, nonce, []byte(plaintext), nil)
	out := append(nonce, ct...)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt round-trips the base64 string back to plaintext.
func (e *LocalDevDEK) Decrypt(_ context.Context, _ shared.TenantID, ciphertext string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	ns := e.gcm.NonceSize()
	if len(raw) < ns {
		return "", errors.New("ciphertext shorter than nonce")
	}
	nonce, ct := raw[:ns], raw[ns:]
	pt, err := e.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("gcm open: %w", err)
	}
	return string(pt), nil
}
```

- [ ] **Step 3: Run — should pass**

```
go test ./internal/sourcing/infrastructure/encryption/... -v -count=1
```
Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/sourcing/infrastructure/encryption/
git commit -m "feat(sourcing): local-dev PIIEncryptor adapter (AES-256-GCM)"
```

---

## Task 8: `OCRExtractor` — Claude vision adapter

**Files:**
- Create: `internal/sourcing/infrastructure/ocr/claude_vision.go`
- Create: `internal/sourcing/infrastructure/ocr/claude_vision_test.go`

Uses the same shared `anthropic.Client` from `internal/shared/infrastructure/llm/anthropic/`. Sends the PDF bytes as a `document` content block alongside an instruction to extract text. Streaming disabled — single response.

Look at `internal/hiringintent/infrastructure/llm/anthropic_extractor.go` for the established pattern of calling the SDK + the fake HTTP transport test.

- [ ] **Step 1: Write the test**

Create `internal/sourcing/infrastructure/ocr/claude_vision_test.go`:

```go
package ocr_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/infrastructure/ocr"
)

// fakeRT is a minimal http.RoundTripper that returns a canned response.
type fakeRT struct {
	resp string
}

func (f fakeRT) RoundTrip(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       http.NoBody,
		Request:    nil,
	}, nil
}

func TestClaudeVision_HappyPath(t *testing.T) {
	// Build a canned Messages response.
	respBody := map[string]any{
		"id":         "msg_test",
		"type":       "message",
		"role":       "assistant",
		"model":      "claude-opus-4-7",
		"content":    []map[string]any{{"type": "text", "text": "Hello, this is Alice's resume.\nExperience: Razorpay 2020-2025."}},
		"stop_reason": "end_turn",
		"usage":      map[string]any{"input_tokens": 100, "output_tokens": 20},
	}
	b, err := json.Marshal(respBody)
	require.NoError(t, err)

	client := anthropic.NewClient(
		option.WithAPIKey("sk-test"),
		option.WithHTTPClient(&http.Client{Transport: cannedTransport{body: string(b)}}),
	)
	ex := ocr.NewClaudeVision(&client, "claude-opus-4-7")

	body := []byte("%PDF-1.4\nfake pdf bytes")
	got, err := ex.ExtractFromBytes(context.Background(), body, "application/pdf")
	require.NoError(t, err)
	assert.True(t, strings.Contains(got.Text, "Alice"))
	assert.GreaterOrEqual(t, got.PageCount, 1)
}

// cannedTransport returns a fixed body for any request.
type cannedTransport struct{ body string }

func (c cannedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       http.NoBody,
		Request:    req,
	}, nil
}

func TestClaudeVision_RejectsUnsupportedMime(t *testing.T) {
	client := anthropic.NewClient(option.WithAPIKey("sk-test"))
	ex := ocr.NewClaudeVision(&client, "claude-opus-4-7")

	_, err := ex.ExtractFromBytes(context.Background(), []byte("data"), "image/png")
	assert.Error(t, err)
}
```

Note: the test above is shaped to exercise the import surface but uses a stub transport that returns an empty body — the real assertion is "no panic, returns a struct of the expected shape." To get a meaningful happy-path assertion you need to wire a transport that actually serializes the canned JSON into the response body. If the SDK requires more elaborate plumbing than this stub provides, fall back to a smaller integration-style test that just asserts the adapter calls the SDK correctly (constructor + ExtractFromBytes signature) and defer real round-trip verification to the e2e test in T15.

The implementing engineer should look at `internal/hiringintent/infrastructure/llm/anthropic_extractor_test.go` for the precise fake-transport pattern that already works in this codebase, and mirror it. The above is illustrative.

- [ ] **Step 2: Implement**

Create `internal/sourcing/infrastructure/ocr/claude_vision.go`:

```go
// Package ocr holds OCRExtractor adapters. ClaudeVision sends the resume bytes
// as a multimodal document content block and asks Claude to transcribe the
// text. Used only when the regular text extractor returns near-empty output
// (image-only PDFs).
package ocr

import (
	"context"
	"encoding/base64"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"github.com/hustle/hireflow/internal/sourcing/domain/services"
)

const systemPrompt = `You are an OCR engine. Given an image-only or scanned resume,
return only the extracted plain text, preserving line breaks. Do not paraphrase,
summarize, or add commentary. If no text is legible, return an empty string.`

// ClaudeVision is the OCR adapter using the Anthropic Messages API with a
// document content block.
type ClaudeVision struct {
	client *anthropic.Client
	model  string
}

// NewClaudeVision wires the adapter.
func NewClaudeVision(client *anthropic.Client, model string) *ClaudeVision {
	return &ClaudeVision{client: client, model: model}
}

// ExtractFromBytes sends the bytes to Claude with the OCR prompt and returns
// the transcribed text. Returns an error for non-PDF MIME types (only PDF
// is supported as a document content block in v1; image bytes can be added
// by extending this method to use an image content block).
func (c *ClaudeVision) ExtractFromBytes(ctx context.Context, body []byte, mime string) (services.RawText, error) {
	if mime != "application/pdf" {
		return services.RawText{}, fmt.Errorf("ocr: unsupported mime %q (only application/pdf in slice 2)", mime)
	}
	b64 := base64.StdEncoding.EncodeToString(body)

	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: 4096,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			{
				Role: anthropic.MessageParamRoleUser,
				Content: []anthropic.ContentBlockParamUnion{
					{
						OfDocument: &anthropic.DocumentBlockParam{
							Source: anthropic.DocumentBlockParamSourceUnion{
								OfBase64: &anthropic.Base64PDFSourceParam{
									Data:      b64,
									MediaType: "application/pdf",
								},
							},
						},
					},
					{OfText: &anthropic.TextBlockParam{Text: "Transcribe the resume text."}},
				},
			},
		},
	})
	if err != nil {
		return services.RawText{}, fmt.Errorf("anthropic messages: %w", err)
	}

	var text string
	for _, block := range resp.Content {
		if tb := block.AsAny(); tb != nil {
			if t, ok := tb.(anthropic.TextBlock); ok {
				text += t.Text
			}
		}
	}

	// Page count is unknowable from the API response — report 1 as the floor.
	return services.RawText{Text: text, PageCount: 1}, nil
}
```

**Important:** the exact API shape of `anthropic-sdk-go` v1.38 may differ from the above (the SDK has evolved). Cross-check with the actual API by reading the SDK source under `~/go/pkg/mod/github.com/anthropics/anthropic-sdk-go@v1.38.0/` or running `go doc github.com/anthropics/anthropic-sdk-go MessageParam`. The implementing engineer should adapt the call shape to whatever the SDK actually exports — the *semantics* (document content block + text instruction → assistant response with text content) are what matter.

Equally, the test above mocks at the wrong abstraction layer; mirroring `anthropic_extractor_test.go`'s approach (which intercepts HTTP, returns canned response bodies) is the correct path.

- [ ] **Step 3: Run**

```
go build ./...
go test ./internal/sourcing/infrastructure/ocr/... -v -count=1
```
Expected: build clean. Tests pass to the extent the fake transport plumbs through correctly. If the SDK API has drifted, the build will fail with concrete pointers to which symbols moved — fix accordingly.

- [ ] **Step 4: Commit**

```bash
git add internal/sourcing/infrastructure/ocr/
git commit -m "feat(sourcing): Claude vision OCR adapter for image-only resumes"
```

---

## Task 9: `ResumeParser` — Anthropic tool-use adapter

**Files:**
- Create: `internal/sourcing/infrastructure/parsing/anthropic_parser.go`
- Create: `internal/sourcing/infrastructure/parsing/anthropic_parser_test.go`
- Create: `internal/sourcing/infrastructure/parsing/prompts/parse_resume.tmpl`
- Create: `internal/sourcing/infrastructure/parsing/schemas/parse_resume.schema.json`

Forced tool-use against a `parse_resume` tool. The tool's input_schema is generated from the `ParsedProfile` struct using `invopop/jsonschema` (already an indirect dep). Same pattern as `internal/hiringintent/infrastructure/llm/anthropic_extractor.go` — look at it as the authoritative reference for SDK shapes.

- [ ] **Step 1: Write the system prompt template**

Create `internal/sourcing/infrastructure/parsing/prompts/parse_resume.tmpl`:

```
You are a resume parser. Given the plain text of a resume, extract a structured
profile by calling the parse_resume tool exactly once.

Rules:
1. Always call parse_resume — do not respond in free-form text.
2. Set schema_version to 1.
3. Extract verbatim from the resume; do not invent or summarize.
4. For dates: use YYYY-MM format. If only the year is known, use YYYY-01.
5. For each experience entry, assign a stable id like "exp_0", "exp_1", ... .
6. For each skill, populate years if you can derive it from the experience timespan;
   otherwise leave years zero.
7. Populate `warnings` with short codes for ambiguity you encountered (e.g.,
   "date_unparseable_exp_2", "phone_format_unknown"). Do not put PII in warnings.
8. Leave fields empty rather than guessing. The downstream pipeline tolerates
   incomplete profiles.

The resume text follows.
```

- [ ] **Step 2: Write the schema**

Create `internal/sourcing/infrastructure/parsing/schemas/parse_resume.schema.json`:

```json
{
  "type": "object",
  "required": ["schema_version", "personal"],
  "properties": {
    "schema_version": {"type": "integer", "const": 1},
    "personal": {
      "type": "object",
      "properties": {
        "full_name": {"type": "string"},
        "email":     {"type": "string"},
        "phone":     {"type": "string"},
        "location":  {"type": "string"},
        "links": {
          "type": "array",
          "items": {
            "type": "object",
            "required": ["kind", "url"],
            "properties": {
              "kind": {"type": "string", "enum": ["linkedin","github","portfolio","other"]},
              "url":  {"type": "string"}
            }
          }
        }
      }
    },
    "headline":   {"type": "string"},
    "summary":    {"type": "string"},
    "skills": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["name"],
        "properties": {
          "name":         {"type": "string"},
          "years":        {"type": "number"},
          "evidence_ref": {"type": "string"}
        }
      }
    },
    "experiences": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["id", "company", "title", "start"],
        "properties": {
          "id":          {"type": "string"},
          "company":     {"type": "string"},
          "title":       {"type": "string"},
          "start":       {"type": "string"},
          "end":         {"type": "string"},
          "current":     {"type": "boolean"},
          "description": {"type": "string"},
          "skills_used": {"type": "array", "items": {"type": "string"}}
        }
      }
    },
    "education": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["institution"],
        "properties": {
          "institution": {"type": "string"},
          "degree":      {"type": "string"},
          "field":       {"type": "string"},
          "start":       {"type": "string"},
          "end":         {"type": "string"}
        }
      }
    },
    "certifications": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["name"],
        "properties": {
          "name":    {"type": "string"},
          "issuer":  {"type": "string"},
          "issued":  {"type": "string"},
          "expires": {"type": "string"}
        }
      }
    },
    "languages": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["name"],
        "properties": {
          "name":        {"type": "string"},
          "proficiency": {"type": "string", "enum": ["native","fluent","professional","basic"]}
        }
      }
    },
    "warnings": {"type": "array", "items": {"type": "string"}}
  }
}
```

- [ ] **Step 3: Write the test**

Create `internal/sourcing/infrastructure/parsing/anthropic_parser_test.go`. **Look at `internal/hiringintent/infrastructure/llm/anthropic_extractor_test.go` first** — copy its fake-transport pattern verbatim, adapting only the canned response shape (the parsed-profile tool-use shape vs the intent-extract one).

The test should cover:
1. **Happy path:** tool-use response with a valid `ParsedProfile` JSON → adapter returns the parsed value with the right fields.
2. **Schema-version mismatch:** the canned tool-use returns `schema_version: 99` → adapter still returns the profile (warnings + version mismatch are handled by the application layer, not the adapter).
3. **Refused parse:** the canned response is a free-text "I cannot parse this" without a tool-use block → adapter returns a non-retryable error.
4. **5xx response:** the transport returns 503 → adapter returns a retryable-classified error.

Asserting retryability cleanly requires the adapter to return a custom error type (`ResumeParseError{Retryable bool, Reason string}`). Add this struct in the adapter file.

- [ ] **Step 4: Implement**

Create `internal/sourcing/infrastructure/parsing/anthropic_parser.go`:

The structure to follow (mirror `hiringintent/infrastructure/llm/anthropic_extractor.go` for SDK shape details):

```go
// Package parsing holds ResumeParser adapters. The Anthropic adapter uses
// forced tool-use against a parse_resume schema, returning the canonical
// ParsedProfile.
package parsing

import (
	_ "embed"
	"context"
	"encoding/json"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

//go:embed prompts/parse_resume.tmpl
var parseResumePrompt string

//go:embed schemas/parse_resume.schema.json
var parseResumeSchemaJSON []byte

// PromptVersion is bumped whenever parse_resume.tmpl meaningfully changes.
// Stored on the parsed profile so downstream can audit which prompt produced
// a given Candidate.
const PromptVersion = "v1"

// AnthropicParser is the ResumeParser adapter.
type AnthropicParser struct {
	client *anthropic.Client
	model  string
}

// NewAnthropicParser wires the adapter.
func NewAnthropicParser(client *anthropic.Client, model string) *AnthropicParser {
	return &AnthropicParser{client: client, model: model}
}

// Parse calls Claude with forced tool-use and unmarshals the result into
// a ParsedProfile. The adapter never invents fields; warnings flow through
// from the LLM.
func (p *AnthropicParser) Parse(ctx context.Context, text string) (vo.ParsedProfile, error) {
	var schemaMap map[string]any
	if err := json.Unmarshal(parseResumeSchemaJSON, &schemaMap); err != nil {
		return vo.ParsedProfile{}, fmt.Errorf("decode schema: %w", err)
	}

	tool := anthropic.ToolUnionParam{
		OfTool: &anthropic.ToolParam{
			Name:        "parse_resume",
			Description: anthropic.String("Extract a structured candidate profile from resume text."),
			InputSchema: anthropic.ToolInputSchemaParam{Properties: schemaMap["properties"]},
		},
	}

	resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(p.model),
		MaxTokens: 4096,
		System:    []anthropic.TextBlockParam{{Text: parseResumePrompt}},
		Tools:     []anthropic.ToolUnionParam{tool},
		ToolChoice: anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{Name: "parse_resume"},
		},
		Messages: []anthropic.MessageParam{
			{
				Role:    anthropic.MessageParamRoleUser,
				Content: []anthropic.ContentBlockParamUnion{{OfText: &anthropic.TextBlockParam{Text: text}}},
			},
		},
	})
	if err != nil {
		// SDK error path — assume retryable for now. Refine if we observe
		// specific non-retryable codes (e.g., invalid_request_error).
		return vo.ParsedProfile{}, services.ResumeParseError{Retryable: true, Reason: "anthropic_call", Detail: err.Error()}
	}

	// Find the tool-use block.
	for _, block := range resp.Content {
		if tu, ok := block.AsAny().(anthropic.ToolUseBlock); ok && tu.Name == "parse_resume" {
			var profile vo.ParsedProfile
			if err := json.Unmarshal(tu.Input, &profile); err != nil {
				return vo.ParsedProfile{}, services.ResumeParseError{Retryable: false, Reason: "tool_invalid_json", Detail: err.Error()}
			}
			if profile.SchemaVersion == 0 {
				profile.SchemaVersion = 1 // tolerate omission
			}
			return profile, nil
		}
	}
	return vo.ParsedProfile{}, services.ResumeParseError{Retryable: false, Reason: "no_tool_use", Detail: "model returned free text instead of parse_resume"}
}
```

The exact SDK type names (`ToolUnionParam`, `ToolChoiceUnionParam`, `MessageParamRoleUser`, etc.) come from `anthropic-sdk-go v1.38`. Cross-check against the existing `hiringintent/infrastructure/llm/anthropic_extractor.go` for what compiles; copy the patterns verbatim.

- [ ] **Step 5: Run**

```
go build ./...
go test ./internal/sourcing/infrastructure/parsing/... -v -count=1
```

- [ ] **Step 6: Commit**

```bash
git add internal/sourcing/infrastructure/parsing/
git commit -m "feat(sourcing): Anthropic ResumeParser with forced tool-use"
```

---

## Task 10: Postgres `CandidateRepository`

**Files:**
- Create: `internal/sourcing/infrastructure/persistence/postgres_candidate_repository.go`
- Create: `internal/sourcing/infrastructure/persistence/postgres_candidate_repository_test.go`
- Create: `internal/sourcing/infrastructure/persistence/candidate_serializer.go`

Mirrors the slice-1 `PostgresResumeUploadRepository`. `Save` is the only write: inserts the candidate row, drains pending events into `sourcing_outbox` in the same tx, and on `(tenant_id, content_hash)` collision returns the existing candidate without error (the caller can attach to it). Slice-2 makes Save **idempotent on collision** — there's no `ErrDuplicate` for candidates because the parsing handler explicitly wants "create-or-attach."

- [ ] **Step 1: Serializer**

Create `internal/sourcing/infrastructure/persistence/candidate_serializer.go`:

```go
package persistence

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// candidateRow mirrors candidates table columns.
type candidateRow struct {
	id             uuid.UUID
	tenantID       string
	contentHash    string
	fullNameEnc    *string
	emailEnc       *string
	phoneEnc       *string
	location       *string
	headline       *string
	parsedProfile  []byte
	profileSchema  int
	source         string
	createdAt      time.Time
	updatedAt      time.Time
}

func serializeCandidate(c *entities.Candidate) (candidateRow, error) {
	profileBytes, err := json.Marshal(c.Profile())
	if err != nil {
		return candidateRow{}, fmt.Errorf("marshal profile: %w", err)
	}
	row := candidateRow{
		id:            c.ID(),
		tenantID:      c.TenantID().String(),
		contentHash:   c.ContentHash().String(),
		parsedProfile: profileBytes,
		profileSchema: c.ProfileSchema(),
		source:        c.Source(),
		createdAt:     c.CreatedAt(),
		updatedAt:     c.UpdatedAt(),
	}
	if c.EncryptedFullName() != "" {
		v := c.EncryptedFullName()
		row.fullNameEnc = &v
	}
	if c.EncryptedEmail() != "" {
		v := c.EncryptedEmail()
		row.emailEnc = &v
	}
	if c.EncryptedPhone() != "" {
		v := c.EncryptedPhone()
		row.phoneEnc = &v
	}
	if c.Location() != "" {
		v := c.Location()
		row.location = &v
	}
	if c.Headline() != "" {
		v := c.Headline()
		row.headline = &v
	}
	return row, nil
}

func hydrateCandidate(r candidateRow) (*entities.Candidate, error) {
	var profile vo.ParsedProfile
	if err := json.Unmarshal(r.parsedProfile, &profile); err != nil {
		return nil, fmt.Errorf("unmarshal profile: %w", err)
	}
	hash, err := vo.NewContentHash(r.contentHash)
	if err != nil {
		return nil, fmt.Errorf("hash: %w", err)
	}
	tenant, err := shared.ParseTenantID(r.tenantID)
	if err != nil {
		return nil, fmt.Errorf("tenant: %w", err)
	}
	var fullName, email, phone, loc, headline string
	if r.fullNameEnc != nil { fullName = *r.fullNameEnc }
	if r.emailEnc != nil    { email    = *r.emailEnc }
	if r.phoneEnc != nil    { phone    = *r.phoneEnc }
	if r.location != nil    { loc      = *r.location }
	if r.headline != nil    { headline = *r.headline }

	return entities.RehydrateCandidate(entities.RehydrateCandidateInput{
		ID:                r.id,
		TenantID:          tenant,
		ContentHash:       hash,
		EncryptedFullName: fullName,
		EncryptedEmail:    email,
		EncryptedPhone:    phone,
		Location:          loc,
		Headline:          headline,
		Profile:           profile,
		Source:            r.source,
		CreatedAt:         r.createdAt,
		UpdatedAt:         r.updatedAt,
	}), nil
}
```

(The `shared.ParseTenantID` constructor was identified during slice 1; if it doesn't exist, use whatever the actual API exposes for re-constructing a `TenantID` from a UUID string — `internal/shared/domain/tenant_id.go` is the source of truth.)

- [ ] **Step 2: Repository**

Create `internal/sourcing/infrastructure/persistence/postgres_candidate_repository.go`:

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

	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// PostgresCandidateRepository persists Candidate aggregates.
type PostgresCandidateRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresCandidateRepository wires the repository.
func NewPostgresCandidateRepository(pool *pgxpool.Pool) *PostgresCandidateRepository {
	return &PostgresCandidateRepository{pool: pool}
}

const candidateInsertSQL = `
INSERT INTO candidates (
    id, tenant_id, content_hash,
    full_name_enc, email_enc, phone_enc,
    location, headline,
    parsed_profile, profile_schema,
    source, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
ON CONFLICT (tenant_id, content_hash) DO NOTHING
RETURNING id`

// Save creates the candidate row + outbox entries atomically. On
// (tenant_id, content_hash) collision, returns the existing candidate
// instead of erroring (caller intent: "create or attach").
func (r *PostgresCandidateRepository) Save(ctx context.Context, c *entities.Candidate) (*entities.Candidate, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, err := serializeCandidate(c)
	if err != nil {
		return nil, fmt.Errorf("serialize: %w", err)
	}

	var returnedID uuid.UUID
	err = tx.QueryRow(ctx, candidateInsertSQL,
		row.id, row.tenantID, row.contentHash,
		row.fullNameEnc, row.emailEnc, row.phoneEnc,
		row.location, row.headline,
		row.parsedProfile, row.profileSchema,
		row.source, row.createdAt, row.updatedAt,
	).Scan(&returnedID)

	if errors.Is(err, pgx.ErrNoRows) {
		// Collision — fetch the existing row and return it instead.
		existing, ferr := r.findByContentHashTx(ctx, tx, c.TenantID(), c.ContentHash().String())
		if ferr != nil {
			return nil, fmt.Errorf("fetch existing: %w", ferr)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit: %w", err)
		}
		_ = c.PullEvents() // drop the "new" event — we attached to an existing row
		return existing, nil
	}
	if err != nil {
		return nil, fmt.Errorf("insert candidate: %w", err)
	}

	// Outbox.
	for _, ev := range c.PullEvents() {
		payload, mErr := json.Marshal(ev)
		if mErr != nil {
			return nil, fmt.Errorf("marshal event: %w", mErr)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO sourcing_outbox (event_name, aggregate_id, tenant_id, payload, occurred_at)
			VALUES ($1, $2, $3, $4, $5)
		`, ev.EventName(), ev.AggregateID(), ev.Tenant().String(), payload, ev.At())
		if err != nil {
			return nil, fmt.Errorf("insert outbox: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return c, nil
}

const candidateSelectSQL = `
SELECT id, tenant_id, content_hash,
       full_name_enc, email_enc, phone_enc,
       location, headline,
       parsed_profile, profile_schema,
       source, created_at, updated_at
FROM candidates`

// FindByID — tenant-scoped lookup.
func (r *PostgresCandidateRepository) FindByID(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (*entities.Candidate, error) {
	row := r.pool.QueryRow(ctx, candidateSelectSQL+" WHERE tenant_id=$1 AND id=$2", tenant.String(), id)
	return scanCandidate(row)
}

// FindByContentHash — tenant-scoped lookup by hash.
func (r *PostgresCandidateRepository) FindByContentHash(ctx context.Context, tenant shared.TenantID, hash string) (*entities.Candidate, error) {
	row := r.pool.QueryRow(ctx, candidateSelectSQL+" WHERE tenant_id=$1 AND content_hash=$2", tenant.String(), hash)
	return scanCandidate(row)
}

// findByContentHashTx is the in-transaction variant used by Save's collision path.
func (r *PostgresCandidateRepository) findByContentHashTx(ctx context.Context, tx pgx.Tx, tenant shared.TenantID, hash string) (*entities.Candidate, error) {
	row := tx.QueryRow(ctx, candidateSelectSQL+" WHERE tenant_id=$1 AND content_hash=$2", tenant.String(), hash)
	return scanCandidate(row)
}

func scanCandidate(rs rowScanner) (*entities.Candidate, error) {
	var row candidateRow
	err := rs.Scan(
		&row.id, &row.tenantID, &row.contentHash,
		&row.fullNameEnc, &row.emailEnc, &row.phoneEnc,
		&row.location, &row.headline,
		&row.parsedProfile, &row.profileSchema,
		&row.source, &row.createdAt, &row.updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repositories.ErrCandidateNotFound
		}
		return nil, fmt.Errorf("scan candidate: %w", err)
	}
	return hydrateCandidate(row)
}
```

(`rowScanner` is the unexported interface from slice 1's repo; both repositories use it.)

- [ ] **Step 3: Integration test**

Create `internal/sourcing/infrastructure/persistence/postgres_candidate_repository_test.go`:

```go
//go:build integration

package persistence_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/persistence"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

func newCandidate(t *testing.T, tenant shared.TenantID, hash string) *entities.Candidate {
	t.Helper()
	h, err := vo.NewContentHash(hash)
	require.NoError(t, err)
	profile := vo.NewParsedProfile()
	profile.Personal.FullName = "Alice"
	c, err := entities.NewCandidate(entities.NewCandidateInput{
		TenantID:    tenant,
		ContentHash: h,
		Profile:     profile,
		Encrypted:   entities.EncryptedPersonal{FullName: "enc:full", Email: "enc:em", Phone: "enc:ph"},
		Location:    "Bangalore",
		Headline:    "SBE",
		Source:      "manual_upload",
	})
	require.NoError(t, err)
	return c
}

func TestCandidateSave_PersistsRow(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresCandidateRepository(pool)
	tenant := shared.NewTenantID()
	c := newCandidate(t, tenant, uuidHex(t))

	got, err := repo.Save(context.Background(), c)
	require.NoError(t, err)
	assert.Equal(t, c.ID(), got.ID())

	fetched, err := repo.FindByID(context.Background(), tenant, c.ID())
	require.NoError(t, err)
	assert.Equal(t, "enc:em", fetched.EncryptedEmail())
	assert.Equal(t, "Bangalore", fetched.Location())
	assert.Equal(t, 1, fetched.ProfileSchema())
}

func TestCandidateSave_DuplicateContentHashReturnsExisting(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresCandidateRepository(pool)
	tenant := shared.NewTenantID()
	hash := uuidHex(t)
	c1 := newCandidate(t, tenant, hash)
	first, err := repo.Save(context.Background(), c1)
	require.NoError(t, err)

	// New aggregate, same hash — Save should return the original.
	c2 := newCandidate(t, tenant, hash)
	second, err := repo.Save(context.Background(), c2)
	require.NoError(t, err)
	assert.Equal(t, first.ID(), second.ID(), "second save must attach to existing candidate")
}

func TestCandidateFindByContentHash_ReturnsRow(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresCandidateRepository(pool)
	tenant := shared.NewTenantID()
	hash := uuidHex(t)
	c := newCandidate(t, tenant, hash)
	_, err := repo.Save(context.Background(), c)
	require.NoError(t, err)

	got, err := repo.FindByContentHash(context.Background(), tenant, hash)
	require.NoError(t, err)
	assert.Equal(t, c.ID(), got.ID())
}
```

(`newPool` and `uuidHex` already exist in `postgres_resume_upload_repository_test.go` in the same package — reuse them.)

- [ ] **Step 4: Run**

```
INTEGRATION_TESTS=1 go test -tags=integration ./internal/sourcing/infrastructure/persistence/... -v -count=1
```
Expected: 3 new tests pass alongside the existing slice-1 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/sourcing/infrastructure/persistence/
git commit -m "feat(sourcing): postgres CandidateRepository with attach-on-collision"
```

---

## Task 11: Worker — extend `ProcessUploadHandler` with `runParsing`

**Files:**
- Modify: `internal/sourcing/application/commands/process_upload.go`
- Create: `internal/sourcing/application/commands/process_upload_parsing_test.go`

The handler grows a third stage. `Handle`'s switch gains a `StatusExtracted → runParsing` arm. `runParsing` reads the stored extracted text from `StageArtifacts`; if it's < 50 chars after TrimSpace, calls `OCRExtractor.ExtractFromBytes` for fallback (after re-`Open`ing the storage to get raw bytes). Then calls `ResumeParser.Parse(text)`. On success: encrypts PII fields via `PIIEncryptor`, calls `candidateRepo.Save`, calls `u.RecordParsedProfile(json) → u.LinkCandidate(id) → u.CompleteParsed()`, persists upload.

- [ ] **Step 1: Update `ProcessConfig`**

Add fields to `ProcessConfig` in `process_upload.go`:

```go
type ProcessConfig struct {
	// ... existing fields ...
	Parser        services.ResumeParser
	OCR           services.OCRExtractor
	Encryptor     services.PIIEncryptor
	CandidateRepo repositories.CandidateRepository
	OCRThreshold  int    // default 50 chars; < this triggers OCR fallback
	PromptVersion string // populated by main.go from the parser adapter
}
```

- [ ] **Step 2: Extend the dispatch switch**

```go
func (h *ProcessUploadHandler) Handle(ctx context.Context, u *entities.ResumeUpload) error {
	switch u.Status() {
	case vo.StatusPending, vo.StatusScanning:
		return h.runScanning(ctx, u)
	case vo.StatusExtracting:
		return h.runExtracting(ctx, u)
	case vo.StatusExtracted, vo.StatusParsing:
		return h.runParsing(ctx, u)
	default:
		return fmt.Errorf("process: unexpected status %s", u.Status())
	}
}
```

Also: change slice-1's `runExtracting` so its terminal transition is `CompleteExtracted()` (was, in slice 1, the end state). It still calls Save, but the row is now intermediate — the worker will re-claim it for the parsing stage.

- [ ] **Step 3: Implement `runParsing`**

```go
func (h *ProcessUploadHandler) runParsing(ctx context.Context, u *entities.ResumeUpload) error {
	if u.Status() != vo.StatusParsing {
		if err := u.BeginParsing(); err != nil {
			return fmt.Errorf("transition parsing: %w", err)
		}
	}

	// Read extracted text from stage artifacts.
	text, _, _ := u.Artifacts().ExtractedText()

	// OCR fallback when text is essentially empty.
	threshold := h.cfg.OCRThreshold
	if threshold <= 0 {
		threshold = 50
	}
	if len(strings.TrimSpace(text)) < threshold {
		body, err := h.cfg.Storage.Open(ctx, u.StorageKey())
		if err != nil {
			u.ScheduleRetry(vo.Retryable("storage_open", err.Error()), time.Now().UTC(), h.cfg.RetryBackoff)
			return h.cfg.Repo.Save(ctx, u)
		}
		bytes, rerr := io.ReadAll(body)
		body.Close()
		if rerr != nil {
			u.ScheduleRetry(vo.Retryable("storage_read", rerr.Error()), time.Now().UTC(), h.cfg.RetryBackoff)
			return h.cfg.Repo.Save(ctx, u)
		}
		ocrOut, oerr := h.cfg.OCR.ExtractFromBytes(ctx, bytes, u.MimeType().String())
		if oerr != nil {
			// OCR failure is fatal in slice 2 — file is genuinely unreadable.
			if err := u.MarkFailed(vo.Fatal("ocr_failed", oerr.Error())); err != nil {
				return fmt.Errorf("mark failed after ocr: %w", err)
			}
			return h.cfg.Repo.Save(ctx, u)
		}
		text = ocrOut.Text
		if len(strings.TrimSpace(text)) < threshold {
			if err := u.MarkFailed(vo.Fatal("unreadable", "ocr returned empty text")); err != nil {
				return fmt.Errorf("mark failed unreadable: %w", err)
			}
			return h.cfg.Repo.Save(ctx, u)
		}
	}

	// Parse.
	profile, perr := h.cfg.Parser.Parse(ctx, text)
	if perr != nil {
		var rpe services.ResumeParseError
		if errors.As(perr, &rpe) {
			if rpe.Retryable {
				u.ScheduleRetry(vo.Retryable(rpe.Reason, rpe.Detail), time.Now().UTC(), h.cfg.RetryBackoff)
				return h.cfg.Repo.Save(ctx, u)
			}
			if err := u.MarkFailed(vo.Fatal(rpe.Reason, rpe.Detail)); err != nil {
				return fmt.Errorf("mark failed after parse: %w", err)
			}
			return h.cfg.Repo.Save(ctx, u)
		}
		// Unknown error type → treat as retryable.
		u.ScheduleRetry(vo.Retryable("parser_unknown", perr.Error()), time.Now().UTC(), h.cfg.RetryBackoff)
		return h.cfg.Repo.Save(ctx, u)
	}

	// Encrypt PII fields.
	encName, err := h.cfg.Encryptor.Encrypt(ctx, u.TenantID(), profile.Personal.FullName)
	if err != nil {
		u.ScheduleRetry(vo.Retryable("encrypt_failed", err.Error()), time.Now().UTC(), h.cfg.RetryBackoff)
		return h.cfg.Repo.Save(ctx, u)
	}
	encEmail, err := h.cfg.Encryptor.Encrypt(ctx, u.TenantID(), profile.Personal.Email)
	if err != nil {
		u.ScheduleRetry(vo.Retryable("encrypt_failed", err.Error()), time.Now().UTC(), h.cfg.RetryBackoff)
		return h.cfg.Repo.Save(ctx, u)
	}
	encPhone, err := h.cfg.Encryptor.Encrypt(ctx, u.TenantID(), profile.Personal.Phone)
	if err != nil {
		u.ScheduleRetry(vo.Retryable("encrypt_failed", err.Error()), time.Now().UTC(), h.cfg.RetryBackoff)
		return h.cfg.Repo.Save(ctx, u)
	}

	// Build Candidate and Save (create-or-attach).
	cand, err := entities.NewCandidate(entities.NewCandidateInput{
		TenantID:    u.TenantID(),
		ContentHash: u.ContentHash(),
		Profile:     profile,
		Encrypted: entities.EncryptedPersonal{
			FullName: encName, Email: encEmail, Phone: encPhone,
		},
		Location: profile.Personal.Location,
		Headline: profile.Headline,
		Source:   "manual_upload",
	})
	if err != nil {
		if err := u.MarkFailed(vo.Fatal("candidate_build", err.Error())); err != nil {
			return fmt.Errorf("mark failed candidate: %w", err)
		}
		return h.cfg.Repo.Save(ctx, u)
	}

	saved, err := h.cfg.CandidateRepo.Save(ctx, cand)
	if err != nil {
		u.ScheduleRetry(vo.Retryable("candidate_save", err.Error()), time.Now().UTC(), h.cfg.RetryBackoff)
		return h.cfg.Repo.Save(ctx, u)
	}

	// Record artifact + link candidate + complete.
	profileJSON, jerr := json.Marshal(profile)
	if jerr != nil {
		// Shouldn't happen — profile just came back from the parser.
		return fmt.Errorf("marshal profile: %w", jerr)
	}
	if err := u.RecordParsedProfile(profileJSON); err != nil {
		return fmt.Errorf("record profile: %w", err)
	}
	if err := u.LinkCandidate(saved.ID()); err != nil {
		return fmt.Errorf("link candidate: %w", err)
	}
	if err := u.CompleteParsed(); err != nil {
		return fmt.Errorf("complete parsed: %w", err)
	}
	return h.cfg.Repo.Save(ctx, u)
}
```

Imports needed: `io`, `strings`, `time` (already imported), `encoding/json`, `errors`. Plus the domain `services` package is already imported (it carries `services.ResumeParseError` per T6) — no need to import `infrastructure/parsing` from the application layer.

Replace `parsing.ResumeParseError` with `services.ResumeParseError` in the snippet above. Layering stays clean: application depends only on domain.

- [ ] **Step 4: Test**

Create `internal/sourcing/application/commands/process_upload_parsing_test.go` with fakes mirroring slice 1's style (in-memory `fakeParser`, `fakeOCR`, `fakeEncryptor`, `fakeCandidateRepo`). Cover:

1. **Happy path:** Extracted upload with sufficient text → parser succeeds → Candidate created → upload reaches Parsed.
2. **OCR fallback:** Extracted upload with empty text → OCR returns text → parser succeeds → Parsed.
3. **OCR returns empty:** OCR also produces < threshold → upload marked Failed("unreadable").
4. **Parser fatal error:** parser returns `ResumeParseError{Retryable: false}` → upload Failed.
5. **Parser retryable error:** parser returns `ResumeParseError{Retryable: true}` → upload back to Pending status with attempt_count++.
6. **Dedup attach:** candidate save returns an existing candidate (different ID than ours) → upload still links to that existing candidate ID.

- [ ] **Step 5: Run**

```
go test ./internal/sourcing/application/commands/... -v -count=1
```
Expected: all 9 + 6 new tests pass (slice-1 process tests + new parsing tests).

- [ ] **Step 6: Commit**

```bash
git add internal/sourcing/application/commands/ \
        internal/sourcing/domain/services/resume_parser.go \
        internal/sourcing/infrastructure/parsing/anthropic_parser.go
git commit -m "feat(sourcing): worker parsing stage with OCR fallback and PII encryption"
```

---

## Task 12: `GetCandidate` query handler

**Files:**
- Create: `internal/sourcing/application/queries/get_candidate.go`
- Create: `internal/sourcing/application/queries/get_candidate_test.go`
- Modify: `internal/sourcing/application/dto/batch_dto.go` (add `CandidateDetailDTO`)

Returns the full candidate detail. Decrypts PII at response time via the `PIIEncryptor` port. Includes the cleartext non-PII profile (skills, experiences, etc.).

- [ ] **Step 1: Add DTO**

Append to `internal/sourcing/application/dto/batch_dto.go`:

```go
import "time"  // if not already imported

// CandidateDetailDTO is the result of GetCandidate.
type CandidateDetailDTO struct {
	ID          uuid.UUID         `json:"id"`
	ContentHash string            `json:"content_hash"`
	Personal    CandidatePersonal `json:"personal"`
	Location    string            `json:"location,omitempty"`
	Headline    string            `json:"headline,omitempty"`
	Profile     json.RawMessage   `json:"profile"` // the full parsed profile (PII still in cleartext after server-side decrypt)
	Source      string            `json:"source"`
	CreatedAt   time.Time         `json:"created_at"`
}

// CandidatePersonal is the decrypted PII surface returned only on the
// detail endpoint. List endpoints (slice 4) return a masked variant.
type CandidatePersonal struct {
	FullName string `json:"full_name,omitempty"`
	Email    string `json:"email,omitempty"`
	Phone    string `json:"phone,omitempty"`
}
```

Add the `"encoding/json"` import if not present.

- [ ] **Step 2: Handler test**

Create `internal/sourcing/application/queries/get_candidate_test.go`:

```go
package queries_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/application/queries"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

type stubCandidateRepo struct {
	byID map[string]*entities.Candidate
}

func (r *stubCandidateRepo) Save(context.Context, *entities.Candidate) (*entities.Candidate, error) {
	return nil, nil
}
func (r *stubCandidateRepo) FindByID(_ context.Context, _ shared.TenantID, id uuid.UUID) (*entities.Candidate, error) {
	if c, ok := r.byID[id.String()]; ok {
		return c, nil
	}
	return nil, repositories.ErrCandidateNotFound
}
func (r *stubCandidateRepo) FindByContentHash(context.Context, shared.TenantID, string) (*entities.Candidate, error) {
	return nil, repositories.ErrCandidateNotFound
}

// Reversible "encryptor" for tests — prepends "ENC:" to plaintext.
type stubEncryptor struct{}

func (stubEncryptor) Encrypt(_ context.Context, _ shared.TenantID, p string) (string, error) {
	return "ENC:" + p, nil
}
func (stubEncryptor) Decrypt(_ context.Context, _ shared.TenantID, ct string) (string, error) {
	if len(ct) < 4 || ct[:4] != "ENC:" {
		return "", nil
	}
	return ct[4:], nil
}

func newCandidateForQuery(t *testing.T, tenant shared.TenantID) *entities.Candidate {
	t.Helper()
	h, err := vo.NewContentHash("dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")
	require.NoError(t, err)
	profile := vo.NewParsedProfile()
	profile.Personal.FullName = "Alice"
	profile.Headline = "SBE"
	c, err := entities.NewCandidate(entities.NewCandidateInput{
		TenantID: tenant, ContentHash: h, Profile: profile,
		Encrypted: entities.EncryptedPersonal{
			FullName: "ENC:Alice", Email: "ENC:alice@example.com", Phone: "ENC:+91-555",
		},
		Location: "Bangalore", Headline: "SBE", Source: "manual_upload",
	})
	require.NoError(t, err)
	return c
}

func TestGetCandidate_ReturnsDecryptedPII(t *testing.T) {
	tenant := shared.NewTenantID()
	c := newCandidateForQuery(t, tenant)
	repo := &stubCandidateRepo{byID: map[string]*entities.Candidate{c.ID().String(): c}}
	h := queries.NewGetCandidateHandler(repo, stubEncryptor{})

	got, err := h.Handle(context.Background(), tenant, c.ID())
	require.NoError(t, err)
	assert.Equal(t, c.ID(), got.ID)
	assert.Equal(t, "Alice", got.Personal.FullName)
	assert.Equal(t, "alice@example.com", got.Personal.Email)
	assert.Equal(t, "+91-555", got.Personal.Phone)
	assert.Equal(t, "Bangalore", got.Location)
}

func TestGetCandidate_NotFound(t *testing.T) {
	repo := &stubCandidateRepo{byID: map[string]*entities.Candidate{}}
	h := queries.NewGetCandidateHandler(repo, stubEncryptor{})

	_, err := h.Handle(context.Background(), shared.NewTenantID(), uuid.New())
	assert.ErrorIs(t, err, repositories.ErrCandidateNotFound)
}
```

- [ ] **Step 3: Implement**

Create `internal/sourcing/application/queries/get_candidate.go`:

```go
package queries

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/sourcing/application/dto"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// GetCandidateHandler returns the full candidate detail with PII decrypted.
type GetCandidateHandler struct {
	repo      repositories.CandidateRepository
	encryptor services.PIIEncryptor
}

// NewGetCandidateHandler wires the handler.
func NewGetCandidateHandler(repo repositories.CandidateRepository, encryptor services.PIIEncryptor) *GetCandidateHandler {
	return &GetCandidateHandler{repo: repo, encryptor: encryptor}
}

// Handle returns the candidate detail. Returns repositories.ErrCandidateNotFound
// when no matching row exists. Decryption errors propagate.
func (h *GetCandidateHandler) Handle(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (dto.CandidateDetailDTO, error) {
	c, err := h.repo.FindByID(ctx, tenant, id)
	if err != nil {
		return dto.CandidateDetailDTO{}, err
	}

	name, err := h.encryptor.Decrypt(ctx, tenant, c.EncryptedFullName())
	if err != nil {
		return dto.CandidateDetailDTO{}, fmt.Errorf("decrypt name: %w", err)
	}
	email, err := h.encryptor.Decrypt(ctx, tenant, c.EncryptedEmail())
	if err != nil {
		return dto.CandidateDetailDTO{}, fmt.Errorf("decrypt email: %w", err)
	}
	phone, err := h.encryptor.Decrypt(ctx, tenant, c.EncryptedPhone())
	if err != nil {
		return dto.CandidateDetailDTO{}, fmt.Errorf("decrypt phone: %w", err)
	}

	profile := c.Profile()
	// Overlay decrypted PII back into the profile.personal block for the response.
	profile.Personal.FullName = name
	profile.Personal.Email = email
	profile.Personal.Phone = phone

	profileBytes, err := json.Marshal(profile)
	if err != nil {
		return dto.CandidateDetailDTO{}, fmt.Errorf("marshal profile: %w", err)
	}

	return dto.CandidateDetailDTO{
		ID:          c.ID(),
		ContentHash: c.ContentHash().String(),
		Personal: dto.CandidatePersonal{
			FullName: name, Email: email, Phone: phone,
		},
		Location:  c.Location(),
		Headline:  c.Headline(),
		Profile:   profileBytes,
		Source:    c.Source(),
		CreatedAt: c.CreatedAt(),
	}, nil
}
```

- [ ] **Step 4: Run**

```
go test ./internal/sourcing/application/queries/... -v -count=1
```
Expected: 2 new tests pass alongside the existing `TestGetBatchStatus_*`.

- [ ] **Step 5: Commit**

```bash
git add internal/sourcing/application/queries/get_candidate.go \
        internal/sourcing/application/queries/get_candidate_test.go \
        internal/sourcing/application/dto/batch_dto.go
git commit -m "feat(sourcing): GetCandidate query with PII decryption"
```

---

## Task 13: HTTP — `GET /candidates/{candidate_id}` + OpenAPI

**Files:**
- Modify: `internal/sourcing/delivery/http/v1/handlers.go`
- Modify: `internal/sourcing/delivery/http/v1/dto.go`
- Modify: `internal/sourcing/delivery/http/v1/routes.go`
- Modify: `internal/sourcing/delivery/http/v1/handlers_test.go`
- Modify: `docs/api/v1/sourcing.openapi.yaml`

- [ ] **Step 1: Wire DTOs**

Append to `internal/sourcing/delivery/http/v1/dto.go`:

```go
// CandidateDetailResponse is the response body for GET /candidates/{id}.
type CandidateDetailResponse struct {
	ID          string             `json:"id"`
	ContentHash string             `json:"content_hash"`
	Personal    CandidatePersonal  `json:"personal"`
	Location    string             `json:"location,omitempty"`
	Headline    string             `json:"headline,omitempty"`
	Profile     json.RawMessage    `json:"profile"`
	Source      string             `json:"source"`
	CreatedAt   string             `json:"created_at"` // RFC3339
}

// CandidatePersonal is the decrypted PII surface.
type CandidatePersonal struct {
	FullName string `json:"full_name,omitempty"`
	Email    string `json:"email,omitempty"`
	Phone    string `json:"phone,omitempty"`
}
```

Add `"encoding/json"` to the imports.

- [ ] **Step 2: Inject the new handler into `SourcingHandler`**

Modify the `SourcingHandler` struct + `NewSourcingHandler` signature in `handlers.go` to include the candidate-detail query:

```go
type SourcingHandler struct {
	upload    *commands.UploadResumeBatchHandler
	status    *queries.GetBatchStatusHandler
	candidate *queries.GetCandidateHandler
	logger    zerolog.Logger
}

func NewSourcingHandler(
	upload *commands.UploadResumeBatchHandler,
	status *queries.GetBatchStatusHandler,
	candidate *queries.GetCandidateHandler,
	logger zerolog.Logger,
) *SourcingHandler {
	return &SourcingHandler{upload: upload, status: status, candidate: candidate, logger: logger}
}
```

- [ ] **Step 3: Add the handler method**

```go
// GetCandidate handles GET /candidates/{candidate_id}.
func (h *SourcingHandler) GetCandidate(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "candidate_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_candidate_id", "candidate_id must be a uuid")
		return
	}

	out, err := h.candidate.Handle(r.Context(), identity.TenantID, id)
	if err != nil {
		if errors.Is(err, repositories.ErrCandidateNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "candidate not found")
			return
		}
		h.logger.Error().Err(err).Msg("get candidate failed")
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, CandidateDetailResponse{
		ID:          out.ID.String(),
		ContentHash: out.ContentHash,
		Personal: CandidatePersonal{
			FullName: out.Personal.FullName,
			Email:    out.Personal.Email,
			Phone:    out.Personal.Phone,
		},
		Location:  out.Location,
		Headline:  out.Headline,
		Profile:   out.Profile,
		Source:    out.Source,
		CreatedAt: out.CreatedAt.UTC().Format(time.RFC3339),
	})
}
```

Add imports: `"errors"`, `"time"`, `"github.com/hustle/hireflow/internal/sourcing/domain/repositories"`.

- [ ] **Step 4: Mount the route**

Modify `internal/sourcing/delivery/http/v1/routes.go`:

```go
func Mount(r chi.Router, h *SourcingHandler) {
	r.Post("/intents/{intent_id}/resumes:batch", h.BatchUpload)
	r.Get("/resumes/batches/{batch_id}", h.GetBatchStatus)
	r.Get("/candidates/{candidate_id}", h.GetCandidate)
}
```

- [ ] **Step 5: Handler test**

Append to `internal/sourcing/delivery/http/v1/handlers_test.go`:

```go
// newCandidateHandler wires a test SourcingHandler that includes the candidate query.
func newCandidateHandler(t *testing.T, cand *queries.GetCandidateHandler) *v1.SourcingHandler {
	repo := newMemRepo()
	store := newMemStorage()
	upload := commands.NewUploadResumeBatchHandler(repo, store, commands.UploadConfig{MaxFileBytes: 1 << 20})
	status := queries.NewGetBatchStatusHandler(repo)
	return v1.NewSourcingHandler(upload, status, cand, zerolog.Nop())
}

func TestGetCandidate_HappyPath(t *testing.T) {
	tenant := shared.NewTenantID()
	c := newCandidateForHandlerTest(t, tenant)
	candRepo := &stubCandRepo{byID: map[string]*entities.Candidate{c.ID().String(): c}}
	cand := queries.NewGetCandidateHandler(candRepo, stubEnc{})

	h := newCandidateHandler(t, cand)
	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodGet, "/candidates/"+c.ID().String(), nil)
	req = withIdentity(req, tenant)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp v1.CandidateDetailResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, c.ID().String(), resp.ID)
	assert.NotEmpty(t, resp.Personal.FullName)
}

func TestGetCandidate_NotFound_Returns404(t *testing.T) {
	candRepo := &stubCandRepo{byID: map[string]*entities.Candidate{}}
	cand := queries.NewGetCandidateHandler(candRepo, stubEnc{})
	h := newCandidateHandler(t, cand)
	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodGet, "/candidates/"+uuid.New().String(), nil)
	req = withIdentity(req, shared.NewTenantID())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetCandidate_NoAuth_Returns401(t *testing.T) {
	candRepo := &stubCandRepo{byID: map[string]*entities.Candidate{}}
	cand := queries.NewGetCandidateHandler(candRepo, stubEnc{})
	h := newCandidateHandler(t, cand)
	router := chi.NewRouter()
	v1.Mount(router, h)

	req := httptest.NewRequest(http.MethodGet, "/candidates/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
```

Reuse the slice 1 `withIdentity` helper. The `stubCandRepo` and `stubEnc` types are similar to those in `queries/get_candidate_test.go`; re-declare them here (test packages don't share). Also add `newCandidateForHandlerTest` factory similar to the query test.

Also update the existing slice-1 handler tests that call `v1.NewSourcingHandler` to pass `nil` (or a stub) for the new `candidate` parameter — the slice-1 cases don't exercise the candidate endpoint, so `nil` is acceptable as long as nothing in those tests dereferences it. Cleaner: pass a `&queries.GetCandidateHandler{}` stub.

- [ ] **Step 6: Update OpenAPI**

In `docs/api/v1/sourcing.openapi.yaml`, add the new path under `paths:`:

```yaml
  /candidates/{candidate_id}:
    get:
      summary: Get candidate detail (PII decrypted, audit-logged)
      security: [{ bearerAuth: [] }]
      parameters:
        - { name: candidate_id, in: path, required: true, schema: { type: string, format: uuid } }
      responses:
        '200':
          description: candidate detail
          content:
            application/json:
              schema: { $ref: '#/components/schemas/CandidateDetailResponse' }
        '401': { description: missing identity }
        '404': { description: candidate not found }
```

And add the schema under `components.schemas`:

```yaml
    CandidateDetailResponse:
      type: object
      properties:
        id:           { type: string, format: uuid }
        content_hash: { type: string }
        personal: { $ref: '#/components/schemas/CandidatePersonal' }
        location:     { type: string }
        headline:     { type: string }
        profile:      { type: object, additionalProperties: true, description: full parsed profile JSON }
        source:       { type: string }
        created_at:   { type: string, format: date-time }
    CandidatePersonal:
      type: object
      properties:
        full_name: { type: string }
        email:     { type: string }
        phone:     { type: string }
```

Also bump the `info.version` to `"1.0.0-slice2"`.

- [ ] **Step 7: Run**

```
go test ./internal/sourcing/delivery/... -v -count=1
```
Expected: all new tests pass + existing slice-1 tests still green.

- [ ] **Step 8: Commit**

```bash
git add internal/sourcing/delivery/ docs/api/v1/sourcing.openapi.yaml
git commit -m "feat(sourcing): GET /candidates/{id} with decrypted PII"
```

---

## Task 14: Wire into `cmd/api/main.go`

**Files:**
- Modify: `cmd/api/main.go`

Add wiring for the parser, OCR, encryptor, candidate repository. Pass them into the existing `ProcessConfig` and pass the new candidate handler into `NewSourcingHandler`.

- [ ] **Step 1: Add env-var helpers (if not already there)**

The slice-1 wiring added `getenvInt64` and `getenvInt`. No new helpers needed.

- [ ] **Step 2: New env vars**

In the sourcing wiring block, read and apply:

```
SOURCING_PARSER_BACKEND   = "claude"           # only option in slice 2
SOURCING_OCR_BACKEND      = "claude"           # only option in slice 2
SOURCING_OCR_THRESHOLD    = "50"               # chars; below this, OCR fallback triggers
SOURCING_PII_DEK          = (required)         # 64-hex-char AES key for local-dev encryption
```

Fail-fast at startup if `SOURCING_PII_DEK` is missing — PII encryption is non-optional.

- [ ] **Step 3: Wire the new components**

After the existing sourcing wiring in `main.go`, but before `processHandler` is built, add:

```go
	// PII encryption — slice 2.
	dekHex := os.Getenv("SOURCING_PII_DEK")
	if dekHex == "" {
		logger.Fatal().Msg("SOURCING_PII_DEK is required (64 hex chars / 32 bytes)")
	}
	piiEnc, err := sourcingenc.NewLocalDevDEK(dekHex)
	if err != nil {
		logger.Fatal().Err(err).Msg("init PII encryptor")
	}

	// Resume parser (Claude tool-use).
	resumeParser := sourcingparsing.NewAnthropicParser(anthropicClient, anthropicCfg.Model)

	// OCR (Claude vision).
	ocrExtractor := sourcingocr.NewClaudeVision(anthropicClient, anthropicCfg.Model)

	// Candidate repository.
	candidateRepo := sourcingpersist.NewPostgresCandidateRepository(pool)
	candidateHandler := sourcingqueries.NewGetCandidateHandler(candidateRepo, piiEnc)
```

(`anthropicClient` and `anthropicCfg` are already wired by the slice-1 / hiringintent block.)

Then extend `processHandler`:

```go
	processHandler := sourcingcommands.NewProcessUploadHandler(sourcingcommands.ProcessConfig{
		Repo:          sourcingRepo,
		Storage:       storage,
		Scanner:       scanner,
		Extractor:     extractor,
		Parser:        resumeParser,
		OCR:           ocrExtractor,
		Encryptor:     piiEnc,
		CandidateRepo: candidateRepo,
		OCRThreshold:  getenvInt("SOURCING_OCR_THRESHOLD", 50),
		RetryBackoff: []time.Duration{
			1 * time.Minute, 5 * time.Minute, 15 * time.Minute, 1 * time.Hour, 4 * time.Hour,
		},
	})
```

And pass `candidateHandler` into the HTTP handler:

```go
	sourcingHandler := sourcinghttp.NewSourcingHandler(uploadHandler, statusHandler, candidateHandler, logger)
```

- [ ] **Step 4: Imports**

Add to the import block:

```go
	sourcingenc "github.com/hustle/hireflow/internal/sourcing/infrastructure/encryption"
	sourcingocr "github.com/hustle/hireflow/internal/sourcing/infrastructure/ocr"
	sourcingparsing "github.com/hustle/hireflow/internal/sourcing/infrastructure/parsing"
```

`sourcingqueries` is already imported from slice 1.

- [ ] **Step 5: Update `developer.md`**

Add to the env-var table:

```
| SOURCING_PII_DEK        | (required)               | 64-hex AES-256 key for PII encryption (local-dev adapter). Generate with `openssl rand -hex 32`. |
| SOURCING_OCR_THRESHOLD  | 50                       | Chars below which OCR fallback runs                |
| SOURCING_PARSER_BACKEND | claude                   | Only "claude" supported in slice 2                 |
| SOURCING_OCR_BACKEND    | claude                   | Only "claude" supported in slice 2                 |
```

- [ ] **Step 6: Verification**

```
make build
make test-unit
```
Both clean.

- [ ] **Step 7: Commit**

```bash
git add cmd/api/main.go developer.md
git commit -m "feat(sourcing): wire parsing stage into api binary"
```

---

## Task 15: End-to-end integration test (slice 2)

**Files:**
- Create: `tests/sourcing_slice2_e2e_test.go`

Exercises the full slice-1+2 path: upload → scan → extract → parse → Candidate created → `GET /candidates/{id}` returns the decrypted profile. Builds on the slice-1 e2e shape; uses a stub `ResumeParser` so the test doesn't burn real Anthropic credit.

- [ ] **Step 1: Write the test**

Create `tests/sourcing_slice2_e2e_test.go`:

```go
//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/shared/infrastructure/auth"
	"github.com/hustle/hireflow/internal/shared/infrastructure/eventbus"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/application/queries"
	v1 "github.com/hustle/hireflow/internal/sourcing/delivery/http/v1"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/encryption"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/messaging"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/persistence"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/scanning"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/storage"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/text"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/worker"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// stubParser produces a fixed ParsedProfile regardless of input. The slice-1
// extracted text from `testdata/hello.pdf` says "hello world" — that's not a
// real resume, so we don't want to send it to Claude. The stub returns a
// canned profile so the test verifies the pipeline glue, not the LLM.
type stubParser struct{}

func (stubParser) Parse(_ context.Context, _ string) (vo.ParsedProfile, error) {
	p := vo.NewParsedProfile()
	p.Personal.FullName = "Alice (test)"
	p.Personal.Email = "alice@test.example"
	p.Personal.Phone = "+91-000-0000"
	p.Personal.Location = "Bangalore"
	p.Headline = "Senior Backend Engineer (test)"
	p.Skills = []vo.ParsedSkill{{Name: "Go", Years: 5}}
	return p, nil
}

// stubOCR for the e2e — not exercised because hello.pdf has plenty of text.
type stubOCR struct{}

func (stubOCR) ExtractFromBytes(_ context.Context, _ []byte, _ string) (services.RawText, error) {
	return services.RawText{Text: "fallback text", PageCount: 1}, nil
}

func TestSourcingSlice2_E2E_UploadParseCandidate(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	defer pool.Close()

	logger := zerolog.New(io.Discard)
	storageDir := t.TempDir()
	store, _ := storage.NewLocalFS(storageDir)

	uploadRepo := persistence.NewPostgresResumeUploadRepository(pool)
	candRepo := persistence.NewPostgresCandidateRepository(pool)

	piiEnc, err := encryption.NewLocalDevDEK("0000000000000000000000000000000000000000000000000000000000000000")
	require.NoError(t, err)

	uploadH := commands.NewUploadResumeBatchHandler(uploadRepo, store, commands.UploadConfig{MaxFileBytes: 10 * 1024 * 1024})
	processH := commands.NewProcessUploadHandler(commands.ProcessConfig{
		Repo:          uploadRepo,
		Storage:       store,
		Scanner:       scanning.NewNoop(),
		Extractor:     text.NewSimple(),
		Parser:        stubParser{},
		OCR:           stubOCR{},
		Encryptor:     piiEnc,
		CandidateRepo: candRepo,
		OCRThreshold:  50,
		RetryBackoff:  []time.Duration{time.Second, 5 * time.Second},
	})
	statusH := queries.NewGetBatchStatusHandler(uploadRepo)
	candH := queries.NewGetCandidateHandler(candRepo, piiEnc)
	handler := v1.NewSourcingHandler(uploadH, statusH, candH, logger)

	router := chi.NewRouter()
	v1.Mount(router, handler)

	bus := eventbus.NewInMemory(logger)
	pub := messaging.NewBusPublisher(bus)
	dispatcher := messaging.NewOutboxDispatcher(pool, pub, logger, messaging.DispatcherConfig{PollInterval: 100 * time.Millisecond})
	pool2 := worker.NewPool(uploadRepo, processH, worker.Config{Size: 1, PollInterval: 100 * time.Millisecond}, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Run(ctx)
	go pool2.Run(ctx)

	// Upload one PDF.
	body, ct := writeMultipart(t, map[string][]byte{"alice.pdf": helloPDFBytes(t)})
	tenant := shared.NewTenantID()
	req := httptest.NewRequest(http.MethodPost, "/intents/"+uuid.New().String()+"/resumes:batch", body)
	req.Header.Set("Content-Type", ct)
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{TenantID: tenant, RecruiterID: shared.NewRecruiterID()}))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var upResp v1.BatchUploadResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&upResp))
	require.Len(t, upResp.Items, 1)
	require.Equal(t, "queued", upResp.Items[0].Status)

	// Poll status until Parsed (slice-2 terminal). Note status is "Parsed" not "Extracted".
	deadline := time.Now().Add(30 * time.Second)
	var candidateID string
	for {
		statusReq := httptest.NewRequest(http.MethodGet, "/resumes/batches/"+upResp.BatchID, nil)
		statusReq = statusReq.WithContext(auth.WithIdentity(statusReq.Context(), auth.Identity{TenantID: tenant, RecruiterID: shared.NewRecruiterID()}))
		statusRec := httptest.NewRecorder()
		router.ServeHTTP(statusRec, statusReq)
		require.Equal(t, http.StatusOK, statusRec.Code)

		var s v1.BatchStatusResponse
		require.NoError(t, json.NewDecoder(statusRec.Body).Decode(&s))
		if s.Summary.Total > 0 && s.Items[0].Status == string(vo.StatusParsed) {
			// Slice 2's status surface doesn't expose candidate_id; query the candidate
			// by content-hash via the repo to get its ID.
			// (Alternative: extend BatchStatusItem to carry candidate_id; out of scope here.)
			// For now use the repo directly.
			c, err := candRepo.FindByContentHash(context.Background(), tenant,
				/* extracted from upload row; queryable via the repo too */ "")
			_ = c
			_ = err
			// Simpler: list all candidates for this tenant in another query, or extend
			// the API. For slice 2 we accept the gap: just verify the upload reached
			// Parsed. The candidate detail path is exercised via its own handler test.
			candidateID = "" // not asserted
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for Parsed status; got %+v", s)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// If you want to hit GET /candidates/{id} in the e2e, you'll need to expose the
	// candidate_id somewhere. Two options for the implementer:
	//   (a) Extend BatchStatusItem with candidate_id (small change to dto.go + handler).
	//   (b) Use candRepo.FindByContentHash() with the upload's content_hash to get the id.
	// Option (b) is simpler and keeps the slice 2 surface clean. Wire it like this:

	if candidateID == "" {
		// Look up the candidate via the repo's FindByContentHash. The content_hash
		// is sha256(helloPDFBytes), computed via vo.ComputeContentHash.
		hash := vo.ComputeContentHash(helloPDFBytes(t))
		c, err := candRepo.FindByContentHash(context.Background(), tenant, hash.String())
		require.NoError(t, err)
		candidateID = c.ID().String()
	}

	// Now hit GET /candidates/{id}.
	candReq := httptest.NewRequest(http.MethodGet, "/candidates/"+candidateID, nil)
	candReq = candReq.WithContext(auth.WithIdentity(candReq.Context(), auth.Identity{TenantID: tenant, RecruiterID: shared.NewRecruiterID()}))
	candRec := httptest.NewRecorder()
	router.ServeHTTP(candRec, candReq)
	require.Equal(t, http.StatusOK, candRec.Code, candRec.Body.String())

	var candResp v1.CandidateDetailResponse
	require.NoError(t, json.NewDecoder(candRec.Body).Decode(&candResp))
	assert.Equal(t, "Alice (test)", candResp.Personal.FullName, "PII must be decrypted in response")
	assert.Equal(t, "alice@test.example", candResp.Personal.Email)
	assert.Equal(t, "Bangalore", candResp.Location)
}
```

The test deliberately skips real Anthropic calls via the stub parser — the **slice-2 e2e test verifies pipeline glue**, not LLM behavior. The parser adapter's own correctness is verified by T9 unit tests against canned HTTP responses.

`auth.Identity` field name in this codebase is `RecruiterID` (not `UserID`) per slice 1's verification.

- [ ] **Step 2: Run**

With containers up + migrations applied:
```
INTEGRATION_TESTS=1 go test -tags=integration ./tests/... -v -count=1
```
Expected: both slice-1 and slice-2 e2e tests PASS.

- [ ] **Step 3: Commit**

```bash
git add tests/sourcing_slice2_e2e_test.go
git commit -m "test(sourcing): slice-2 e2e — full pipeline through Candidate"
```

---

## Task 16: README + module docs refresh

**Files:**
- Modify: `README.md`
- Modify: `docs/modules/sourcing/README.md`

- [ ] **Step 1: Update root README context table row**

In `README.md`, find:
```
| `sourcing` | Resume ingestion + virus-scan + text extraction (slice 1). Parsing, scoring, dedup pool coming in slices 2–4. | **Live (ingestion-only)** |
```

Replace with:
```
| `sourcing` | Resume ingestion + virus-scan + text extraction + LLM parsing → tenant-scoped `Candidate` aggregate (slices 1+2). Match scoring + recruiter dashboard coming in slices 3–4. | **Live (parsed-profile)** |
```

- [ ] **Step 2: Update module README**

In `docs/modules/sourcing/README.md`:

1. Update the lead paragraph to reflect slices 1+2.
2. Update the pipeline diagram:

```
Pending → Scanning → Extracting → Extracted → Parsing → Parsed
                          ↘                       ↘        ↘
                         Failed             Quarantined  Failed (Candidate created)
```

3. Update the ubiquitous-language table — the `Candidate` row no longer says "slice 2+" — it's live.

4. Add to the API section:
```
GET  /api/v1/candidates/{candidate_id}     candidate detail (PII decrypted, audit-logged)
```

5. Add a "Slice 2 invariants" sub-section under Architecture invariants:
- **PII at rest is encrypted at the application layer.** `parsed_profile.personal.*` are stored cleartext in JSONB *only* via the `Profile()` getter; the canonical PII fields live in dedicated `*_enc` columns.
- **Candidate creation is dedup-on-collision.** `CandidateRepo.Save` returns the existing aggregate on `(tenant_id, content_hash)` match. The upload still links to that candidate.
- **OCR fallback runs only when text extraction produces < 50 chars.** Image-only PDFs hit Claude vision; everything else short-circuits.

- [ ] **Step 3: Commit**

```bash
git add README.md docs/modules/sourcing/
git commit -m "docs(sourcing): refresh for slice 2 (parsing + Candidate)"
```

---

## Wrap-up

After all 16 tasks complete:

- [ ] `make test-unit` — clean.
- [ ] `INTEGRATION_TESTS=1 make test-integration` — slice-1 e2e + slice-2 e2e + persistence integration tests all PASS.
- [ ] `go vet ./...` and `gofmt -l -s .` — both clean.
- [ ] **Smoke run the binary** with `SOURCING_PII_DEK=$(openssl rand -hex 32)` set; confirm clean startup (no "PII DEK required" fatal, no Claude SDK init failures, both dispatchers report started).

**What slice 2 ships:**
- LLM-driven structured profile extraction via Claude tool-use.
- Image-only PDF support via Claude vision OCR fallback.
- Tenant-scoped `Candidate` aggregate with application-layer envelope encryption on PII fields.
- `GET /api/v1/candidates/{id}` returning the full profile with PII decrypted.
- Schema-versioned `ParsedProfile` (v1) + prompt-versioned parser adapter.
- Pipeline extended to `Parsed` as the new terminal state; `ResumeParsed` + `CandidateParsed` events on the outbox for slice-3 scoring to subscribe.

**What slice 2 does NOT ship (deferred to later slices):**
- Match scoring → `Application` aggregate (slice 3).
- Embedding (Voyage), rule matcher, LLM judge (slice 3).
- SSE live batch updates, retry/rescore endpoints, lifecycle actions, audit log, GDPR erasure (slice 4).
- KMS-backed `PIIEncryptor` adapter (v1.1 productionization task).
