package commands_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/application/dto"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// inMemSource yields items from a slice for testing.
type inMemSource struct {
	items []dto.BatchItem
	i     int
}

func (s *inMemSource) Next() (dto.BatchItem, error) {
	if s.i >= len(s.items) {
		return dto.BatchItem{}, io.EOF
	}
	out := s.items[s.i]
	s.i++
	return out, nil
}

// fakeRepo is a minimal in-memory ResumeUploadRepository for unit tests.
// It maintains two indices:
//   - byTenantHash: keyed by "tenantID:contentHash" (cross-intent)
//   - byTenantIntentHash: keyed by "tenantID:intentID:contentHash" (per-intent)
type fakeRepo struct {
	byTenantHash       map[string]*entities.ResumeUpload
	byTenantIntentHash map[string]*entities.ResumeUpload
	saved              []*entities.ResumeUpload
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		byTenantHash:       map[string]*entities.ResumeUpload{},
		byTenantIntentHash: map[string]*entities.ResumeUpload{},
	}
}

func (r *fakeRepo) Save(_ context.Context, u *entities.ResumeUpload) error {
	tenantHash := u.TenantID().String() + ":" + u.ContentHash().String()
	tenantIntentHash := u.TenantID().String() + ":" + u.IntentID().String() + ":" + u.ContentHash().String()
	r.byTenantHash[tenantHash] = u
	r.byTenantIntentHash[tenantIntentHash] = u
	r.saved = append(r.saved, u)
	_ = u.PullEvents()
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.ResumeUpload, error) {
	return nil, repositories.ErrNotFound
}

func (r *fakeRepo) FindByContentHash(_ context.Context, t shared.TenantID, h string) (*entities.ResumeUpload, error) {
	u, ok := r.byTenantHash[t.String()+":"+h]
	if !ok {
		return nil, repositories.ErrNotFound
	}
	return u, nil
}

func (r *fakeRepo) FindByContentHashAndIntent(_ context.Context, t shared.TenantID, intentID uuid.UUID, h string) (*entities.ResumeUpload, error) {
	key := t.String() + ":" + intentID.String() + ":" + h
	u, ok := r.byTenantIntentHash[key]
	if !ok {
		return nil, repositories.ErrNotFound
	}
	return u, nil
}

func (r *fakeRepo) ClaimNextPending(_ context.Context) (*entities.ResumeUpload, error) {
	return nil, repositories.ErrNotFound
}
func (r *fakeRepo) ListByBatch(_ context.Context, _ shared.TenantID, _ uuid.UUID) ([]*entities.ResumeUpload, error) {
	return nil, nil
}
func (r *fakeRepo) BatchExistsForTenant(_ context.Context, _ shared.TenantID, _ uuid.UUID) (bool, error) {
	return false, nil
}

// fakeStorage records puts and serves opens from memory.
type fakeStorage struct {
	puts map[string][]byte
}

func newFakeStorage() *fakeStorage { return &fakeStorage{puts: map[string][]byte{}} }

func (s *fakeStorage) Put(_ context.Context, key string, body io.Reader) error {
	b, _ := io.ReadAll(body)
	s.puts[key] = b
	return nil
}
func (s *fakeStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.puts[key])), nil
}
func (s *fakeStorage) MoveToQuarantine(_ context.Context, key string) (string, error) {
	s.puts["quarantine/"+key] = s.puts[key]
	delete(s.puts, key)
	return "quarantine/" + key, nil
}
func (s *fakeStorage) Delete(_ context.Context, key string) error {
	delete(s.puts, key)
	return nil
}

// 1.4 PDF magic so SniffMimeType accepts it.
const pdfMagic = "%PDF-1.4\n%fake content\n"

// pdfBytes returns a valid-looking PDF body with a unique suffix so content hashes differ.
func pdfBytes(unique string) string {
	return "%PDF-1.4\n%" + unique + "\n%%EOF\n"
}

// zipFile is a name/body pair for buildTestZip.
type zipFile struct{ Name, Body string }

// buildTestZip creates an in-memory ZIP file from the given entries.
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

// newHandler is a convenience constructor for tests.
func newHandler(repo *fakeRepo, store *fakeStorage) *commands.UploadResumeBatchHandler {
	return commands.NewUploadResumeBatchHandler(repo, store, commands.UploadConfig{MaxFileBytes: 1 << 20})
}

