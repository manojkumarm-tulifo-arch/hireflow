-- Restore the slice-1 partial index that excluded 'Extracted'.
DROP INDEX IF EXISTS resume_uploads_pending_idx;

CREATE INDEX resume_uploads_pending_idx
    ON resume_uploads (next_attempt_at)
    WHERE status NOT IN ('Extracted','Scored','Failed','Quarantined');
