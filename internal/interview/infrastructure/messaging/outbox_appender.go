package messaging

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hustle/hireflow/internal/interview/domain/events"
)

// PostgresOutboxAppender writes one row to interview_outbox synchronously.
// Used by commands that emit events without going through an aggregate Save
// (e.g., RecordFeedback — feedback is not an aggregate).
type PostgresOutboxAppender struct{ pool *pgxpool.Pool }

func NewPostgresOutboxAppender(pool *pgxpool.Pool) *PostgresOutboxAppender {
	return &PostgresOutboxAppender{pool: pool}
}

func (a *PostgresOutboxAppender) Append(ctx context.Context, ev events.Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	_, err = a.pool.Exec(ctx, `
		INSERT INTO interview_outbox (event_name, aggregate_id, tenant_id, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5)
	`, ev.EventName(), ev.AggregateID(), ev.Tenant().String(), payload, ev.At())
	if err != nil {
		return fmt.Errorf("insert outbox: %w", err)
	}
	return nil
}
