// Package persistence holds Postgres-backed implementations of the interview
// repositories.
package persistence

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/hustle/hireflow/internal/interview/domain/entities"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// rowScanner abstracts over pgx.Row and pgx.Rows so scan helpers can serve both.
type rowScanner interface {
	Scan(dest ...any) error
}

// processRow mirrors the interview_processes table columns for scan targets.
type processRow struct {
	id            uuid.UUID
	tenantID      string
	applicationID uuid.UUID
	candidateID   uuid.UUID
	intentID      uuid.UUID
	status        string
	createdAt     time.Time
	updatedAt     time.Time
}

// roundRow mirrors the interview_rounds table columns for scan targets.
type roundRow struct {
	id            uuid.UUID
	tenantID      string
	processID     uuid.UUID
	kind          string
	sequence      int
	status        string
	questions     []byte // JSONB — nil if no questions yet
	attemptCount  int
	lastError     string
	nextAttemptAt time.Time
	createdAt     time.Time
	updatedAt     time.Time
}

// serializeProcess flattens an InterviewProcess aggregate into its row
// representation for upsert.
func serializeProcess(p *entities.InterviewProcess) (processRow, []roundRow, error) {
	pr := processRow{
		id:            p.ID(),
		tenantID:      p.TenantID().String(),
		applicationID: p.ApplicationID(),
		candidateID:   p.CandidateID(),
		intentID:      p.IntentID(),
		status:        string(p.Status()),
		createdAt:     p.CreatedAt(),
		updatedAt:     p.UpdatedAt(),
	}

	rrs := make([]roundRow, 0, len(p.Rounds()))
	for _, r := range p.Rounds() {
		rr := roundRow{
			id:            r.ID(),
			tenantID:      p.TenantID().String(),
			processID:     p.ID(),
			kind:          string(r.Kind()),
			sequence:      r.Sequence(),
			status:        string(r.Status()),
			attemptCount:  r.AttemptCount(),
			lastError:     r.LastError(),
			nextAttemptAt: r.NextAttemptAt(),
			createdAt:     r.CreatedAt(),
			updatedAt:     r.UpdatedAt(),
		}

		qs := r.Questions()
		if len(qs) > 0 {
			b, err := json.Marshal(qs)
			if err != nil {
				return processRow{}, nil, fmt.Errorf("marshal questions for round %s: %w", r.ID(), err)
			}
			rr.questions = b
		}

		rrs = append(rrs, rr)
	}

	return pr, rrs, nil
}

// hydrateProcess rebuilds an InterviewProcess aggregate from scanned rows.
func hydrateProcess(pr processRow, rrs []roundRow) (*entities.InterviewProcess, error) {
	tenant, err := shared.ParseTenantID(pr.tenantID)
	if err != nil {
		return nil, fmt.Errorf("process tenant: %w", err)
	}

	status, err := vo.ParseProcessStatus(pr.status)
	if err != nil {
		return nil, fmt.Errorf("process status: %w", err)
	}

	roundInputs := make([]entities.RehydrateRoundInput, 0, len(rrs))
	for _, rr := range rrs {
		kind, err := vo.ParseRoundKind(rr.kind)
		if err != nil {
			return nil, fmt.Errorf("round kind: %w", err)
		}
		rs, err := vo.ParseRoundStatus(rr.status)
		if err != nil {
			return nil, fmt.Errorf("round status: %w", err)
		}

		var questions []vo.Question
		if len(rr.questions) > 0 {
			if err := json.Unmarshal(rr.questions, &questions); err != nil {
				return nil, fmt.Errorf("unmarshal questions for round %s: %w", rr.id, err)
			}
		}

		roundInputs = append(roundInputs, entities.RehydrateRoundInput{
			ID:            rr.id,
			Kind:          kind,
			Sequence:      rr.sequence,
			Status:        rs,
			Questions:     questions,
			AttemptCount:  rr.attemptCount,
			LastError:     rr.lastError,
			NextAttemptAt: rr.nextAttemptAt,
			CreatedAt:     rr.createdAt,
			UpdatedAt:     rr.updatedAt,
		})
	}

	return entities.RehydrateInterviewProcess(entities.RehydrateInterviewProcessInput{
		ID:            pr.id,
		TenantID:      tenant,
		ApplicationID: pr.applicationID,
		CandidateID:   pr.candidateID,
		IntentID:      pr.intentID,
		Status:        status,
		Rounds:        roundInputs,
		CreatedAt:     pr.createdAt,
		UpdatedAt:     pr.updatedAt,
	}), nil
}
