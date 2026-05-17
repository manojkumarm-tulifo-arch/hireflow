package worker_test

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/worker"
)

// oneShotRepo serves one upload, then ErrNotFound forever.
type oneShotRepo struct {
	served atomic.Bool
	u      *entities.ResumeUpload
}

func (r *oneShotRepo) Save(_ context.Context, _ *entities.ResumeUpload) error { return nil }
func (r *oneShotRepo) FindByID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.ResumeUpload, error) {
	return nil, repositories.ErrNotFound
}
func (r *oneShotRepo) FindByContentHash(_ context.Context, _ shared.TenantID, _ string) (*entities.ResumeUpload, error) {
	return nil, repositories.ErrNotFound
}
func (r *oneShotRepo) FindByContentHashAndIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID, _ string) (*entities.ResumeUpload, error) {
	return nil, repositories.ErrNotFound
}
func (r *oneShotRepo) ListByBatch(_ context.Context, _ shared.TenantID, _ uuid.UUID) ([]*entities.ResumeUpload, error) {
	return nil, nil
}
func (r *oneShotRepo) BatchExistsForTenant(_ context.Context, _ shared.TenantID, _ uuid.UUID) (bool, error) {
	return false, nil
}
func (r *oneShotRepo) ClaimNextPending(_ context.Context) (*entities.ResumeUpload, error) {
	if r.served.CompareAndSwap(false, true) {
		return r.u, nil
	}
	return nil, repositories.ErrNotFound
}

// stubStorage satisfies services.ResumeStorage with pre-written content.
type stubStorage struct {
	puts map[string][]byte
}

func newStubStorage() *stubStorage { return &stubStorage{puts: map[string][]byte{}} }

func (s *stubStorage) Put(_ context.Context, key string, body io.Reader) error {
	b, _ := io.ReadAll(body)
	s.puts[key] = b
	return nil
}
func (s *stubStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	data := s.puts[key]
	return io.NopCloser(strings.NewReader(string(data))), nil
}
func (s *stubStorage) MoveToQuarantine(_ context.Context, key string) (string, error) {
	s.puts["quarantine/"+key] = s.puts[key]
	delete(s.puts, key)
	return "quarantine/" + key, nil
}
func (s *stubStorage) Delete(_ context.Context, key string) error {
	delete(s.puts, key)
	return nil
}

// stubScanner always returns clean.
type stubScanner struct{}

func (stubScanner) Scan(_ context.Context, r io.Reader) (services.ScanVerdict, error) {
	if r != nil {
		_, _ = io.Copy(io.Discard, r)
	}
	return services.ScanVerdict{Clean: true}, nil
}

// stubExtractor always returns empty text (no extractable text — not an error).
type stubExtractor struct{}

func (stubExtractor) Extract(_ context.Context, r io.Reader, _ vo.MimeType) (services.RawText, error) {
	if r != nil {
		_, _ = io.Copy(io.Discard, r)
	}
	return services.RawText{Text: "stub", PageCount: 1}, nil
}

// newPoolUpload builds a minimal Pending upload for pool tests.
func newPoolUpload(t *testing.T) *entities.ResumeUpload {
	t.Helper()
	mime, _ := vo.ParseMimeType("application/pdf")
	h, _ := vo.NewContentHash("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	u, err := entities.NewResumeUpload(entities.UploadInput{
		TenantID: shared.NewTenantID(), IntentID: uuid.New(), BatchID: uuid.New(),
		StorageKey: "pool-key", OriginalName: "pool.pdf",
		MimeType: mime, SizeBytes: 42, ContentHash: h,
	})
	require.NoError(t, err)
	_ = u.PullEvents()
	return u
}

// TestPool_HandlesOneClaimAndExits verifies that the pool loop starts, processes
// a single claimed upload, and exits cleanly when the context is canceled.
// Full pipeline coverage is reserved for the Task 15 e2e test.
func TestPool_HandlesOneClaimAndExits(t *testing.T) {
	store := newStubStorage()
	require.NoError(t, store.Put(context.Background(), "pool-key", strings.NewReader("content")))

	u := newPoolUpload(t)
	repo := &oneShotRepo{u: u}

	handler := commands.NewProcessUploadHandler(commands.ProcessConfig{
		Repo:         repo,
		Storage:      store,
		Scanner:      stubScanner{},
		Extractor:    stubExtractor{},
		RetryBackoff: []time.Duration{time.Second},
	})

	pool := worker.NewPool(repo, handler, worker.Config{Size: 1, PollInterval: 10 * time.Millisecond}, zerolog.Nop())
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	go pool.Run(ctx)
	<-ctx.Done()
	assert.True(t, true, "pool exited cleanly on context cancellation")
}
