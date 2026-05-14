// Package sse provides Server-Sent Events infrastructure for the sourcing context.
// BatchEventFanout subscribes to the in-process eventbus and routes domain events
// to per-connection SSE subscribers, keyed by batch_id.
package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/hustle/hireflow/internal/sourcing/domain/events"
)

const subscriberChannelBuffer = 16

// Subscriber is a per-connection channel that receives pre-formatted SSE-line
// bytes for one batch. Create via BatchEventFanout.Subscribe.
type Subscriber struct {
	BatchID uuid.UUID
	C       chan []byte   // pre-formatted SSE-line bytes
	Done    chan struct{} // closed when the subscriber is removed
}

// BatchEventFanout subscribes to the in-process eventbus once and routes events
// to all interested HTTP subscribers based on their batch_id.
type BatchEventFanout struct {
	mu     sync.Mutex
	subs   map[uuid.UUID][]*Subscriber // batch_id -> active subscribers
	logger zerolog.Logger
}

// NewBatchEventFanout constructs a fanout. The logger receives a warning line
// whenever an event is dropped due to a full subscriber channel.
func NewBatchEventFanout(logger zerolog.Logger) *BatchEventFanout {
	return &BatchEventFanout{
		subs:   make(map[uuid.UUID][]*Subscriber),
		logger: logger.With().Str("component", "sse.BatchEventFanout").Logger(),
	}
}

// Subscribe registers a new SSE subscriber for the given batch_id.
// Returns the read-only channel and a cleanup function the caller must invoke
// on disconnect (e.g. when the HTTP request context is cancelled).
func (f *BatchEventFanout) Subscribe(batchID uuid.UUID) (<-chan []byte, func()) {
	sub := &Subscriber{
		BatchID: batchID,
		C:       make(chan []byte, subscriberChannelBuffer),
		Done:    make(chan struct{}),
	}

	f.mu.Lock()
	f.subs[batchID] = append(f.subs[batchID], sub)
	f.mu.Unlock()

	cleanup := func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		list := f.subs[batchID]
		for i, s := range list {
			if s == sub {
				// Remove from slice without preserving order.
				list[i] = list[len(list)-1]
				list[len(list)-1] = nil
				f.subs[batchID] = list[:len(list)-1]
				break
			}
		}
		if len(f.subs[batchID]) == 0 {
			delete(f.subs, batchID)
		}
		close(sub.Done)
		close(sub.C)
	}
	return sub.C, cleanup
}

// OnEvent is the eventbus.Handler callback. It routes domain events that carry
// a batch_id to all registered SSE subscribers for that batch.
//
// Events without a batch_id (e.g. CandidateParsed, ApplicationScored) are
// silently ignored. The method always returns nil — a full subscriber channel
// must never abort the publish chain.
func (f *BatchEventFanout) OnEvent(_ context.Context, event any) error {
	batchID, line, ok := formatSSELine(event)
	if !ok {
		// Event type has no batch_id — not our concern.
		return nil
	}

	f.mu.Lock()
	list := f.subs[batchID]
	// Copy slice under the lock so we can send outside the lock after the copy.
	// Since sends are non-blocking (select + default) we can also send while
	// holding the lock without risk of deadlock; we choose to hold the lock for
	// simplicity since the critical section is tiny.
	for _, sub := range list {
		select {
		case sub.C <- line:
		default:
			f.logger.Warn().
				Str("batch_id", batchID.String()).
				Msg("sse subscriber channel full; dropping event")
		}
	}
	f.mu.Unlock()

	return nil
}

// ---------------------------------------------------------------------------
// SSE-line formatting helpers
// ---------------------------------------------------------------------------

// formatSSELine converts a domain event into (batchID, sseLine, true) when the
// event carries a batch_id, or (uuid.Nil, nil, false) otherwise.
//
// Wire format:
//
//	event: <name>\ndata: <json>\n\n
func formatSSELine(event any) (uuid.UUID, []byte, bool) {
	switch ev := event.(type) {
	case events.ResumeUploadAccepted:
		payload, err := json.Marshal(struct {
			UploadID string `json:"upload_id"`
			BatchID  string `json:"batch_id"`
			Status   string `json:"status"`
			At       string `json:"occurred_at"`
		}{
			UploadID: ev.UploadID.String(),
			BatchID:  ev.BatchID.String(),
			Status:   "Pending",
			At:       ev.OccurredAt.Format(time.RFC3339Nano),
		})
		if err != nil {
			return uuid.Nil, nil, false
		}
		return ev.BatchID, buildSSELine("item_accepted", payload), true

	case events.ResumeUploadFailed:
		payload, err := json.Marshal(struct {
			UploadID string `json:"upload_id"`
			BatchID  string `json:"batch_id"`
			Reason   string `json:"reason"`
			At       string `json:"occurred_at"`
		}{
			UploadID: ev.UploadID.String(),
			BatchID:  ev.BatchID.String(),
			Reason:   ev.Reason,
			At:       ev.OccurredAt.Format(time.RFC3339Nano),
		})
		if err != nil {
			return uuid.Nil, nil, false
		}
		return ev.BatchID, buildSSELine("item_failed", payload), true

	case events.ResumeExtracted:
		payload, err := json.Marshal(struct {
			UploadID  string `json:"upload_id"`
			BatchID   string `json:"batch_id"`
			PageCount int    `json:"page_count"`
			At        string `json:"occurred_at"`
		}{
			UploadID:  ev.UploadID.String(),
			BatchID:   ev.BatchID.String(),
			PageCount: ev.PageCount,
			At:        ev.OccurredAt.Format(time.RFC3339Nano),
		})
		if err != nil {
			return uuid.Nil, nil, false
		}
		return ev.BatchID, buildSSELine("item_extracted", payload), true

	case events.ResumeParsed:
		payload, err := json.Marshal(struct {
			UploadID    string `json:"upload_id"`
			BatchID     string `json:"batch_id"`
			CandidateID string `json:"candidate_id"`
			At          string `json:"occurred_at"`
		}{
			UploadID:    ev.UploadID.String(),
			BatchID:     ev.BatchID.String(),
			CandidateID: ev.CandidateID.String(),
			At:          ev.OccurredAt.Format(time.RFC3339Nano),
		})
		if err != nil {
			return uuid.Nil, nil, false
		}
		return ev.BatchID, buildSSELine("item_parsed", payload), true

	default:
		return uuid.Nil, nil, false
	}
}

// buildSSELine formats a single SSE message: "event: <name>\ndata: <json>\n\n"
func buildSSELine(eventType string, jsonPayload []byte) []byte {
	return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, jsonPayload))
}
