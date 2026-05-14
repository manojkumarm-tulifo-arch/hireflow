package commands

import (
	"context"
	"errors"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
)

// ErrNotRetryable is returned when a RetryResumeUpload is attempted on an
// upload whose status is neither Failed nor Quarantined.
var ErrNotRetryable = errors.New("upload not retryable")

// RetryResumeUploadInput carries the parameters for retrying an upload.
type RetryResumeUploadInput struct {
	TenantID shared.TenantID
	UploadID uuid.UUID
}

// RetryResumeUploadHandler resets a Failed or Quarantined upload back to
// Pending so the worker pool will re-process it.
type RetryResumeUploadHandler struct {
	repo repositories.ResumeUploadRepository
}

// NewRetryResumeUploadHandler wires the handler.
func NewRetryResumeUploadHandler(repo repositories.ResumeUploadRepository) *RetryResumeUploadHandler {
	return &RetryResumeUploadHandler{repo: repo}
}

// Handle loads the upload, validates it is retryable, resets it to Pending,
// and saves it so the worker pool will re-claim it.
//
// Error semantics:
//   - repositories.ErrNotFound    → upload not found (caller should 404)
//   - ErrNotRetryable             → status is not Failed or Quarantined (caller should 400)
func (h *RetryResumeUploadHandler) Handle(ctx context.Context, in RetryResumeUploadInput) error {
	u, err := h.repo.FindByID(ctx, in.TenantID, in.UploadID)
	if err != nil {
		return err
	}

	if err := u.ResetForRetry(); err != nil {
		if errors.Is(err, entities.ErrInvalidTransition) {
			return ErrNotRetryable
		}
		return err
	}

	return h.repo.Save(ctx, u)
}
