// Package subscribers wires the interview context to events published on the
// in-process bus by other contexts.
package subscribers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/hustle/hireflow/internal/interview/application/commands"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// shortlistedPayload mirrors the JSON shape of sourcing.ApplicationShortlisted.
// We don't import the sourcing event struct (cross-context isolation); we
// re-marshal the bus payload into a local shape with the fields we need.
type shortlistedPayload struct {
	ApplicationID uuid.UUID `json:"application_id"`
	CandidateID   uuid.UUID `json:"candidate_id"`
	IntentID      uuid.UUID `json:"intent_id"`
	TenantID      string    `json:"tenant_id"`
}

// ApplicationShortlistedConsumer translates sourcing.ApplicationShortlisted
// events into StartInterviewProcess commands.
type ApplicationShortlistedConsumer struct {
	start  *commands.StartInterviewProcessHandler
	logger zerolog.Logger
}

// NewApplicationShortlistedConsumer wires the consumer.
func NewApplicationShortlistedConsumer(
	start *commands.StartInterviewProcessHandler,
	logger zerolog.Logger,
) *ApplicationShortlistedConsumer {
	return &ApplicationShortlistedConsumer{
		start:  start,
		logger: logger.With().Str("component", "application_shortlisted_consumer").Logger(),
	}
}

// Handle is the eventbus.Handler entry point. The bus publishes the event
// struct directly; we re-marshal then unmarshal into our local payload to
// avoid cross-context Go imports.
func (c *ApplicationShortlistedConsumer) Handle(ctx context.Context, event any) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("application_shortlisted_consumer: marshal event: %w", err)
	}
	var p shortlistedPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("application_shortlisted_consumer: unmarshal payload: %w", err)
	}
	if p.ApplicationID == uuid.Nil || p.CandidateID == uuid.Nil || p.IntentID == uuid.Nil {
		return errors.New("application_shortlisted_consumer: incomplete payload: nil UUID field")
	}
	tenantID, err := shared.ParseTenantID(p.TenantID)
	if err != nil {
		return fmt.Errorf("application_shortlisted_consumer: tenant: %w", err)
	}

	c.logger.Debug().
		Str("application_id", p.ApplicationID.String()).
		Str("tenant_id", p.TenantID).
		Msg("handling sourcing.ApplicationShortlisted")

	return c.start.Handle(ctx, commands.StartInterviewProcessInput{
		TenantID:      tenantID,
		ApplicationID: p.ApplicationID,
		CandidateID:   p.CandidateID,
		IntentID:      p.IntentID,
	})
}
