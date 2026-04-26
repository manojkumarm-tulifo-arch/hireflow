-- hiring_intents: aggregate root storage for the hiringintent context.
-- JSONB columns for the rich value-object payloads keep the schema stable as
-- domain shape evolves. Indexed columns are the ones we filter/sort by.
CREATE TABLE hiring_intents (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL,
    recruiter_id    UUID NOT NULL,

    role            JSONB NOT NULL,
    priority        TEXT  NOT NULL,
    intent_signals  JSONB NOT NULL DEFAULT '[]'::jsonb,
    trust_signals   JSONB NOT NULL DEFAULT '[]'::jsonb,
    budget          JSONB,

    status          TEXT  NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    confirmed_at    TIMESTAMPTZ,
    cancelled_at    TIMESTAMPTZ,
    cancel_reason   TEXT NOT NULL DEFAULT '',

    CONSTRAINT hiring_intents_status_check
        CHECK (status IN ('DRAFTED', 'CONFIRMED', 'CANCELLED', 'CLOSED')),
    CONSTRAINT hiring_intents_priority_check
        CHECK (priority IN ('LOW', 'MEDIUM', 'HIGH', 'CRITICAL'))
);

CREATE INDEX hiring_intents_tenant_status_created_idx
    ON hiring_intents (tenant_id, status, created_at DESC);

CREATE INDEX hiring_intents_tenant_recruiter_idx
    ON hiring_intents (tenant_id, recruiter_id);

-- hiring_intent_outbox: durable event log written in the same transaction as
-- aggregate updates. A separate dispatcher reads pending rows and publishes
-- to the message broker, then marks them dispatched.
CREATE TABLE hiring_intent_outbox (
    id              BIGSERIAL PRIMARY KEY,
    event_name      TEXT NOT NULL,
    aggregate_id    UUID NOT NULL,
    tenant_id       UUID NOT NULL,
    payload         JSONB NOT NULL,
    occurred_at     TIMESTAMPTZ NOT NULL,
    dispatched_at   TIMESTAMPTZ
);

CREATE INDEX hiring_intent_outbox_pending_idx
    ON hiring_intent_outbox (occurred_at)
    WHERE dispatched_at IS NULL;
