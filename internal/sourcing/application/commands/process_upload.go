package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// ProcessConfig wires the handler.
type ProcessConfig struct {
	Repo         repositories.ResumeUploadRepository
	Storage      services.ResumeStorage
	Scanner      services.FileScanner
	Extractor    services.TextExtractor
	RetryBackoff []time.Duration // attempt[n-1] picks the n-th duration
}

// ProcessUploadHandler runs one ResumeUpload through the next pipeline stage.
// The worker pool calls Handle in a loop, claiming and saving in between.
type ProcessUploadHandler struct {
	cfg ProcessConfig
}

// NewProcessUploadHandler wires the handler.
func NewProcessUploadHandler(cfg ProcessConfig) *ProcessUploadHandler {
	return &ProcessUploadHandler{cfg: cfg}
}

// Handle advances u by one stage. Always persists the resulting state via
// the repository's Save — so events are emitted and the row is durable.
func (h *ProcessUploadHandler) Handle(ctx context.Context, u *entities.ResumeUpload) error {
	switch u.Status() {
	case vo.StatusPending:
		return h.runScanning(ctx, u)
	case vo.StatusScanning:
		// Scanning was claimed but didn't complete in a prior attempt (crash).
		// Re-run the scan idempotently.
		return h.runScanning(ctx, u)
	case vo.StatusExtracting:
		return h.runExtracting(ctx, u)
	default:
		return fmt.Errorf("process: unexpected status %s", u.Status())
	}
}

func (h *ProcessUploadHandler) runScanning(ctx context.Context, u *entities.ResumeUpload) error {
	if u.Status() != vo.StatusScanning {
		if err := u.BeginScanning(); err != nil {
			return fmt.Errorf("transition: %w", err)
		}
	}

	body, err := h.cfg.Storage.Open(ctx, u.StorageKey())
	if err != nil {
		u.ScheduleRetry(vo.Retryable("storage_open", err.Error()), time.Now().UTC(), h.cfg.RetryBackoff)
		return h.cfg.Repo.Save(ctx, u)
	}
	defer body.Close()

	verdict, err := h.cfg.Scanner.Scan(ctx, body)
	if err != nil {
		u.ScheduleRetry(vo.Retryable("scanner_error", err.Error()), time.Now().UTC(), h.cfg.RetryBackoff)
		return h.cfg.Repo.Save(ctx, u)
	}
	if !verdict.Clean {
		if _, qerr := h.cfg.Storage.MoveToQuarantine(ctx, u.StorageKey()); qerr != nil {
			// Best-effort — even if move fails, still quarantine the row.
		}
		if err := u.Quarantine(verdict.Signature); err != nil {
			return fmt.Errorf("quarantine: %w", err)
		}
		return h.cfg.Repo.Save(ctx, u)
	}

	// Clean → transition to Extracting and run extraction. We could split this
	// into two worker turns (Save after Scanning, claim again, run Extracting)
	// but that doubles latency for no real safety benefit in slice 1. Single-pass
	// is fine because stages are idempotent on the row.
	if err := u.BeginExtracting(); err != nil {
		return fmt.Errorf("transition extracting: %w", err)
	}
	return h.runExtracting(ctx, u)
}

func (h *ProcessUploadHandler) runExtracting(ctx context.Context, u *entities.ResumeUpload) error {
	body, err := h.cfg.Storage.Open(ctx, u.StorageKey())
	if err != nil {
		u.ScheduleRetry(vo.Retryable("storage_open", err.Error()), time.Now().UTC(), h.cfg.RetryBackoff)
		return h.cfg.Repo.Save(ctx, u)
	}
	defer body.Close()

	res, err := h.cfg.Extractor.Extract(ctx, body, u.MimeType())
	if err != nil {
		// Slice 1: extraction failure is fatal (no OCR fallback yet — slice 2 adds it).
		if ferr := u.MarkFailed(vo.Fatal("extract_failed", err.Error())); ferr != nil {
			return fmt.Errorf("mark failed: %w", ferr)
		}
		return h.cfg.Repo.Save(ctx, u)
	}

	if err := u.RecordExtractedText(res.Text, res.PageCount); err != nil {
		return fmt.Errorf("record text: %w", err)
	}
	if err := u.CompleteExtracted(); err != nil {
		return fmt.Errorf("complete: %w", err)
	}
	return h.cfg.Repo.Save(ctx, u)
}
