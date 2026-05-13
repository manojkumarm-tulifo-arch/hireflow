package subscribers

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	sourcingevents "github.com/hustle/hireflow/internal/sourcing/domain/events"
)

// CandidateParsedConsumer reacts to sourcing.CandidateParsed by fanning out
// Application rows for all confirmed intents in the tenant (ScoreCandidateHandler).
// The bus delivers the event as `any`; we type-assert to the concrete event type.
type CandidateParsedConsumer struct {
	cmd    *commands.ScoreCandidateHandler
	logger zerolog.Logger
}

// NewCandidateParsedConsumer wires the consumer.
func NewCandidateParsedConsumer(cmd *commands.ScoreCandidateHandler, logger zerolog.Logger) *CandidateParsedConsumer {
	return &CandidateParsedConsumer{cmd: cmd, logger: logger}
}

// Handle is the bus callback. The bus delivers the event as `any`; we
// type-assert to sourcingevents.CandidateParsed before delegating to the command.
func (c *CandidateParsedConsumer) Handle(ctx context.Context, event any) error {
	ev, ok := event.(sourcingevents.CandidateParsed)
	if !ok {
		return fmt.Errorf("candidate_parsed_consumer: unexpected event type %T", event)
	}

	c.logger.Debug().
		Str("candidate_id", ev.AggregateID().String()).
		Str("tenant_id", ev.Tenant().String()).
		Msg("candidate_parsed_consumer: handling event")

	return c.cmd.Handle(ctx, commands.ScoreCandidateInput{
		TenantID:    ev.Tenant(),
		CandidateID: ev.AggregateID(),
	})
}
