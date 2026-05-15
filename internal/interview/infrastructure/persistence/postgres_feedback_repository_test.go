//go:build integration

package persistence_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	"github.com/hustle/hireflow/internal/interview/infrastructure/persistence"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// newFeedbackRow creates a valid FeedbackRow for tests.
func newFeedbackRow(tenant shared.TenantID, roundID uuid.UUID, submittedAt time.Time) repositories.FeedbackRow {
	return repositories.FeedbackRow{
		ID:       uuid.New(),
		TenantID: tenant,
		RoundID:  roundID,
		Feedback: vo.Feedback{
			InterviewerName:  "Alice Recruiter",
			InterviewerEmail: "alice@example.com",
			Decision:         vo.FeedbackDecisionYes,
			Notes:            "Good candidate",
			SubmittedBy:      uuid.New(),
			SubmittedAt:      submittedAt,
		},
	}
}

// TestFeedbackAppend_PersistsRow appends a row and verifies it can be listed.
func TestFeedbackAppend_PersistsRow(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresFeedbackRepository(pool)
	tenant := shared.NewTenantID()
	roundID := uuid.New()

	row := newFeedbackRow(tenant, roundID, time.Now().UTC())
	require.NoError(t, repo.Append(context.Background(), row))

	rows, err := repo.ListByRound(context.Background(), tenant, roundID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, row.ID, rows[0].ID)
	assert.Equal(t, "Alice Recruiter", rows[0].InterviewerName)
	assert.Equal(t, vo.FeedbackDecisionYes, rows[0].Decision)
}

// TestFeedbackAppend_InvalidDecision_RejectedAtDB bypasses the in-Go enum
// validation by crafting a raw INSERT with a bogus decision value and asserts
// Postgres rejects it via the CHECK constraint.
func TestFeedbackAppend_InvalidDecision_RejectedAtDB(t *testing.T) {
	pool := newPool(t)
	tenant := shared.NewTenantID()
	roundID := uuid.New()

	// Bypass the repository Validate() call by inserting directly via pool.
	_, err := pool.Exec(context.Background(), `
		INSERT INTO interview_feedback (id, tenant_id, round_id, interviewer_name, decision, submitted_by, submitted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.New(), tenant.String(), roundID, "Bob", "garbage", uuid.New(), time.Now().UTC(),
	)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "interview_feedback_decision_valid"),
		"expected CHECK constraint violation, got: %v", err)
}

// TestFeedbackListByRound_OrdersNewestFirst appends 3 rows with increasing
// submitted_at and verifies ListByRound returns them in reverse order.
func TestFeedbackListByRound_OrdersNewestFirst(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresFeedbackRepository(pool)
	tenant := shared.NewTenantID()
	roundID := uuid.New()

	base := time.Now().UTC()
	row1 := newFeedbackRow(tenant, roundID, base.Add(-2*time.Minute))
	row2 := newFeedbackRow(tenant, roundID, base.Add(-1*time.Minute))
	row3 := newFeedbackRow(tenant, roundID, base)

	require.NoError(t, repo.Append(context.Background(), row1))
	require.NoError(t, repo.Append(context.Background(), row2))
	require.NoError(t, repo.Append(context.Background(), row3))

	got, err := repo.ListByRound(context.Background(), tenant, roundID)
	require.NoError(t, err)
	require.Len(t, got, 3)

	// Newest first.
	assert.Equal(t, row3.ID, got[0].ID, "newest row must be first")
	assert.Equal(t, row2.ID, got[1].ID, "second newest must be second")
	assert.Equal(t, row1.ID, got[2].ID, "oldest must be last")
}

// TestFeedbackListByRound_TenantScoped verifies that tenant B cannot see
// tenant A's feedback rows.
func TestFeedbackListByRound_TenantScoped(t *testing.T) {
	pool := newPool(t)
	repo := persistence.NewPostgresFeedbackRepository(pool)
	tenantA := shared.NewTenantID()
	tenantB := shared.NewTenantID()
	roundID := uuid.New()

	rowA := newFeedbackRow(tenantA, roundID, time.Now().UTC())
	require.NoError(t, repo.Append(context.Background(), rowA))

	gotA, err := repo.ListByRound(context.Background(), tenantA, roundID)
	require.NoError(t, err)
	require.Len(t, gotA, 1)

	gotB, err := repo.ListByRound(context.Background(), tenantB, roundID)
	require.NoError(t, err)
	assert.Empty(t, gotB, "tenant B must not see tenant A's feedback")
}
