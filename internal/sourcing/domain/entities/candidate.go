package entities

import (
	"errors"
	"time"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/events"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// EncryptedPersonal holds the ciphertext form of PII fields produced by the
// PIIEncryptor port. The aggregate doesn't know how the encryption works —
// just that these are opaque strings to be stored as-is.
type EncryptedPersonal struct {
	FullName string
	Email    string
	Phone    string
}

// NewCandidateInput is the constructor input.
type NewCandidateInput struct {
	TenantID    shared.TenantID
	ContentHash vo.ContentHash
	Profile     vo.ParsedProfile
	Encrypted   EncryptedPersonal // ciphertext for personal.*; aggregate stores as-is
	Location    string            // cleartext, non-PII
	Headline    string            // cleartext, non-PII
	Source      string            // "manual_upload" for slice 2
	// Optional overrides for deterministic tests; nil → real values.
	Now func() time.Time
	ID  uuid.UUID
}

// Candidate is the tenant-scoped person aggregate. Unique on (tenant_id, content_hash).
type Candidate struct {
	id               uuid.UUID
	tenantID         shared.TenantID
	contentHash      vo.ContentHash
	encFullName      string
	encEmail         string
	encPhone         string
	location         string
	headline         string
	profile          vo.ParsedProfile
	profileEmbedding []float32 // nil until the match worker embeds it
	source           string
	createdAt        time.Time
	updatedAt        time.Time
	pendingEvents    []events.Event
}

// NewCandidate constructs a fresh candidate, validating the profile and
// emitting CandidateParsed.
func NewCandidate(in NewCandidateInput) (*Candidate, error) {
	if err := in.Profile.Validate(); err != nil {
		return nil, err
	}
	if in.ContentHash.String() == "" {
		return nil, errors.New("content_hash required")
	}
	if in.Source == "" {
		in.Source = "manual_upload"
	}
	now := time.Now().UTC
	if in.Now != nil {
		now = in.Now
	}
	id := in.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	t := now().UTC()

	c := &Candidate{
		id:          id,
		tenantID:    in.TenantID,
		contentHash: in.ContentHash,
		encFullName: in.Encrypted.FullName,
		encEmail:    in.Encrypted.Email,
		encPhone:    in.Encrypted.Phone,
		location:    in.Location,
		headline:    in.Headline,
		profile:     in.Profile,
		source:      in.Source,
		createdAt:   t,
		updatedAt:   t,
	}
	c.emit(events.CandidateParsed{
		CandidateID:   id,
		TenantID:      in.TenantID,
		ContentHash:   in.ContentHash.String(),
		SchemaVersion: in.Profile.SchemaVersion,
		OccurredAt:    t,
	})
	return c, nil
}

// Accessors.
func (c *Candidate) ID() uuid.UUID               { return c.id }
func (c *Candidate) TenantID() shared.TenantID   { return c.tenantID }
func (c *Candidate) ContentHash() vo.ContentHash { return c.contentHash }
func (c *Candidate) EncryptedFullName() string   { return c.encFullName }
func (c *Candidate) EncryptedEmail() string      { return c.encEmail }
func (c *Candidate) EncryptedPhone() string      { return c.encPhone }
func (c *Candidate) Location() string            { return c.location }
func (c *Candidate) Headline() string            { return c.headline }
func (c *Candidate) Profile() vo.ParsedProfile   { return c.profile }
func (c *Candidate) ProfileSchema() int          { return c.profile.SchemaVersion }
// ProfileEmbedding returns the cached 1024-dim embedding, or nil if not yet computed.
func (c *Candidate) ProfileEmbedding() []float32 { return c.profileEmbedding }
func (c *Candidate) Source() string              { return c.source }
func (c *Candidate) CreatedAt() time.Time        { return c.createdAt }
func (c *Candidate) UpdatedAt() time.Time        { return c.updatedAt }

// PullEvents drains pending events. Same pattern as ResumeUpload.
func (c *Candidate) PullEvents() []events.Event {
	out := c.pendingEvents
	c.pendingEvents = nil
	return out
}

func (c *Candidate) emit(ev events.Event) {
	c.pendingEvents = append(c.pendingEvents, ev)
}

// RehydrateCandidateInput is for repository reads — bypasses event emission.
type RehydrateCandidateInput struct {
	ID                uuid.UUID
	TenantID          shared.TenantID
	ContentHash       vo.ContentHash
	EncryptedFullName string
	EncryptedEmail    string
	EncryptedPhone    string
	Location          string
	Headline          string
	Profile           vo.ParsedProfile
	ProfileEmbedding  []float32 // nil when not yet computed
	Source            string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// RehydrateCandidate reconstructs an aggregate from a persisted row.
func RehydrateCandidate(in RehydrateCandidateInput) *Candidate {
	return &Candidate{
		id:               in.ID,
		tenantID:         in.TenantID,
		contentHash:      in.ContentHash,
		encFullName:      in.EncryptedFullName,
		encEmail:         in.EncryptedEmail,
		encPhone:         in.EncryptedPhone,
		location:         in.Location,
		headline:         in.Headline,
		profile:          in.Profile,
		profileEmbedding: in.ProfileEmbedding,
		source:           in.Source,
		createdAt:        in.CreatedAt,
		updatedAt:        in.UpdatedAt,
	}
}
