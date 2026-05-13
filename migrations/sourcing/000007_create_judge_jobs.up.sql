CREATE TABLE judge_jobs (
    id              UUID         PRIMARY KEY,
    tenant_id       UUID         NOT NULL,
    application_id  UUID         NOT NULL,
    intent_id       UUID         NOT NULL,
    coarse_score    NUMERIC(7,4) NOT NULL,
    status          TEXT         NOT NULL DEFAULT 'Pending',
    attempt_count   INT          NOT NULL DEFAULT 0,
    last_error      TEXT,
    next_attempt_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    enqueued_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ,

    CONSTRAINT judge_jobs_status_check
        CHECK (status IN ('Pending','Running','Done','Failed'))
);

CREATE INDEX judge_jobs_pending_idx
    ON judge_jobs (next_attempt_at)
    WHERE status IN ('Pending','Running');

CREATE INDEX judge_jobs_intent_idx ON judge_jobs (intent_id);
