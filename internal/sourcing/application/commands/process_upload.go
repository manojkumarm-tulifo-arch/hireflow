package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// ProcessConfig wires the handler.
type ProcessConfig struct {
	Repo          repositories.ResumeUploadRepository
	Storage       services.ResumeStorage
	Scanner       services.FileScanner
	Extractor     services.TextExtractor
	RetryBackoff  []time.Duration // attempt[n-1] picks the n-th duration
	Parser        services.ResumeParser
	OCR           services.OCRExtractor
	Encryptor     services.PIIEncryptor
	CandidateRepo repositories.CandidateRepository
	OCRThreshold  int    // default 50; < this triggers OCR fallback
	PromptVersion string // unused in slice 2 logic, kept for audit/wiring
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
		rawBytes, rerr := io.ReadAll(body)
		body.Close()
		if rerr != nil {
			u.ScheduleRetry(vo.Retryable("storage_read", rerr.Error()), time.Now().UTC(), h.cfg.RetryBackoff)
			return h.cfg.Repo.Save(ctx, u)
		}
		ocrOut, oerr := h.cfg.OCR.ExtractFromBytes(ctx, rawBytes, u.MimeType().String())
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
	cand, cerr := entities.NewCandidate(entities.NewCandidateInput{
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
	if cerr != nil {
		if err := u.MarkFailed(vo.Fatal("candidate_build", cerr.Error())); err != nil {
			return fmt.Errorf("mark failed candidate: %w", err)
		}
		return h.cfg.Repo.Save(ctx, u)
	}

	saved, serr := h.cfg.CandidateRepo.Save(ctx, cand)
	if serr != nil {
		u.ScheduleRetry(vo.Retryable("candidate_save", serr.Error()), time.Now().UTC(), h.cfg.RetryBackoff)
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