// seedUpload pre-inserts a ResumeUpload into the fakeRepo for the given (tenant, intent, body).
func seedUpload(t *testing.T, repo *fakeRepo, tenant shared.TenantID, intentID uuid.UUID, body string) {
	t.Helper()
	mime, err := vo.SniffMimeType([]byte(body))
	require.NoError(t, err)
	hash := vo.ComputeContentHash([]byte(body))
	upload, err := entities.NewResumeUpload(entities.UploadInput{
		TenantID:     tenant,
		IntentID:     intentID,
		BatchID:      uuid.New(),
		StorageKey:   "seed/key",
		OriginalName: "seed.pdf",
		MimeType:     mime,
		SizeBytes:    int64(len(body)),
		ContentHash:  hash,
	})
	require.NoError(t, err)
	require.NoError(t, repo.Save(context.Background(), upload))
}

// ---------------------------------------------------------------------------
// Existing tests (updated to reflect new per-intent dedup semantics)
// ---------------------------------------------------------------------------

func TestUpload_ValidPDF_Queues(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	cfg := commands.UploadConfig{MaxFileBytes: 1 << 20}
	h := commands.NewUploadResumeBatchHandler(repo, store, cfg)

	tenant := shared.NewTenantID()
	out, err := h.Handle(context.Background(), dto.BatchUploadInput{
		TenantID: tenant,
		IntentID: uuid.New(),
		Source: &inMemSource{items: []dto.BatchItem{
			{Filename: "alice.pdf", Body: strings.NewReader(pdfMagic)},
		}},
	})
	require.NoError(t, err)
	require.Len(t, out.Items, 1)
	assert.Equal(t, "queued", out.Items[0].Status)
	require.NotNil(t, out.Items[0].UploadID)
	assert.Len(t, repo.saved, 1)
	assert.Len(t, store.puts, 1)
}

func TestUpload_OversizeFile_RejectedNotPersisted(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	cfg := commands.UploadConfig{MaxFileBytes: 10}
	h := commands.NewUploadResumeBatchHandler(repo, store, cfg)

	out, err := h.Handle(context.Background(), dto.BatchUploadInput{
		TenantID: shared.NewTenantID(),
		IntentID: uuid.New(),
		Source: &inMemSource{items: []dto.BatchItem{
			{Filename: "big.pdf", Body: strings.NewReader(pdfMagic + strings.Repeat("x", 1000))},
		}},
	})
	require.NoError(t, err)
	require.Len(t, out.Items, 1)
	require.NotNil(t, out.Items[0].Error)
	assert.Equal(t, "size_exceeded", out.Items[0].Error.Code)
	assert.Len(t, repo.saved, 0)
	assert.Len(t, store.puts, 0)
}

func TestUpload_UnsupportedMime_Rejected(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	cfg := commands.UploadConfig{MaxFileBytes: 1 << 20}
	h := commands.NewUploadResumeBatchHandler(repo, store, cfg)

	out, err := h.Handle(context.Background(), dto.BatchUploadInput{
		TenantID: shared.NewTenantID(),
		IntentID: uuid.New(),
		Source: &inMemSource{items: []dto.BatchItem{
			{Filename: "evil.png", Body: bytes.NewReader([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})},
		}},
	})
	require.NoError(t, err)
	require.NotNil(t, out.Items[0].Error)
	assert.Equal(t, "mime_unsupported", out.Items[0].Error.Code)
	assert.Len(t, repo.saved, 0)
}

// TestUpload_DuplicateContentHash_ReturnsDeduplicated verifies that re-uploading
// the same bytes to the same intent returns "duplicate_in_intent".
func TestUpload_DuplicateContentHash_ReturnsDeduplicated(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	cfg := commands.UploadConfig{MaxFileBytes: 1 << 20}
	h := commands.NewUploadResumeBatchHandler(repo, store, cfg)

	tenant := shared.NewTenantID()
	intent := uuid.New()

	// First upload — queues.
	out1, err := h.Handle(context.Background(), dto.BatchUploadInput{
		TenantID: tenant, IntentID: intent,
		Source: &inMemSource{items: []dto.BatchItem{
			{Filename: "alice.pdf", Body: strings.NewReader(pdfMagic)},
		}},
	})
	require.NoError(t, err)
	require.Equal(t, "queued", out1.Items[0].Status)

	// Second upload of identical bytes to same intent — duplicate_in_intent.
	out2, err := h.Handle(context.Background(), dto.BatchUploadInput{
		TenantID: tenant, IntentID: intent,
		Source: &inMemSource{items: []dto.BatchItem{
			{Filename: "alice-again.pdf", Body: strings.NewReader(pdfMagic)},
		}},
	})
	require.NoError(t, err)
	require.Equal(t, "duplicate_in_intent", out2.Items[0].Status)
	assert.Len(t, repo.saved, 1, "second submit must not create a new aggregate")
}

