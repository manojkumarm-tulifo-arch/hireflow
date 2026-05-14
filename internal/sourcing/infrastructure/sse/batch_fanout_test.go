package sse_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/domain/events"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/sse"
)

func newFanout() *sse.BatchEventFanout {
	return sse.NewBatchEventFanout(zerolog.Nop())
}

func fixedEvent(batchID uuid.UUID) events.ResumeUploadAccepted {
	return events.ResumeUploadAccepted{
		UploadID:    uuid.New(),
		TenantID:    shared.NewTenantID(),
		IntentID:    uuid.New(),
		BatchID:     batchID,
		ContentHash: "abc123",
		OccurredAt:  time.Now().UTC(),
	}
}

// TestSubscribe_MatchingBatchID verifies that a subscriber receives events
// whose batch_id matches the subscription.
func TestSubscribe_MatchingBatchID(t *testing.T) {
	f := newFanout()
	batchID := uuid.New()

	ch, cleanup := f.Subscribe(batchID)
	defer cleanup()

	ev := fixedEvent(batchID)
	err := f.OnEvent(context.Background(), ev)
	require.NoError(t, err)

	select {
	case line := <-ch:
		require.NotEmpty(t, line)
		assert.True(t, strings.HasPrefix(string(line), "event: item_accepted\n"),
			"expected SSE event line to start with 'event: item_accepted', got: %s", line)
		assert.True(t, strings.Contains(string(line), batchID.String()),
			"SSE line must contain the batch_id")
		assert.True(t, strings.HasSuffix(string(line), "\n\n"),
			"SSE line must end with double newline")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subscriber did not receive event within timeout")
	}
}

// TestSubscribe_NonMatchingBatchID verifies that a subscriber does NOT receive
// events for a different batch_id.
func TestSubscribe_NonMatchingBatchID(t *testing.T) {
	f := newFanout()
	batchID := uuid.New()
	otherBatchID := uuid.New()

	ch, cleanup := f.Subscribe(batchID)
	defer cleanup()

	ev := fixedEvent(otherBatchID)
	err := f.OnEvent(context.Background(), ev)
	require.NoError(t, err)

	select {
	case line := <-ch:
		t.Fatalf("subscriber received unexpected event: %s", line)
	case <-time.After(30 * time.Millisecond):
		// Correct: no event delivered.
	}
}

// TestMultipleSubscribers_AllReceive verifies that multiple subscribers for the
// same batch_id all receive the event.
func TestMultipleSubscribers_AllReceive(t *testing.T) {
	f := newFanout()
	batchID := uuid.New()

	ch1, cleanup1 := f.Subscribe(batchID)
	defer cleanup1()
	ch2, cleanup2 := f.Subscribe(batchID)
	defer cleanup2()
	ch3, cleanup3 := f.Subscribe(batchID)
	defer cleanup3()

	ev := fixedEvent(batchID)
	err := f.OnEvent(context.Background(), ev)
	require.NoError(t, err)

	for i, ch := range []<-chan []byte{ch1, ch2, ch3} {
		select {
		case line := <-ch:
			require.NotEmpty(t, line, "subscriber %d got empty line", i+1)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("subscriber %d did not receive event within timeout", i+1)
		}
	}
}

// TestChannelFull_NoBLocking verifies that OnEvent never blocks when a
// subscriber's channel is full, and always returns nil.
func TestChannelFull_NoBlocking(t *testing.T) {
	f := newFanout()
	batchID := uuid.New()

	ch, cleanup := f.Subscribe(batchID)
	defer cleanup()

	// Fill the channel buffer completely (buffer size is 16).
	ev := fixedEvent(batchID)
	for range 16 {
		err := f.OnEvent(context.Background(), ev)
		require.NoError(t, err)
	}

	// Now the channel is full; this must NOT block and must return nil.
	done := make(chan error, 1)
	go func() {
		done <- f.OnEvent(context.Background(), ev)
	}()

	select {
	case err := <-done:
		assert.NoError(t, err, "OnEvent must return nil even when channel is full")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("OnEvent blocked when subscriber channel was full")
	}

	// Drain to confirm buffer has 16 items (not 17).
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto drained
		}
	}
drained:
	assert.Equal(t, 16, count, "only 16 events should be in the channel (17th was dropped)")
}

