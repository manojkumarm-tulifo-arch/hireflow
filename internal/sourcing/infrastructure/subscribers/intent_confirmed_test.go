package subscribers_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	intentevents "github.com/hustle/hireflow/internal/hiringintent/domain/events"
	intentvo "github.com/hustle/hireflow/internal/hiringintent/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/subscribers"
)

// ---------------------------------------------------------------------------
// Minimal fakes for ScoreIntentHandler dependencies
// ---------------------------------------------------------------------------

// intentReaderStub returns a configurable IntentSnapshot or error.
type intentReaderStub struct {
	snap    services.IntentSnapshot
	findErr error
}

func (r *intentReaderStub) FindByID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (services.IntentSnapshot, error) {
	if r.findErr != nil {
		return services.IntentSnapshot{}, r.findErr
	}
	return r.snap, nil
}

func (r *intentReaderStub) ListConfirmedIntents(_ context.Context, _ shared.TenantID) ([]services.IntentSnapshot, error) {
	return nil, nil
}

// appRepoStub is a minimal ApplicationRepository that records saves.
type appRepoStub struct {
	mu      sync.Mutex
	saves   int
	saveErr error
}

func (r *appRepoStub) Save(_ context.Context, a *entities.Application) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saveErr != nil {
		return r.saveErr
	}
	_ = a.PullEvents()
	r.saves++
	return nil
}

func (r *appRepoStub) FindByID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.Application, error) {
	return nil, repositories.ErrApplicationNotFound
}

func (r *appRepoStub) FindByCandidateAndIntent(_ context.Context, _ shared.TenantID, _, _ uuid.UUID) (*entities.Application, error) {
	return nil, repositories.ErrApplicationNotFound
}

func (r *appRepoStub) ListByIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID, _ repositories.ApplicationListFilter) ([]*entities.Application, error) {
	return nil, nil
}

func (r *appRepoStub) ClaimNextNew(_ context.Context) (*entities.Application, error) {
	return nil, repositories.ErrApplicationNotFound
}

func (r *appRepoStub) TopByCoarseScoreForIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID, _ int) ([]*entities.Application, error) {
	return nil, nil
}

func (r *appRepoStub) savedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saves
}

// candidateRepoStub returns no candidates (empty tenant).
type candidateRepoStub struct{}

func (c *candidateRepoStub) Save(_ context.Context, cand *entities.Candidate) (*entities.Candidate, error) {
	return cand, nil
}

func (c *candidateRepoStub) FindByID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.Candidate, error) {
	return nil, repositories.ErrCandidateNotFound
}

func (c *candidateRepoStub) FindByContentHash(_ context.Context, _ shared.TenantID, _ string) (*entities.Candidate, error) {
	return nil, repositories.ErrCandidateNotFound
}

func (c *candidateRepoStub) ListByTenant(_ context.Context, _ shared.TenantID) ([]*entities.Candidate, error) {
	return nil, nil
}

func (c *candidateRepoStub) UpdateProfileEmbedding(_ context.Context, _ uuid.UUID, _ shared.TenantID, _ []float32) error {
	return nil
}

// judgeJobRepoStub accepts saves without storing.
type judgeJobRepoStub struct{}

func (j *judgeJobRepoStub) Save(_ context.Context, _ *entities.JudgeJob) error { return nil }
func (j *judgeJobRepoStub) ClaimNextPending(_ context.Context) (*entities.JudgeJob, error) {
	return nil, repositories.ErrJudgeJobNotFound
}
func (j *judgeJobRepoStub) FindByID(_ context.Context, _ uuid.UUID) (*entities.JudgeJob, error) {
	return nil, repositories.ErrJudgeJobNotFound
}

// errIntentReader always returns an error from FindByID.
type errIntentReader struct{ err error }

func (r *errIntentReader) FindByID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (services.IntentSnapshot, error) {
	return services.IntentSnapshot{}, r.err
}

func (r *errIntentReader) ListConfirmedIntents(_ context.Context, _ shared.TenantID) ([]services.IntentSnapshot, error) {
	return nil, nil
}

// buildScoreIntentHandler wires a ScoreIntentHandler with controllable stubs.
func buildScoreIntentHandler(ir services.IntentReader, ar repositories.ApplicationRepository) *commands.ScoreIntentHandler {
	return commands.NewScoreIntentHandler(
		ir,
		ar,
		&candidateRepoStub{},
		&judgeJobRepoStub{},
		commands.ScoreIntentConfig{JudgeTopK: 5},
	)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestIntentConfirmedConsumer_HappyPath(t *testing.T) {
	tenantID := shared.NewTenantID()
	intentID := intentvo.NewIntentID()
	parsedID, _ := uuid.Parse(intentID.String())

	// Intent must be Confirmed for ScoreIntentHandler to proceed.
	reader := &intentReaderStub{snap: services.IntentSnapshot{
		ID:          parsedID,
		TenantID:    tenantID,
		Status:      "Confirmed",
		SpecVersion: 1,
	}}
	appRepo := &appRepoStub{}
	handler := buildScoreIntentHandler(reader, appRepo)
	consumer := subscribers.NewIntentConfirmedConsumer(handler, zerolog.Nop())

	recruiterID := shared.NewRecruiterID()
	event := intentevents.NewIntentConfirmed(intentID, tenantID, recruiterID, intentvo.PriorityHigh, time.Now().UTC())

	err := consumer.Handle(context.Background(), event)
	require.NoError(t, err)
}

func TestIntentConfirmedConsumer_WrongEventType_ReturnsError(t *testing.T) {
	handler := buildScoreIntentHandler(&intentReaderStub{}, &appRepoStub{})
	consumer := subscribers.NewIntentConfirmedConsumer(handler, zerolog.Nop())

	// Deliver a string instead of intentevents.IntentConfirmed.
	err := consumer.Handle(context.Background(), "not an IntentConfirmed")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected event type")
}

func TestIntentConfirmedConsumer_CommandError_Propagated(t *testing.T) {
	tenantID := shared.NewTenantID()
	intentID := intentvo.NewIntentID()
	parsedID, _ := uuid.Parse(intentID.String())

	sentinel := errors.New("intent reader: downstream failure")
	reader := &intentReaderStub{
		snap: services.IntentSnapshot{
			ID:          parsedID,
			TenantID:    tenantID,
			Status:      "Confirmed",
			SpecVersion: 1,
		},
		findErr: sentinel,
	}
	handler := buildScoreIntentHandler(reader, &appRepoStub{})
	consumer := subscribers.NewIntentConfirmedConsumer(handler, zerolog.Nop())

	recruiterID := shared.NewRecruiterID()
	event := intentevents.NewIntentConfirmed(intentID, tenantID, recruiterID, intentvo.PriorityLow, time.Now().UTC())

	err := consumer.Handle(context.Background(), event)
	require.Error(t, err)
	assert.True(t, errors.Is(err, sentinel), "original error must be wrapped and propagated")
}