func TestUpload_EmptyBody_Rejected(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	cfg := commands.UploadConfig{MaxFileBytes: 1 << 20}
	h := commands.NewUploadResumeBatchHandler(repo, store, cfg)

	out, err := h.Handle(context.Background(), dto.BatchUploadInput{
		TenantID: shared.NewTenantID(), IntentID: uuid.New(),
		Source: &inMemSource{items: []dto.BatchItem{
			{Filename: "x.pdf", Body: strings.NewReader("")},
		}},
	})
	require.NoError(t, err)
	require.NotNil(t, out.Items[0].Error)
	assert.Equal(t, "empty_file", out.Items[0].Error.Code)
}

// ---------------------------------------------------------------------------
// New tests: ZIP fan-out + per-intent dedup
// ---------------------------------------------------------------------------

// TestUploadBatch_ZipFanOut_QueuesEachEntry verifies that a ZIP containing two
// PDF files produces 3 outcomes: one parent (extracted_from_zip) + two children (queued).
func TestUploadBatch_ZipFanOut_QueuesEachEntry(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	h := newHandler(repo, store)

	aliceBody := pdfBytes("alice-unique")
	bharatBody := pdfBytes("bharat-unique")
	zipBody := buildTestZip(t, []zipFile{
		{Name: "alice.pdf", Body: aliceBody},
		{Name: "bharat.pdf", Body: bharatBody},
	})

	out, err := h.Handle(context.Background(), dto.BatchUploadInput{
		TenantID: shared.NewTenantID(),
		IntentID: uuid.New(),
		Source: &inMemSource{items: []dto.BatchItem{
			{Filename: "batch.zip", Body: bytes.NewReader(zipBody)},
		}},
	})
	require.NoError(t, err)
	require.Len(t, out.Items, 3, "parent + 2 children")

	parent := out.Items[0]
	assert.Equal(t, "batch.zip", parent.Filename)
	assert.Equal(t, "extracted_from_zip", parent.Status)
	assert.Nil(t, parent.ParentItemID, "parent has no parent")

	var parentID *string
	for i, child := range out.Items[1:] {
		assert.Equal(t, "queued", child.Status, "child %s should be queued", child.Filename)
		require.NotNil(t, child.ParentFilename)
		assert.Equal(t, "batch.zip", *child.ParentFilename)
		require.NotNil(t, child.ParentItemID)
		if i == 0 {
			parentID = child.ParentItemID
		} else {
			assert.Equal(t, *parentID, *child.ParentItemID, "all children should share same parent ID")
		}
	}

	assert.Len(t, repo.saved, 2)
}

// TestUploadBatch_DuplicateInIntent verifies that re-uploading the same file to
// the same intent returns "duplicate_in_intent" (per-intent dedup).
func TestUploadBatch_DuplicateInIntent(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	h := newHandler(repo, store)

	tenant := shared.NewTenantID()
	intentA := uuid.New()
	aliceBody := pdfBytes("alice-dedup")

	// Pre-seed alice for intentA.
	seedUpload(t, repo, tenant, intentA, aliceBody)

	out, err := h.Handle(context.Background(), dto.BatchUploadInput{
		TenantID: tenant,
		IntentID: intentA,
		Source: &inMemSource{items: []dto.BatchItem{
			{Filename: "alice.pdf", Body: strings.NewReader(aliceBody)},
		}},
	})
	require.NoError(t, err)
	require.Len(t, out.Items, 1)
	assert.Equal(t, "duplicate_in_intent", out.Items[0].Status)
	// No new saves — the pre-seeded one + nothing new.
	assert.Len(t, repo.saved, 1)
}

