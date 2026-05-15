package commands_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/application/commands"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ---------------------------------------------------------------------------
// In-memory fakes
// ---------------------------------------------------------------------------

// fakeProcessRepo is an in-memory implementation of ProcessRepository.
type fakeProcessRepo struct {
	mu        sync.Mutex
	byID      map[uuid.UUID]*entities.InterviewProcess
	byAppID   map[uuid.UUID]*entities.InterviewProcess
	byRoundID map[uuid.UUID]*entities.InterviewProcess
	saveErr   error // if set, Save returns this error
}

func newFakeProcessRepo() *fakeProcessRepo {
	return &fakeProcessRepo{
		byID:      make(map[uuid.UUID]*entities.InterviewProcess),
		byAppID:   make(map[uuid.UUID]*entities.InterviewProcess),
		byRoundID: make(map[uuid.UUID]*entities.InterviewProcess),
	}
}

func (r *fakeProcessRepo) Save(_ context.Context, p *entities.InterviewProcess) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saveErr != nil {
		return r.saveErr
	}
	// Simulate UNIQUE constraint on (tenant, application_id).
	if existing, ok := r.byAppID[p.ApplicationID()]; ok && existing.ID() != p.ID() {
		return repositories.ErrProcessDuplicate
	}
	r.byID[p.ID()] = p
	r.byAppID[p.ApplicationID()] = p
	for _, round := range p.Rounds() {
		r.byRoundID[round.ID()] = p
	}
	return nil
}

func (r *fakeProcessRepo) FindByID(_ context.Context, _ shared.TenantID, id uuid.UUID) (*entities.InterviewProcess, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byID[id]
	if !ok {
		return nil, repositories.ErrProcessNotFound
	}
	return p, nil
}

func (r *fakeProcessRepo) FindByApplicationID(_ context.Context, tenant shared.TenantID, applicationID uuid.UUID) (*entities.InterviewProcess, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byAppID[applicationID]
	if !ok {
		return nil, repositories.ErrProcessNotFound
	}
	if p.TenantID() != tenant {
		return nil, repositories.ErrProcessNotFound
	}
	return p, nil
}

func (r *fakeProcessRepo) FindByRoundID(_ context.Context, _ shared.TenantID, roundID uuid.UUID) (*entities.InterviewProcess, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byRoundID[roundID]
	if !ok {
		return nil, repositories.ErrProcessNotFound
	}
	return p, nil
}

func (r *fakeProcessRepo) ListByTenant(_ context.Context, _ shared.TenantID, _ repositories.ProcessListFilter) ([]*entities.InterviewProcess, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*entities.InterviewProcess, 0, len(r.byID))
	for _, p := range r.byID {
		out = append(out, p)
	}
	return out, nil
}

func (r *fakeProcessRepo) ClaimNextPendingRound(_ context.Context) (*entities.InterviewProcess, uuid.UUID, error) {
	return nil, uuid.Nil, nil
}

// fakeTemplateRepo is an in-memory implementation of LoopTemplateRepository.
type fakeTemplateRepo struct {
	mu        sync.Mutex
	byIntent  map[uuid.UUID]*entities.LoopTemplate
	findErr   error // if set, FindByIntent returns this error
}

func newFakeTemplateRepo() *fakeTemplateRepo {
	return &fakeTemplateRepo{
		byIntent: make(map[uuid.UUID]*entities.LoopTemplate),
	}
}

func (r *fakeTemplateRepo) Save(_ context.Context, t *entities.LoopTemplate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byIntent[t.IntentID()] = t
	return nil
}

