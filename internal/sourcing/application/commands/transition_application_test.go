package commands_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	auditdomain "github.com/hustle/hireflow/internal/shared/audit/domain"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// ---------------------------------------------------------------------------
// fakeAuditWriter — captures writes and supports injecting errors.
// ---------------------------------------------------------------------------

type fakeAuditWriter struct {
	events   []auditdomain.AuditEvent
	writeErr error
}

func (w *fakeAuditWriter) Write(_ context.Context, ev auditdomain.AuditEvent) error {
	if w.writeErr != nil {
		return w.writeErr
	}
	w.events = append(w.events, ev)
	return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// makeShortlistedApp returns a scored Application that has then been shortlisted.
func makeShortlistedApp(t *testing.T, tenantID shared.TenantID) *entities.Application {
	t.Helper()
	app := makeScoredApplication(t, tenantID, uuid.New(), uuid.New())
	actor := uuid.New()
	require.NoError(t, app.Shortlist(actor))
	_ = app.PullEvents()
	return app
}

func buildTransitionHandler(
	appRepo *fakeApplicationRepo,
	audit auditdomain.AuditWriter,
) *commands.TransitionApplicationHandler {
	return commands.NewTransitionApplicationHandler(appRepo, audit)
}

// ---------------------------------------------------------------------------
// Shortlist tests
// ---------------------------------------------------------------------------

func TestTransitionApplication_Shortlist_HappyPath(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	app := makeScoredApplication(t, tenantID, uuid.New(), uuid.New())
	appRepo := newFakeApplicationRepo()
	require.NoError(t, appRepo.Save(ctx, app))
	appRepo.saves = 0

	audit := &fakeAuditWriter{}
	h := buildTransitionHandler(appRepo, audit)

	err := h.Handle(ctx, commands.TransitionApplicationInput{
		TenantID:      tenantID,
		ActorUserID:   uuid.New(),
		ApplicationID: app.ID(),
		Action:        commands.ActionShortlist,
	})

	require.NoError(t, err)
	assert.Equal(t, vo.AppStatusShortlisted, app.Status())
	assert.Equal(t, 1, appRepo.savedCount())
	require.Len(t, audit.events, 1)
	assert.Equal(t, "application_shortlist", audit.events[0].Action)
	assert.Equal(t, app.ID(), audit.events[0].ResourceID)
	assert.Nil(t, audit.events[0].Payload, "no payload for shortlist")
}

func TestTransitionApplication_Shortlist_InvalidTransition(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	// Rejected app cannot be shortlisted.
	app := makeScoredApplication(t, tenantID, uuid.New(), uuid.New())
	actor := uuid.New()
	require.NoError(t, app.Reject(actor, "does not fit"))
	_ = app.PullEvents()

	appRepo := newFakeApplicationRepo()
	require.NoError(t, appRepo.Save(ctx, app))
	appRepo.saves = 0

	audit := &fakeAuditWriter{}
	h := buildTransitionHandler(appRepo, audit)

	err := h.Handle(ctx, commands.TransitionApplicationInput{
		TenantID:      tenantID,
		ActorUserID:   uuid.New(),
		ApplicationID: app.ID(),
		Action:        commands.ActionShortlist,
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrInvalidTransition))
	assert.Empty(t, audit.events, "audit must not be written on error")
}

// ---------------------------------------------------------------------------
// Reject tests
// ---------------------------------------------------------------------------

func TestTransitionApplication_Reject_HappyPath(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	app := makeScoredApplication(t, tenantID, uuid.New(), uuid.New())
	appRepo := newFakeApplicationRepo()
	require.NoError(t, appRepo.Save(ctx, app))
	appRepo.saves = 0

	audit := &fakeAuditWriter{}
	h := buildTransitionHandler(appRepo, audit)

	err := h.Handle(ctx, commands.TransitionApplicationInput{
		TenantID:      tenantID,
		ActorUserID:   uuid.New(),
		ApplicationID: app.ID(),
		Action:        commands.ActionReject,
		RejectReason:  "overqualified",
	})

	require.NoError(t, err)
	assert.Equal(t, vo.AppStatusRejected, app.Status())
	require.Len(t, audit.events, 1)
	assert.Equal(t, "application_reject", audit.events[0].Action)
	assert.Equal(t, "overqualified", audit.events[0].Payload["reason"])
}

func TestTransitionApplication_Reject_MissingReason_ReturnsError(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	app := makeScoredApplication(t, tenantID, uuid.New(), uuid.New())
	appRepo := newFakeApplicationRepo()
	require.NoError(t, appRepo.Save(ctx, app))

	audit := &fakeAuditWriter{}
	h := buildTransitionHandler(appRepo, audit)

	err := h.Handle(ctx, commands.TransitionApplicationInput{
		TenantID:      tenantID,
		ActorUserID:   uuid.New(),
		ApplicationID: app.ID(),
		Action:        commands.ActionReject,
		RejectReason:  "", // missing
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reason required")
	assert.Empty(t, audit.events)
}

func TestTransitionApplication_Reject_InvalidTransition(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	// New application cannot be rejected.
	app := makeNewApplication(t, tenantID, uuid.New(), uuid.New())
	appRepo := newFakeApplicationRepo()
	require.NoError(t, appRepo.Save(ctx, app))

	audit := &fakeAuditWriter{}
	h := buildTransitionHandler(appRepo, audit)

	err := h.Handle(ctx, commands.TransitionApplicationInput{
		TenantID:      tenantID,
		ActorUserID:   uuid.New(),
		ApplicationID: app.ID(),
		Action:        commands.ActionReject,
		RejectReason:  "not a fit",
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrInvalidTransition))
	assert.Empty(t, audit.events)
}

// ---------------------------------------------------------------------------
// Hire tests
// ---------------------------------------------------------------------------

func TestTransitionApplication_Hire_HappyPath(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	app := makeScoredApplication(t, tenantID, uuid.New(), uuid.New())
	appRepo := newFakeApplicationRepo()
	require.NoError(t, appRepo.Save(ctx, app))
	appRepo.saves = 0

	audit := &fakeAuditWriter{}
	h := buildTransitionHandler(appRepo, audit)

	err := h.Handle(ctx, commands.TransitionApplicationInput{
		TenantID:      tenantID,
		ActorUserID:   uuid.New(),
		ApplicationID: app.ID(),
		Action:        commands.ActionHire,
	})

	require.NoError(t, err)
	assert.Equal(t, vo.AppStatusHired, app.Status())
	require.Len(t, audit.events, 1)
	assert.Equal(t, "application_hire", audit.events[0].Action)
	assert.Nil(t, audit.events[0].Payload, "no payload for hire")
}

func TestTransitionApplication_Hire_InvalidTransition(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	// New application cannot be hired.
	app := makeNewApplication(t, tenantID, uuid.New(), uuid.New())
	appRepo := newFakeApplicationRepo()
	require.NoError(t, appRepo.Save(ctx, app))

	audit := &fakeAuditWriter{}
	h := buildTransitionHandler(appRepo, audit)

	err := h.Handle(ctx, commands.TransitionApplicationInput{
		TenantID:      tenantID,
		ActorUserID:   uuid.New(),
		ApplicationID: app.ID(),
		Action:        commands.ActionHire,
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrInvalidTransition))
	assert.Empty(t, audit.events)
}

// ---------------------------------------------------------------------------
// MoveToInterviewing tests
// ---------------------------------------------------------------------------

func TestTransitionApplication_MoveToInterviewing_HappyPath(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	app := makeShortlistedApp(t, tenantID)
	appRepo := newFakeApplicationRepo()
	require.NoError(t, appRepo.Save(ctx, app))
	appRepo.saves = 0

	audit := &fakeAuditWriter{}
	h := buildTransitionHandler(appRepo, audit)

	err := h.Handle(ctx, commands.TransitionApplicationInput{
		TenantID:      tenantID,
		ActorUserID:   uuid.New(),
		ApplicationID: app.ID(),
		Action:        commands.ActionMoveToInterviewing,
	})

	require.NoError(t, err)
	assert.Equal(t, vo.AppStatusInterviewing, app.Status())
	require.Len(t, audit.events, 1)
	assert.Equal(t, "application_move_to_interviewing", audit.events[0].Action)
}

func TestTransitionApplication_MoveToInterviewing_InvalidTransition(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	// Scored (not shortlisted) cannot move to interviewing.
	app := makeScoredApplication(t, tenantID, uuid.New(), uuid.New())
	appRepo := newFakeApplicationRepo()
	require.NoError(t, appRepo.Save(ctx, app))

	audit := &fakeAuditWriter{}
	h := buildTransitionHandler(appRepo, audit)

	err := h.Handle(ctx, commands.TransitionApplicationInput{
		TenantID:      tenantID,
		ActorUserID:   uuid.New(),
		ApplicationID: app.ID(),
		Action:        commands.ActionMoveToInterviewing,
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrInvalidTransition))
	assert.Empty(t, audit.events)
}

// ---------------------------------------------------------------------------
// Audit failure propagation
// ---------------------------------------------------------------------------

func TestTransitionApplication_AuditFailure_PropagatesError(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	app := makeScoredApplication(t, tenantID, uuid.New(), uuid.New())
	appRepo := newFakeApplicationRepo()
	require.NoError(t, appRepo.Save(ctx, app))

	auditErr := errors.New("audit: database unavailable")
	audit := &fakeAuditWriter{writeErr: auditErr}
	h := buildTransitionHandler(appRepo, audit)

	err := h.Handle(ctx, commands.TransitionApplicationInput{
		TenantID:      tenantID,
		ActorUserID:   uuid.New(),
		ApplicationID: app.ID(),
		Action:        commands.ActionShortlist,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, auditErr)
}

// ---------------------------------------------------------------------------
// Application not found
// ---------------------------------------------------------------------------

func TestTransitionApplication_AppNotFound_ReturnsNotFoundError(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	appRepo := newFakeApplicationRepo() // empty
	audit := &fakeAuditWriter{}
	h := buildTransitionHandler(appRepo, audit)

	err := h.Handle(ctx, commands.TransitionApplicationInput{
		TenantID:      tenantID,
		ActorUserID:   uuid.New(),
		ApplicationID: uuid.New(),
		Action:        commands.ActionShortlist,
	})

	require.Error(t, err)
	// fakeApplicationRepo returns repositories.ErrApplicationNotFound
	assert.Empty(t, audit.events)
}