// TestUploadBatch_SameHashDifferentIntent_Queued verifies that uploading the same
// bytes to a DIFFERENT intent is NOT treated as a duplicate (per-intent dedup).
func TestUploadBatch_SameHashDifferentIntent_Queued(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	h := newHandler(repo, store)

	tenant := shared.NewTenantID()
	intentA := uuid.New()
	intentB := uuid.New()
	aliceBody := pdfBytes("alice-cross-intent")

	// Pre-seed alice for intentA.
	seedUpload(t, repo, tenant, intentA, aliceBody)

	out, err := h.Handle(context.Background(), dto.BatchUploadInput{
		TenantID: tenant,
		IntentID: intentB, // different intent
		Source: &inMemSource{items: []dto.BatchItem{
			{Filename: "alice.pdf", Body: strings.NewReader(aliceBody)},
		}},
	})
	require.NoError(t, err)
	require.Len(t, out.Items, 1)
	assert.Equal(t, "queued", out.Items[0].Status, "same hash, different intent → should be queued")
	// Total saves: 1 (seed) + 1 (new) = 2.
	assert.Len(t, repo.saved, 2)
}

// TestUploadBatch_ZipWithDuplicate_PartialOutcomes verifies mixed ZIP outcomes:
// one child that is a duplicate_in_intent and one that is queued.
func TestUploadBatch_ZipWithDuplicate_PartialOutcomes(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	h := newHandler(repo, store)

	tenant := shared.NewTenantID()
	intentA := uuid.New()
	aliceBody := pdfBytes("alice-zip-dup")
	bharatBody := pdfBytes("bharat-zip-new")

	// Pre-seed alice for intentA.
	seedUpload(t, repo, tenant, intentA, aliceBody)

	zipBody := buildTestZip(t, []zipFile{
		{Name: "alice.pdf", Body: aliceBody},
		{Name: "bharat.pdf", Body: bharatBody},
	})

	out, err := h.Handle(context.Background(), dto.BatchUploadInput{
		TenantID: tenant,
		IntentID: intentA,
		Source: &inMemSource{items: []dto.BatchItem{
			{Filename: "batch.zip", Body: bytes.NewReader(zipBody)},
		}},
	})
	require.NoError(t, err)
	require.Len(t, out.Items, 3, "parent + alice (dup) + bharat (queued)")

	parent := out.Items[0]
	assert.Equal(t, "extracted_from_zip", parent.Status)

	// Find alice and bharat outcomes (order follows ZIP entry order).
	alice := out.Items[1]
	bharat := out.Items[2]
	assert.Equal(t, "alice.pdf", alice.Filename)
	assert.Equal(t, "duplicate_in_intent", alice.Status)
	assert.Equal(t, "bharat.pdf", bharat.Filename)
	assert.Equal(t, "queued", bharat.Status)

	// Both children carry parent linkage.
	require.NotNil(t, alice.ParentFilename)
	assert.Equal(t, "batch.zip", *alice.ParentFilename)
	require.NotNil(t, bharat.ParentFilename)
	assert.Equal(t, "batch.zip", *bharat.ParentFilename)
}

// TestUploadBatch_ZipRejection_NoChildren verifies that a ZIP exceeding
// MaxEntries (101 entries) produces exactly 1 rejected outcome with code
// "zip_too_many_entries" and no child entries.
func TestUploadBatch_ZipRejection_NoChildren(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	h := newHandler(repo, store)

	// Build a ZIP with 101 entries (default limit is 100).
	entries := make([]zipFile, 101)
	for i := range entries {
		entries[i] = zipFile{
			Name: strings.Repeat("a", i+1) + ".pdf", // unique names
			Body: pdfBytes(strings.Repeat("x", i+1)),
		}
	}
	zipBody := buildTestZip(t, entries)

	out, err := h.Handle(context.Background(), dto.BatchUploadInput{
		TenantID: shared.NewTenantID(),
		IntentID: uuid.New(),
		Source: &inMemSource{items: []dto.BatchItem{
			{Filename: "big.zip", Body: bytes.NewReader(zipBody)},
		}},
	})
	require.NoError(t, err)
	require.Len(t, out.Items, 1, "only the rejected parent ZIP outcome, no children")
	require.NotNil(t, out.Items[0].Error)
	assert.Equal(t, "zip_too_many_entries", out.Items[0].Error.Code)
	assert.Len(t, repo.saved, 0)
}
