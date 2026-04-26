-- tenants: pre-seeded list of organizations a user can join at signup.
-- Full Tenant aggregate (creation, billing, settings) belongs in a future
-- platform-admin context; this is the read-only lookup layer.
CREATE TABLE tenants (
    id         UUID PRIMARY KEY,
    slug       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed a default tenant for local development.
INSERT INTO tenants (id, slug, name)
VALUES ('00000000-0000-0000-0000-000000000001', 'demo', 'Demo Tenant');

-- users: aggregate root of the auth context.
-- email is unique GLOBALLY (not per-tenant) so signin can resolve the
-- tenant from the email alone — see FindByEmailAcrossTenants.
CREATE TABLE users (
    id                UUID PRIMARY KEY,
    tenant_id         UUID NOT NULL REFERENCES tenants(id),
    email             TEXT NOT NULL,
    name              TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL,
    roles             TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    failed_attempts   INTEGER NOT NULL DEFAULT 0,
    locked_until      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL,
    verified_at       TIMESTAMPTZ,
    last_signed_in_at TIMESTAMPTZ,

    CONSTRAINT users_email_unique UNIQUE (email),
    CONSTRAINT users_status_check CHECK (status IN ('PENDING_VERIFICATION', 'ACTIVE', 'LOCKED', 'SUSPENDED'))
);

CREATE INDEX users_tenant_id_idx ON users (tenant_id);

-- otp_sessions: one in-flight challenge per (email, purpose).
-- Older un-verified sessions for the same pair are deleted on insert of a
-- new one (see PostgresOTPSessionRepository.Save).
CREATE TABLE otp_sessions (
    id            UUID PRIMARY KEY,
    tenant_id     UUID NOT NULL REFERENCES tenants(id),
    email         TEXT NOT NULL,
    purpose       TEXT NOT NULL,
    code_hash     TEXT NOT NULL,
    attempts_left INTEGER NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    verified_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL,

    CONSTRAINT otp_sessions_purpose_check CHECK (purpose IN ('SIGNUP', 'SIGNIN'))
);

CREATE INDEX otp_sessions_email_purpose_created_idx
    ON otp_sessions (email, purpose, created_at DESC);

-- refresh_tokens: opaque, hashed. Plain secret only known at issue time.
CREATE TABLE refresh_tokens (
    id         UUID PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(id),
    tenant_id  UUID NOT NULL REFERENCES tenants(id),
    hash       TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ
);

CREATE INDEX refresh_tokens_user_active_idx
    ON refresh_tokens (user_id)
    WHERE revoked_at IS NULL;

-- auth_outbox: domain events emitted by the User aggregate.
-- Drained by the same dispatcher pattern as the other contexts.
CREATE TABLE auth_outbox (
    id            BIGSERIAL PRIMARY KEY,
    event_name    TEXT NOT NULL,
    aggregate_id  UUID NOT NULL,
    tenant_id     UUID NOT NULL,
    payload       JSONB NOT NULL,
    occurred_at   TIMESTAMPTZ NOT NULL,
    dispatched_at TIMESTAMPTZ
);

CREATE INDEX auth_outbox_pending_idx
    ON auth_outbox (occurred_at)
    WHERE dispatched_at IS NULL;
