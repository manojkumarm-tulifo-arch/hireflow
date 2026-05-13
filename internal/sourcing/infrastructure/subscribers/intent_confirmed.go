// Package subscribers wires cross-context event consumers for the sourcing
// context. Each subscriber maps an upstream domain event to a use case here.
package subscribers

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	intentevents "github.com/hustle/hireflow/internal/hiringintent/domain/events"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
)

// IntentConfirmedConsumer reacts to hiringintent.IntentConfirmed by fanning out
// Application rows for all parsed candidates in the tenant (ScoreIntentHandler).
// The bus delivers the event as `any`; we type-assert to the concrete event type.
type IntentConfirmedConsumer struct {
	cmd    *commands.ScoreIntentHandler
	logger zerolog.Logger
}

// NewIntentConfirmedConsumer wires the consumer.
func NewIntentConfirmedConsumer(cmd *commands.ScoreIntentHandler, logger zerolog.Logger) *IntentConfirmedConsumer {
	return &IntentConfirmedConsumer{cmd: cmd, logger: logger}
}

// Handle is the bus callback. The bus delivers the event as `any`; we
// type-assert to intentevents.IntentConfirmed before delegating to the command.
func (c *IntentConfirmedConsumer) Handle(ctx context.Context, event any) error {
	ev, ok := event.(intentevents.IntentConfirmed)
	if !ok {
		return fmt.Errorf("intent_confirmed_consumer: unexpected event type %T", event)
	}

	intentID, err := uuid.Parse(ev.AggregateID().String())
	if err != nil {
		return fmt.Errorf("intent_confirmed_consumer: parse intent id: %w", err)
	}

	c.logger.Debug().
		Str("intent_id", intentID.String()).
		Str("tenant_id", ev.Tenant().String()).
		Msg("intent_confirmed_consumer: handling event")

	return c.cmd.Handle(ctx, commands.ScoreIntentInput{
		TenantID: ev.Tenant(),
		IntentID: intentID,
	})
}
