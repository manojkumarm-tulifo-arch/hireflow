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

func TestMarkRoundSkipped_FromPending_Succeeds(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()

	// Round starts as Pending — no advancement needed.
	_, roundID := seedProcess(t, processes, tenantID)

	audit := &captureAuditWriter{}
	h := commands.NewMarkRoundSkippedHandler(processes, audit)

	err := h.Handle(context.Background(), commands.MarkRoundSkippedInput{
		TenantID:    tenantID,
		ActorUserID: uuid.New(),
		RoundID:     roundID,
	})
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	saved, _ := processes.FindByRoundID(context.Background(), tenantID, roundID)
	if saved.Rounds()[0].Status() != vo.RoundStatusSkipped {
		t.Errorf("status: want Skipped, got %v", saved.Rounds()[0].Status())
	}
}

func TestMarkRoundSkipped_FromQuestionsReady_Succeeds(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()

	p, roundID := seedProcess(t, processes, tenantID)
	advanceRoundToStatus(t, processes, p, roundID, vo.RoundStatusQuestionsReady)

	audit := &captureAuditWriter{}
	h := commands.NewMarkRoundSkippedHandler(processes, audit)

	err := h.Handle(context.Background(), commands.MarkRoundSkippedInput{
		TenantID:    tenantID,
		ActorUserID: uuid.New(),
		RoundID:     roundID,
	})
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	saved, _ := processes.FindByRoundID(context.Background(), tenantID, roundID)
	if saved.Rounds()[0].Status() != vo.RoundStatusSkipped {
		t.Errorf("status: want Skipped, got %v", saved.Rounds()[0].Status())
	}
}

func TestMarkRoundSkipped_FromGenerationFailed_Succeeds(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()

	p, roundID := seedProcess(t, processes, tenantID)
	advanceRoundToStatus(t, processes, p, roundID, vo.RoundStatusGenerationFailed)

	audit := &captureAuditWriter{}
	h := commands.NewMarkRoundSkippedHandler(processes, audit)

	err := h.Handle(context.Background(), commands.MarkRoundSkippedInput{
		TenantID:    tenantID,
		ActorUserID: uuid.New(),
		RoundID:     roundID,
	})
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	saved, _ := processes.FindByRoundID(context.Background(), tenantID, roundID)
	if saved.Rounds()[0].Status() != vo.RoundStatusSkipped {
		t.Errorf("status: want Skipped, got %v", saved.Rounds()[0].Status())
	}
}

func TestMarkRoundSkipped_FromCompleted_ReturnsErr(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()

	p, roundID := seedProcess(t, processes, tenantID)
	advanceRoundToStatus(t, processes, p, roundID, vo.RoundStatusCompleted)

	audit := &captureAuditWriter{}
	h := commands.NewMarkRoundSkippedHandler(processes, audit)

	err := h.Handle(context.Background(), commands.MarkRoundSkippedInput{
		TenantID:    tenantID,
		ActorUserID: uuid.New(),
		RoundID:     roundID,
	})
	if !errors.Is(err, commands.ErrRoundInvalidTransition) {
		t.Errorf("want ErrRoundInvalidTransition, got: %v", err)
	}
}

func TestMarkRoundSkipped_AuditWritten(t *testing.T) {
	processes := newFakeProcessRepo()
	tenantID := shared.NewTenantID()
	actorID := uuid.New()

	_, roundID := seedProcess(t, processes, tenantID)

	audit := &captureAuditWriter{}
	h := commands.NewMarkRoundSkippedHandler(processes, audit)

	if err := h.Handle(context.Background(), commands.MarkRoundSkippedInput{
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
	if ae.Action != "interview_round_skipped" {
		t.Errorf("action: want %q, got %q", "interview_round_skipped", ae.Action)
	}
	if ae.ResourceID != roundID {
		t.Errorf("resource_id: want %v, got %v", roundID, ae.ResourceID)
	}
}
