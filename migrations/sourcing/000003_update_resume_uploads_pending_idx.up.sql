-- Slice 2 makes 'Extracted' an intermediate state (worker re-claims it to run
-- the parsing stage). Drop the slice-1 partial index whose WHERE clause excluded
-- 'Extracted', and replace it with one that only excludes the true terminal
-- states: Parsed, Scored, Failed, Quarantined.
DROP INDEX IF EXISTS resume_uploads_pending_idx;

CREATE INDEX resume_uploads_pending_idx
    ON resume_uploads (next_attempt_at)
    WHERE status NOT IN ('Parsed','Scored','Failed','Quarantined');
