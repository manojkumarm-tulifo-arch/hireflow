package queries

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	auditdomain "github.com/hustle/hireflow/internal/shared/audit/domain"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/dto"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
)

// GetCandidateHandler returns the full candidate detail with PII decrypted.
type GetCandidateHandler struct {
	repo      repositories.CandidateRepository
	encryptor services.PIIEncryptor
	audit     auditdomain.AuditWriter
}

// NewGetCandidateHandler wires the handler.
func NewGetCandidateHandler(repo repositories.CandidateRepository, encryptor services.PIIEncryptor, audit auditdomain.AuditWriter) *GetCandidateHandler {
	return &GetCandidateHandler{repo: repo, encryptor: encryptor, audit: audit}
}

// Handle returns the candidate detail. Returns repositories.ErrCandidateNotFound
// when no matching row exists. Decryption errors propagate.
// actorUserID is the recruiter performing the read and is used for audit logging.
// The audit write is load-bearing: if it fails the PII is NOT returned to the caller.
func (h *GetCandidateHandler) Handle(ctx context.Context, tenant shared.TenantID, actorUserID uuid.UUID, id uuid.UUID) (dto.CandidateDetailDTO, error) {
	c, err := h.repo.FindByID(ctx, tenant, id)
	if err != nil {
		return dto.CandidateDetailDTO{}, err
	}

	name, err := h.encryptor.Decrypt(ctx, tenant, c.EncryptedFullName())
	if err != nil {
		return dto.CandidateDetailDTO{}, fmt.Errorf("decrypt name: %w", err)
	}
	email, err := h.encryptor.Decrypt(ctx, tenant, c.EncryptedEmail())
	if err != nil {
		return dto.CandidateDetailDTO{}, fmt.Errorf("decrypt email: %w", err)
	}
	phone, err := h.encryptor.Decrypt(ctx, tenant, c.EncryptedPhone())
	if err != nil {
		return dto.CandidateDetailDTO{}, fmt.Errorf("decrypt phone: %w", err)
	}

	profile := c.Profile()
	// Overlay decrypted PII back into the profile.personal block for the response.
	profile.Personal.FullName = name
	profile.Personal.Email = email
	profile.Personal.Phone = phone

	profileBytes, err := json.Marshal(profile)
	if err != nil {
		return dto.CandidateDetailDTO{}, fmt.Errorf("marshal profile: %w", err)
	}

	out := dto.CandidateDetailDTO{
		ID:          c.ID(),
		ContentHash: c.ContentHash().String(),
		Personal: dto.CandidatePersonal{
			FullName: name, Email: email, Phone: phone,
		},
		Location:  c.Location(),
		Headline:  c.Headline(),
		Profile:   profileBytes,
		Source:    c.Source(),
		CreatedAt: c.CreatedAt(),
	}

	// Audit AFTER the read returns successfully. Failed reads (404, decrypt errors)
	// are not audited. Audit failure is load-bearing: don't return PII to a caller
	// we couldn't audit.
	if err := h.audit.Write(ctx, auditdomain.AuditEvent{
		ActorUserID:  actorUserID,
		TenantID:     tenant,
		Action:       "candidate_read",
		ResourceKind: "candidate",
		ResourceID:   id,
		OccurredAt:   time.Now().UTC(),
	}); err != nil {
		return dto.CandidateDetailDTO{}, fmt.Errorf("audit candidate read: %w", err)
	}

	return out, nil
}
