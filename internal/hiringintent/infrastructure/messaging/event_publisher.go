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
