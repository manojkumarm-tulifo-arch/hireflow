package commands_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeParser implements services.ResumeParser.
type fakeParser struct {
	profile vo.ParsedProfile
	err     error
}

func (f *fakeParser) Parse(_ context.Context, _ string) (vo.ParsedProfile, error) {
	return f.profile, f.err
}

// fakeOCR implements services.OCRExtractor.
type fakeOCR struct {
	result services.RawText
	err    error
}

func (f *fakeOCR) ExtractFromBytes(_ context.Context, _ []byte, _ string) (services.RawText, error) {
	return f.result, f.err
}

// fakeEncryptor implements services.PIIEncryptor — passthrough for test simplicity.
type fakeEncryptor struct {
	err error
}

func (f *fakeEncryptor) Encrypt(_ context.Context, _ shared.TenantID, plaintext string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return "enc:" + plaintext, nil
}

func (f *fakeEncryptor) Decrypt(_ context.Context, _ shared.TenantID, ciphertext string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return strings.TrimPrefix(ciphertext, "enc:"), nil
}

// fakeCandidateRepo implements repositories.CandidateRepository.
type fakeCandidateRepo struct {
	// if returnExisting is non-nil, Save will return that candidate (simulate dedup).
	returnExisting *entities.Candidate
	err            error
	saved          []*entities.Candidate
}

func (r *fakeCandidateRepo) Save(_ context.Context, c *entities.Candidate) (*entities.Candidate, error) {
	if r.err != nil {
		return nil, r.err
	}
	r.saved = append(r.saved, c)
	_ = c.PullEvents()
	if r.returnExisting != nil {
		return r.returnExisting, nil
	}
	return c, nil
}

func (r *fakeCandidateRepo) FindByID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.Candidate, error) {
	return nil, repositories.ErrCandidateNotFound
}

func (r *fakeCandidateRepo) FindByContentHash(_ context.Context, _ shared.TenantID, _ string) (*entities.Candidate, error) {
	return nil, repositories.ErrCandidateNotFound
}

func (r *fakeCandidateRepo) ListByTenant(_ context.Context, _ shared.TenantID) ([]*entities.Candidate, error) {
	return nil, nil
}

