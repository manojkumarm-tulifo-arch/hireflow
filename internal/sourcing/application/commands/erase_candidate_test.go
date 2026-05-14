package commands_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
)

// ---------------------------------------------------------------------------
// eraseCandRepo — fake CandidateRepository for erase tests
// ---------------------------------------------------------------------------

type eraseCandRepo struct {
	*fakeExtendedCandidateRepo
	cascadeKeys []string
	cascadeErr  error
}

func newEraseCandRepo(keys []string, err error) *eraseCandRepo {
	return &eraseCandRepo{
		fakeExtendedCandidateRepo: newFakeExtendedCandidateRepo(),
		cascadeKeys:               keys,
		cascadeErr:                err,
	}
}

func (r *eraseCandRepo) EraseCascade(_ context.Context, _ shared.TenantID, _ uuid.UUID) ([]string, error) {
	if r.cascadeErr != nil {
		return nil, r.cascadeErr
	}
	return r.cascadeKeys, nil
}

// ---------------------------------------------------------------------------
// eraseStorage — fake ResumeStorage that tracks Delete calls
// ---------------------------------------------------------------------------

type eraseStorage struct {
	deleteCalls []string
	// deleteErrs maps key → error to inject; keys not present succeed.
	deleteErrs map[string]error
}

func newEraseStorage() *eraseStorage {
	return &eraseStorage{deleteErrs: map[string]error{}}
}

func (s *eraseStorage) Put(_ context.Context, _ string, _ io.Reader) error  { return nil }
func (s *eraseStorage) Open(_ context.Context, _ string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}
func (s *eraseStorage) MoveToQuarantine(_ context.Context, k string) (string, error) {
	return k, nil
}
func (s *eraseStorage) Delete(_ context.Context, k string) error {
	s.deleteCalls = append(s.deleteCalls, k)
	if err, ok := s.deleteErrs[k]; ok {
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// eraseEventBus — fake erasureEventPublisher that tracks Publish calls
// ---------------------------------------------------------------------------

type eraseEventBus struct {
	calls  []string
	retErr error
}

func (b *eraseEventBus) Publish(_ context.Context, eventName string, _ any) error {
	b.calls = append(b.calls, eventName)
	return b.retErr
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func buildEraseHandler(
	repo *eraseCandRepo,
	stor *eraseStorage,
	audit *fakeAuditWriter,
	bus *eraseEventBus,
) *commands.EraseCandidateHandler {
	return commands.NewEraseCandidateHandler(repo, stor, audit, bus, zerolog.Nop())
}

func defaultInput() commands.EraseCandidateInput {
	return commands.EraseCandidateInput{
		TenantID:    shared.NewTenantID(),
		ActorUserID: uuid.New(),
		CandidateID: uuid.New(),
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestEraseCandidate_HappyPath: cascade returns 3 keys → storage deleted 3x
// → audit written → bus published → returns nil.
func TestEraseCandidate_HappyPath(t *testing.T) {
	keys := []string{"k1", "k2", "k3"}
	repo := newEraseCandRepo(keys, nil)
	stor := newEraseStorage()
	audit := &fakeAuditWriter{}
	bus := &eraseEventBus{}

	h := buildEraseHandler(repo, stor, audit, bus)
	err := h.Handle(context.Background(), defaultInput())

	require.NoError(t, err)
	assert.ElementsMatch(t, keys, stor.deleteCalls, "all 3 storage keys must be deleted")
	require.Len(t, audit.events, 1)
	assert.Equal(t, "candidate_erased", audit.events[0].Action)
	assert.Equal(t, "candidate", audit.events[0].ResourceKind)
	assert.Equal(t, map[string]any{"keys_deleted": 3}, audit.events[0].Payload)
	require.Len(t, bus.calls, 1)
	assert.Equal(t, "sourcing.CandidateErased", bus.calls[0])
}

// TestEraseCandidate_CascadeFails: repo error → immediately returned, no other steps.
func TestEraseCandidate_CascadeFails(t *testing.T) {
	dbErr := errors.New("db: connection lost")
	repo := newEraseCandRepo(nil, dbErr)
	stor := newEraseStorage()
	audit := &fakeAuditWriter{}
	bus := &eraseEventBus{}

	h := buildEraseHandler(repo, stor, audit, bus)
	err := h.Handle(context.Background(), defaultInput())

	require.ErrorIs(t, err, dbErr)
	assert.Empty(t, stor.deleteCalls, "storage must NOT be called")
	assert.Empty(t, audit.events, "audit must NOT be called")
	assert.Empty(t, bus.calls, "bus must NOT be called")
}

// TestEraseCandidate_StoragePartialFail: key2 of 3 fails → continues to key3,
// then audit + bus are still called → command returns nil (best-effort).
func TestEraseCandidate_StoragePartialFail(t *testing.T) {
	keys := []string{"k1", "k2", "k3"}
	repo := newEraseCandRepo(keys, nil)
	stor := newEraseStorage()
	stor.deleteErrs["k2"] = errors.New("s3: access denied")
	audit := &fakeAuditWriter{}
	bus := &eraseEventBus{}

	h := buildEraseHandler(repo, stor, audit, bus)
	err := h.Handle(context.Background(), defaultInput())

	require.NoError(t, err, "storage failures are best-effort — command must succeed")
	assert.ElementsMatch(t, keys, stor.deleteCalls,
		"all 3 delete attempts must be made regardless of partial failure")
	require.Len(t, audit.events, 1)
	require.Len(t, bus.calls, 1)
}

// TestEraseCandidate_AuditFails: audit fails → error returned, bus NOT called.
func TestEraseCandidate_AuditFails(t *testing.T) {
	repo := newEraseCandRepo([]string{"k1"}, nil)
	stor := newEraseStorage()
	auditErr := errors.New("audit: db down")
	audit := &fakeAuditWriter{writeErr: auditErr}
	bus := &eraseEventBus{}

	h := buildEraseHandler(repo, stor, audit, bus)
	err := h.Handle(context.Background(), defaultInput())

	require.ErrorIs(t, err, auditErr)
	assert.Empty(t, bus.calls, "bus must NOT be called when audit fails")
}

// TestEraseCandidate_BusFails: bus publish fails → error returned (DB already committed).
func TestEraseCandidate_BusFails(t *testing.T) {
	repo := newEraseCandRepo([]string{"k1"}, nil)
	stor := newEraseStorage()
	audit := &fakeAuditWriter{}
	busErr := errors.New("eventbus: subscriber failed")
	bus := &eraseEventBus{retErr: busErr}

	h := buildEraseHandler(repo, stor, audit, bus)
	err := h.Handle(context.Background(), defaultInput())

	require.ErrorIs(t, err, busErr)
	// Audit was still written before bus.
	require.Len(t, audit.events, 1)
}

// TestEraseCandidate_NotFound: repo returns ErrCandidateNotFound → propagated.
func TestEraseCandidate_NotFound(t *testing.T) {
	repo := newEraseCandRepo(nil, repositories.ErrCandidateNotFound)
	stor := newEraseStorage()
	audit := &fakeAuditWriter{}
	bus := &eraseEventBus{}

	h := buildEraseHandler(repo, stor, audit, bus)
	err := h.Handle(context.Background(), defaultInput())

	require.ErrorIs(t, err, repositories.ErrCandidateNotFound)
	assert.Empty(t, stor.deleteCalls)
	assert.Empty(t, audit.events)
	assert.Empty(t, bus.calls)
}
