-- audit_log: cross-context immutable append log. Every PII read, lifecycle
-- transition, and erasure writes one row here. Lives in the `shared` namespace
-- because any bounded context can write to it via the AuditWriter port.
CREATE TABLE audit_log (
    id            BIGSERIAL    PRIMARY KEY,
    actor_user_id UUID         NOT NULL,
    tenant_id     UUID         NOT NULL,
    action        TEXT         NOT NULL,          -- e.g. "candidate_read", "application_shortlisted"
    resource_kind TEXT         NOT NULL,          -- e.g. "candidate", "application", "resume_upload"
    resource_id   UUID         NOT NULL,
    payload       JSONB        NOT NULL DEFAULT '{}'::jsonb,
    occurred_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),

    -- audit_log is append-only — no UPDATE expected. No FK constraints because
    -- this table outlives the rows it references (the whole point is
    -- post-erasure forensics).

    CONSTRAINT audit_log_action_nonempty CHECK (length(action) > 0),
    CONSTRAINT audit_log_resource_kind_nonempty CHECK (length(resource_kind) > 0)
);

-- Compliance queries: "who accessed this candidate's PII?"
CREATE INDEX audit_log_resource_idx
    ON audit_log (tenant_id, resource_kind, resource_id, occurred_at DESC);

-- Actor accountability: "what did this user do this month?"
CREATE INDEX audit_log_actor_idx
    ON audit_log (tenant_id, actor_user_id, occurred_at DESC);