func (r *fakeCandidateRepo) UpdateProfileEmbedding(_ context.Context, _ uuid.UUID, _ shared.TenantID, _ []float32) error {
	return nil
}
func (r *fakeCandidateRepo) EraseCascade(_ context.Context, _ shared.TenantID, _ uuid.UUID) ([]string, error) {
	return nil, repositories.ErrCandidateNotFound
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func goodProfile() vo.ParsedProfile {
	p := vo.NewParsedProfile()
	p.Personal.FullName = "Alice"
	p.Personal.Email = "alice@example.com"
	p.Personal.Phone = "+91-9999999999"
	p.Personal.Location = "Bangalore"
	p.Headline = "Senior Backend Engineer"
	return p
}

// newExtractedUpload builds a ResumeUpload in StatusExtracted state with the
// given extracted text already recorded on the artifacts.
func newExtractedUpload(t *testing.T, text string) *entities.ResumeUpload {
	t.Helper()
	u := newPendingUpload(t)
	_ = u.PullEvents()
	require.NoError(t, u.BeginScanning())
	require.NoError(t, u.BeginExtracting())
	require.NoError(t, u.RecordExtractedText(text, 1))
	require.NoError(t, u.CompleteExtracted())
	_ = u.PullEvents()
	return u
}

func newParsingConfig(
	store *fakeStorage,
	repo *fakeRepo,
	parser *fakeParser,
	ocr *fakeOCR,
	enc *fakeEncryptor,
	candRepo *fakeCandidateRepo,
) commands.ProcessConfig {
	return commands.ProcessConfig{
		Repo:          repo,
		Storage:       store,
		Scanner:       &fakeScanner{verdict: services.ScanVerdict{Clean: true}},
		Extractor:     &fakeExtractor{res: services.RawText{Text: "x", PageCount: 1}},
		RetryBackoff:  []time.Duration{30 * time.Second, time.Minute},
		Parser:        parser,
		OCR:           ocr,
		Encryptor:     enc,
		CandidateRepo: candRepo,
		OCRThreshold:  50,
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// Test 1: Happy path — sufficient extracted text → parser succeeds → Parsed.
func TestRunParsing_HappyPath_SufficientText(t *testing.T) {
	store := newFakeStorage()
	repo := newFakeRepo()
	require.NoError(t, store.Put(context.Background(), "k", strings.NewReader("body")))

	longText := strings.Repeat("a", 100)
	u := newExtractedUpload(t, longText)
	require.NoError(t, repo.Save(context.Background(), u))

	candRepo := &fakeCandidateRepo{}
	cfg := newParsingConfig(store, repo, &fakeParser{profile: goodProfile()}, &fakeOCR{}, &fakeEncryptor{}, candRepo)
	h := commands.NewProcessUploadHandler(cfg)

	require.NoError(t, h.Handle(context.Background(), u))

	assert.Equal(t, vo.StatusParsed, u.Status())
	assert.NotEqual(t, uuid.Nil, u.CandidateID())
	_, ok := u.Artifacts().ParsedProfile()
	assert.True(t, ok, "parsed profile artifact must be set")
	assert.Len(t, candRepo.saved, 1)
}

// Test 2: OCR fallback — empty extracted text → OCR succeeds → parsing succeeds → Parsed.
func TestRunParsing_OCRFallback_EmptyText(t *testing.T) {
	store := newFakeStorage()
	repo := newFakeRepo()
	require.NoError(t, store.Put(context.Background(), "k", strings.NewReader("%PDF-1.4 fake")))

	u := newExtractedUpload(t, "") // empty extracted text — triggers OCR
	require.NoError(t, repo.Save(context.Background(), u))

	ocrText := strings.Repeat("b", 100) // plenty of text from OCR
	candRepo := &fakeCandidateRepo{}
	cfg := newParsingConfig(
		store, repo,
		&fakeParser{profile: goodProfile()},
		&fakeOCR{result: services.RawText{Text: ocrText, PageCount: 1}},
		&fakeEncryptor{},
		candRepo,
	)
	h := commands.NewProcessUploadHandler(cfg)

	require.NoError(t, h.Handle(context.Background(), u))

	assert.Equal(t, vo.StatusParsed, u.Status())
	assert.Len(t, candRepo.saved, 1)
}

// Test 3: OCR returns empty → upload MarkFailed("unreadable").
func TestRunParsing_OCRReturnsEmpty_MarksFailed(t *testing.T) {
	store := newFakeStorage()
	repo := newFakeRepo()
	require.NoError(t, store.Put(context.Background(), "k", strings.NewReader("%PDF-1.4 fake")))

	u := newExtractedUpload(t, "") // triggers OCR
	require.NoError(t, repo.Save(context.Background(), u))

	candRepo := &fakeCandidateRepo{}
	cfg := newParsingConfig(
		store, repo,
		&fakeParser{profile: goodProfile()},
		&fakeOCR{result: services.RawText{Text: "  ", PageCount: 1}}, // < threshold
		&fakeEncryptor{},
		candRepo,
	)
	h := commands.NewProcessUploadHandler(cfg)

	require.NoError(t, h.Handle(context.Background(), u))

	assert.Equal(t, vo.StatusFailed, u.Status())
	assert.Contains(t, u.LastError(), "ocr returned empty text")
}

// Test 4: Parser fatal error → upload Failed.
func TestRunParsing_ParserFatalError_MarksFailed(t *testing.T) {
	store := newFakeStorage()
	repo := newFakeRepo()
	require.NoError(t, store.Put(context.Background(), "k", strings.NewReader("body")))

	longText := strings.Repeat("c", 100)
	u := newExtractedUpload(t, longText)
	require.NoError(t, repo.Save(context.Background(), u))

	fatalErr := services.ResumeParseError{Retryable: false, Reason: "no_tool_use", Detail: "model refused"}
	candRepo := &fakeCandidateRepo{}
	cfg := newParsingConfig(
		store, repo,
		&fakeParser{err: fatalErr},
		&fakeOCR{},
		&fakeEncryptor{},
		candRepo,
	)
	h := commands.NewProcessUploadHandler(cfg)

	require.NoError(t, h.Handle(context.Background(), u))

	assert.Equal(t, vo.StatusFailed, u.Status())
	assert.Contains(t, u.LastError(), "model refused")
}

// Test 5: Parser retryable error → upload back to Pending with attempt_count++.
func TestRunParsing_ParserRetryableError_SchedulesRetry(t *testing.T) {
	store := newFakeStorage()
	repo := newFakeRepo()
	require.NoError(t, store.Put(context.Background(), "k", strings.NewReader("body")))

	longText := strings.Repeat("d", 100)
	u := newExtractedUpload(t, longText)
	require.NoError(t, repo.Save(context.Background(), u))

	retryErr := services.ResumeParseError{Retryable: true, Reason: "anthropic_5xx", Detail: "503 service unavailable"}
	candRepo := &fakeCandidateRepo{}
	cfg := newParsingConfig(
		store, repo,
		&fakeParser{err: retryErr},
		&fakeOCR{},
		&fakeEncryptor{},
		candRepo,
	)
	h := commands.NewProcessUploadHandler(cfg)

	require.NoError(t, h.Handle(context.Background(), u))

	assert.Equal(t, vo.StatusPending, u.Status(), "row reverts to Pending for re-claim")
	assert.Equal(t, 1, u.AttemptCount())
	assert.True(t, u.NextAttemptAt().After(time.Now()))
}

// Test 6: Dedup attach — candidate Save returns existing candidate with different ID.
// Upload must link to the existing candidate's ID.
func TestRunParsing_DedupAttach_LinksToExistingCandidate(t *testing.T) {
	store := newFakeStorage()
	repo := newFakeRepo()
	require.NoError(t, store.Put(context.Background(), "k", strings.NewReader("body")))

	longText := strings.Repeat("e", 100)
	u := newExtractedUpload(t, longText)
	require.NoError(t, repo.Save(context.Background(), u))

	// Build an existing candidate with a different ID that the repo will return.
	existingHash, err := vo.NewContentHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.NoError(t, err)
	profile := goodProfile()
	existingCand, err := entities.NewCandidate(entities.NewCandidateInput{
		TenantID:    u.TenantID(),
		ContentHash: existingHash,
		Profile:     profile,
		Encrypted:   entities.EncryptedPersonal{FullName: "enc:Alice", Email: "enc:alice@example.com", Phone: "enc:+91"},
		Location:    "Bangalore",
		Headline:    "Senior Backend Engineer",
		Source:      "manual_upload",
	})
	require.NoError(t, err)
	_ = existingCand.PullEvents()

	candRepo := &fakeCandidateRepo{returnExisting: existingCand}
	cfg := newParsingConfig(
		store, repo,
		&fakeParser{profile: goodProfile()},
		&fakeOCR{},
		&fakeEncryptor{},
		candRepo,
	)
	h := commands.NewProcessUploadHandler(cfg)

	require.NoError(t, h.Handle(context.Background(), u))

	assert.Equal(t, vo.StatusParsed, u.Status())
	assert.Equal(t, existingCand.ID(), u.CandidateID(), "upload must link to the existing candidate")
}
