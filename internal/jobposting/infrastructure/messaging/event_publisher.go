// Package messaging holds infrastructure-side messaging implementations
// for the jobposting context.
package messaging

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/hustle/hireflow/internal/jobposting/domain/events"
)

// EventPublisher publishes jobposting domain events to a downstream broker.
type EventPublisher interface {
	Publish(ctx context.Context, event events.Event) error
}

// LogPublisher is a stand-in publisher that logs events.
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
		Str("posting_id", event.AggregateID().String()).
		Str("tenant_id", event.Tenant().String()).
		Time("occurred_at", event.At()).
		Msg("domain event published")
	return nil
}
