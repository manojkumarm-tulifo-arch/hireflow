package messaging_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/interview/domain/events"
	"github.com/hustle/hireflow/internal/interview/infrastructure/messaging"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// recordingBus captures Publish calls from BusPublisher.
type recordingBus struct {
	mu    sync.Mutex
	calls []struct {
		name  string
		event any
	}
}

func (b *recordingBus) Publish(_ context.Context, name string, event any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, struct {
		name  string
		event any
	}{name, event})
	return nil
}

func TestBusPublisher_ForwardsCorrectEventName(t *testing.T) {
	bus := &recordingBus{}
	pub := messaging.NewBusPublisher(bus)

	ev := events.InterviewProcessCreated{
		ProcessID:     uuid.New(),
		TenantID:      shared.NewTenantID(),
		ApplicationID: uuid.New(),
		CandidateID:   uuid.New(),
		IntentID:      uuid.New(),
		OccurredAt:    time.Now().UTC(),
	}

	err := pub.Publish(context.Background(), ev)
	require.NoError(t, err)

	bus.mu.Lock()
	defer bus.mu.Unlock()
	assert.Len(t, bus.calls, 1)
	assert.Equal(t, "interview.InterviewProcessCreated", bus.calls[0].name)
	assert.Equal(t, ev, bus.calls[0].event)
}
