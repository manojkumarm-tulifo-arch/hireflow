package subscribers_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	intentevents "github.com/hustle/hireflow/internal/hiringintent/domain/events"
	intentvo "github.com/hustle/hireflow/internal/hiringintent/domain/valueobjects"
	"github.com/hustle/hireflow/internal/jobposting/application/commands"
	"github.com/hustle/hireflow/internal/jobposting/infrastructure/subscribers"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/shared/infrastructure/eventbus"
)

// TestBusWiring_PublishedIntentConfirmedDraftsAPosting mirrors the closure in
// cmd/api/main.go that bridges the event bus to the IntentConfirmedConsumer.
// If the event name or struct shape ever drifts, this test fails before
// production wiring goes silently broken.
func TestBusWiring_PublishedIntentConfirmedDraftsAPosting(t *testing.T) {
	repo := newFakeRepo()
	consumer := subscribers.NewIntentConfirmedConsumer(
		&fakeReader{snap: subscribers.IntentSnapshot{
			RoleTitle:      "Senior Backend Engineer",
			RequiredSkills: []string{"Go"},
			Headcount:      1,
			MinYears:       3,
			MaxYears:       6,
		}},
		commands.NewCreateFromIntentHandler(repo),
	)

	bus := eventbus.NewInMemory(zerolog.Nop())
	bus.Subscribe("hiringintent.IntentConfirmed", func(ctx context.Context, ev any) error {
		typed, ok := ev.(intentevents.IntentConfirmed)
		if !ok {
			return fmt.Errorf("unexpected event type %T", ev)
		}
		return consumer.Consume(ctx, typed)
	})

	event := intentevents.NewIntentConfirmed(
		intentvo.NewIntentID(),
		shared.NewTenantID(),
		shared.NewRecruiterID(),
		intentvo.PriorityHigh,
		time.Now().UTC(),
	)

	require.NoError(t, bus.Publish(context.Background(), event.EventName(), event))
	assert.Equal(t, 1, repo.saves, "exactly one draft posting should be created")
}
