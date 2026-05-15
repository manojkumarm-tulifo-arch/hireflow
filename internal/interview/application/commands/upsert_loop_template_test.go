package commands_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/application/commands"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	auditdomain "github.com/hustle/hireflow/internal/shared/audit/domain"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ---------------------------------------------------------------------------
// Capturing AuditWriter fake
// ---------------------------------------------------------------------------

type captureAuditWriter struct {
	events []auditdomain.AuditEvent
	err    error // if set, Write returns this error
}

func (w *captureAuditWriter) Write(_ context.Context, e auditdomain.AuditEvent) error {
	if w.err != nil {
		return w.err
	}
	w.events = append(w.events, e)
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func twoRounds() []entities.TemplateRound {
	return []entities.TemplateRound{
		{Kind: vo.RoundKindScreen, Sequence: 1},
		{Kind: vo.RoundKindTechnical, Sequence: 2},
	}
}

func threeTemplateRounds() []entities.TemplateRound {
	return []entities.TemplateRound{
		{Kind: vo.RoundKindScreen, Sequence: 1},
		{Kind: vo.RoundKindTechnical, Sequence: 2},
		{Kind: vo.RoundKindBarRaiser, Sequence: 3},
	}
}

func seedTemplate(t *testing.T, repo *fakeTemplateRepo, tenantID shared.TenantID, intentID uuid.UUID, rounds []entities.TemplateRound) *entities.LoopTemplate {
	t.Helper()
	tmpl, err := entities.NewLoopTemplate(entities.NewLoopTemplateInput{
		TenantID: tenantID,
		IntentID: intentID,
		Rounds:   rounds,
	})
	if err != nil {
		t.Fatalf("seedTemplate: %v", err)
	}
	if err := repo.Save(context.Background(), tmpl); err != nil {
		t.Fatalf("seedTemplate save: %v", err)
	}
	return tmpl
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestUpsertLoopTemplate_CreatesWhenAbsent(t *testing.T) {
	templates := newFakeTemplateRepo()
	audit := &captureAuditWriter{}

	h := commands.NewUpsertLoopTemplateHandler(templates, audit)
	tenantID := shared.NewTenantID()
	intentID := uuid.New()

	in := commands.UpsertLoopTemplateInput{
		TenantID:    tenantID,
		ActorUserID: uuid.New(),
		IntentID:    intentID,
		Rounds:      twoRounds(),
	}

	if err := h.Handle(context.Background(), in); err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	tmpl, err := templates.FindByIntent(context.Background(), tenantID, intentID)
	if err != nil {
		t.Fatalf("FindByIntent after create: %v", err)
	}
	if len(tmpl.Rounds()) != 2 {
		t.Errorf("expected 2 rounds, got %d", len(tmpl.Rounds()))
	}
}

func TestUpsertLoopTemplate_ReplacesWhenPresent(t *testing.T) {
	templates := newFakeTemplateRepo()
	audit := &captureAuditWriter{}

	tenantID := shared.NewTenantID()
	intentID := uuid.New()
	seedTemplate(t, templates, tenantID, intentID, twoRounds())

	h := commands.NewUpsertLoopTemplateHandler(templates, audit)
	in := commands.UpsertLoopTemplateInput{
		TenantID:    tenantID,
		ActorUserID: uuid.New(),
		IntentID:    intentID,
		Rounds:      threeTemplateRounds(),
	}

	if err := h.Handle(context.Background(), in); err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	tmpl, err := templates.FindByIntent(context.Background(), tenantID, intentID)
	if err != nil {
		t.Fatalf("FindByIntent after replace: %v", err)
	}
	if len(tmpl.Rounds()) != 3 {
		t.Errorf("expected 3 rounds after replace, got %d", len(tmpl.Rounds()))
	}
}

func TestUpsertLoopTemplate_AuditWritten(t *testing.T) {
	templates := newFakeTemplateRepo()
	audit := &captureAuditWriter{}

	h := commands.NewUpsertLoopTemplateHandler(templates, audit)
	tenantID := shared.NewTenantID()
	intentID := uuid.New()
	actorID := uuid.New()

	in := commands.UpsertLoopTemplateInput{
		TenantID:    tenantID,
		ActorUserID: actorID,
		IntentID:    intentID,
		Rounds:      twoRounds(),
	}

	if err := h.Handle(context.Background(), in); err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	if len(audit.events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(audit.events))
	}
	ev := audit.events[0]
	if ev.Action != "interview_loop_template_upserted" {
		t.Errorf("action: want %q, got %q", "interview_loop_template_upserted", ev.Action)
	}
	if ev.ResourceKind != "intent" {
		t.Errorf("resource_kind: want %q, got %q", "intent", ev.ResourceKind)
	}
	if ev.ResourceID != intentID {
		t.Errorf("resource_id: want %v, got %v", intentID, ev.ResourceID)
	}
	if ev.ActorUserID != actorID {
		t.Errorf("actor_user_id: want %v, got %v", actorID, ev.ActorUserID)
	}
	roundCount, ok := ev.Payload["round_count"]
	if !ok {
		t.Error("payload missing round_count")
	} else if roundCount != 2 {
		t.Errorf("payload round_count: want 2, got %v", roundCount)
	}
}

func TestUpsertLoopTemplate_AuditFailurePropagates(t *testing.T) {
	templates := newFakeTemplateRepo()
	auditErr := errors.New("audit db down")
	audit := &captureAuditWriter{err: auditErr}

	h := commands.NewUpsertLoopTemplateHandler(templates, audit)
	in := commands.UpsertLoopTemplateInput{
		TenantID:    shared.NewTenantID(),
		ActorUserID: uuid.New(),
		IntentID:    uuid.New(),
		Rounds:      twoRounds(),
	}

	err := h.Handle(context.Background(), in)
	if err == nil {
		t.Fatal("expected error from audit failure, got nil")
	}
	if !errors.Is(err, auditErr) {
		t.Errorf("expected error to wrap auditErr, got: %v", err)
	}
}

func TestUpsertLoopTemplate_InvalidRounds_RejectedByConstructor(t *testing.T) {
	templates := newFakeTemplateRepo()
	audit := &captureAuditWriter{}

	h := commands.NewUpsertLoopTemplateHandler(templates, audit)
	in := commands.UpsertLoopTemplateInput{
		TenantID:    shared.NewTenantID(),
		ActorUserID: uuid.New(),
		IntentID:    uuid.New(),
		// Duplicate sequence — should fail validation.
		Rounds: []entities.TemplateRound{
			{Kind: vo.RoundKindScreen, Sequence: 1},
			{Kind: vo.RoundKindTechnical, Sequence: 1},
		},
	}

	err := h.Handle(context.Background(), in)
	if err == nil {
		t.Fatal("expected error for duplicate-sequence rounds, got nil")
	}
}

func TestUpsertLoopTemplate_LoadFailure_Propagates(t *testing.T) {
	templates := newFakeTemplateRepo()
	dbErr := errors.New("template store unavailable")
	templates.findErr = dbErr

	audit := &captureAuditWriter{}

	h := commands.NewUpsertLoopTemplateHandler(templates, audit)
	in := commands.UpsertLoopTemplateInput{
		TenantID:    shared.NewTenantID(),
		ActorUserID: uuid.New(),
		IntentID:    uuid.New(),
		Rounds:      twoRounds(),
	}

	err := h.Handle(context.Background(), in)
	if err == nil {
		t.Fatal("expected error from template load failure, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("expected error to wrap dbErr, got: %v", err)
	}
}
