// Package eventbus provides a process-local pub/sub bus used to deliver
// domain events between bounded contexts after they leave the outbox.
//
// This is *not* a domain primitive: contexts publish their own typed events
// through their own EventPublisher port; an adapter forwards each event into
// this bus, keyed by EventName(). Subscribers register handlers per event
// name and receive the event as `any`, type-asserting to the concrete type.
//
// Delivery is synchronous: Publish returns the first handler error so the
// caller (typically an outbox dispatcher) can leave the event undispatched
// and retry. This is the deliberate trade-off vs. fire-and-forget — losing
// a cross-context event silently is far worse than retrying one.
//
// Subscriptions are static after startup: handlers register once during
// wiring; we don't support unsubscribe to keep the lock surface small.
package eventbus

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog"
)

// Handler reacts to an event delivered through the bus. The handler must
// type-assert `event` to the concrete event type matching the name it
// registered under. Returning a non-nil error aborts the Publish call.
type Handler func(ctx context.Context, event any) error

// InMemory is a process-local event bus. The zero value is not usable;
// construct via NewInMemory.
type InMemory struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
	logger   zerolog.Logger
}

// NewInMemory builds an empty bus. The logger receives one debug line per
// publish for observability; failures bubble up via the returned error.
func NewInMemory(logger zerolog.Logger) *InMemory {
	return &InMemory{
		handlers: make(map[string][]Handler),
		logger:   logger.With().Str("component", "eventbus").Logger(),
	}
}

// Subscribe registers handler against the named event. Multiple handlers
// for the same name are delivered in registration order. Safe to call
// before Publish; mid-flight registration is allowed but rare.
func (b *InMemory) Subscribe(eventName string, handler Handler) {
	if eventName == "" {
		panic("eventbus: empty event name")
	}
	if handler == nil {
		panic("eventbus: nil handler")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventName] = append(b.handlers[eventName], handler)
}

// Publish delivers event to every subscriber of eventName synchronously.
// Returns the first handler error wrapped with the event name so callers
// can decide to retry. Events with no subscribers are a no-op (logged at
// debug level, not an error — useful events may simply have no listeners
// today).
func (b *InMemory) Publish(ctx context.Context, eventName string, event any) error {
	b.mu.RLock()
	hs := make([]Handler, len(b.handlers[eventName]))
	copy(hs, b.handlers[eventName])
	b.mu.RUnlock()

	if len(hs) == 0 {
		b.logger.Debug().Str("event", eventName).Msg("no subscribers")
		return nil
	}

	for i, h := range hs {
		if err := h(ctx, event); err != nil {
			b.logger.Warn().
				Err(err).
				Str("event", eventName).
				Int("handler_index", i).
				Msg("handler failed; aborting publish")
			return fmt.Errorf("eventbus: %s handler %d: %w", eventName, i, err)
		}
	}
	b.logger.Debug().
		Str("event", eventName).
		Int("handlers", len(hs)).
		Msg("delivered")
	return nil
}
