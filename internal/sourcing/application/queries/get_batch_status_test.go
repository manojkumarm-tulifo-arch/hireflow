package queries_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/queries"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

type fakeListRepo struct {
	items map[string][]*entities.ResumeUpload // batchID -> uploads
}

func (r *fakeListRepo) Save(context.Context, *entities.ResumeUpload) error { return nil }
func (r *fakeListRepo) FindByID(context.Context, shared.TenantID, uuid.UUID) (*entities.ResumeUpload, error) {
	return nil, repositories.ErrNotFound
}
func (r *fakeListRepo) FindByContentHash(context.Context, shared.TenantID, string) (*entities.ResumeUpload, error) {
	return nil, repositories.ErrNotFound
}
func (r *fakeListRepo) ClaimNextPending(context.Context) (*entities.ResumeUpload, error) {
	return nil, repositories.ErrNotFound
}
func (r *fakeListRepo) ListByBatch(_ context.Context, _ shared.TenantID, b uuid.UUID) ([]*entities.ResumeUpload, error) {
	return r.items[b.String()], nil
}

func newUpload(t *testing.T, batchID uuid.UUID, status vo.UploadStatus) *entities.ResumeUpload {
	t.Helper()
	mime, _ := vo.ParseMimeType("application/pdf")
	h, _ := vo.NewContentHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	u, err := entities.NewResumeUpload(entities.UploadInput{
		TenantID: shared.NewTenantID(), IntentID: uuid.New(), BatchID: batchID,
		StorageKey: "k", OriginalName: "f.pdf", MimeType: mime, SizeBytes: 1, ContentHash: h,
	})
	require.NoError(t, err)
	// Walk to target status.
	if status == vo.StatusScanning || status == vo.StatusExtracting || status == vo.StatusExtracted {
		require.NoError(t, u.BeginScanning())
	}
	if status == vo.StatusExtracting || status == vo.StatusExtracted {
		require.NoError(t, u.BeginExtracting())
	}
	if status == vo.StatusExtracted {
		require.NoError(t, u.RecordExtractedText("x", 1))
		require.NoError(t, u.CompleteExtracted())
	}
	if status == vo.StatusFailed {
		require.NoError(t, u.MarkFailed(vo.Fatal("test", "boom")))
	}
	return u
}

func TestGetBatchStatus_SumsByStatus(t *testing.T) {
	batchID := uuid.New()
	repo := &fakeListRepo{items: map[string][]*entities.ResumeUpload{
		batchID.String(): {
			newUpload(t, batchID, vo.StatusPending),
			newUpload(t, batchID, vo.StatusScanning),
			newUpload(t, batchID, vo.StatusExtracting),
			newUpload(t, batchID, vo.StatusExtracted),
			newUpload(t, batchID, vo.StatusFailed),
		},
	}}
	h := queries.NewGetBatchStatusHandler(repo)
	out, err := h.Handle(context.Background(), shared.NewTenantID(), batchID)
	require.NoError(t, err)
	assert.Equal(t, 5, out.Summary.Total)
	assert.Equal(t, 3, out.Summary.InFlight) // Pending+Scanning+Extracting
	assert.Equal(t, 1, out.Summary.Extracted)
	assert.Equal(t, 1, out.Summary.Failed)
	assert.Len(t, out.Items, 5)
}
