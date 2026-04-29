// Package messaging holds infrastructure-side implementations of the
// hiringintent context's messaging concerns (event publishing).
package messaging

import (
	"context"

	"github.com/hustle/hireflow/internal/hiringintent/domain/events"
	"github.com/rs/zerolog"
)

// EventPublisher publishes domain events to a downstream broker.
// The repository's outbox writes events; a separate dispatcher reads
// the outbox and calls Publish — keeping aggregate write atomic.
type EventPublisher interface {
	Publish(ctx context.Context, event events.Event) error
}

// LogPublisher is a stand-in publisher that just logs events.
// Replace with a Kafka/NATS/Redis Streams publisher when broker is chosen.
type LogPublisher struct {
	logger zerolog.Logger
}

// NewLogPublisher wires the log-only publisher.
func NewLogPublisher(logger zerolog.Logger) *LogPublisher {
	return &LogPublisher{logger: logger}
}

// Publish logs the event at info level.
func (p *LogPublisher) Publish(_ context.Context, event events.Event) error {
	p.logger.Info().
		Str("event", event.EventName()).
		Str("intent_id", event.AggregateID().String()).
		Str("tenant_id", event.Tenant().String()).
		Time("occurred_at", event.At()).
		Msg("domain event published")
	return nil
}

// Bus is the minimum surface BusPublisher needs from a process-local event
// bus. Defined here (rather than imported) so this package doesn't depend
// on the concrete bus implementation — the wiring layer connects them.
type Bus interface {
	Publish(ctx context.Context, eventName string, event any) error
}

// BusPublisher forwards intent events into a process-local Bus, where
// downstream contexts (jobposting, sourcing) subscribe by event name.
// The dispatcher's outbox guarantees at-least-once delivery; the bus
// handles in-process fan-out.
type BusPublisher struct {
	bus Bus
}

// NewBusPublisher wires the publisher.
func NewBusPublisher(bus Bus) *BusPublisher {
	return &BusPublisher{bus: bus}
}

// Publish hands the event to the bus keyed by EventName(). Errors from
// any subscriber propagate so the dispatcher will retry.
func (p *BusPublisher) Publish(ctx context.Context, event events.Event) error {
	return p.bus.Publish(ctx, event.EventName(), event)
}
