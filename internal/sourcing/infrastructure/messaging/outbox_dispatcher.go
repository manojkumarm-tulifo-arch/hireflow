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

	"github.com/hustle/hireflow/internal/sourcing/domain/events"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// DispatcherConfig tunes the dispatcher's polling behavior. Zero values use defaults.
type DispatcherConfig struct {
	PollInterval time.Duration // default 500ms
	BatchSize    int           // default 50
}

// OutboxDispatcher polls sourcing_outbox and forwards rows to a Publisher.
type OutboxDispatcher struct {
	pool   *pgxpool.Pool
	pub    EventPublisher
	logger zerolog.Logger
	cfg    DispatcherConfig
}

// NewOutboxDispatcher wires the dispatcher.
func NewOutboxDispatcher(pool *pgxpool.Pool, pub EventPublisher, logger zerolog.Logger, cfg DispatcherConfig) *OutboxDispatcher {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 500 * time.Millisecond
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 50
	}
	return &OutboxDispatcher{pool: pool, pub: pub, logger: logger, cfg: cfg}
}

// Run blocks until ctx is canceled, periodically draining pending outbox rows.
func (d *OutboxDispatcher) Run(ctx context.Context) {
	t := time.NewTicker(d.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := d.drain(ctx); err != nil && !errors.Is(err, context.Canceled) {
				d.logger.Error().Err(err).Msg("outbox drain error")
			}
		}
	}
}

func (d *OutboxDispatcher) drain(ctx context.Context) error {
	rows, err := d.pool.Query(ctx, `
		SELECT id, event_name, aggregate_id, tenant_id, payload, occurred_at
		FROM sourcing_outbox
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
		_, err = d.pool.Exec(ctx, `UPDATE sourcing_outbox SET dispatched_at=now() WHERE id=$1`, p.id)
		if err != nil {
			d.logger.Error().Err(err).Int64("id", p.id).Msg("mark dispatched failed")
		}
	}
	return nil
}

// decodeEvent inflates a payload into the matching event struct.
func decodeEvent(name string, aggID uuid.UUID, tenant shared.TenantID, at time.Time, payload []byte) (events.Event, error) {
	switch name {
	case "sourcing.ResumeUploadAccepted":
		var e events.ResumeUploadAccepted
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, err
		}
		return e, nil
	case "sourcing.ResumeUploadFailed":
		var e events.ResumeUploadFailed
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, err
		}
		return e, nil
	case "sourcing.ResumeExtracted":
		var e events.ResumeExtracted
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, err
		}
		return e, nil
	}
	return nil, fmt.Errorf("unknown event name: %s", name)
}
