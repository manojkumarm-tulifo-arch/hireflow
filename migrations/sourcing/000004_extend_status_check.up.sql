-- Slice 2 introduces `Parsed` as the new terminal status. The status CHECK
-- constraint in 000001 listed all then-known statuses but omitted Parsed.
ALTER TABLE resume_uploads
    DROP CONSTRAINT IF EXISTS resume_uploads_status_check;
ALTER TABLE resume_uploads
    ADD  CONSTRAINT resume_uploads_status_check
    CHECK (status IN (
        'Pending','Scanning','Extracting','Parsing','Embedding','Scoring',
        'Extracted','Parsed','Scored','Failed','Quarantined'
    ));
