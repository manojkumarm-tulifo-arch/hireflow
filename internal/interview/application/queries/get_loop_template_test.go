package queries_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/interview/application/commands"
	"github.com/hustle/hireflow/internal/interview/application/queries"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
)

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestGetTemplate_ReturnsStoredTemplate_NotDefault(t *testing.T) {
	tenantID := shared.NewTenantID()
	templates := newFakeTemplateRepo()

	intentID := uuid.New()
	customRounds := []entities.TemplateRound{
		{Kind: vo.RoundKindScreen, Sequence: 1},
		{Kind: vo.RoundKindSystemDesign, Sequence: 2},
		{Kind: vo.RoundKindBarRaiser, Sequence: 3},
	}
	tmpl, err := entities.NewLoopTemplate(entities.NewLoopTemplateInput{
		TenantID: tenantID,
		IntentID: intentID,
		Rounds:   customRounds,
	})
	if err != nil {
		t.Fatalf("NewLoopTemplate: %v", err)
	}
	if err := templates.Save(context.Background(), tmpl); err != nil {
		t.Fatalf("Save: %v", err)
	}

	h := queries.NewGetLoopTemplateHandler(templates)
	result, err := h.Handle(context.Background(), tenantID, intentID)
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	if result.IsDefault {
		t.Error("IsDefault: want false for stored template, got true")
	}
	if result.IntentID != intentID {
		t.Errorf("IntentID: want %v, got %v", intentID, result.IntentID)
	}
	if len(result.Rounds) != 3 {
		t.Fatalf("Rounds: want 3, got %d", len(result.Rounds))
	}
	if result.Rounds[0].Kind != string(vo.RoundKindScreen) {
		t.Errorf("Round[0] Kind: want %v, got %v", vo.RoundKindScreen, result.Rounds[0].Kind)
	}
	if result.Rounds[1].Kind != string(vo.RoundKindSystemDesign) {
		t.Errorf("Round[1] Kind: want %v, got %v", vo.RoundKindSystemDesign, result.Rounds[1].Kind)
	}
	if result.Rounds[2].Kind != string(vo.RoundKindBarRaiser) {
		t.Errorf("Round[2] Kind: want %v, got %v", vo.RoundKindBarRaiser, result.Rounds[2].Kind)
	}
}

func TestGetTemplate_ReturnsDefault_WhenAbsent(t *testing.T) {
	tenantID := shared.NewTenantID()
	templates := newFakeTemplateRepo() // empty — no template saved

	intentID := uuid.New()

	h := queries.NewGetLoopTemplateHandler(templates)
	result, err := h.Handle(context.Background(), tenantID, intentID)
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	if !result.IsDefault {
		t.Error("IsDefault: want true when no template exists, got false")
	}
	if result.IntentID != intentID {
		t.Errorf("IntentID: want %v, got %v", intentID, result.IntentID)
	}

	// Rounds must match DefaultLoop exactly.
	defaultLoop := commands.DefaultLoop
	if len(result.Rounds) != len(defaultLoop) {
		t.Fatalf("Rounds count: want %d (DefaultLoop), got %d", len(defaultLoop), len(result.Rounds))
	}
	for i, r := range result.Rounds {
		want := defaultLoop[i]
		if r.Kind != string(want.Kind) {
			t.Errorf("Round[%d] Kind: want %v, got %v", i, want.Kind, r.Kind)
		}
		if r.Sequence != want.Sequence {
			t.Errorf("Round[%d] Sequence: want %d, got %d", i, want.Sequence, r.Sequence)
		}
	}
}

func TestGetTemplate_TenantScoped(t *testing.T) {
	// Two different tenants; each stores a template for the same intentID.
	tenant1 := shared.NewTenantID()
	tenant2 := shared.NewTenantID()
	templates := newFakeTemplateRepo()

	intentID := uuid.New()

	// Store a 1-round template for tenant1.
	tmpl1, err := entities.NewLoopTemplate(entities.NewLoopTemplateInput{
		TenantID: tenant1,
		IntentID: intentID,
		Rounds:   []entities.TemplateRound{{Kind: vo.RoundKindScreen, Sequence: 1}},
	})
	if err != nil {
		t.Fatalf("NewLoopTemplate tenant1: %v", err)
	}
	if err := templates.Save(context.Background(), tmpl1); err != nil {
		t.Fatalf("Save tenant1: %v", err)
	}

	h := queries.NewGetLoopTemplateHandler(templates)

	// tenant1 should get its stored template (IsDefault=false).
	r1, err := h.Handle(context.Background(), tenant1, intentID)
	if err != nil {
		t.Fatalf("Handle tenant1 error: %v", err)
	}
	if r1.IsDefault {
		t.Error("tenant1: IsDefault want false, got true")
	}
	if len(r1.Rounds) != 1 {
		t.Errorf("tenant1: want 1 round, got %d", len(r1.Rounds))
	}

	// tenant2 has no template — should get default.
	r2, err := h.Handle(context.Background(), tenant2, intentID)
	if err != nil {
		t.Fatalf("Handle tenant2 error: %v", err)
	}
	if !r2.IsDefault {
		t.Error("tenant2: IsDefault want true (no template stored), got false")
	}
	if len(r2.Rounds) != len(commands.DefaultLoop) {
		t.Errorf("tenant2: want %d rounds (DefaultLoop), got %d", len(commands.DefaultLoop), len(r2.Rounds))
	}
}
