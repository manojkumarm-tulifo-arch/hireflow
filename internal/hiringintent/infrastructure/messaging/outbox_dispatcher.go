package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/hustle/hireflow/internal/hiringintent/domain/events"
)

// OutboxDispatcher polls the hiring_intent_outbox table for undispatched events
// and forwards them to an EventPublisher. Designed to run as a goroutine and
// stop cleanly on context cancellation.
//
// Locking strategy: claims rows with FOR UPDATE SKIP LOCKED so multiple
// dispatcher instances can run side-by-side without double-publishing.
type OutboxDispatcher struct {
	pool      *pgxpool.Pool
	publisher EventPublisher
	logger    zerolog.Logger
	batchSize int
	idleSleep time.Duration
	errSleep  time.Duration
}

// DispatcherConfig tunes the dispatcher.
type DispatcherConfig struct {
	BatchSize int
	IdleSleep time.Duration
	ErrSleep  time.Duration
}

// NewOutboxDispatcher wires the dispatcher with sane defaults.
func NewOutboxDispatcher(pool *pgxpool.Pool, publisher EventPublisher, logger zerolog.Logger, cfg DispatcherConfig) *OutboxDispatcher {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	if cfg.IdleSleep <= 0 {
		cfg.IdleSleep = 500 * time.Millisecond
	}
	if cfg.ErrSleep <= 0 {
		cfg.ErrSleep = 5 * time.Second
	}
	return &OutboxDispatcher{
		pool:      pool,
		publisher: publisher,
		logger:    logger.With().Str("component", "outbox_dispatcher").Logger(),
		batchSize: cfg.BatchSize,
		idleSleep: cfg.IdleSleep,
		errSleep:  cfg.ErrSleep,
	}
}

// Run blocks until the context is cancelled, polling the outbox in a loop.
func (d *OutboxDispatcher) Run(ctx context.Context) {
	d.logger.Info().Int("batch", d.batchSize).Msg("dispatcher started")
	for {
		select {
		case <-ctx.Done():
			d.logger.Info().Msg("dispatcher stopped")
			return
		default:
		}

		dispatched, err := d.processBatch(ctx)
		switch {
		case errors.Is(err, context.Canceled):
			d.logger.Info().Msg("dispatcher stopped")
			return
		case err != nil:
			d.logger.Error().Err(err).Msg("batch error, backing off")
			d.sleep(ctx, d.errSleep)
		case dispatched == 0:
			d.sleep(ctx, d.idleSleep)
		}
	}
}

// processBatch claims, publishes, and marks one batch of outbox rows.
// Returns the number of rows successfully dispatched.
func (d *OutboxDispatcher) processBatch(ctx context.Context) (int, error) {
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, claimSQL, d.batchSize)
	if err != nil {
		return 0, fmt.Errorf("claim batch: %w", err)
	}

	type claimed struct {
		id        int64
		eventName string
		payload   []byte
	}
	var batch []claimed
	for rows.Next() {
		var c claimed
		if err := rows.Scan(&c.id, &c.eventName, &c.payload); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan: %w", err)
		}
		batch = append(batch, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("rows iter: %w", err)
	}
	if len(batch) == 0 {
		return 0, nil
	}

	dispatched := 0
	for _, c := range batch {
		ev, err := decodeEvent(c.eventName, c.payload)
		if err != nil {
			d.logger.Error().Err(err).Str("event", c.eventName).Int64("id", c.id).Msg("decode failed; will retry")
			continue
		}
		if err := d.publisher.Publish(ctx, ev); err != nil {
			d.logger.Warn().Err(err).Str("event", c.eventName).Int64("id", c.id).Msg("publish failed; will retry")
			continue
		}
		if _, err := tx.Exec(ctx, markDispatchedSQL, c.id); err != nil {
			return dispatched, fmt.Errorf("mark dispatched: %w", err)
		}
		dispatched++
	}

	if err := tx.Commit(ctx); err != nil {
		return dispatched, fmt.Errorf("commit: %w", err)
	}
	return dispatched, nil
}

func (d *OutboxDispatcher) sleep(ctx context.Context, dur time.Duration) {
	t := time.NewTimer(dur)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

const claimSQL = `
SELECT id, event_name, payload
FROM hiring_intent_outbox
WHERE dispatched_at IS NULL
ORDER BY id
LIMIT $1
FOR UPDATE SKIP LOCKED
`

const markDispatchedSQL = `
UPDATE hiring_intent_outbox SET dispatched_at = NOW() WHERE id = $1
`

// decodeEvent rebuilds a typed domain event from its serialized payload.
// Unknown event names produce an error so they can be retried after a deploy.
func decodeEvent(name string, payload []byte) (events.Event, error) {
	switch name {
	case "hiringintent.IntentDrafted":
		var e events.IntentDrafted
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, fmt.Errorf("unmarshal %s: %w", name, err)
		}
		return e, nil
	case "hiringintent.IntentRoleUpdated":
		var e events.IntentRoleUpdated
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, fmt.Errorf("unmarshal %s: %w", name, err)
		}
		return e, nil
	case "hiringintent.IntentConfirmed":
		var e events.IntentConfirmed
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, fmt.Errorf("unmarshal %s: %w", name, err)
		}
		return e, nil
	case "hiringintent.IntentCancelled":
		var e events.IntentCancelled
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, fmt.Errorf("unmarshal %s: %w", name, err)
		}
		return e, nil
	default:
		return nil, fmt.Errorf("unknown event name: %s", name)
	}
}