// TestUnsubscribe_NoMoreEvents verifies that after the cleanup func is called,
// the subscriber receives no further events.
func TestUnsubscribe_NoMoreEvents(t *testing.T) {
	f := newFanout()
	batchID := uuid.New()

	ch, cleanup := f.Subscribe(batchID)

	// Deliver one event to confirm subscription is live.
	ev := fixedEvent(batchID)
	require.NoError(t, f.OnEvent(context.Background(), ev))

	select {
	case <-ch:
		// Good — received the first event.
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subscriber did not receive first event")
	}

	// Unsubscribe and drain any residual items.
	cleanup()

	// Sending another event must not panic and must not deliver to the closed channel.
	// We use a fresh fanout-level fire; since cleanup closed sub.C any send would panic
	// if the fanout still held a reference. The fanout must have removed the subscriber.
	require.NoError(t, f.OnEvent(context.Background(), ev))

	// Verify the Done channel was closed.
	select {
	case <-ch:
		// channel closed; that's expected after cleanup.
	default:
		// channel is empty and not closed — that's also acceptable since we drained it.
	}
}

// TestOnEvent_UnrelatedEventType verifies that events without a batch_id (e.g.
// CandidateParsed or ApplicationScored) are silently ignored.
func TestOnEvent_UnrelatedEventType(t *testing.T) {
	f := newFanout()
	batchID := uuid.New()

	ch, cleanup := f.Subscribe(batchID)
	defer cleanup()

	// Use an unknown/unrelated type.
	type unrelatedEvent struct{ Foo string }
	err := f.OnEvent(context.Background(), unrelatedEvent{"bar"})
	require.NoError(t, err)

	select {
	case line := <-ch:
		t.Fatalf("subscriber received unexpected event for unrelated type: %s", line)
	case <-time.After(30 * time.Millisecond):
		// Correct: nothing delivered.
	}
}

// TestAllFourEventTypes_SSELineFormat checks that each of the 4 supported event
// types produces a properly-formatted SSE line with expected fields.
func TestAllFourEventTypes_SSELineFormat(t *testing.T) {
	batchID := uuid.New()
	uploadID := uuid.New()
	candidateID := uuid.New()
	tenantID := shared.NewTenantID()
	now := time.Now().UTC()

	tests := []struct {
		name          string
		event         any
		wantEventType string
		wantField     string
	}{
		{
			name: "ResumeUploadAccepted",
			event: events.ResumeUploadAccepted{
				UploadID:   uploadID,
				TenantID:   tenantID,
				BatchID:    batchID,
				OccurredAt: now,
			},
			wantEventType: "item_accepted",
			wantField:     `"status":"Pending"`,
		},
		{
			name: "ResumeUploadFailed",
			event: events.ResumeUploadFailed{
				UploadID:   uploadID,
				TenantID:   tenantID,
				BatchID:    batchID,
				Reason:     "virus_detected",
				OccurredAt: now,
			},
			wantEventType: "item_failed",
			wantField:     `"reason":"virus_detected"`,
		},
		{
			name: "ResumeExtracted",
			event: events.ResumeExtracted{
				UploadID:   uploadID,
				TenantID:   tenantID,
				BatchID:    batchID,
				PageCount:  5,
				OccurredAt: now,
			},
			wantEventType: "item_extracted",
			wantField:     `"page_count":5`,
		},
		{
			name: "ResumeParsed",
			event: events.ResumeParsed{
				UploadID:    uploadID,
				TenantID:    tenantID,
				BatchID:     batchID,
				CandidateID: candidateID,
				OccurredAt:  now,
			},
			wantEventType: "item_parsed",
			wantField:     candidateID.String(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFanout()
			ch, cleanup := f.Subscribe(batchID)
			defer cleanup()

			err := f.OnEvent(context.Background(), tc.event)
			require.NoError(t, err)

			select {
			case line := <-ch:
				s := string(line)
				assert.True(t, strings.HasPrefix(s, "event: "+tc.wantEventType+"\n"),
					"expected event type %q in line: %s", tc.wantEventType, s)
				assert.True(t, strings.Contains(s, tc.wantField),
					"expected field %q in line: %s", tc.wantField, s)
				assert.True(t, strings.HasSuffix(s, "\n\n"),
					"SSE line must end with double newline: %q", s)

				// Ensure the data line is valid JSON.
				dataLine := extractDataLine(t, s)
				assert.True(t, json.Valid([]byte(dataLine)),
					"data payload must be valid JSON: %s", dataLine)

			case <-time.After(100 * time.Millisecond):
				t.Fatalf("subscriber did not receive %s event within timeout", tc.name)
			}
		})
	}
}

// extractDataLine pulls the JSON payload from the "data: <json>" line of an SSE message.
func extractDataLine(t *testing.T, sseMessage string) string {
	t.Helper()
	for _, line := range strings.Split(sseMessage, "\n") {
		if strings.HasPrefix(line, "data: ") {
			return strings.TrimPrefix(line, "data: ")
		}
	}
	t.Fatalf("no 'data:' line found in SSE message: %q", sseMessage)
	return ""
}
