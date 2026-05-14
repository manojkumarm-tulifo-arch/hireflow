package commands_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// retryUploadRepo is an in-memory ResumeUploadRepository for retry command tests.
type retryUploadRepo struct {
	byID    map[uuid.UUID]*entities.ResumeUpload
	saved   []*entities.ResumeUpload
	findErr error
	saveErr error
}

func newRetryUploadRepo() *retryUploadRepo {
	return &retryUploadRepo{byID: map[uuid.UUID]*entities.ResumeUpload{}}
}

func (r *retryUploadRepo) Save(_ context.Context, u *entities.ResumeUpload) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	_ = u.PullEvents()
	r.byID[u.ID()] = u
	r.saved = append(r.saved, u)
	return nil
}

func (r *retryUploadRepo) FindByID(_ context.Context, _ shared.TenantID, id uuid.UUID) (*entities.ResumeUpload, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	if u, ok := r.byID[id]; ok {
		return u, nil
	}
	return nil, repositories.ErrNotFound
}

func (r *retryUploadRepo) FindByContentHash(_ context.Context, _ shared.TenantID, _ string) (*entities.ResumeUpload, error) {
	return nil, repositories.ErrNotFound
}

func (r *retryUploadRepo) ClaimNextPending(_ context.Context) (*entities.ResumeUpload, error) {
	return nil, repositories.ErrNotFound
}

func (r *retryUploadRepo) ListByBatch(_ context.Context, _ shared.TenantID, _ uuid.UUID) ([]*entities.ResumeUpload, error) {
	return nil, nil
}

// buildUploadInStatus builds and seeds a ResumeUpload with the given status.
func buildUploadInStatus(t *testing.T, repo *retryUploadRepo, tenant shared.TenantID, status vo.UploadStatus) *entities.ResumeUpload {
	t.Helper()
	u := entities.RehydrateResumeUpload(entities.RehydrateInput{
		ID:           uuid.New(),
		TenantID:     tenant,
		IntentID:     uuid.New(),
		BatchID:      uuid.New(),
		StorageKey:   "key/file.pdf",
		OriginalName: "resume.pdf",
		Status:       status,
		AttemptCount: 2,
		LastError:    "previous error",
		NextAttemptAt: time.Now().Add(-1 * time.Hour),
		CreatedAt:    time.Now().Add(-2 * time.Hour),
		UpdatedAt:    time.Now().Add(-1 * time.Hour),
	})
	repo.byID[u.ID()] = u
	return u
}

func TestRetryResumeUpload_FromFailed_ResetsAndSaves(t *testing.T) {
	tenant := shared.NewTenantID()
	repo := newRetryUploadRepo()
	upload := buildUploadInStatus(t, repo, tenant, vo.StatusFailed)

	handler := commands.NewRetryResumeUploadHandler(repo)
	err := handler.Handle(context.Background(), commands.RetryResumeUploadInput{
		TenantID: tenant,
		UploadID: upload.ID(),
	})
	require.NoError(t, err)

	// Verify the saved state.
	require.Len(t, repo.saved, 1)
	saved := repo.saved[0]
	assert.Equal(t, vo.StatusPending, saved.Status())
	assert.Equal(t, 0, saved.AttemptCount())
	assert.Equal(t, "", saved.LastError())
}

func TestRetryResumeUpload_FromQuarantined_ResetsAndSaves(t *testing.T) {
	tenant := shared.NewTenantID()
	repo := newRetryUploadRepo()
	upload := buildUploadInStatus(t, repo, tenant, vo.StatusQuarantined)

	handler := commands.NewRetryResumeUploadHandler(repo)
	err := handler.Handle(context.Background(), commands.RetryResumeUploadInput{
		TenantID: tenant,
		UploadID: upload.ID(),
	})
	require.NoError(t, err)

	require.Len(t, repo.saved, 1)
	saved := repo.saved[0]
	assert.Equal(t, vo.StatusPending, saved.Status())
	assert.Equal(t, 0, saved.AttemptCount())
	assert.Equal(t, "", saved.LastError())
}

func TestRetryResumeUpload_FromPending_ReturnsErrNotRetryable(t *testing.T) {
	tenant := shared.NewTenantID()
	repo := newRetryUploadRepo()
	upload := buildUploadInStatus(t, repo, tenant, vo.StatusPending)

	handler := commands.NewRetryResumeUploadHandler(repo)
	err := handler.Handle(context.Background(), commands.RetryResumeUploadInput{
		TenantID: tenant,
		UploadID: upload.ID(),
	})
	assert.ErrorIs(t, err, commands.ErrNotRetryable)
}

func TestRetryResumeUpload_FromScored_ReturnsErrNotRetryable(t *testing.T) {
	tenant := shared.NewTenantID()
	repo := newRetryUploadRepo()
	upload := buildUploadInStatus(t, repo, tenant, vo.StatusScored)

	handler := commands.NewRetryResumeUploadHandler(repo)
	err := handler.Handle(context.Background(), commands.RetryResumeUploadInput{
		TenantID: tenant,
		UploadID: upload.ID(),
	})
	assert.ErrorIs(t, err, commands.ErrNotRetryable)
}

func TestRetryResumeUpload_NotFound_ReturnsErrNotFound(t *testing.T) {
	tenant := shared.NewTenantID()
	repo := newRetryUploadRepo() // empty — nothing seeded

	handler := commands.NewRetryResumeUploadHandler(repo)
	err := handler.Handle(context.Background(), commands.RetryResumeUploadInput{
		TenantID: tenant,
		UploadID: uuid.New(),
	})
	assert.ErrorIs(t, err, repositories.ErrNotFound)
}

func TestRetryResumeUpload_SaveError_Propagates(t *testing.T) {
	tenant := shared.NewTenantID()
	repo := newRetryUploadRepo()
	upload := buildUploadInStatus(t, repo, tenant, vo.StatusFailed)
	repo.saveErr = assert.AnError

	handler := commands.NewRetryResumeUploadHandler(repo)
	err := handler.Handle(context.Background(), commands.RetryResumeUploadInput{
		TenantID: tenant,
		UploadID: upload.ID(),
	})
	assert.Error(t, err)
	assert.ErrorIs(t, err, assert.AnError)
}
