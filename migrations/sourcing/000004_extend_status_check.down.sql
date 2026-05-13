ALTER TABLE resume_uploads
    DROP CONSTRAINT IF EXISTS resume_uploads_status_check;
ALTER TABLE resume_uploads
    ADD  CONSTRAINT resume_uploads_status_check
    CHECK (status IN (
        'Pending','Scanning','Extracting','Parsing','Embedding','Scoring',
        'Extracted','Scored','Failed','Quarantined'
    ));
