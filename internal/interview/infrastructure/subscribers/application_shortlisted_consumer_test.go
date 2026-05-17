package subscribers_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/interview/application/commands"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	"github.com/hustle/hireflow/internal/interview/infrastructure/subscribers"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ---------------------------------------------------------------------------
// Stub ProcessRepository — records saves, returns ErrProcessNotFound on lookups
// ---------------------------------------------------------------------------

type stubProcessRepo struct {
	saved []*entities.InterviewProcess
}

func (r *stubProcessRepo) Save(_ context.Context, p *entities.InterviewProcess) error {
	r.saved = append(r.saved, p)
	return nil
}
func (r *stubProcessRepo) FindByID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.InterviewProcess, error) {
	return nil, repositories.ErrProcessNotFound
}
func (r *stubProcessRepo) FindByApplicationID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.InterviewProcess, error) {
	return nil, repositories.ErrProcessNotFound
}
func (r *stubProcessRepo) FindByRoundID(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.InterviewProcess, error) {
	return nil, repositories.ErrProcessNotFound
}
func (r *stubProcessRepo) ListByTenant(_ context.Context, _ shared.TenantID, _ repositories.ProcessListFilter) ([]*entities.InterviewProcess, error) {
	return nil, nil
}
func (r *stubProcessRepo) ClaimNextPendingRound(_ context.Context) (*entities.InterviewProcess, uuid.UUID, error) {
	return nil, uuid.Nil, repositories.ErrProcessNotFound
}

// ---------------------------------------------------------------------------
// Stub LoopTemplateRepository — always returns ErrLoopTemplateNotFound
// so StartInterviewProcess falls back to DefaultLoop.
// ---------------------------------------------------------------------------

type stubTemplateRepo struct{}

func (r *stubTemplateRepo) Save(_ context.Context, _ *entities.LoopTemplate) error {
	return nil
}
func (r *stubTemplateRepo) FindByIntent(_ context.Context, _ shared.TenantID, _ uuid.UUID) (*entities.LoopTemplate, error) {
	return nil, repositories.ErrLoopTemplateNotFound
}

// ---------------------------------------------------------------------------
// Helper: build a StartInterviewProcessHandler with stub repos.
// ---------------------------------------------------------------------------

func buildStartHandler(processRepo *stubProcessRepo) *commands.StartInterviewProcessHandler {
	return commands.NewStartInterviewProcessHandler(processRepo, &stubTemplateRepo{})
}

// ---------------------------------------------------------------------------
// shortlistedEvent is a minimal struct whose JSON matches shortlistedPayload.
// It exists only in the test to construct valid bus events without importing
// the sourcing event type.
// ---------------------------------------------------------------------------

type shortlistedEvent struct {
	ApplicationID uuid.UUID `json:"application_id"`
	CandidateID   uuid.UUID `json:"candidate_id"`
	IntentID      uuid.UUID `json:"intent_id"`
	TenantID      string    `json:"tenant_id"`
}

// marshaledEvent round-trips through JSON to mimic how the bus re-delivers it.
func marshaledEvent(ev shortlistedEvent) any {
	raw, _ := json.Marshal(ev)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestConsumer_HappyPath_CallsStartHandler verifies that a valid event payload
// results in a saved InterviewProcess.
func TestConsumer_HappyPath_CallsStartHandler(t *testing.T) {
	tenantID := shared.NewTenantID()
	appID := uuid.New()
	candidateID := uuid.New()
	intentID := uuid.New()

	processRepo := &stubProcessRepo{}
	handler := buildStartHandler(processRepo)
	consumer := subscribers.NewApplicationShortlistedConsumer(handler, zerolog.Nop())

	ev := shortlistedEvent{
		ApplicationID: appID,
		CandidateID:   candidateID,
		IntentID:      intentID,
		TenantID:      tenantID.String(),
	}
	err := consumer.Handle(context.Background(), marshaledEvent(ev))
	require.NoError(t, err)

	require.Len(t, processRepo.saved, 1, "one process should have been saved")
	saved := processRepo.saved[0]
	assert.Equal(t, appID, saved.ApplicationID())
	assert.Equal(t, candidateID, saved.CandidateID())
	assert.Equal(t, intentID, saved.IntentID())
}

// TestConsumer_MissingFields_ReturnsErr verifies that a payload with a nil
// UUID in a required field is rejected before the handler is called.
func TestConsumer_MissingFields_ReturnsErr(t *testing.T) {
	tenantID := shared.NewTenantID()

	processRepo := &stubProcessRepo{}
	handler := buildStartHandler(processRepo)
	consumer := subscribers.NewApplicationShortlistedConsumer(handler, zerolog.Nop())

	// CandidateID is nil — should cause an error.
	ev := shortlistedEvent{
		ApplicationID: uuid.New(),
		CandidateID:   uuid.Nil, // missing
		IntentID:      uuid.New(),
		TenantID:      tenantID.String(),
	}
	err := consumer.Handle(context.Background(), marshaledEvent(ev))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "incomplete payload")
	assert.Empty(t, processRepo.saved, "handler should not have been called")
}

// TestConsumer_BadTenantID_ReturnsErr verifies that a payload with an invalid
// tenant_id string is rejected.
func TestConsumer_BadTenantID_ReturnsErr(t *testing.T) {
	processRepo := &stubProcessRepo{}
	handler := buildStartHandler(processRepo)
	consumer := subscribers.NewApplicationShortlistedConsumer(handler, zerolog.Nop())

	ev := shortlistedEvent{
		ApplicationID: uuid.New(),
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
		TenantID:      "not-a-uuid", // invalid
	}
	err := consumer.Handle(context.Background(), marshaledEvent(ev))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant")
	assert.Empty(t, processRepo.saved, "handler should not have been called")
}
