ALTER TABLE resume_uploads
    DROP CONSTRAINT IF EXISTS resume_uploads_candidate_fk;

DROP TABLE IF EXISTS candidates;
