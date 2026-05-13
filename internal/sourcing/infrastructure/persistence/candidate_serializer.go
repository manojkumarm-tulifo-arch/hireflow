package persistence

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// candidateRow mirrors candidates table columns.
type candidateRow struct {
	id            uuid.UUID
	tenantID      string
	contentHash   string
	fullNameEnc   *string
	emailEnc      *string
	phoneEnc      *string
	location      *string
	headline      *string
	parsedProfile []byte
	profileSchema int
	source        string
	createdAt     time.Time
	updatedAt     time.Time
}

func serializeCandidate(c *entities.Candidate) (candidateRow, error) {
	profileBytes, err := json.Marshal(c.Profile())
	if err != nil {
		return candidateRow{}, fmt.Errorf("marshal profile: %w", err)
	}
	row := candidateRow{
		id:            c.ID(),
		tenantID:      c.TenantID().String(),
		contentHash:   c.ContentHash().String(),
		parsedProfile: profileBytes,
		profileSchema: c.ProfileSchema(),
		source:        c.Source(),
		createdAt:     c.CreatedAt(),
		updatedAt:     c.UpdatedAt(),
	}
	if c.EncryptedFullName() != "" {
		v := c.EncryptedFullName()
		row.fullNameEnc = &v
	}
	if c.EncryptedEmail() != "" {
		v := c.EncryptedEmail()
		row.emailEnc = &v
	}
	if c.EncryptedPhone() != "" {
		v := c.EncryptedPhone()
		row.phoneEnc = &v
	}
	if c.Location() != "" {
		v := c.Location()
		row.location = &v
	}
	if c.Headline() != "" {
		v := c.Headline()
		row.headline = &v
	}
	return row, nil
}

func hydrateCandidate(r candidateRow) (*entities.Candidate, error) {
	var profile vo.ParsedProfile
	if err := json.Unmarshal(r.parsedProfile, &profile); err != nil {
		return nil, fmt.Errorf("unmarshal profile: %w", err)
	}
	hash, err := vo.NewContentHash(r.contentHash)
	if err != nil {
		return nil, fmt.Errorf("hash: %w", err)
	}
	tenant, err := shared.ParseTenantID(r.tenantID)
	if err != nil {
		return nil, fmt.Errorf("tenant: %w", err)
	}
	var fullName, email, phone, loc, headline string
	if r.fullNameEnc != nil {
		fullName = *r.fullNameEnc
	}
	if r.emailEnc != nil {
		email = *r.emailEnc
	}
	if r.phoneEnc != nil {
		phone = *r.phoneEnc
	}
	if r.location != nil {
		loc = *r.location
	}
	if r.headline != nil {
		headline = *r.headline
	}

	return entities.RehydrateCandidate(entities.RehydrateCandidateInput{
		ID:                r.id,
		TenantID:          tenant,
		ContentHash:       hash,
		EncryptedFullName: fullName,
		EncryptedEmail:    email,
		EncryptedPhone:    phone,
		Location:          loc,
		Headline:          headline,
		Profile:           profile,
		Source:            r.source,
		CreatedAt:         r.createdAt,
		UpdatedAt:         r.updatedAt,
	}), nil
}
