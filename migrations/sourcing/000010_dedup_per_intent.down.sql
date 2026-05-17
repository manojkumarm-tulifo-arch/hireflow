BEGIN;

-- WARNING: Reversing after new cross-intent uploads have happened will
-- violate the old uniqueness. We DELETE the offending rows (keeping the
-- earliest upload per (tenant, hash)) before re-adding the constraint.
-- The table has no id column; we use the composite key (tenant_id, content_hash)
-- to identify duplicates.
DELETE FROM resume_uploads_dedup
WHERE (tenant_id, content_hash) IN (
    SELECT tenant_id, content_hash FROM (
        SELECT tenant_id, content_hash, ROW_NUMBER() OVER (
            PARTITION BY tenant_id, content_hash
            ORDER BY created_at
        ) AS rn
        FROM resume_uploads_dedup
    ) ranked
    WHERE rn > 1
);

ALTER TABLE resume_uploads_dedup
    DROP CONSTRAINT resume_uploads_dedup_tenant_intent_hash_key;

-- Restore the original composite primary key (tenant_id, content_hash).
ALTER TABLE resume_uploads_dedup
    ADD CONSTRAINT resume_uploads_dedup_pkey
    PRIMARY KEY (tenant_id, content_hash);

ALTER TABLE resume_uploads_dedup
    DROP COLUMN intent_id;

COMMIT;
