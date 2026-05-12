// Package messaging holds the sourcing context's publisher + outbox dispatcher.
// Same shape as hiringintent/infrastructure/messaging.
package messaging

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/hustle/hireflow/internal/sourcing/domain/events"
)

// EventPublisher publishes domain events to a downstream broker / in-process bus.
type EventPublisher interface {
	Publish(ctx context.Context, event events.Event) error
}

// LogPublisher logs events at info level. Useful for dev / before wiring the bus.
type LogPublisher struct{ logger zerolog.Logger }

// NewLogPublisher wires the log-only publisher.
func NewLogPublisher(logger zerolog.Logger) *LogPublisher { return &LogPublisher{logger: logger} }

func (p *LogPublisher) Publish(_ context.Context, ev events.Event) error {
	p.logger.Info().
		Str("event", ev.EventName()).
		Str("upload_id", ev.AggregateID().String()).
		Time("occurred_at", ev.At()).
		Msg("sourcing event published")
	return nil
}

// Bus is the minimum surface BusPublisher needs from a process-local event bus.
type Bus interface {
	Publish(ctx context.Context, eventName string, event any) error
}

// BusPublisher forwards into a process-local Bus.
type BusPublisher struct{ bus Bus }

// NewBusPublisher wires the publisher.
func NewBusPublisher(bus Bus) *BusPublisher { return &BusPublisher{bus: bus} }

func (p *BusPublisher) Publish(ctx context.Context, ev events.Event) error {
	return p.bus.Publish(ctx, ev.EventName(), ev)
}
