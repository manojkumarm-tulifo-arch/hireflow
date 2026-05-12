-- sourcing_outbox: per-context outbox, mirrors hiring_intent_outbox.
CREATE TABLE sourcing_outbox (
    id              BIGSERIAL PRIMARY KEY,
    event_name      TEXT NOT NULL,
    aggregate_id    UUID NOT NULL,
    tenant_id       UUID NOT NULL,
    payload         JSONB NOT NULL,
    occurred_at     TIMESTAMPTZ NOT NULL,
    dispatched_at   TIMESTAMPTZ
);

CREATE INDEX sourcing_outbox_pending_idx
    ON sourcing_outbox (occurred_at)
    WHERE dispatched_at IS NULL;

-- resume_uploads: one row per uploaded file. Partition-ready (range by created_at)
-- so we can detach monthly partitions for archival at scale. v1 ships with a
-- default partition only; partition strategy is metadata-only to flip on later.
CREATE TABLE resume_uploads (
    id              UUID         NOT NULL,
    tenant_id       UUID         NOT NULL,
    intent_id       UUID         NOT NULL,
    batch_id        UUID         NOT NULL,
    candidate_id    UUID,
    storage_key     TEXT         NOT NULL,
    original_name   TEXT         NOT NULL,
    mime_type       TEXT         NOT NULL,
    size_bytes      BIGINT       NOT NULL,
    content_hash    TEXT         NOT NULL,
    status          TEXT         NOT NULL,
    stage_artifacts JSONB        NOT NULL DEFAULT '{}'::jsonb,
    attempt_count   INT          NOT NULL DEFAULT 0,
    last_error      TEXT,
    next_attempt_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (created_at, id),

    CONSTRAINT resume_uploads_status_check
        CHECK (status IN (
            'Pending','Scanning','Extracting','Parsing','Embedding','Scoring',
            'Extracted','Scored','Failed','Quarantined'
        ))
) PARTITION BY RANGE (created_at);

CREATE TABLE resume_uploads_default PARTITION OF resume_uploads DEFAULT;

CREATE INDEX resume_uploads_pending_idx
    ON resume_uploads (next_attempt_at)
    WHERE status NOT IN ('Extracted','Scored','Failed','Quarantined');

CREATE INDEX resume_uploads_batch_idx ON resume_uploads (batch_id);
CREATE INDEX resume_uploads_tenant_intent_idx ON resume_uploads (tenant_id, intent_id);

CREATE UNIQUE INDEX resume_uploads_tenant_hash_idx
    ON resume_uploads (tenant_id, content_hash);
