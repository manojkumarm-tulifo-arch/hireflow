-- Profile embedding column on candidates. Slice 2 introduced the candidates
-- table without this column because pgvector wasn't yet enabled.
ALTER TABLE candidates
    ADD COLUMN profile_embedding vector(1024);

-- ivfflat ANN index. Lists=100 is a reasonable default for up to ~1M rows;
-- can be REINDEX-ed with higher lists once we scale.
CREATE INDEX candidates_profile_embedding_idx
    ON candidates USING ivfflat (profile_embedding vector_cosine_ops)
    WITH (lists = 100);

-- hiring_intent_embeddings: cached embedding for each (intent, spec_version).
-- Re-confirming an intent with a changed RoleSpec bumps spec_version and
-- triggers a fresh embedding compute.
CREATE TABLE hiring_intent_embeddings (
    intent_id      UUID         NOT NULL,
    tenant_id      UUID         NOT NULL,
    spec_version   INT          NOT NULL,
    role_embedding vector(1024) NOT NULL,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (intent_id, spec_version)
);

CREATE INDEX hiring_intent_embeddings_tenant_idx
    ON hiring_intent_embeddings (tenant_id);

-- applications: per (Candidate, Intent) pair. Partition-ready by tenant.
CREATE TABLE applications (
    id                     UUID         NOT NULL,
    tenant_id              UUID         NOT NULL,
    candidate_id           UUID         NOT NULL,
    intent_id              UUID         NOT NULL,
    intent_spec_version    INT          NOT NULL,
    profile_schema_version INT          NOT NULL,

    status                 TEXT         NOT NULL,
    overall_score          NUMERIC(5,2),                   -- 0..100, populated after LLM judge
    score_band             TEXT,                            -- 'strong' | 'moderate' | 'weak' | NULL
    rule_match             JSONB        NOT NULL,           -- structured per-criterion report
    embedding_score        NUMERIC(5,4),                   -- cosine sim, null if rule-failed or embed-failed
    llm_judgment           JSONB,                           -- {score, evidence[], summary, concerns[], prompt_version}
    last_error             TEXT,
    attempt_count          INT          NOT NULL DEFAULT 0,
    next_attempt_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),

    scored_at              TIMESTAMPTZ,
    created_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, id),

    CONSTRAINT applications_status_check
        CHECK (status IN (
            'New','Scored','Excluded','EmbedFailed','JudgeFailed','Stale',
            'Shortlisted','Rejected','Interviewing','Hired'   -- slice 4 statuses, declared now for forward-compat
        )),
    CONSTRAINT applications_score_band_check
        CHECK (score_band IS NULL OR score_band IN ('strong','moderate','weak'))
) PARTITION BY LIST (tenant_id);

CREATE TABLE applications_default PARTITION OF applications DEFAULT;

-- Unique constraint: at most one Application per (Candidate, Intent) per tenant.
-- Partitioned-table rule: must include the partition key.
CREATE UNIQUE INDEX applications_uniq_idx
    ON applications (tenant_id, candidate_id, intent_id);

-- Recruiter list view: per-intent, sorted by overall_score desc (judged first).
CREATE INDEX applications_intent_score_idx
    ON applications (tenant_id, intent_id, overall_score DESC NULLS LAST);

-- Match worker poll index.
CREATE INDEX applications_match_pending_idx
    ON applications (tenant_id, next_attempt_at)
    WHERE status IN ('New');

-- Stale detection (slice 4 background reconciler will use this).
CREATE INDEX applications_stale_idx
    ON applications (tenant_id, intent_id)
    WHERE status = 'Stale';
