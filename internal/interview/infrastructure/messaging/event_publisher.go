// Package messaging holds the interview context's outbox publisher +
// dispatcher. Same shape as sourcing/infrastructure/messaging.
package messaging

import (
	"context"

	"github.com/hustle/hireflow/internal/interview/domain/events"
)

// EventPublisher publishes domain events to a downstream broker / in-process bus.
type EventPublisher interface {
	Publish(ctx context.Context, event events.Event) error
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
