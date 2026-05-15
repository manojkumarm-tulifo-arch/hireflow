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

func TestCompleteProcess_AllRoundsTerminal_Succeeds(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()

	p, roundID := seedProcess(t, processes, tenantID)
	// Advance round to Completed (terminal).
	advanceRoundToStatus(t, processes, p, roundID, vo.RoundStatusCompleted)

	audit := &captureAuditWriter{}
	h := commands.NewCompleteProcessHandler(processes, audit)

	err := h.Handle(context.Background(), commands.CompleteProcessInput{
		TenantID:    tenantID,
		ActorUserID: uuid.New(),
		ProcessID:   p.ID(),
	})
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	saved, _ := processes.FindByID(context.Background(), tenantID, p.ID())
	if saved.Status() != vo.ProcessStatusCompleted {
		t.Errorf("status: want Completed, got %v", saved.Status())
	}
}

func TestCompleteProcess_PendingRound_ReturnsErr(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()

	// Round stays in Pending — cannot complete process.
	p, _ := seedProcess(t, processes, tenantID)

	audit := &captureAuditWriter{}
	h := commands.NewCompleteProcessHandler(processes, audit)

	err := h.Handle(context.Background(), commands.CompleteProcessInput{
		TenantID:    tenantID,
		ActorUserID: uuid.New(),
		ProcessID:   p.ID(),
	})
	// The entity returns a plain error (not ErrInvalidTransition) when rounds are not terminal.
	if err == nil {
		t.Fatal("expected error for pending round, got nil")
	}
	// It must NOT be ErrProcessInvalidTransition (that's only for already-terminal process).
	if errors.Is(err, commands.ErrProcessInvalidTransition) {
		t.Errorf("should not be ErrProcessInvalidTransition for pending round case, got: %v", err)
	}
}

func TestCompleteProcess_AlreadyCompleted_ReturnsErrProcessInvalidTransition(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()

	p, roundID := seedProcess(t, processes, tenantID)
	// Make all rounds terminal, then complete the process once.
	advanceRoundToStatus(t, processes, p, roundID, vo.RoundStatusCompleted)

	audit := &captureAuditWriter{}
	h := commands.NewCompleteProcessHandler(processes, audit)

	input := commands.CompleteProcessInput{
		TenantID:    tenantID,
		ActorUserID: uuid.New(),
		ProcessID:   p.ID(),
	}

	// First call succeeds.
	if err := h.Handle(context.Background(), input); err != nil {
		t.Fatalf("first Handle error: %v", err)
	}

	// Second call — process is already Completed (terminal) → ErrProcessInvalidTransition.
	err := h.Handle(context.Background(), input)
	if !errors.Is(err, commands.ErrProcessInvalidTransition) {
		t.Errorf("want ErrProcessInvalidTransition, got: %v", err)
	}
}

func TestCompleteProcess_AuditWritten(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	actorID := uuid.New()

	p, roundID := seedProcess(t, processes, tenantID)
	advanceRoundToStatus(t, processes, p, roundID, vo.RoundStatusCompleted)

	audit := &captureAuditWriter{}
	h := commands.NewCompleteProcessHandler(processes, audit)

	if err := h.Handle(context.Background(), commands.CompleteProcessInput{
		TenantID:    tenantID,
		ActorUserID: actorID,
		ProcessID:   p.ID(),
	}); err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	if len(audit.events) != 1 {
		t.Fatalf("audit events: want 1, got %d", len(audit.events))
	}
	ae := audit.events[0]
	if ae.Action != "interview_process_completed" {
		t.Errorf("action: want %q, got %q", "interview_process_completed", ae.Action)
	}
	if ae.ResourceID != p.ID() {
		t.Errorf("resource_id: want %v, got %v", p.ID(), ae.ResourceID)
	}
	if ae.ActorUserID != actorID {
		t.Errorf("actor_user_id: want %v, got %v", actorID, ae.ActorUserID)
	}
}
