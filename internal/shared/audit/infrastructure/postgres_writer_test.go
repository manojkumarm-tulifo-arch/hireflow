//go:build integration

package infrastructure_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/shared/audit/domain"
	"github.com/hustle/hireflow/internal/shared/audit/infrastructure"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

func newAuditPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(context.Background(), `TRUNCATE audit_log`)
	require.NoError(t, err)
	return pool
}

func TestPostgresAuditWriter_Write_RoundTrip(t *testing.T) {
	pool := newAuditPool(t)
	w := infrastructure.NewPostgresAuditWriter(pool)

	tid, err := shared.ParseTenantID(uuid.New().String())
	require.NoError(t, err)

	actorID := uuid.New()
	resourceID := uuid.New()
	// Truncate to microseconds: Postgres TIMESTAMPTZ stores microsecond precision;
	// Go time.Time may carry nanoseconds that would not survive the round-trip.
	occurredAt := time.Now().UTC().Truncate(time.Microsecond)

	event := domain.AuditEvent{
		ActorUserID:  actorID,
		TenantID:     tid,
		Action:       "candidate.viewed",
		ResourceKind: "candidate",
		ResourceID:   resourceID,
		Payload:      map[string]any{"note": "integration test", "count": float64(42)},
		OccurredAt:   occurredAt,
	}

	require.NoError(t, w.Write(context.Background(), event))

	// Read the row back directly.
	var (
		gotActorID   uuid.UUID
		gotTenantID  string
		gotAction    string
		gotResKind   string
		gotResID     uuid.UUID
		gotPayload   []byte
		gotOccurred  time.Time
	)
	row := pool.QueryRow(context.Background(), `
		SELECT actor_user_id, tenant_id, action, resource_kind, resource_id, payload, occurred_at
		FROM audit_log
		LIMIT 1
	`)
	require.NoError(t, row.Scan(
		&gotActorID, &gotTenantID, &gotAction, &gotResKind, &gotResID, &gotPayload, &gotOccurred,
	))

	assert.Equal(t, actorID, gotActorID, "actor_user_id")
	assert.Equal(t, tid.String(), gotTenantID, "tenant_id")
	assert.Equal(t, "candidate.viewed", gotAction, "action")
	assert.Equal(t, "candidate", gotResKind, "resource_kind")
	assert.Equal(t, resourceID, gotResID, "resource_id")
	assert.Equal(t, occurredAt, gotOccurred.UTC(), "occurred_at")

	var gotPayloadMap map[string]any
	require.NoError(t, json.Unmarshal(gotPayload, &gotPayloadMap))
	assert.Equal(t, "integration test", gotPayloadMap["note"], "payload.note")
	assert.Equal(t, float64(42), gotPayloadMap["count"], "payload.count")
}

func TestPostgresAuditWriter_Write_EmptyPayloadBecomesEmptyObject(t *testing.T) {
	pool := newAuditPool(t)
	w := infrastructure.NewPostgresAuditWriter(pool)

	tid, err := shared.ParseTenantID(uuid.New().String())
	require.NoError(t, err)

	event := domain.AuditEvent{
		ActorUserID:  uuid.New(),
		TenantID:     tid,
		Action:       "candidate.deleted",
		ResourceKind: "candidate",
		ResourceID:   uuid.New(),
		Payload:      nil,
		OccurredAt:   time.Now().UTC().Truncate(time.Microsecond),
	}

	require.NoError(t, w.Write(context.Background(), event))

	var gotPayload []byte
	row := pool.QueryRow(context.Background(), `SELECT payload FROM audit_log LIMIT 1`)
	require.NoError(t, row.Scan(&gotPayload))

	var m map[string]any
	require.NoError(t, json.Unmarshal(gotPayload, &m))
	assert.Empty(t, m, "empty payload should persist as {}")
}

func TestPostgresAuditWriter_Write_InvalidEventReturnsValidateError(t *testing.T) {
	pool := newAuditPool(t)
	w := infrastructure.NewPostgresAuditWriter(pool)

	tid, err := shared.ParseTenantID(uuid.New().String())
	require.NoError(t, err)

	event := domain.AuditEvent{
		ActorUserID:  uuid.New(),
		TenantID:     tid,
		Action:       "", // invalid: empty action
		ResourceKind: "candidate",
		ResourceID:   uuid.New(),
		OccurredAt:   time.Now().UTC(),
	}

	writeErr := w.Write(context.Background(), event)
	require.Error(t, writeErr)
	assert.NotErrorIs(t, writeErr, domain.ErrAuditFailed, "validate errors should NOT be wrapped with ErrAuditFailed")

	// The table should be untouched.
	var count int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_log`).Scan(&count))
	assert.Equal(t, 0, count)
}

