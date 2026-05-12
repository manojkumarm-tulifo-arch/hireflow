package commands_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// fakeScanner returns a configurable verdict.
type fakeScanner struct {
	verdict services.ScanVerdict
	err     error
}

func (f *fakeScanner) Scan(_ context.Context, r io.Reader) (services.ScanVerdict, error) {
	if r != nil {
		_, _ = io.Copy(io.Discard, r)
	}
	return f.verdict, f.err
}

// fakeExtractor returns a configurable result.
type fakeExtractor struct {
	res services.RawText
	err error
}

func (f *fakeExtractor) Extract(_ context.Context, r io.Reader, _ vo.MimeType) (services.RawText, error) {
	if r != nil {
		_, _ = io.Copy(io.Discard, r)
	}
	return f.res, f.err
}

// existing fakeStorage and fakeRepo from upload_resume_batch_test.go file
// are in the same package_test, so reusable here.

func newPendingUpload(t *testing.T) *entities.ResumeUpload {
	t.Helper()
	mime, _ := vo.ParseMimeType("application/pdf")
	h, _ := vo.NewContentHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	u, err := entities.NewResumeUpload(entities.UploadInput{
		TenantID: shared.NewTenantID(), IntentID: uuid.New(), BatchID: uuid.New(),
		StorageKey: "k", OriginalName: "alice.pdf",
		MimeType: mime, SizeBytes: 100, ContentHash: h,
	})
	require.NoError(t, err)
	_ = u.PullEvents() // drain Accepted
	return u
}

func TestProcess_PendingScansAndExtractsSuccessfully(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()

	// Pre-write the bytes the worker will Open.
	require.NoError(t, store.Put(context.Background(), "k", strings.NewReader("body")))

	u := newPendingUpload(t)
	require.NoError(t, repo.Save(context.Background(), u))

	h := commands.NewProcessUploadHandler(commands.ProcessConfig{
		Repo:         repo,
		Storage:      store,
		Scanner:      &fakeScanner{verdict: services.ScanVerdict{Clean: true}},
		Extractor:    &fakeExtractor{res: services.RawText{Text: "hello", PageCount: 1}},
		RetryBackoff: []time.Duration{time.Second},
	})

	require.NoError(t, h.Handle(context.Background(), u))

	// Final state.
	assert.Equal(t, vo.StatusExtracted, u.Status())
	text, pages, ok := u.Artifacts().ExtractedText()
	require.True(t, ok)
	assert.Equal(t, "hello", text)
	assert.Equal(t, 1, pages)
}

func TestProcess_VirusDetected_Quarantines(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	require.NoError(t, store.Put(context.Background(), "k", strings.NewReader("body")))

	u := newPendingUpload(t)
	require.NoError(t, repo.Save(context.Background(), u))

	h := commands.NewProcessUploadHandler(commands.ProcessConfig{
		Repo:    repo,
		Storage: store,
		Scanner: &fakeScanner{verdict: services.ScanVerdict{Clean: false, Signature: "EICAR-TEST"}},
		// Extractor must not be called.
		Extractor:    &fakeExtractor{err: errors.New("must not be called")},
		RetryBackoff: []time.Duration{time.Second},
	})

	require.NoError(t, h.Handle(context.Background(), u))
	assert.Equal(t, vo.StatusQuarantined, u.Status())
	assert.Equal(t, "EICAR-TEST", u.LastError())
	// File moved to quarantine prefix.
	_, ok := store.puts["quarantine/k"]
	assert.True(t, ok)
}

func TestProcess_ScannerTransientError_SchedulesRetry(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	require.NoError(t, store.Put(context.Background(), "k", bytes.NewReader([]byte("x"))))

	u := newPendingUpload(t)
	require.NoError(t, repo.Save(context.Background(), u))

	h := commands.NewProcessUploadHandler(commands.ProcessConfig{
		Repo:      repo,
		Storage:   store,
		Scanner:   &fakeScanner{err: errors.New("clamd connection refused")},
		Extractor: &fakeExtractor{},
		RetryBackoff: []time.Duration{30 * time.Second, time.Minute},
	})

	require.NoError(t, h.Handle(context.Background(), u))
	assert.Equal(t, vo.StatusPending, u.Status(), "row reverts to Pending for re-claim")
	assert.Equal(t, 1, u.AttemptCount())
	assert.True(t, u.NextAttemptAt().After(time.Now()))
}

func TestProcess_ExtractorFatalError_MarksFailed(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	require.NoError(t, store.Put(context.Background(), "k", strings.NewReader("body")))

	u := newPendingUpload(t)
	require.NoError(t, repo.Save(context.Background(), u))

	h := commands.NewProcessUploadHandler(commands.ProcessConfig{
		Repo:    repo,
		Storage: store,
		Scanner: &fakeScanner{verdict: services.ScanVerdict{Clean: true}},
		// Treat extraction error as fatal in slice 1 (no OCR fallback yet).
		Extractor:    &fakeExtractor{err: errors.New("corrupt pdf")},
		RetryBackoff: []time.Duration{time.Second},
	})

	require.NoError(t, h.Handle(context.Background(), u))
	assert.Equal(t, vo.StatusFailed, u.Status())
	assert.Contains(t, u.LastError(), "corrupt pdf")
}
