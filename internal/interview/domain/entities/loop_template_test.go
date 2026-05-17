package entities_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/domain/entities"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// fixedNow returns a deterministic clock for tests.
func fixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func validInput() entities.NewLoopTemplateInput {
	return entities.NewLoopTemplateInput{
		TenantID: shared.NewTenantID(),
		IntentID: uuid.New(),
		Rounds: []entities.TemplateRound{
			{Kind: vo.RoundKindScreen, Sequence: 1},
			{Kind: vo.RoundKindTechnical, Sequence: 2},
			{Kind: vo.RoundKindBehavioral, Sequence: 3},
		},
		Now: fixedNow(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
	}
}

// --- construction happy path ---

func TestNewLoopTemplate_HappyPath(t *testing.T) {
	in := validInput()
	lt, err := entities.NewLoopTemplate(in)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if lt.ID() == uuid.Nil {
		t.Error("expected non-nil ID")
	}
	if lt.TenantID() != in.TenantID {
		t.Error("tenant mismatch")
	}
	if lt.IntentID() != in.IntentID {
		t.Error("intent mismatch")
	}
	rounds := lt.Rounds()
	if len(rounds) != 3 {
		t.Fatalf("expected 3 rounds, got %d", len(rounds))
	}
	if rounds[0].Kind != vo.RoundKindScreen || rounds[0].Sequence != 1 {
		t.Errorf("round 0 mismatch: %+v", rounds[0])
	}
	if rounds[1].Kind != vo.RoundKindTechnical || rounds[1].Sequence != 2 {
		t.Errorf("round 1 mismatch: %+v", rounds[1])
	}
	if rounds[2].Kind != vo.RoundKindBehavioral || rounds[2].Sequence != 3 {
		t.Errorf("round 2 mismatch: %+v", rounds[2])
	}
	want := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if !lt.CreatedAt().Equal(want) {
		t.Errorf("createdAt: want %v, got %v", want, lt.CreatedAt())
	}
	if !lt.UpdatedAt().Equal(want) {
		t.Errorf("updatedAt: want %v, got %v", want, lt.UpdatedAt())
	}
}

// --- validation failures ---

func TestNewLoopTemplate_EmptyRounds(t *testing.T) {
	in := validInput()
	in.Rounds = nil
	_, err := entities.NewLoopTemplate(in)
	if err == nil {
		t.Fatal("expected error for empty rounds")
	}
}

func TestNewLoopTemplate_MissingTenant(t *testing.T) {
	in := validInput()
	in.TenantID = shared.TenantID{}
	_, err := entities.NewLoopTemplate(in)
	if err == nil {
		t.Fatal("expected error for missing tenant")
	}
}

func TestNewLoopTemplate_MissingIntent(t *testing.T) {
	in := validInput()
	in.IntentID = uuid.Nil
	_, err := entities.NewLoopTemplate(in)
	if err == nil {
		t.Fatal("expected error for missing intent")
	}
}

func TestNewLoopTemplate_InvalidRoundKind(t *testing.T) {
	in := validInput()
	in.Rounds = []entities.TemplateRound{
		{Kind: vo.RoundKind("bogus"), Sequence: 1},
	}
	_, err := entities.NewLoopTemplate(in)
	if err == nil {
		t.Fatal("expected error for invalid round kind")
	}
}

func TestNewLoopTemplate_ZeroSequence(t *testing.T) {
	in := validInput()
	in.Rounds = []entities.TemplateRound{
		{Kind: vo.RoundKindScreen, Sequence: 0},
	}
	_, err := entities.NewLoopTemplate(in)
	if err == nil {
		t.Fatal("expected error for zero sequence")
	}
}

func TestNewLoopTemplate_DuplicateSequence(t *testing.T) {
	in := validInput()
	in.Rounds = []entities.TemplateRound{
		{Kind: vo.RoundKindScreen, Sequence: 1},
		{Kind: vo.RoundKindTechnical, Sequence: 1},
	}
	_, err := entities.NewLoopTemplate(in)
	if err == nil {
		t.Fatal("expected error for duplicate sequence")
	}
}

func TestNewLoopTemplate_NonContiguousSequence(t *testing.T) {
	in := validInput()
	in.Rounds = []entities.TemplateRound{
		{Kind: vo.RoundKindScreen, Sequence: 1},
		{Kind: vo.RoundKindTechnical, Sequence: 3},
	}
	_, err := entities.NewLoopTemplate(in)
	if err == nil {
		t.Fatal("expected error for non-contiguous sequences [1,3]")
	}
}

// --- Replace ---

func TestLoopTemplate_Replace_ValidSet(t *testing.T) {
	in := validInput()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	in.Now = fixedNow(t0)
	lt, err := entities.NewLoopTemplate(in)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	t1 := t0.Add(time.Hour)
	newRounds := []entities.TemplateRound{
		{Kind: vo.RoundKindSystemDesign, Sequence: 1},
		{Kind: vo.RoundKindBarRaiser, Sequence: 2},
	}
	if err := lt.Replace(newRounds, fixedNow(t1)); err != nil {
		t.Fatalf("Replace failed: %v", err)
	}

	rounds := lt.Rounds()
	if len(rounds) != 2 {
		t.Fatalf("expected 2 rounds after Replace, got %d", len(rounds))
	}
	if rounds[0].Kind != vo.RoundKindSystemDesign {
		t.Errorf("expected SystemDesign, got %v", rounds[0].Kind)
	}
	if rounds[1].Kind != vo.RoundKindBarRaiser {
		t.Errorf("expected BarRaiser, got %v", rounds[1].Kind)
	}
	if !lt.UpdatedAt().Equal(t1) {
		t.Errorf("updatedAt: want %v, got %v", t1, lt.UpdatedAt())
	}
}

func TestLoopTemplate_Replace_InvalidSet_LeavesAggregateUnchanged(t *testing.T) {
	in := validInput()
	lt, err := entities.NewLoopTemplate(in)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	originalRounds := lt.Rounds()
	originalUpdatedAt := lt.UpdatedAt()

	// Try to replace with non-contiguous sequences — should fail.
	badRounds := []entities.TemplateRound{
		{Kind: vo.RoundKindScreen, Sequence: 1},
		{Kind: vo.RoundKindTechnical, Sequence: 3},
	}
	if err := lt.Replace(badRounds, nil); err == nil {
		t.Fatal("expected error from Replace with invalid rounds")
	}

	// Aggregate must be unchanged.
	afterRounds := lt.Rounds()
	if len(afterRounds) != len(originalRounds) {
		t.Fatalf("rounds length changed: want %d, got %d", len(originalRounds), len(afterRounds))
	}
	for i, r := range originalRounds {
		if afterRounds[i] != r {
			t.Errorf("round %d changed: want %+v, got %+v", i, r, afterRounds[i])
		}
	}
	if !lt.UpdatedAt().Equal(originalUpdatedAt) {
		t.Errorf("updatedAt changed after failed Replace: want %v, got %v", originalUpdatedAt, lt.UpdatedAt())
	}
}

// --- Defensive copy on Rounds() ---

func TestLoopTemplate_Rounds_DefensiveCopy(t *testing.T) {
	in := validInput()
	lt, err := entities.NewLoopTemplate(in)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Mutate the returned slice.
	rounds := lt.Rounds()
	rounds[0] = entities.TemplateRound{Kind: vo.RoundKindBarRaiser, Sequence: 99}

	// Aggregate must be unchanged.
	fresh := lt.Rounds()
	if fresh[0].Kind != vo.RoundKindScreen || fresh[0].Sequence != 1 {
		t.Errorf("aggregate mutated via Rounds() return value: %+v", fresh[0])
	}
}

// --- RehydrateLoopTemplate ---

func TestRehydrateLoopTemplate(t *testing.T) {
	id := uuid.New()
	tenantID := shared.NewTenantID()
	intentID := uuid.New()
	createdAt := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	rounds := []entities.TemplateRound{
		{Kind: vo.RoundKindScreen, Sequence: 1},
	}

	lt := entities.RehydrateLoopTemplate(entities.RehydrateLoopTemplateInput{
		ID:        id,
		TenantID:  tenantID,
		IntentID:  intentID,
		Rounds:    rounds,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	})

	if lt.ID() != id {
		t.Errorf("id mismatch")
	}
	if lt.TenantID() != tenantID {
		t.Errorf("tenant mismatch")
	}
	if lt.IntentID() != intentID {
		t.Errorf("intent mismatch")
	}
	if !lt.CreatedAt().Equal(createdAt) {
		t.Errorf("createdAt mismatch")
	}
	if !lt.UpdatedAt().Equal(updatedAt) {
		t.Errorf("updatedAt mismatch")
	}
	r := lt.Rounds()
	if len(r) != 1 || r[0].Kind != vo.RoundKindScreen {
		t.Errorf("rounds mismatch: %+v", r)
	}
}
