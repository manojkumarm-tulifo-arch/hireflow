package persistence

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hustle/hireflow/internal/jobposting/domain/entities"
	"github.com/hustle/hireflow/internal/jobposting/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// postingRow mirrors the columns of the job_postings table.
type postingRow struct {
	id          string
	tenantID    string
	intentID    string
	jd          []byte
	sources     []byte
	status      string
	createdAt   time.Time
	updatedAt   time.Time
	publishedAt *time.Time
	closedAt    *time.Time
	closeReason string
}

type jdPayload struct {
	Title            string   `json:"title"`
	Summary          string   `json:"summary"`
	Responsibilities []string `json:"responsibilities"`
	Requirements     []string `json:"requirements"`
	Version          int      `json:"version"`
}

type sourcePayload struct {
	Channel    valueobjects.SourceChannel `json:"channel"`
	Status     valueobjects.SourceStatus  `json:"status"`
	ExternalID string                     `json:"external_id,omitempty"`
	URL        string                     `json:"url,omitempty"`
	LastSync   *time.Time                 `json:"last_sync,omitempty"`
}

func serialize(p *entities.JobPosting) (postingRow, error) {
	jd := p.JD()
	jdBytes, err := json.Marshal(jdPayload{
		Title:            jd.Title(),
		Summary:          jd.Summary(),
		Responsibilities: jd.Responsibilities(),
		Requirements:     jd.Requirements(),
		Version:          jd.Version(),
	})
	if err != nil {
		return postingRow{}, fmt.Errorf("marshal jd: %w", err)
	}

	sources := p.Sources()
	srcs := make([]sourcePayload, len(sources))
	for i, s := range sources {
		srcs[i] = sourcePayload{
			Channel:    s.Channel,
			Status:     s.Status,
			ExternalID: s.ExternalID,
			URL:        s.URL,
			LastSync:   s.LastSync,
		}
	}
	srcBytes, err := json.Marshal(srcs)
	if err != nil {
		return postingRow{}, fmt.Errorf("marshal sources: %w", err)
	}

	return postingRow{
		id:          p.ID().String(),
		tenantID:    p.TenantID().String(),
		intentID:    p.IntentID(),
		jd:          jdBytes,
		sources:     srcBytes,
		status:      string(p.Status()),
		createdAt:   p.CreatedAt(),
		updatedAt:   p.UpdatedAt(),
		publishedAt: p.PublishedAt(),
		closedAt:    p.ClosedAt(),
		closeReason: p.CloseReason(),
	}, nil
}

func deserialize(row postingRow) (*entities.JobPosting, error) {
	id, err := valueobjects.ParsePostingID(row.id)
	if err != nil {
		return nil, err
	}
	tenantID, err := shared.ParseTenantID(row.tenantID)
	if err != nil {
		return nil, err
	}
	status, err := valueobjects.ParsePostingStatus(row.status)
	if err != nil {
		return nil, err
	}

	var jp jdPayload
	if err := json.Unmarshal(row.jd, &jp); err != nil {
		return nil, fmt.Errorf("unmarshal jd: %w", err)
	}
	jd, err := valueobjects.NewJDContent(jp.Title, jp.Summary, jp.Responsibilities, jp.Requirements, jp.Version)
	if err != nil {
		return nil, err
	}

	var srcs []sourcePayload
	if len(row.sources) > 0 {
		if err := json.Unmarshal(row.sources, &srcs); err != nil {
			return nil, fmt.Errorf("unmarshal sources: %w", err)
		}
	}
	targets := make([]valueobjects.SourceTarget, len(srcs))
	for i, s := range srcs {
		targets[i] = valueobjects.SourceTarget{
			Channel:    s.Channel,
			Status:     s.Status,
			ExternalID: s.ExternalID,
			URL:        s.URL,
			LastSync:   s.LastSync,
		}
	}

	return entities.HydrateJobPosting(
		id, tenantID, row.intentID,
		jd, targets, status,
		row.createdAt, row.updatedAt, row.publishedAt, row.closedAt, row.closeReason,
	), nil
}
