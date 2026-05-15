package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/hustle/hireflow/internal/interview/domain/events"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// DispatcherConfig controls polling behavior.
type DispatcherConfig struct {
	BatchSize    int           // default 50
	PollInterval time.Duration // default 1s
}

// OutboxDispatcher polls interview_outbox, decodes pending rows, and
// publishes each event via the EventPublisher. Same loop as
// sourcing.OutboxDispatcher.
type OutboxDispatcher struct {
	pool   *pgxpool.Pool
	pub    EventPublisher
	logger zerolog.Logger
	cfg    DispatcherConfig
}

// NewOutboxDispatcher wires the dispatcher.
func NewOutboxDispatcher(pool *pgxpool.Pool, pub EventPublisher, logger zerolog.Logger, cfg DispatcherConfig) *OutboxDispatcher {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	return &OutboxDispatcher{
		pool:   pool,
		pub:    pub,
		logger: logger.With().Str("component", "interview_outbox_dispatcher").Logger(),
		cfg:    cfg,
	}
}

// Run loops until ctx is done, dispatching pending events on each tick.
func (d *OutboxDispatcher) Run(ctx context.Context) {
	d.logger.Info().Int("batch", d.cfg.BatchSize).Msg("dispatcher started")
	defer d.logger.Info().Msg("dispatcher stopped")
	t := time.NewTicker(d.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := d.dispatchBatch(ctx); err != nil {
				d.logger.Error().Err(err).Msg("dispatch batch failed")
			}
		}
	}
}

// DispatchBatch is an exported wrapper around dispatchBatch for use in tests.
func (d *OutboxDispatcher) DispatchBatch(ctx context.Context) error {
	return d.dispatchBatch(ctx)
}

func (d *OutboxDispatcher) dispatchBatch(ctx context.Context) error {
	rows, err := d.pool.Query(ctx, `
		SELECT id, event_name, aggregate_id, tenant_id, payload, occurred_at
		FROM interview_outbox
		WHERE dispatched_at IS NULL
		ORDER BY id
		LIMIT $1
	`, d.cfg.BatchSize)
	if err != nil {
		return fmt.Errorf("query outbox: %w", err)
	}
	defer rows.Close()

	type pending struct {
		id          int64
		eventName   string
		aggregateID uuid.UUID
		tenantID    string
		payload     []byte
		occurredAt  time.Time
	}
	var batch []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.eventName, &p.aggregateID, &p.tenantID, &p.payload, &p.occurredAt); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		batch = append(batch, p)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows.Err: %w", err)
	}

	for _, p := range batch {
		tenantID, err := shared.ParseTenantID(p.tenantID)
		if err != nil {
			d.logger.Error().Err(err).Str("event", p.eventName).Int64("id", p.id).Msg("invalid tenant_id; leaving row undispatched")
			continue
		}
		ev, err := decodeEvent(p.eventName, p.aggregateID, tenantID, p.occurredAt, p.payload)
		if err != nil {
			d.logger.Error().Err(err).Str("event", p.eventName).Msg("decode failed; leaving row undispatched")
			continue
		}
		if err := d.pub.Publish(ctx, ev); err != nil {
			d.logger.Error().Err(err).Str("event", p.eventName).Msg("publish failed; leaving row undispatched")
			continue
		}
		if _, err := d.pool.Exec(ctx, `UPDATE interview_outbox SET dispatched_at=now() WHERE id=$1`, p.id); err != nil {
			d.logger.Error().Err(err).Int64("id", p.id).Msg("mark dispatched failed")
		}
	}
	return nil
}

// decodeEvent inflates a payload into the matching event struct.
func decodeEvent(name string, _ uuid.UUID, _ shared.TenantID, _ time.Time, payload []byte) (events.Event, error) {
	switch name {
	case "interview.InterviewProcessCreated":
		var e events.InterviewProcessCreated
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, err
		}
		return e, nil
	case "interview.InterviewQuestionsGenerated":
		var e events.InterviewQuestionsGenerated
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, err
		}
		return e, nil
	case "interview.InterviewFeedbackRecorded":
		var e events.InterviewFeedbackRecorded
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, err
		}
		return e, nil
	}
	return nil, errors.New("unknown event name: " + name)
}
