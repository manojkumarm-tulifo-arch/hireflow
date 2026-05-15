package persistence

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// Compile-time interface assertion.
var _ repositories.FeedbackRepository = (*PostgresFeedbackRepository)(nil)

// PostgresFeedbackRepository persists interview feedback rows (append-only).
type PostgresFeedbackRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresFeedbackRepository wires the repository.
func NewPostgresFeedbackRepository(pool *pgxpool.Pool) *PostgresFeedbackRepository {
	return &PostgresFeedbackRepository{pool: pool}
}

// Append validates the feedback and inserts a new row.
func (r *PostgresFeedbackRepository) Append(ctx context.Context, row repositories.FeedbackRow) error {
	if err := row.Feedback.Validate(); err != nil {
		return err
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO interview_feedback (
		    id, tenant_id, round_id, interviewer_name, interviewer_email,
		    decision, notes, submitted_by, submitted_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		row.ID, row.TenantID.String(), row.RoundID,
		row.InterviewerName, row.InterviewerEmail,
		string(row.Decision), row.Notes,
		row.SubmittedBy, row.SubmittedAt,
	)
	if err != nil {
		return fmt.Errorf("insert feedback: %w", err)
	}
	return nil
}

// ListByRound returns all feedback rows for the given round, ordered newest first.
func (r *PostgresFeedbackRepository) ListByRound(ctx context.Context, tenant shared.TenantID, roundID uuid.UUID) ([]repositories.FeedbackRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, round_id, interviewer_name, interviewer_email,
		       decision, notes, submitted_by, submitted_at
		FROM interview_feedback
		WHERE tenant_id=$1 AND round_id=$2
		ORDER BY submitted_at DESC`,
		tenant.String(), roundID,
	)
	if err != nil {
		return nil, fmt.Errorf("query feedback: %w", err)
	}
	defer rows.Close()

	var out []repositories.FeedbackRow
	for rows.Next() {
		var fr repositories.FeedbackRow
		var tenantIDStr string
		var decisionStr string
		if err := rows.Scan(
			&fr.ID, &tenantIDStr, &fr.RoundID,
			&fr.InterviewerName, &fr.InterviewerEmail,
			&decisionStr, &fr.Notes,
			&fr.SubmittedBy, &fr.SubmittedAt,
		); err != nil {
			return nil, fmt.Errorf("scan feedback: %w", err)
		}

		parsedTenant, err := shared.ParseTenantID(tenantIDStr)
		if err != nil {
			return nil, fmt.Errorf("parse tenant: %w", err)
		}
		fr.TenantID = parsedTenant
		fr.Decision = vo.FeedbackDecision(decisionStr)

		out = append(out, fr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Return empty slice (not nil) for consistency.
	if out == nil {
		return []repositories.FeedbackRow{}, nil
	}
	return out, nil
}

