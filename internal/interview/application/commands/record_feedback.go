package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/domain/events"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	auditdomain "github.com/hustle/hireflow/internal/shared/audit/domain"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

type RecordFeedbackInput struct {
	TenantID    shared.TenantID
	ActorUserID uuid.UUID
	RoundID     uuid.UUID
	Feedback    vo.Feedback
}

// OutboxAppender is the narrow interface for writing one event row directly
// into the interview_outbox table.
type OutboxAppender interface {
	Append(ctx context.Context, event events.Event) error
}

// RecordFeedbackHandler appends a feedback row + writes an audit + emits
// an InterviewFeedbackRecorded event via direct outbox write.
type RecordFeedbackHandler struct {
	feedback  repositories.FeedbackRepository
	processes repositories.ProcessRepository
	audit     auditdomain.AuditWriter
	outbox    OutboxAppender
}

func NewRecordFeedbackHandler(
	feedback repositories.FeedbackRepository,
	processes repositories.ProcessRepository,
	audit auditdomain.AuditWriter,
	outbox OutboxAppender,
) *RecordFeedbackHandler {
	return &RecordFeedbackHandler{feedback: feedback, processes: processes, audit: audit, outbox: outbox}
}

func (h *RecordFeedbackHandler) Handle(ctx context.Context, in RecordFeedbackInput) error {
	process, err := h.processes.FindByRoundID(ctx, in.TenantID, in.RoundID)
	if err != nil {
		return err
	}
	var roundStatus *vo.RoundStatus
	for _, r := range process.Rounds() {
		if r.ID() == in.RoundID {
			s := r.Status()
			roundStatus = &s
			break
		}
	}
	if roundStatus == nil {
		return fmt.Errorf("round not in returned process")
	}
	if *roundStatus != vo.RoundStatusQuestionsReady {
		return fmt.Errorf("feedback: round must be QuestionsReady, was %s", *roundStatus)
	}

	id := uuid.New()
	now := time.Now().UTC()
	in.Feedback.SubmittedAt = now
	if err := h.feedback.Append(ctx, repositories.FeedbackRow{
		ID:       id,
		TenantID: in.TenantID,
		RoundID:  in.RoundID,
		Feedback: in.Feedback,
	}); err != nil {
		return fmt.Errorf("append feedback: %w", err)
	}

	if err := h.outbox.Append(ctx, events.InterviewFeedbackRecorded{
		FeedbackID: id,
		RoundID:    in.RoundID,
		Decision:   string(in.Feedback.Decision),
		TenantID:   in.TenantID,
		OccurredAt: now,
	}); err != nil {
		return fmt.Errorf("emit event: %w", err)
	}

	if err := h.audit.Write(ctx, auditdomain.AuditEvent{
		ActorUserID:  in.ActorUserID,
		TenantID:     in.TenantID,
		Action:       "interview_round_feedback_recorded",
		ResourceKind: "interview_round",
		ResourceID:   in.RoundID,
		Payload:      map[string]any{"decision": string(in.Feedback.Decision)},
		OccurredAt:   now,
	}); err != nil {
		return err
	}
	return nil
}
