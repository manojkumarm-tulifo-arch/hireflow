package commands_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/application/commands"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

func TestMarkRoundCompleted_FromQuestionsReady_Succeeds(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()

	p, roundID := seedProcess(t, processes, tenantID)
	advanceRoundToStatus(t, processes, p, roundID, vo.RoundStatusQuestionsReady)

	audit := &captureAuditWriter{}
	h := commands.NewMarkRoundCompletedHandler(processes, audit)

	err := h.Handle(context.Background(), commands.MarkRoundCompletedInput{
		TenantID:    tenantID,
		ActorUserID: uuid.New(),
		RoundID:     roundID,
	})
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	saved, err := processes.FindByID(context.Background(), tenantID, p.ID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if saved.Rounds()[0].Status() != vo.RoundStatusCompleted {
		t.Errorf("status: want Completed, got %v", saved.Rounds()[0].Status())
	}
}

func TestMarkRoundCompleted_FromPending_ReturnsErrRoundInvalidTransition(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()

	// Round stays in Pending.
	p, roundID := seedProcess(t, processes, tenantID)
	_ = p

	audit := &captureAuditWriter{}
	h := commands.NewMarkRoundCompletedHandler(processes, audit)

	err := h.Handle(context.Background(), commands.MarkRoundCompletedInput{
		TenantID:    tenantID,
		ActorUserID: uuid.New(),
		RoundID:     roundID,
	})
	if !errors.Is(err, commands.ErrRoundInvalidTransition) {
		t.Errorf("want ErrRoundInvalidTransition, got: %v", err)
	}
}

func TestMarkRoundCompleted_AuditWritten(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	actorID := uuid.New()

	p, roundID := seedProcess(t, processes, tenantID)
	advanceRoundToStatus(t, processes, p, roundID, vo.RoundStatusQuestionsReady)

	audit := &captureAuditWriter{}
	h := commands.NewMarkRoundCompletedHandler(processes, audit)

	if err := h.Handle(context.Background(), commands.MarkRoundCompletedInput{
		TenantID:    tenantID,
		ActorUserID: actorID,
		RoundID:     roundID,
	}); err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	if len(audit.events) != 1 {
		t.Fatalf("audit events: want 1, got %d", len(audit.events))
	}
	ae := audit.events[0]
	if ae.Action != "interview_round_completed" {
		t.Errorf("action: want %q, got %q", "interview_round_completed", ae.Action)
	}
	if ae.ResourceID != roundID {
		t.Errorf("resource_id: want %v, got %v", roundID, ae.ResourceID)
	}
	if ae.ActorUserID != actorID {
		t.Errorf("actor_user_id: want %v, got %v", actorID, ae.ActorUserID)
	}
}

func TestMarkRoundCompleted_AuditFailurePropagates(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()

	p, roundID := seedProcess(t, processes, tenantID)
	advanceRoundToStatus(t, processes, p, roundID, vo.RoundStatusQuestionsReady)

	auditErr := errors.New("audit db unavailable")
	audit := &captureAuditWriter{err: auditErr}
	h := commands.NewMarkRoundCompletedHandler(processes, audit)

	err := h.Handle(context.Background(), commands.MarkRoundCompletedInput{
		TenantID:    tenantID,
		ActorUserID: uuid.New(),
		RoundID:     roundID,
	})
	if err == nil {
		t.Fatal("expected error from audit failure, got nil")
	}
	if !errors.Is(err, auditErr) {
		t.Errorf("expected error to wrap auditErr, got: %v", err)
	}
}
