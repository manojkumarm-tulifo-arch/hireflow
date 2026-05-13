// Package queries holds the sourcing context's read-side handlers.
package queries

import (
	"context"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/dto"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// GetBatchStatusHandler returns the aggregated status of a batch.
type GetBatchStatusHandler struct {
	repo repositories.ResumeUploadRepository
}

// NewGetBatchStatusHandler wires the handler.
func NewGetBatchStatusHandler(repo repositories.ResumeUploadRepository) *GetBatchStatusHandler {
	return &GetBatchStatusHandler{repo: repo}
}

// Handle returns the BatchStatusDTO for (tenant, batchID).
func (h *GetBatchStatusHandler) Handle(ctx context.Context, tenant shared.TenantID, batchID uuid.UUID) (dto.BatchStatusDTO, error) {
	rows, err := h.repo.ListByBatch(ctx, tenant, batchID)
	if err != nil {
		return dto.BatchStatusDTO{}, err
	}

	out := dto.BatchStatusDTO{BatchID: batchID}
	for _, u := range rows {
		if out.IntentID == uuid.Nil {
			out.IntentID = u.IntentID()
		}
		out.Summary.Total++
		switch u.Status() {
		case vo.StatusExtracted:
			out.Summary.Extracted++
		case vo.StatusFailed:
			out.Summary.Failed++
		case vo.StatusQuarantined:
			out.Summary.Quarantined++
		default:
			out.Summary.InFlight++
		}
		item := dto.BatchStatusItemDTO{
			UploadID:  u.ID(),
			Filename:  u.OriginalName(),
			Status:    string(u.Status()),
			Attempt:   u.AttemptCount(),
			LastError: u.LastError(),
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}
