// Package commands holds the sourcing application-layer command handlers.
package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/sourcing/application/dto"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// UploadConfig tunes the command's limits.
type UploadConfig struct {
	MaxFileBytes int64 // per-file cap (bytes); enforced as a hard cap
}

// UploadResumeBatchHandler is the entry point for HR uploading a batch of resumes
// against a confirmed intent.
type UploadResumeBatchHandler struct {
	repo    repositories.ResumeUploadRepository
	storage services.ResumeStorage
	cfg     UploadConfig
}

// NewUploadResumeBatchHandler wires the handler.
func NewUploadResumeBatchHandler(
	repo repositories.ResumeUploadRepository,
	storage services.ResumeStorage,
	cfg UploadConfig,
) *UploadResumeBatchHandler {
	return &UploadResumeBatchHandler{repo: repo, storage: storage, cfg: cfg}
}

// Handle drains the source, processing each item independently. Per-file
// failures land in the response; the command never aborts the batch on a
// per-item error.
func (h *UploadResumeBatchHandler) Handle(ctx context.Context, in dto.BatchUploadInput) (dto.BatchUploadOutput, error) {
	out := dto.BatchUploadOutput{BatchID: uuid.New()}
	tenant := in.TenantID

	for {
		item, err := in.Source.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return dto.BatchUploadOutput{}, fmt.Errorf("source: %w", err)
		}
		outcome := h.processItem(ctx, tenant, in.IntentID, out.BatchID, item)
		out.Items = append(out.Items, outcome)
	}
	return out, nil
}

func (h *UploadResumeBatchHandler) processItem(
	ctx context.Context, tenant shared.TenantID, intentID, batchID uuid.UUID, item dto.BatchItem,
) dto.ItemOutcome {
	// Read into bounded buffer. cfg.MaxFileBytes+1 lets us detect overshoot
	// in a single pass without holding the entire excess.
	limit := h.cfg.MaxFileBytes
	if limit <= 0 {
		limit = 10 * 1024 * 1024 // safe default
	}
	buf := &bytes.Buffer{}
	n, err := io.Copy(buf, io.LimitReader(item.Body, limit+1))
	if err != nil {
		return rejected(item.Filename, "read_failed", err.Error(), nil)
	}
	if n == 0 {
		return rejected(item.Filename, "empty_file", "no bytes read", nil)
	}
	if n > limit {
		return rejected(item.Filename, "size_exceeded", "file exceeds limit",
			map[string]any{"limit_bytes": limit})
	}

	body := buf.Bytes()

	// MIME sniff (truth source over filename extension).
	mime, err := vo.SniffMimeType(body)
	if err != nil {
		return rejected(item.Filename, "mime_unsupported", err.Error(), nil)
	}

	// Hash + dedup.
	hash := vo.ComputeContentHash(body)
	hashStr := hash.String()
	existing, err := h.repo.FindByContentHash(ctx, tenant, hashStr)
	if err == nil {
		uid := existing.ID()
		return dto.ItemOutcome{
			Filename: item.Filename, UploadID: &uid, Status: "deduplicated",
		}
	}
	if !errors.Is(err, repositories.ErrNotFound) {
		return rejected(item.Filename, "lookup_failed", err.Error(), nil)
	}

	// Persist bytes to storage keyed by hash.
	key := hashStr[:2] + "/" + hashStr[2:4] + "/" + hashStr
	if err := h.storage.Put(ctx, key, bytes.NewReader(body)); err != nil {
		return rejected(item.Filename, "storage_write_failed", err.Error(), nil)
	}

	upload, err := entities.NewResumeUpload(entities.UploadInput{
		TenantID:     tenant,
		IntentID:     intentID,
		BatchID:      batchID,
		StorageKey:   key,
		OriginalName: item.Filename,
		MimeType:     mime,
		SizeBytes:    n,
		ContentHash:  hash,
	})
	if err != nil {
		return rejected(item.Filename, "build_failed", err.Error(), nil)
	}

	if err := h.repo.Save(ctx, upload); err != nil {
		if errors.Is(err, repositories.ErrDuplicate) {
			// Race: someone else inserted between FindByContentHash and Save.
			// Re-fetch and treat as deduplicated.
			if dup, derr := h.repo.FindByContentHash(ctx, tenant, hashStr); derr == nil {
				uid := dup.ID()
				return dto.ItemOutcome{
					Filename: item.Filename, UploadID: &uid, Status: "deduplicated",
				}
			}
		}
		return rejected(item.Filename, "persist_failed", err.Error(), nil)
	}

	uid := upload.ID()
	return dto.ItemOutcome{Filename: item.Filename, UploadID: &uid, Status: "queued"}
}

func rejected(filename, code, msg string, detail map[string]any) dto.ItemOutcome {
	return dto.ItemOutcome{
		Filename: filename,
		Error:    &dto.ItemError{Code: code, Message: msg, Detail: detail},
	}
}
