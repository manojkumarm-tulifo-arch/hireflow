package queries_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/application/queries"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	auditdomain "github.com/hustle/hireflow/internal/shared/audit/domain"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ---------------------------------------------------------------------------
// In-memory fakes
// ---------------------------------------------------------------------------

type fakeProcessRepo struct {
	mu      sync.Mutex
	byID    map[uuid.UUID]*entities.InterviewProcess
	byAppID map[uuid.UUID]*entities.InterviewProcess
	listErr error
}

func newFakeProcessRepo() *fakeProcessRepo {
	return &fakeProcessRepo{
		byID:    make(map[uuid.UUID]*entities.InterviewProcess),
		byAppID: make(map[uuid.UUID]*entities.InterviewProcess),
	}
}

func (r *fakeProcessRepo) Save(_ context.Context, p *entities.InterviewProcess) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.byAppID[p.ApplicationID()]; ok && existing.ID() != p.ID() {
		return repositories.ErrProcessDuplicate
	}
	r.byID[p.ID()] = p
	r.byAppID[p.ApplicationID()] = p
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
	for _, p := range r.byID {
		for _, round := range p.Rounds() {
			if round.ID() == roundID {
				return p, nil
			}
		}
	}
	return nil, repositories.ErrProcessNotFound
}

func (r *fakeProcessRepo) ListByTenant(_ context.Context, tenant shared.TenantID, filter repositories.ProcessListFilter) ([]*entities.InterviewProcess, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, r.listErr
	}
	out := make([]*entities.InterviewProcess, 0, len(r.byID))
	for _, p := range r.byID {
		if p.TenantID() != tenant {
			continue
		}
		if filter.IntentID != uuid.Nil && p.IntentID() != filter.IntentID {
			continue
		}
		if filter.Status != "" && string(p.Status()) != filter.Status {
			continue
		}
		out = append(out, p)
	}
	// Apply offset + limit.
	if filter.Offset > len(out) {
		return []*entities.InterviewProcess{}, nil
	}
	out = out[filter.Offset:]
	if filter.Limit > 0 && filter.Limit < len(out) {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (r *fakeProcessRepo) ClaimNextPendingRound(_ context.Context) (*entities.InterviewProcess, uuid.UUID, error) {
	return nil, uuid.Nil, nil
}

// fakeFeedbackRepo is an in-memory FeedbackRepository for query tests.
type fakeFeedbackRepo struct {
	mu      sync.Mutex
	byRound map[uuid.UUID][]repositories.FeedbackRow
	listErr error
}

func newFakeFeedbackRepo() *fakeFeedbackRepo {
	return &fakeFeedbackRepo{byRound: make(map[uuid.UUID][]repositories.FeedbackRow)}
}

func (r *fakeFeedbackRepo) Append(_ context.Context, row repositories.FeedbackRow) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Prepend to simulate newest-first order (each append is newer than previous).
	r.byRound[row.RoundID] = append([]repositories.FeedbackRow{row}, r.byRound[row.RoundID]...)
	return nil
}

func (r *fakeFeedbackRepo) ListByRound(_ context.Context, _ shared.TenantID, roundID uuid.UUID) ([]repositories.FeedbackRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.byRound[roundID], nil
}

// templateKey is a composite key for tenant-scoped template storage.
type templateKey struct {
	tenantID shared.TenantID
	intentID uuid.UUID
}

// fakeTemplateRepo is an in-memory LoopTemplateRepository for query tests.
// Keys on (tenantID, intentID) to enforce tenant isolation.
type fakeTemplateRepo struct {
	mu      sync.Mutex
	byKey   map[templateKey]*entities.LoopTemplate
	findErr error
}

func newFakeTemplateRepo() *fakeTemplateRepo {
	return &fakeTemplateRepo{byKey: make(map[templateKey]*entities.LoopTemplate)}
}

func (r *fakeTemplateRepo) Save(_ context.Context, t *entities.LoopTemplate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byKey[templateKey{tenantID: t.TenantID(), intentID: t.IntentID()}] = t
	return nil
}

