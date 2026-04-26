-- job_postings: aggregate root storage for the jobposting context.
CREATE TABLE job_postings (
    id            UUID PRIMARY KEY,
    tenant_id     UUID NOT NULL,
    intent_id     UUID NOT NULL,

    jd            JSONB NOT NULL,
    sources       JSONB NOT NULL DEFAULT '[]'::jsonb,

    status        TEXT  NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL,
    published_at  TIMESTAMPTZ,
    closed_at     TIMESTAMPTZ,
    close_reason  TEXT NOT NULL DEFAULT '',

    CONSTRAINT job_postings_status_check
        CHECK (status IN ('DRAFT', 'PUBLISHED', 'CLOSED', 'ARCHIVED')),
    -- One posting per (tenant, intent). Enforces idempotent creation from
    -- the IntentConfirmed event consumer.
    CONSTRAINT job_postings_tenant_intent_unique UNIQUE (tenant_id, intent_id)
);

CREATE INDEX job_postings_tenant_status_created_idx
    ON job_postings (tenant_id, status, created_at DESC);

-- Outbox table for jobposting domain events.
CREATE TABLE job_posting_outbox (
    id              BIGSERIAL PRIMARY KEY,
    event_name      TEXT NOT NULL,
    aggregate_id    UUID NOT NULL,
    tenant_id       UUID NOT NULL,
    payload         JSONB NOT NULL,
    occurred_at     TIMESTAMPTZ NOT NULL,
    dispatched_at   TIMESTAMPTZ
);

CREATE INDEX job_posting_outbox_pending_idx
    ON job_posting_outbox (occurred_at)
    WHERE dispatched_at IS NULL;

-- Idempotency log for cross-context event consumers in this module.
-- One row per (subscriber_name, source_event_id) — the consumer skips
-- events whose key is already present.
CREATE TABLE jobposting_processed_events (
    subscriber_name  TEXT NOT NULL,
    source_event_id  TEXT NOT NULL,
    processed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (subscriber_name, source_event_id)
);
