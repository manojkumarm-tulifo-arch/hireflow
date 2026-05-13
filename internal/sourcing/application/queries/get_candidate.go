package queries

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/dto"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
)

// GetCandidateHandler returns the full candidate detail with PII decrypted.
type GetCandidateHandler struct {
	repo      repositories.CandidateRepository
	encryptor services.PIIEncryptor
}

// NewGetCandidateHandler wires the handler.
func NewGetCandidateHandler(repo repositories.CandidateRepository, encryptor services.PIIEncryptor) *GetCandidateHandler {
	return &GetCandidateHandler{repo: repo, encryptor: encryptor}
}

// Handle returns the candidate detail. Returns repositories.ErrCandidateNotFound
// when no matching row exists. Decryption errors propagate.
func (h *GetCandidateHandler) Handle(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (dto.CandidateDetailDTO, error) {
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

	return dto.CandidateDetailDTO{
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
	}, nil
}
