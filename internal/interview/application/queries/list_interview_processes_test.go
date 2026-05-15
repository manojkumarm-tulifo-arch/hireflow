package queries_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/application/queries"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func seedProcessForIntent(t *testing.T, repo *fakeProcessRepo, tenantID shared.TenantID, intentID uuid.UUID) *entities.InterviewProcess {
	t.Helper()
	p, err := entities.NewInterviewProcess(entities.NewInterviewProcessInput{
		TenantID:      tenantID,
		ApplicationID: uuid.New(),
		CandidateID:   uuid.New(),
		IntentID:      intentID,
		Rounds: []entities.TemplateRound{
			{Kind: vo.RoundKindScreen, Sequence: 1},
		},
	})
	if err != nil {
		t.Fatalf("seedProcessForIntent: %v", err)
	}
	if err := repo.Save(context.Background(), p); err != nil {
		t.Fatalf("seedProcessForIntent save: %v", err)
	}
	return p
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestList_ReturnsProcessesForIntent(t *testing.T) {
	tenantID := shared.NewTenantID()
	processes := newFakeProcessRepo()

	intentID := uuid.New()
	otherIntentID := uuid.New()

	seedProcessForIntent(t, processes, tenantID, intentID)
	seedProcessForIntent(t, processes, tenantID, intentID)
	seedProcessForIntent(t, processes, tenantID, otherIntentID) // different intent

	h := queries.NewListInterviewProcessesHandler(processes)
	result, err := h.Handle(context.Background(), queries.ListInput{
		TenantID: tenantID,
		IntentID: intentID,
	})
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("want 2 processes for intentID, got %d", len(result))
	}
	for _, dto := range result {
		if dto.IntentID != intentID {
			t.Errorf("unexpected intentID %v in results", dto.IntentID)
		}
	}
}

func TestList_FiltersByStatus(t *testing.T) {
	tenantID := shared.NewTenantID()
	processes := newFakeProcessRepo()

	intentID := uuid.New()

	// Seed one process and cancel it.
	p1 := seedProcessForIntent(t, processes, tenantID, intentID)
	if err := p1.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if err := processes.Save(context.Background(), p1); err != nil {
		t.Fatalf("save cancelled process: %v", err)
	}

	// Seed one in default (new) status.
	seedProcessForIntent(t, processes, tenantID, intentID)

	h := queries.NewListInterviewProcessesHandler(processes)

	// Filter by cancelled.
	cancelled, err := h.Handle(context.Background(), queries.ListInput{
		TenantID: tenantID,
		IntentID: intentID,
		Status:   string(vo.ProcessStatusCancelled),
	})
	if err != nil {
		t.Fatalf("Handle (cancelled) error: %v", err)
	}
	if len(cancelled) != 1 {
		t.Errorf("want 1 cancelled process, got %d", len(cancelled))
	}
	if cancelled[0].Status != string(vo.ProcessStatusCancelled) {
		t.Errorf("status: want %v, got %v", vo.ProcessStatusCancelled, cancelled[0].Status)
	}

	// Filter by new.
	newProcesses, err := h.Handle(context.Background(), queries.ListInput{
		TenantID: tenantID,
		IntentID: intentID,
		Status:   string(vo.ProcessStatusNew),
	})
	if err != nil {
		t.Fatalf("Handle (new) error: %v", err)
	}
	if len(newProcesses) != 1 {
		t.Errorf("want 1 new process, got %d", len(newProcesses))
	}
}

func TestList_RespectsLimitOffset(t *testing.T) {
	tenantID := shared.NewTenantID()
	processes := newFakeProcessRepo()

	intentID := uuid.New()

	// Seed 5 processes.
	for i := 0; i < 5; i++ {
		seedProcessForIntent(t, processes, tenantID, intentID)
	}

	h := queries.NewListInterviewProcessesHandler(processes)

	// Limit 2.
	page1, err := h.Handle(context.Background(), queries.ListInput{
		TenantID: tenantID,
		IntentID: intentID,
		Limit:    2,
		Offset:   0,
	})
	if err != nil {
		t.Fatalf("Handle (limit 2) error: %v", err)
	}
	if len(page1) != 2 {
		t.Errorf("want 2, got %d", len(page1))
	}

	// Offset beyond total.
	beyondOffset, err := h.Handle(context.Background(), queries.ListInput{
		TenantID: tenantID,
		IntentID: intentID,
		Limit:    10,
		Offset:   10,
	})
	if err != nil {
		t.Fatalf("Handle (offset beyond) error: %v", err)
	}
	if len(beyondOffset) != 0 {
		t.Errorf("want 0 for offset beyond total, got %d", len(beyondOffset))
	}
}

func TestList_EmptyResult(t *testing.T) {
	tenantID := shared.NewTenantID()
	processes := newFakeProcessRepo()

	h := queries.NewListInterviewProcessesHandler(processes)
	result, err := h.Handle(context.Background(), queries.ListInput{
		TenantID: tenantID,
		IntentID: uuid.New(),
	})
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("want empty result, got %d", len(result))
	}
}
