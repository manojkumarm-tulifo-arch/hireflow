package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	auditdomain "github.com/hustle/hireflow/internal/shared/audit/domain"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
)

// ApplicationAction names the lifecycle transition a recruiter requests.
type ApplicationAction string

const (
	ActionShortlist          ApplicationAction = "shortlist"
	ActionReject             ApplicationAction = "reject"
	ActionHire               ApplicationAction = "hire"
	ActionMoveToInterviewing ApplicationAction = "move_to_interviewing"
)

// TransitionApplicationInput carries the input for a single lifecycle transition.
type TransitionApplicationInput struct {
	TenantID      shared.TenantID
	ActorUserID   uuid.UUID
	ApplicationID uuid.UUID
	Action        ApplicationAction
	// RejectReason is required when Action == ActionReject; ignored otherwise.
	RejectReason string
}

// TransitionApplicationHandler applies a recruiter-initiated lifecycle action
// to an Application and writes an audit event.
type TransitionApplicationHandler struct {
	repo  repositories.ApplicationRepository
	audit auditdomain.AuditWriter
}

// NewTransitionApplicationHandler wires the handler.
func NewTransitionApplicationHandler(
	repo repositories.ApplicationRepository,
	audit auditdomain.AuditWriter,
) *TransitionApplicationHandler {
	return &TransitionApplicationHandler{repo: repo, audit: audit}
}

// Handle applies the requested action to the Application identified by in.ApplicationID.
//
// Error semantics:
//   - repositories.ErrApplicationNotFound  → app not found (caller should 404)
//   - entities.ErrInvalidTransition        → bad state machine transition (caller should 400)
//   - errors.New("reject: reason required")→ missing Reject reason (caller should 400)
//   - any audit error                      → load-bearing; propagated to caller (caller should 500)
func (h *TransitionApplicationHandler) Handle(ctx context.Context, in TransitionApplicationInput) error {
	app, err := h.repo.FindByID(ctx, in.TenantID, in.ApplicationID)
	if err != nil {
		return err
	}

	switch in.Action {
	case ActionShortlist:
		if err := app.Shortlist(in.ActorUserID); err != nil {
			return err
		}
	case ActionReject:
		if err := app.Reject(in.ActorUserID, in.RejectReason); err != nil {
			return err
		}
	case ActionHire:
		if err := app.Hire(in.ActorUserID); err != nil {
			return err
		}
	case ActionMoveToInterviewing:
		if err := app.MoveToInterviewing(in.ActorUserID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown action: %s", in.Action)
	}

	if err := h.repo.Save(ctx, app); err != nil {
		return err
	}

	// Build audit payload — only include reason for Reject actions.
	var payload map[string]any
	if in.Action == ActionReject {
		payload = map[string]any{"reason": in.RejectReason}
	}

	auditErr := h.audit.Write(ctx, auditdomain.AuditEvent{
		ActorUserID:  in.ActorUserID,
		TenantID:     in.TenantID,
		Action:       "application_" + string(in.Action),
		ResourceKind: "application",
		ResourceID:   in.ApplicationID,
		Payload:      payload,
		OccurredAt:   time.Now().UTC(),
	})
	if auditErr != nil {
		// Audit is load-bearing: surface the error to the caller.
		return auditErr
	}
	return nil
}
