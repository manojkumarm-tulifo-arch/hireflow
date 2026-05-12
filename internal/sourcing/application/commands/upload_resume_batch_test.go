package commands_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/application/dto"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	shared "github.com/hustle/hireflow/internal/shared/domain"
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
type fakeRepo struct {
	byHash map[string]*entities.ResumeUpload
	saved  []*entities.ResumeUpload
}

func newFakeRepo() *fakeRepo { return &fakeRepo{byHash: map[string]*entities.ResumeUpload{}} }

func (r *fakeRepo) Save(_ context.Context, u *entities.ResumeUpload) error {
	r.byHash[u.TenantID().String()+":"+u.ContentHash().String()] = u
	r.saved = append(r.saved, u)
	_ = u.PullEvents()
	return nil
}
func (r *fakeRepo) FindByID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.ResumeUpload, error) {
	return nil, repositories.ErrNotFound
}
func (r *fakeRepo) FindByContentHash(_ context.Context, t shared.TenantID, h string) (*entities.ResumeUpload, error) {
	u, ok := r.byHash[t.String()+":"+h]
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

// 1.4 PDF magic so SniffMimeType accepts it.
const pdfMagic = "%PDF-1.4\n%fake content\n"

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

	// Second upload of identical bytes — deduplicated.
	out2, err := h.Handle(context.Background(), dto.BatchUploadInput{
		TenantID: tenant, IntentID: intent,
		Source: &inMemSource{items: []dto.BatchItem{
			{Filename: "alice-again.pdf", Body: strings.NewReader(pdfMagic)},
		}},
	})
	require.NoError(t, err)
	require.Equal(t, "deduplicated", out2.Items[0].Status)
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
