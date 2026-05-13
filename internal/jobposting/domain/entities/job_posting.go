// Package entities holds the aggregate roots and entities of the
// jobposting bounded context.
package entities

import (
	"errors"
	"time"

	"github.com/hustle/hireflow/internal/jobposting/domain/events"
	"github.com/hustle/hireflow/internal/jobposting/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// Domain errors enforced at the aggregate boundary.
var (
	// ErrCannotPublishTerminal is returned when Publish is called on Closed/Archived.
	ErrCannotPublishTerminal = errors.New("cannot publish a posting in a terminal state")
	// ErrPublishNeedsChannels is returned when Publish is called with zero channels.
	ErrPublishNeedsChannels = errors.New("publish requires at least one channel")
	// ErrCannotCloseTerminal is returned when Close is called on already-terminal posting.
	ErrCannotCloseTerminal = errors.New("cannot close a posting in a terminal state")
	// ErrCannotAmendTerminal is returned when JD is amended on terminal posting.
	ErrCannotAmendTerminal = errors.New("cannot amend a posting in a terminal state")
)

// JobPosting is the aggregate root of the jobposting bounded context.
// It carries a back-reference to the originating intent (a string, not a
// typed value object — different bounded contexts must not share VOs).
type JobPosting struct {
	id            valueobjects.PostingID
	tenantID      shared.TenantID
	intentID      string // opaque reference to hiringintent.IntentID
	jd            valueobjects.JDContent
	sources       []valueobjects.SourceTarget
	status        valueobjects.PostingStatus
	createdAt     time.Time
	updatedAt     time.Time
	publishedAt   *time.Time
	closedAt      *time.Time
	closeReason   string

	pendingEvents []events.Event
}

// NewJobPosting constructs a fresh draft posting from a confirmed intent.
// The first JD version is 1; sources are empty until Publish is called.
func NewJobPosting(tenantID shared.TenantID, intentID string, jd valueobjects.JDContent) (*JobPosting, error) {
	if tenantID.IsZero() {
		return nil, errors.New("tenantID is required")
	}
	if intentID == "" {
		return nil, errors.New("intentID is required")
	}
	now := time.Now().UTC()
	id := valueobjects.NewPostingID()
	posting := &JobPosting{
		id:        id,
		tenantID:  tenantID,
		intentID:  intentID,
		jd:        jd,
		status:    valueobjects.StatusDraft,
		createdAt: now,
		updatedAt: now,
	}
	posting.raise(events.NewJobPostingCreated(id, tenantID, intentID, jd.Title(), now))
	return posting, nil
}

// HydrateJobPosting reconstitutes an aggregate from persistence. Used only
// by repository implementations — does not raise events.
func HydrateJobPosting(
	id valueobjects.PostingID,
	tenantID shared.TenantID,
	intentID string,
	jd valueobjects.JDContent,
	sources []valueobjects.SourceTarget,
	status valueobjects.PostingStatus,
	createdAt, updatedAt time.Time,
	publishedAt, closedAt *time.Time,
	closeReason string,
) *JobPosting {
	return &JobPosting{
		id:          id,
		tenantID:    tenantID,
		intentID:    intentID,
		jd:          jd,
		sources:     append([]valueobjects.SourceTarget(nil), sources...),
		status:      status,
		createdAt:   createdAt,
		updatedAt:   updatedAt,
		publishedAt: publishedAt,
		closedAt:    closedAt,
		closeReason: closeReason,
	}
}

// Getters.
func (p *JobPosting) ID() valueobjects.PostingID            { return p.id }
func (p *JobPosting) TenantID() shared.TenantID             { return p.tenantID }
func (p *JobPosting) IntentID() string                      { return p.intentID }
func (p *JobPosting) JD() valueobjects.JDContent            { return p.jd }
func (p *JobPosting) Sources() []valueobjects.SourceTarget  { return append([]valueobjects.SourceTarget(nil), p.sources...) }
func (p *JobPosting) Status() valueobjects.PostingStatus    { return p.status }
func (p *JobPosting) CreatedAt() time.Time                  { return p.createdAt }
func (p *JobPosting) UpdatedAt() time.Time                  { return p.updatedAt }
func (p *JobPosting) PublishedAt() *time.Time               { return p.publishedAt }
func (p *JobPosting) ClosedAt() *time.Time                  { return p.closedAt }
func (p *JobPosting) CloseReason() string                   { return p.closeReason }

// Publish distributes the posting to the given channels and marks it Published.
// Idempotent for re-publishes — adds new channels, leaves existing ones alone.
func (p *JobPosting) Publish(channels []valueobjects.SourceChannel) error {
	if p.status.IsTerminal() {
		return ErrCannotPublishTerminal
	}
	if len(channels) == 0 {
		return ErrPublishNeedsChannels
	}

	existing := make(map[valueobjects.SourceChannel]struct{}, len(p.sources))
	for _, s := range p.sources {
		existing[s.Channel] = struct{}{}
	}
	for _, c := range channels {
		if _, dup := existing[c]; dup {
			continue
		}
		p.sources = append(p.sources, valueobjects.SourceTarget{
			Channel: c,
			Status:  valueobjects.SourceStatusPending,
		})
	}

	now := time.Now().UTC()
	p.status = valueobjects.StatusPublished
	p.publishedAt = &now
	p.updatedAt = now
	p.raise(events.NewJobPostingPublished(p.id, p.tenantID, channels, p.jd.Version(), now))
	return nil
}

// AmendJD replaces the JD with a new version. Allowed in Draft or Published.
// Caller must pass an already-validated JDContent (typically from
// old.WithBumpedVersion()). Emits JobPostingAmended so sourcing can
// republish and analytics can observe the version bump without
// rehydrating the aggregate.
func (p *JobPosting) AmendJD(jd valueobjects.JDContent) error {
	if p.status.IsTerminal() {
		return ErrCannotAmendTerminal
	}
	p.jd = jd
	p.updatedAt = time.Now().UTC()
	p.raise(events.NewJobPostingAmended(p.id, p.tenantID, jd.Version(), p.updatedAt))
	return nil
}

// Close marks the posting as terminal Closed.
func (p *JobPosting) Close(reason string) error {
	if p.status.IsTerminal() {
		return ErrCannotCloseTerminal
	}
	now := time.Now().UTC()
	p.status = valueobjects.StatusClosed
	p.closedAt = &now
	p.closeReason = reason
	p.updatedAt = now
	p.raise(events.NewJobPostingClosed(p.id, p.tenantID, reason, now))
	return nil
}

// PullEvents returns and clears the pending event buffer.
func (p *JobPosting) PullEvents() []events.Event {
	out := p.pendingEvents
	p.pendingEvents = nil
	return out
}

func (p *JobPosting) raise(e events.Event) {
	p.pendingEvents = append(p.pendingEvents, e)
}
