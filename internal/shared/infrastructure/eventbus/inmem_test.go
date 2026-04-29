package eventbus_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/shared/infrastructure/eventbus"
)

func newBus() *eventbus.InMemory {
	return eventbus.NewInMemory(zerolog.Nop())
}

func TestPublish_NoSubscribers_IsNoOp(t *testing.T) {
	bus := newBus()
	err := bus.Publish(context.Background(), "missing.event", struct{}{})
	require.NoError(t, err)
}

func TestPublish_DeliversToSubscriber(t *testing.T) {
	bus := newBus()
	var received string
	bus.Subscribe("foo.Bar", func(_ context.Context, event any) error {
		received = event.(string)
		return nil
	})

	require.NoError(t, bus.Publish(context.Background(), "foo.Bar", "hello"))
	assert.Equal(t, "hello", received)
}

func TestPublish_DeliversToMultipleSubscribersInOrder(t *testing.T) {
	bus := newBus()
	var calls []int
	for i := 0; i < 3; i++ {
		idx := i
		bus.Subscribe("foo.Bar", func(_ context.Context, _ any) error {
			calls = append(calls, idx)
			return nil
		})
	}

	require.NoError(t, bus.Publish(context.Background(), "foo.Bar", nil))
	assert.Equal(t, []int{0, 1, 2}, calls)
}

func TestPublish_HandlerErrorAborts(t *testing.T) {
	bus := newBus()
	var second int32
	boom := errors.New("boom")
	bus.Subscribe("foo.Bar", func(_ context.Context, _ any) error { return boom })
	bus.Subscribe("foo.Bar", func(_ context.Context, _ any) error {
		atomic.AddInt32(&second, 1)
		return nil
	})

	err := bus.Publish(context.Background(), "foo.Bar", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
	assert.Equal(t, int32(0), atomic.LoadInt32(&second), "handlers after a failure must not run")
}

func TestPublish_DoesNotDeliverToOtherEventNames(t *testing.T) {
	bus := newBus()
	called := false
	bus.Subscribe("foo.Bar", func(_ context.Context, _ any) error {
		called = true
		return nil
	})

	require.NoError(t, bus.Publish(context.Background(), "foo.Other", nil))
	assert.False(t, called)
}

func TestSubscribe_PanicsOnEmptyName(t *testing.T) {
	bus := newBus()
	assert.Panics(t, func() {
		bus.Subscribe("", func(_ context.Context, _ any) error { return nil })
	})
}

func TestSubscribe_PanicsOnNilHandler(t *testing.T) {
	bus := newBus()
	assert.Panics(t, func() { bus.Subscribe("foo.Bar", nil) })
}