func (r *fakeTemplateRepo) FindByIntent(_ context.Context, _ shared.TenantID, intentID uuid.UUID) (*entities.LoopTemplate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.findErr != nil {
		return nil, r.findErr
	}
	t, ok := r.byIntent[intentID]
	if !ok {
		return nil, repositories.ErrLoopTemplateNotFound
	}
	return t, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newValidInput(tenantID shared.TenantID) commands.StartInterviewProcessInput {
	return commands.StartInterviewProcessInput{
		TenantID:      tenantID,
		ApplicationID: uuid.New(),
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestStartInterviewProcess_NewApplication_CreatesWithDefaultLoop(t *testing.T) {
	processes := newFakeProcessRepo()
	templates := newFakeTemplateRepo() // empty — no template

	h := commands.NewStartInterviewProcessHandler(processes, templates)
	tenantID := shared.NewTenantID()
	in := newValidInput(tenantID)

	if err := h.Handle(context.Background(), in); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	p, err := processes.FindByApplicationID(context.Background(), tenantID, in.ApplicationID)
	if err != nil {
		t.Fatalf("process not saved: %v", err)
	}

	rounds := p.Rounds()
	if len(rounds) != len(commands.DefaultLoop) {
		t.Fatalf("expected %d rounds (DefaultLoop), got %d", len(commands.DefaultLoop), len(rounds))
	}
	for i, r := range rounds {
		want := commands.DefaultLoop[i]
		if r.Kind() != want.Kind {
			t.Errorf("round %d: kind want %v, got %v", i, want.Kind, r.Kind())
		}
		if r.Sequence() != want.Sequence {
			t.Errorf("round %d: sequence want %d, got %d", i, want.Sequence, r.Sequence())
		}
	}
	// Verify DefaultLoop order: screen, technical, bar_raiser
	if rounds[0].Kind() != vo.RoundKindScreen {
		t.Errorf("round 0 should be screen, got %v", rounds[0].Kind())
	}
	if rounds[1].Kind() != vo.RoundKindTechnical {
		t.Errorf("round 1 should be technical, got %v", rounds[1].Kind())
	}
	if rounds[2].Kind() != vo.RoundKindBarRaiser {
		t.Errorf("round 2 should be bar_raiser, got %v", rounds[2].Kind())
	}
}

func TestStartInterviewProcess_UsesIntentTemplate(t *testing.T) {
	processes := newFakeProcessRepo()
	templates := newFakeTemplateRepo()

	tenantID := shared.NewTenantID()
	intentID := uuid.New()

	// Seed a 4-round template.
	fourRounds := []entities.TemplateRound{
		{Kind: vo.RoundKindScreen, Sequence: 1},
		{Kind: vo.RoundKindTechnical, Sequence: 2},
		{Kind: vo.RoundKindSystemDesign, Sequence: 3},
		{Kind: vo.RoundKindBarRaiser, Sequence: 4},
	}
	tmpl, err := entities.NewLoopTemplate(entities.NewLoopTemplateInput{
		TenantID: tenantID,
		IntentID: intentID,
		Rounds:   fourRounds,
	})
	if err != nil {
		t.Fatalf("setup template: %v", err)
	}
	_ = templates.Save(context.Background(), tmpl)

	h := commands.NewStartInterviewProcessHandler(processes, templates)
	in := commands.StartInterviewProcessInput{
		TenantID:      tenantID,
		ApplicationID: uuid.New(),
		CandidateID:   uuid.New(),
		IntentID:      intentID,
	}

	if err := h.Handle(context.Background(), in); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	p, err := processes.FindByApplicationID(context.Background(), tenantID, in.ApplicationID)
	if err != nil {
		t.Fatalf("process not saved: %v", err)
	}

	rounds := p.Rounds()
	if len(rounds) != 4 {
		t.Fatalf("expected 4 rounds from template, got %d", len(rounds))
	}
	if rounds[2].Kind() != vo.RoundKindSystemDesign {
		t.Errorf("round 2: want system_design, got %v", rounds[2].Kind())
	}
	if rounds[3].Kind() != vo.RoundKindBarRaiser {
		t.Errorf("round 3: want bar_raiser, got %v", rounds[3].Kind())
	}
}

func TestStartInterviewProcess_IdempotentOnSecondCall(t *testing.T) {
	processes := newFakeProcessRepo()
	templates := newFakeTemplateRepo()

	h := commands.NewStartInterviewProcessHandler(processes, templates)
	tenantID := shared.NewTenantID()
	in := newValidInput(tenantID)

	// First call.
	if err := h.Handle(context.Background(), in); err != nil {
		t.Fatalf("first Handle error: %v", err)
	}

	// Second call with same input.
	if err := h.Handle(context.Background(), in); err != nil {
		t.Fatalf("second Handle error: %v", err)
	}

	// Only one process should exist.
	all, err := processes.ListByTenant(context.Background(), tenantID, repositories.ProcessListFilter{})
	if err != nil {
		t.Fatalf("ListByTenant: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected 1 process, got %d", len(all))
	}
}

func TestStartInterviewProcess_HandlesSaveDuplicate(t *testing.T) {
	processes := newFakeProcessRepo()
	processes.saveErr = repositories.ErrProcessDuplicate
	templates := newFakeTemplateRepo()

	h := commands.NewStartInterviewProcessHandler(processes, templates)
	in := newValidInput(shared.NewTenantID())

	// ErrProcessDuplicate on Save must be treated as idempotent (nil).
	if err := h.Handle(context.Background(), in); err != nil {
		t.Fatalf("expected nil for ErrProcessDuplicate, got: %v", err)
	}
}

func TestStartInterviewProcess_SaveFailure_Propagates(t *testing.T) {
	processes := newFakeProcessRepo()
	genericErr := errors.New("db connection refused")
	processes.saveErr = genericErr
	templates := newFakeTemplateRepo()

	h := commands.NewStartInterviewProcessHandler(processes, templates)
	in := newValidInput(shared.NewTenantID())

	err := h.Handle(context.Background(), in)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, genericErr) {
		t.Errorf("expected error to wrap genericErr, got: %v", err)
	}
}

func TestStartInterviewProcess_TemplateLookupFailure_Propagates(t *testing.T) {
	processes := newFakeProcessRepo()
	templates := newFakeTemplateRepo()
	dbErr := errors.New("template db timeout")
	templates.findErr = dbErr

	h := commands.NewStartInterviewProcessHandler(processes, templates)
	in := newValidInput(shared.NewTenantID())

	err := h.Handle(context.Background(), in)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("expected error to wrap dbErr, got: %v", err)
	}
}
