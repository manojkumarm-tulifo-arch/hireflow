package commands

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	auditdomain "github.com/hustle/hireflow/internal/shared/audit/domain"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/events"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
)

// erasureEventPublisher is a narrow interface satisfied by *eventbus.InMemory.
// Defined locally so tests can substitute a lightweight stub without importing
// the eventbus package.
type erasureEventPublisher interface {
	Publish(ctx context.Context, eventName string, event any) error
}

// EraseCandidateInput carries the inputs for the GDPR erasure command.
type EraseCandidateInput struct {
	TenantID    shared.TenantID
	ActorUserID uuid.UUID
	CandidateID uuid.UUID
}

// EraseCandidateHandler implements the GDPR "right to erasure" for a candidate.
// It:
//  1. Cascade-deletes the candidate + all dependent rows in a single transaction.
//  2. Best-effort deletes the associated blob storage objects (outside the tx).
//  3. Writes an audit log entry (load-bearing — propagates error to caller).
//  4. Publishes CandidateErased on the event bus.
type EraseCandidateHandler struct {
	repo    repositories.CandidateRepository
	storage services.ResumeStorage
	audit   auditdomain.AuditWriter
	bus     erasureEventPublisher
	logger  zerolog.Logger
}

// NewEraseCandidateHandler wires the handler.
func NewEraseCandidateHandler(
	repo repositories.CandidateRepository,
	storage services.ResumeStorage,
	audit auditdomain.AuditWriter,
	bus erasureEventPublisher,
	logger zerolog.Logger,
) *EraseCandidateHandler {
	return &EraseCandidateHandler{
		repo:    repo,
		storage: storage,
		audit:   audit,
		bus:     bus,
		logger:  logger,
	}
}

// Handle executes the erasure. Returns:
//   - repositories.ErrCandidateNotFound — caller should 404.
//   - any audit error                   — load-bearing; caller should 500.
//   - any bus error                     — DB commit already happened; logged and returned.
func (h *EraseCandidateHandler) Handle(ctx context.Context, in EraseCandidateInput) error {
	// 1. Cascade delete in a single DB transaction.
	keys, err := h.repo.EraseCascade(ctx, in.TenantID, in.CandidateID)
	if err != nil {
		return err
	}

	// 2. Best-effort blob deletion — DB is the source of truth. Log failures
	// but do not abort: the candidate's PII is already gone from the API surface.
	for _, k := range keys {
		if delErr := h.storage.Delete(ctx, k); delErr != nil {
			h.logger.Warn().
				Err(delErr).
				Str("storage_key", k).
				Str("candidate_id", in.CandidateID.String()).
				Msg("erase_candidate: failed to delete storage blob (best-effort, non-fatal)")
		}
	}

	// 3. Audit log (load-bearing).
	auditErr := h.audit.Write(ctx, auditdomain.AuditEvent{
		ActorUserID:  in.ActorUserID,
		TenantID:     in.TenantID,
		Action:       "candidate_erased",
		ResourceKind: "candidate",
		ResourceID:   in.CandidateID,
		Payload:      map[string]any{"keys_deleted": len(keys)},
		OccurredAt:   time.Now().UTC(),
	})
	if auditErr != nil {
		return auditErr
	}

	// 4. Publish CandidateErased event.
	publishErr := h.bus.Publish(ctx, "sourcing.CandidateErased", events.CandidateErased{
		CandidateID: in.CandidateID,
		TenantID:    in.TenantID,
		ActorUserID: in.ActorUserID,
		OccurredAt:  time.Now().UTC(),
	})
	if publishErr != nil {
		// DB commit already succeeded — log so it is recoverable manually.
		h.logger.Error().
			Err(publishErr).
			Str("candidate_id", in.CandidateID.String()).
			Msg("erase_candidate: bus publish failed after successful DB erase")
		return publishErr
	}

	return nil
}
