BEGIN;

-- Add intent_id column, nullable while we backfill.
ALTER TABLE resume_uploads_dedup
    ADD COLUMN intent_id UUID;

-- Backfill from resume_uploads. Each dedup row maps to the intent of the
-- ORIGINAL upload that won the dedup; that's the only intent_id the row
-- ever knew about. Once backfilled, the column becomes NOT NULL.
UPDATE resume_uploads_dedup d
SET intent_id = (
    SELECT u.intent_id
    FROM resume_uploads u
    WHERE u.id = d.upload_id
);

-- Defensive: drop any orphan rows whose upload row no longer exists.
DELETE FROM resume_uploads_dedup WHERE intent_id IS NULL;

ALTER TABLE resume_uploads_dedup
    ALTER COLUMN intent_id SET NOT NULL;

-- Drop the old composite PRIMARY KEY that enforced global-per-tenant uniqueness.
-- The PK was (tenant_id, content_hash) — replace with per-intent key.
ALTER TABLE resume_uploads_dedup
    DROP CONSTRAINT resume_uploads_dedup_pkey;

-- New primary key is (tenant_id, intent_id, content_hash): the same resume
-- is allowed for different intents; only re-uploading the SAME resume to
-- the SAME intent is flagged as a duplicate.
ALTER TABLE resume_uploads_dedup
    ADD CONSTRAINT resume_uploads_dedup_tenant_intent_hash_key
    UNIQUE (tenant_id, intent_id, content_hash);

COMMIT;
