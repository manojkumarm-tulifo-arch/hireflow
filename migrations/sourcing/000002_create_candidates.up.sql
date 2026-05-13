-- candidates: tenant-scoped person identity. One row per (tenant_id, content_hash).
-- The unique index lives on candidates directly (not partitioned, so no constraint
-- issue). Slice 2 ships an unpartitioned table — partition-readiness for candidates
-- can come at scale (~50M+ rows per spec §Scalability).
CREATE TABLE candidates (
    id                 UUID         PRIMARY KEY,
    tenant_id          UUID         NOT NULL,
    content_hash       TEXT         NOT NULL,

    -- PII fields encrypted at the application layer via the PIIEncryptor port.
    -- TEXT columns hold base64-encoded AES-GCM ciphertext + 12-byte nonce prefix.
    full_name_enc      TEXT,
    email_enc          TEXT,
    phone_enc          TEXT,

    -- Non-PII fields stored cleartext.
    location           TEXT,
    headline           TEXT,
    parsed_profile     JSONB        NOT NULL,
    profile_schema     INT          NOT NULL DEFAULT 1,

    source             TEXT         NOT NULL DEFAULT 'manual_upload',
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),

    UNIQUE (tenant_id, content_hash)
);

-- Lookup helpers. email_enc isn't indexable for content search since it's
-- ciphertext; the index is intentionally absent in slice 2. Slice 4's audit
-- + erasure work may introduce a per-tenant deterministic hash index.
CREATE INDEX candidates_tenant_created_idx ON candidates (tenant_id, created_at DESC);

-- Wire resume_uploads.candidate_id → candidates(id). No CASCADE on delete in v1:
-- candidate deletion (GDPR slice 4) explicitly cascades via application code so
-- audit-log entries are correctly written.
ALTER TABLE resume_uploads
    ADD CONSTRAINT resume_uploads_candidate_fk
        FOREIGN KEY (candidate_id) REFERENCES candidates(id);
