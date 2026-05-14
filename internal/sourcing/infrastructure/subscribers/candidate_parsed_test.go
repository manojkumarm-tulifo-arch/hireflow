package subscribers_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	sourcingevents "github.com/hustle/hireflow/internal/sourcing/domain/events"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/subscribers"
)

// ---------------------------------------------------------------------------
// Stubs specific to CandidateParsed tests
// ---------------------------------------------------------------------------

// emptyIntentReader returns no confirmed intents so ScoreCandidate
// proceeds without creating any Application rows (no fan-out).
type emptyIntentReader struct{}

func (e *emptyIntentReader) FindByID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (services.IntentSnapshot, error) {
	panic("FindByID not expected in CandidateParsed test")
}

func (e *emptyIntentReader) ListConfirmedIntents(_ context.Context, _ shared.TenantID) ([]services.IntentSnapshot, error) {
	return nil, nil
}

// candidateFinderStub is a CandidateRepository that returns a given candidate on FindByID.
type candidateFinderStub struct {
	cand *entities.Candidate
	err  error
}

func (r *candidateFinderStub) Save(_ context.Context, c *entities.Candidate) (*entities.Candidate, error) {
	return c, nil
}

func (r *candidateFinderStub) FindByID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.Candidate, error) {
	return r.cand, r.err
}

func (r *candidateFinderStub) FindByContentHash(_ context.Context, _ shared.TenantID, _ string) (*entities.Candidate, error) {
	return nil, repositories.ErrCandidateNotFound
}

func (r *candidateFinderStub) ListByTenant(_ context.Context, _ shared.TenantID) ([]*entities.Candidate, error) {
	return nil, nil
}

func (r *candidateFinderStub) UpdateProfileEmbedding(_ context.Context, _ uuid.UUID, _ shared.TenantID, _ []float32) error {
	return nil
}
func (r *candidateFinderStub) EraseCascade(_ context.Context, _ shared.TenantID, _ uuid.UUID) ([]string, error) {
	return nil, repositories.ErrCandidateNotFound
}

// makeTestCandidate builds a minimal valid Candidate for use in subscriber tests.
func makeTestCandidate(t *testing.T, tenantID shared.TenantID) *entities.Candidate {
	t.Helper()
	hash, err := vo.NewContentHash("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	require.NoError(t, err)
	profile := vo.NewParsedProfile()
	profile.Personal.FullName = "Subscriber Test Candidate"
	profile.Personal.Email = "sub@example.com"
	cand, err := entities.NewCandidate(entities.NewCandidateInput{
		TenantID:    tenantID,
		ContentHash: hash,
		Profile:     profile,
		Encrypted:   entities.EncryptedPersonal{FullName: "enc:Sub", Email: "enc:sub@example.com"},
		Location:    "Remote",
		Headline:    "Engineer",
		Source:      "manual_upload",
	})
	require.NoError(t, err)
	_ = cand.PullEvents()
	return cand
}

// buildScoreCandidateHandler wires a ScoreCandidateHandler with the given stubs.
func buildScoreCandidateHandler(cr repositories.CandidateRepository) *commands.ScoreCandidateHandler {
	return commands.NewScoreCandidateHandler(
		cr,
		&emptyIntentReader{},
		&appRepoStub{}, // reused from intent_confirmed_test.go
	)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestCandidateParsedConsumer_HappyPath(t *testing.T) {
	tenantID := shared.NewTenantID()
	candidateID := uuid.New()

	cand := makeTestCandidate(t, tenantID)
	cr := &candidateFinderStub{cand: cand}
	handler := buildScoreCandidateHandler(cr)
	consumer := subscribers.NewCandidateParsedConsumer(handler, zerolog.Nop())

	event := sourcingevents.CandidateParsed{
		CandidateID:   candidateID,
		TenantID:      tenantID,
		ContentHash:   "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		SchemaVersion: 1,
		OccurredAt:    time.Now().UTC(),
	}

	err := consumer.Handle(context.Background(), event)
	require.NoError(t, err)
}

func TestCandidateParsedConsumer_WrongEventType_ReturnsError(t *testing.T) {
	handler := buildScoreCandidateHandler(&candidateFinderStub{})
	consumer := subscribers.NewCandidateParsedConsumer(handler, zerolog.Nop())

	// Deliver a string instead of sourcingevents.CandidateParsed.
	err := consumer.Handle(context.Background(), "not a CandidateParsed event")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected event type")
}

func TestCandidateParsedConsumer_CommandError_Propagated(t *testing.T) {
	tenantID := shared.NewTenantID()
	candidateID := uuid.New()

	sentinel := errors.New("candidate repo: find failure")
	cr := &candidateFinderStub{err: sentinel}
	handler := buildScoreCandidateHandler(cr)
	consumer := subscribers.NewCandidateParsedConsumer(handler, zerolog.Nop())

	event := sourcingevents.CandidateParsed{
		CandidateID:   candidateID,
		TenantID:      tenantID,
		ContentHash:   "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		SchemaVersion: 1,
		OccurredAt:    time.Now().UTC(),
	}

	err := consumer.Handle(context.Background(), event)
	require.Error(t, err)
	assert.True(t, errors.Is(err, sentinel), "original error must be wrapped and propagated")
}
