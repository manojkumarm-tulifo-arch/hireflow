package persistence

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// uploadRow mirrors the resume_uploads columns we read/write.
// UUID-typed domain objects that don't expose uuid.UUID directly (e.g. TenantID)
// are stored as strings and parsed on hydration.
type uploadRow struct {
	id             uuid.UUID
	tenantID       string
	intentID       uuid.UUID
	batchID        uuid.UUID
	candidateID    *uuid.UUID
	storageKey     string
	originalName   string
	mimeType       string
	sizeBytes      int64
	contentHash    string
	status         string
	stageArtifacts []byte
	attemptCount   int
	lastError      *string
	nextAttemptAt  time.Time
	createdAt      time.Time
	updatedAt      time.Time
}

func serialize(u *entities.ResumeUpload) (uploadRow, error) {
	artifacts, err := u.Artifacts().Marshal()
	if err != nil {
		return uploadRow{}, fmt.Errorf("marshal artifacts: %w", err)
	}
	var lastErr *string
	if u.LastError() != "" {
		e := u.LastError()
		lastErr = &e
	}
	row := uploadRow{
		id:             u.ID(),
		tenantID:       u.TenantID().String(),
		intentID:       u.IntentID(),
		batchID:        u.BatchID(),
		storageKey:     u.StorageKey(),
		originalName:   u.OriginalName(),
		mimeType:       u.MimeType().String(),
		sizeBytes:      u.SizeBytes(),
		contentHash:    u.ContentHash().String(),
		status:         string(u.Status()),
		stageArtifacts: artifacts,
		attemptCount:   u.AttemptCount(),
		lastError:      lastErr,
		nextAttemptAt:  u.NextAttemptAt(),
		createdAt:      u.CreatedAt(),
		updatedAt:      u.UpdatedAt(),
	}
	if u.CandidateID() != uuid.Nil {
		cid := u.CandidateID()
		row.candidateID = &cid
	}
	return row, nil
}

// hydrate reconstructs a ResumeUpload from a row. Used by repository reads.
// It bypasses the constructor (which emits events) by setting fields via a
// dedicated package-internal builder.
func hydrate(r uploadRow) (*entities.ResumeUpload, error) {
	mime, err := vo.ParseMimeType(r.mimeType)
	if err != nil {
		return nil, fmt.Errorf("mime: %w", err)
	}
	hash, err := vo.NewContentHash(r.contentHash)
	if err != nil {
		return nil, fmt.Errorf("hash: %w", err)
	}
	artifacts, err := vo.UnmarshalStageArtifacts(r.stageArtifacts)
	if err != nil {
		return nil, fmt.Errorf("artifacts: %w", err)
	}
	status, err := vo.ParseUploadStatus(r.status)
	if err != nil {
		return nil, fmt.Errorf("status: %w", err)
	}
	tenantID, err := shared.ParseTenantID(r.tenantID)
	if err != nil {
		return nil, fmt.Errorf("tenant_id: %w", err)
	}
	var lastErr string
	if r.lastError != nil {
		lastErr = *r.lastError
	}
	var candidateID uuid.UUID
	if r.candidateID != nil {
		candidateID = *r.candidateID
	}
	return entities.RehydrateResumeUpload(entities.RehydrateInput{
		ID:            r.id,
		TenantID:      tenantID,
		IntentID:      r.intentID,
		BatchID:       r.batchID,
		CandidateID:   candidateID,
		StorageKey:    r.storageKey,
		OriginalName:  r.originalName,
		MimeType:      mime,
		SizeBytes:     r.sizeBytes,
		ContentHash:   hash,
		Status:        status,
		Artifacts:     artifacts,
		AttemptCount:  r.attemptCount,
		LastError:     lastErr,
		NextAttemptAt: r.nextAttemptAt,
		CreatedAt:     r.createdAt,
		UpdatedAt:     r.updatedAt,
	}), nil
}
