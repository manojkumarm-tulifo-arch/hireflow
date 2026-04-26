// Package dto holds data transfer objects for the jobposting application layer.
package dto

import (
	"time"

	"github.com/hustle/hireflow/internal/jobposting/domain/entities"
)

// JDDTO mirrors valueobjects.JDContent for API responses.
type JDDTO struct {
	Title            string   `json:"title"`
	Summary          string   `json:"summary"`
	Responsibilities []string `json:"responsibilities"`
	Requirements     []string `json:"requirements"`
	Version          int      `json:"version"`
}

// SourceTargetDTO mirrors valueobjects.SourceTarget.
type SourceTargetDTO struct {
	Channel    string     `json:"channel"`
	Status     string     `json:"status"`
	ExternalID string     `json:"external_id,omitempty"`
	URL        string     `json:"url,omitempty"`
	LastSync   *time.Time `json:"last_sync,omitempty"`
}

// PostingDTO is the response shape for a single job posting.
type PostingDTO struct {
	ID          string            `json:"id"`
	TenantID    string            `json:"tenant_id"`
	IntentID    string            `json:"intent_id"`
	JD          JDDTO             `json:"jd"`
	Sources     []SourceTargetDTO `json:"sources"`
	Status      string            `json:"status"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	PublishedAt *time.Time        `json:"published_at,omitempty"`
	ClosedAt    *time.Time        `json:"closed_at,omitempty"`
	CloseReason string            `json:"close_reason,omitempty"`
}

// FromEntity maps a domain aggregate to its API DTO.
func FromEntity(p *entities.JobPosting) PostingDTO {
	jd := p.JD()
	sources := p.Sources()
	srcDTOs := make([]SourceTargetDTO, len(sources))
	for i, s := range sources {
		srcDTOs[i] = SourceTargetDTO{
			Channel:    string(s.Channel),
			Status:     string(s.Status),
			ExternalID: s.ExternalID,
			URL:        s.URL,
			LastSync:   s.LastSync,
		}
	}
	return PostingDTO{
		ID:       p.ID().String(),
		TenantID: p.TenantID().String(),
		IntentID: p.IntentID(),
		JD: JDDTO{
			Title:            jd.Title(),
			Summary:          jd.Summary(),
			Responsibilities: jd.Responsibilities(),
			Requirements:     jd.Requirements(),
			Version:          jd.Version(),
		},
		Sources:     srcDTOs,
		Status:      string(p.Status()),
		CreatedAt:   p.CreatedAt(),
		UpdatedAt:   p.UpdatedAt(),
		PublishedAt: p.PublishedAt(),
		ClosedAt:    p.ClosedAt(),
		CloseReason: p.CloseReason(),
	}
}

// CreateFromIntentInput is what the cross-context subscriber passes when a
// hiringintent IntentConfirmed event arrives.
//
// Suggested mapping policy: Title = role title, Summary = "Hiring N
// <Title>(s)", Responsibilities = first 3 required skills, Requirements =
// experience range. Real wording can come from a JD generator service later.
type CreateFromIntentInput struct {
	TenantID         string
	IntentID         string
	Title            string
	Summary          string
	Responsibilities []string
	Requirements     []string
}
