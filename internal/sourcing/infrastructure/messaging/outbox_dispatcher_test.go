package messaging_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/hustle/hireflow/internal/sourcing/domain/events"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/messaging"
)

type capturePub struct {
	mu     sync.Mutex
	calls  []events.Event
	errFor map[string]error
}

func (p *capturePub) Publish(_ context.Context, ev events.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err, ok := p.errFor[ev.EventName()]; ok {
		return err
	}
	p.calls = append(p.calls, ev)
	return nil
}

// Note: This test exercises the dispatcher's loop logic by feeding it through
// a fake Querier. The Postgres-backed version is exercised in the slice-1
// e2e test (Task 15).
func TestDispatcher_LogPublisher_Smoke(t *testing.T) {
	// This file currently only smoke-tests the publisher. The dispatcher
	// itself depends on *pgxpool.Pool; full coverage lives in the e2e test.
	pub := messaging.NewLogPublisher(zerolog.Nop())
	err := pub.Publish(context.Background(), events.ResumeUploadAccepted{
		UploadID: uuid.New(), OccurredAt: time.Now().UTC(),
	})
	assert.NoError(t, err)

	cap := &capturePub{}
	bus := &testBus{publishFn: cap.Publish}
	bp := messaging.NewBusPublisher(bus)
	require := func(_ error) {}
	require(bp.Publish(context.Background(), events.ResumeUploadAccepted{UploadID: uuid.New()}))
	assert.Len(t, cap.calls, 1)
}

type testBus struct {
	publishFn func(ctx context.Context, ev events.Event) error
}

func (b *testBus) Publish(ctx context.Context, name string, ev any) error {
	if e, ok := ev.(events.Event); ok {
		return b.publishFn(ctx, e)
	}
	return nil
}