func (r *fakeTemplateRepo) FindByIntent(_ context.Context, tenant shared.TenantID, intentID uuid.UUID) (*entities.LoopTemplate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.findErr != nil {
		return nil, r.findErr
	}
	t, ok := r.byKey[templateKey{tenantID: tenant, intentID: intentID}]
	if !ok {
		return nil, repositories.ErrLoopTemplateNotFound
	}
	return t, nil
}

// captureAuditWriter records audit events.
type captureAuditWriter struct {
	events []auditdomain.AuditEvent
	err    error
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

func seedProcess(t *testing.T, repo *fakeProcessRepo, tenantID shared.TenantID) (*entities.InterviewProcess, uuid.UUID) {
	t.Helper()
	p, err := entities.NewInterviewProcess(entities.NewInterviewProcessInput{
		TenantID:      tenantID,
		ApplicationID: uuid.New(),
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
		Rounds: []entities.TemplateRound{
			{Kind: vo.RoundKindTechnical, Sequence: 1},
		},
	})
	if err != nil {
		t.Fatalf("seedProcess: %v", err)
	}
	if err := repo.Save(context.Background(), p); err != nil {
		t.Fatalf("seedProcess save: %v", err)
	}
	return p, p.Rounds()[0].ID()
}

func appendFeedback(t *testing.T, repo *fakeFeedbackRepo, tenantID shared.TenantID, roundID uuid.UUID, decision vo.FeedbackDecision) {
	t.Helper()
	row := repositories.FeedbackRow{
		ID:       uuid.New(),
		TenantID: tenantID,
		RoundID:  roundID,
		Feedback: vo.Feedback{
			InterviewerName: "Interviewer",
			Decision:        decision,
			SubmittedBy:     uuid.New(),
			SubmittedAt:     time.Now().UTC(),
		},
	}
	if err := repo.Append(context.Background(), row); err != nil {
		t.Fatalf("appendFeedback: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestGet_HappyPath_ReturnsProcessWithRounds(t *testing.T) {
	tenantID := shared.NewTenantID()
	processes := newFakeProcessRepo()
	feedback := newFakeFeedbackRepo()
	audit := &captureAuditWriter{}

	p, _ := seedProcess(t, processes, tenantID)
	actorID := uuid.New()

	h := queries.NewGetInterviewProcessHandler(processes, feedback, audit)
	result, err := h.Handle(context.Background(), tenantID, actorID, p.ID())
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	// Assert core fields.
	if result.ID != p.ID() {
		t.Errorf("ID: want %v, got %v", p.ID(), result.ID)
	}
	if result.TenantID != tenantID.String() {
		t.Errorf("TenantID: want %v, got %v", tenantID.String(), result.TenantID)
	}
	if result.ApplicationID != p.ApplicationID() {
		t.Errorf("ApplicationID: want %v, got %v", p.ApplicationID(), result.ApplicationID)
	}
	if result.CandidateID != p.CandidateID() {
		t.Errorf("CandidateID: want %v, got %v", p.CandidateID(), result.CandidateID)
	}
	if result.IntentID != p.IntentID() {
		t.Errorf("IntentID: want %v, got %v", p.IntentID(), result.IntentID)
	}
	if result.Status != string(p.Status()) {
		t.Errorf("Status: want %v, got %v", p.Status(), result.Status)
	}

	// Assert rounds.
	if len(result.Rounds) != 1 {
		t.Fatalf("Rounds: want 1, got %d", len(result.Rounds))
	}
	round := result.Rounds[0]
	if round.ID != p.Rounds()[0].ID() {
		t.Errorf("Round ID: want %v, got %v", p.Rounds()[0].ID(), round.ID)
	}
	if round.Kind != string(vo.RoundKindTechnical) {
		t.Errorf("Round Kind: want %v, got %v", vo.RoundKindTechnical, round.Kind)
	}

	// Assert audit written.
	if len(audit.events) != 1 {
		t.Fatalf("audit events: want 1, got %d", len(audit.events))
	}
	ae := audit.events[0]
	if ae.Action != "interview_process_read" {
		t.Errorf("audit action: want %q, got %q", "interview_process_read", ae.Action)
	}
	if ae.ResourceID != p.ID() {
		t.Errorf("audit resource_id: want %v, got %v", p.ID(), ae.ResourceID)
	}
	if ae.ActorUserID != actorID {
		t.Errorf("audit actor_user_id: want %v, got %v", actorID, ae.ActorUserID)
	}
}

func TestGet_AggregatesFeedback(t *testing.T) {
	tenantID := shared.NewTenantID()
	processes := newFakeProcessRepo()
	feedback := newFakeFeedbackRepo()
	audit := &captureAuditWriter{}

	p, roundID := seedProcess(t, processes, tenantID)

	// 2 strong_yes + 1 mixed.
	appendFeedback(t, feedback, tenantID, roundID, vo.FeedbackDecisionStrongYes)
	appendFeedback(t, feedback, tenantID, roundID, vo.FeedbackDecisionStrongYes)
	appendFeedback(t, feedback, tenantID, roundID, vo.FeedbackDecisionMixed)

	h := queries.NewGetInterviewProcessHandler(processes, feedback, audit)
	result, err := h.Handle(context.Background(), tenantID, uuid.New(), p.ID())
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	if len(result.Rounds) != 1 {
		t.Fatalf("Rounds: want 1, got %d", len(result.Rounds))
	}
	fs := result.Rounds[0].FeedbackSummary
	if fs.StrongYes != 2 {
		t.Errorf("StrongYes: want 2, got %d", fs.StrongYes)
	}
	if fs.Mixed != 1 {
		t.Errorf("Mixed: want 1, got %d", fs.Mixed)
	}
	if fs.Total != 3 {
		t.Errorf("Total: want 3, got %d", fs.Total)
	}
	if fs.Yes != 0 {
		t.Errorf("Yes: want 0, got %d", fs.Yes)
	}
	if fs.No != 0 {
		t.Errorf("No: want 0, got %d", fs.No)
	}
	if fs.StrongNo != 0 {
		t.Errorf("StrongNo: want 0, got %d", fs.StrongNo)
	}
}

func TestGet_LatestDecision(t *testing.T) {
	tenantID := shared.NewTenantID()
	processes := newFakeProcessRepo()
	feedback := newFakeFeedbackRepo()
	audit := &captureAuditWriter{}

	p, roundID := seedProcess(t, processes, tenantID)

	// Append in chronological order; fake prepends so newest-first.
	// After appending: [mixed, yes, strong_yes] (newest first = mixed).
	appendFeedback(t, feedback, tenantID, roundID, vo.FeedbackDecisionStrongYes)
	appendFeedback(t, feedback, tenantID, roundID, vo.FeedbackDecisionYes)
	appendFeedback(t, feedback, tenantID, roundID, vo.FeedbackDecisionMixed)

	h := queries.NewGetInterviewProcessHandler(processes, feedback, audit)
	result, err := h.Handle(context.Background(), tenantID, uuid.New(), p.ID())
	if err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	fs := result.Rounds[0].FeedbackSummary
	// Newest row (last appended) is first in the list — mixed.
	if fs.LatestDecision != string(vo.FeedbackDecisionMixed) {
		t.Errorf("LatestDecision: want %q, got %q", vo.FeedbackDecisionMixed, fs.LatestDecision)
	}
}

func TestGet_ProcessNotFound_ReturnsErr(t *testing.T) {
	tenantID := shared.NewTenantID()
	processes := newFakeProcessRepo()
	feedback := newFakeFeedbackRepo()
	audit := &captureAuditWriter{}

	h := queries.NewGetInterviewProcessHandler(processes, feedback, audit)
	_, err := h.Handle(context.Background(), tenantID, uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, repositories.ErrProcessNotFound) {
		t.Errorf("expected ErrProcessNotFound, got: %v", err)
	}
}

func TestGet_AuditFailurePropagates(t *testing.T) {
	tenantID := shared.NewTenantID()
	processes := newFakeProcessRepo()
	feedback := newFakeFeedbackRepo()
	auditErr := errors.New("audit store unavailable")
	audit := &captureAuditWriter{err: auditErr}

	p, _ := seedProcess(t, processes, tenantID)

	h := queries.NewGetInterviewProcessHandler(processes, feedback, audit)
	_, err := h.Handle(context.Background(), tenantID, uuid.New(), p.ID())
	if err == nil {
		t.Fatal("expected error from audit failure, got nil")
	}
	if !errors.Is(err, auditErr) {
		t.Errorf("expected error to wrap auditErr, got: %v", err)
	}
}
