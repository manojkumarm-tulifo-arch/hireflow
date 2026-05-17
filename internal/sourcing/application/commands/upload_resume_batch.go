// Package commands holds the sourcing application-layer command handlers.
package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/dto"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/text"
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
//
// ZIP files are fanned out: the ZIP itself becomes an "extracted_from_zip"
// parent outcome, and each entry inside becomes a child outcome (queued or
// duplicate_in_intent) carrying ParentFilename and ParentItemID.
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

		body, n, readErr := h.readBounded(item)
		if readErr != nil {
			out.Items = append(out.Items, rejected(item.Filename, readErr.code, readErr.msg, readErr.detail))
			continue
		}

		mime, err := vo.SniffMimeType(body)
		if err != nil {
			out.Items = append(out.Items, rejected(item.Filename, "mime_unsupported", err.Error(), nil))
			continue
		}

		if mime.String() == "application/zip" {
			outcomes := h.processZip(ctx, tenant, in.IntentID, out.BatchID, item.Filename, body)
			out.Items = append(out.Items, outcomes...)
			continue
		}

		outcome := h.processOneFile(ctx, tenant, in.IntentID, out.BatchID, item.Filename, body, mime, n, nil, nil)
		out.Items = append(out.Items, outcome)
	}
	return out, nil
}

// readError is an internal struct carrying a structured read-phase error.
type readError struct {
	code   string
	msg    string
	detail map[string]any
}

// readBounded reads up to MaxFileBytes from item.Body into a buffer.
// Returns (body, sizeBytes, nil) on success or (nil, 0, *readError) on failure.
func (h *UploadResumeBatchHandler) readBounded(item dto.BatchItem) ([]byte, int64, *readError) {
	limit := h.cfg.MaxFileBytes
	if limit <= 0 {
		limit = 10 * 1024 * 1024 // safe default
	}
	buf := &bytes.Buffer{}
	n, err := io.Copy(buf, io.LimitReader(item.Body, limit+1))
	if err != nil {
		return nil, 0, &readError{"read_failed", err.Error(), nil}
	}
	if n == 0 {
		return nil, 0, &readError{"empty_file", "no bytes read", nil}
	}
	if n > limit {
		return nil, 0, &readError{"size_exceeded", "file exceeds limit", map[string]any{"limit_bytes": limit}}
	}
	return buf.Bytes(), n, nil
}

// processZip fans out a ZIP file into per-entry outcomes. Returns a parent
// "extracted_from_zip" outcome followed by one outcome per ZIP entry.
// If the ZIP cannot be opened or violates limits, returns a single rejected outcome.
func (h *UploadResumeBatchHandler) processZip(
	ctx context.Context,
	tenant shared.TenantID,
	intentID, batchID uuid.UUID,
	filename string,
	body []byte,
) []dto.ItemOutcome {
	entries, err := text.ExtractZip(body, text.DefaultZipLimits)
	if err != nil {
		code := mapZipError(err)
		return []dto.ItemOutcome{rejected(filename, code, err.Error(), nil)}
	}

	parentID := uuid.New().String()
	out := make([]dto.ItemOutcome, 0, 1+len(entries))
	out = append(out, dto.ItemOutcome{
		Filename:     filename,
		Status:       "extracted_from_zip",
		ParentItemID: &parentID,
	})

	for _, entry := range entries {
		mime, err := vo.SniffMimeType(entry.Bytes)
		var child dto.ItemOutcome
		if err != nil {
			child = rejected(entry.Filename, "unsupported_format", err.Error(), nil)
		} else {
			child = h.processOneFile(ctx, tenant, intentID, batchID,
				entry.Filename, entry.Bytes, mime, int64(len(entry.Bytes)),
				&filename, &parentID)
		}
		// Always attach parent linkage, even on rejection paths.
		child.ParentFilename = &filename
		child.ParentItemID = &parentID
		out = append(out, child)
	}
	return out
}

// mapZipError converts a text.ExtractZip sentinel error to an outcome error code.
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

// processOneFile handles a single non-ZIP file: deduplicates by (intent, content_hash),
// writes to storage, persists the aggregate, and returns the outcome.
// parentFilename and parentItemID are non-nil for entries extracted from a ZIP.
func (h *UploadResumeBatchHandler) processOneFile(
	ctx context.Context,
	tenant shared.TenantID,
	intentID, batchID uuid.UUID,
	filename string,
	body []byte,
	mime vo.MimeType,
	sizeBytes int64,
	parentFilename *string,
	parentItemID *string,
) dto.ItemOutcome {
	hash := vo.ComputeContentHash(body)
	hashStr := hash.String()

	// Per-intent dedup: if this (tenant, intent, hash) already exists, skip.
	existing, err := h.repo.FindByContentHashAndIntent(ctx, tenant, intentID, hashStr)
	if err == nil {
		uid := existing.ID()
		return dto.ItemOutcome{
			Filename:       filename,
			UploadID:       &uid,
			Status:         "duplicate_in_intent",
			ParentFilename: parentFilename,
			ParentItemID:   parentItemID,
		}
	}
	if !errors.Is(err, repositories.ErrNotFound) {
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
		SizeBytes:    sizeBytes,
		ContentHash:  hash,
	})
	if err != nil {
		return rejected(filename, "build_failed", err.Error(), nil)
	}

	if err := h.repo.Save(ctx, upload); err != nil {
		if errors.Is(err, repositories.ErrDuplicate) {
			// Race: another goroutine inserted between FindByContentHashAndIntent and Save.
			// Re-fetch and return duplicate_in_intent.
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
		Filename:       filename,
		UploadID:       &uid,
		Status:         "queued",
		ParentFilename: parentFilename,
		ParentItemID:   parentItemID,
	}
}

func rejected(filename, code, msg string, detail map[string]any) dto.ItemOutcome {
	return dto.ItemOutcome{
		Filename: filename,
		Error:    &dto.ItemError{Code: code, Message: msg, Detail: detail},
	}
}
