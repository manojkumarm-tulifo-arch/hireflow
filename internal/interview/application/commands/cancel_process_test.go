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

func TestCancelProcess_FromNew_Succeeds(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()

	// Process starts as New (no transition needed).
	p, _ := seedProcess(t, processes, tenantID)

	audit := &captureAuditWriter{}
	h := commands.NewCancelProcessHandler(processes, audit)

	err := h.Handle(context.Background(), commands.CancelProcessInput{
		TenantID:    tenantID,
		ActorUserID: uuid.New(),
		ProcessID:   p.ID(),
	})
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	saved, _ := processes.FindByID(context.Background(), tenantID, p.ID())
	if saved.Status() != vo.ProcessStatusCancelled {
		t.Errorf("status: want Cancelled, got %v", saved.Status())
	}
}

func TestCancelProcess_AlreadyCancelled_ReturnsErrProcessInvalidTransition(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()

	p, _ := seedProcess(t, processes, tenantID)

	audit := &captureAuditWriter{}
	h := commands.NewCancelProcessHandler(processes, audit)

	input := commands.CancelProcessInput{
		TenantID:    tenantID,
		ActorUserID: uuid.New(),
		ProcessID:   p.ID(),
	}

	// First cancel succeeds.
	if err := h.Handle(context.Background(), input); err != nil {
		t.Fatalf("first Handle error: %v", err)
	}

	// Second cancel — already Cancelled (terminal) → ErrProcessInvalidTransition.
	err := h.Handle(context.Background(), input)
	if !errors.Is(err, commands.ErrProcessInvalidTransition) {
		t.Errorf("want ErrProcessInvalidTransition, got: %v", err)
	}
}

func TestCancelProcess_AuditWritten(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	actorID := uuid.New()

	p, _ := seedProcess(t, processes, tenantID)

	audit := &captureAuditWriter{}
	h := commands.NewCancelProcessHandler(processes, audit)

	if err := h.Handle(context.Background(), commands.CancelProcessInput{
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
	if ae.Action != "interview_process_cancelled" {
		t.Errorf("action: want %q, got %q", "interview_process_cancelled", ae.Action)
	}
	if ae.ResourceID != p.ID() {
		t.Errorf("resource_id: want %v, got %v", p.ID(), ae.ResourceID)
	}
	if ae.ActorUserID != actorID {
		t.Errorf("actor_user_id: want %v, got %v", actorID, ae.ActorUserID)
	}
}
