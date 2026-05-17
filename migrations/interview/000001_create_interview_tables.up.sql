-- intent_loops: per-intent template defining the sequence of rounds for the
-- interview process. Recruiter sets it via UpsertLoopTemplate. If absent
-- when an InterviewProcess is created, the StartInterviewProcess command
-- uses a hardcoded default (screen → technical → bar_raiser).
CREATE TABLE intent_loops (
    id          UUID         PRIMARY KEY,
    tenant_id   UUID         NOT NULL,
    intent_id   UUID         NOT NULL,
    rounds      JSONB        NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),

    UNIQUE (tenant_id, intent_id),
    CONSTRAINT intent_loops_rounds_nonempty CHECK (jsonb_array_length(rounds) > 0)
);

-- interview_processes: one per shortlisted application.
CREATE TABLE interview_processes (
    id              UUID         PRIMARY KEY,
    tenant_id       UUID         NOT NULL,
    application_id  UUID         NOT NULL,
    candidate_id    UUID         NOT NULL,
    intent_id       UUID         NOT NULL,
    status          TEXT         NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),

    UNIQUE (tenant_id, application_id),
    CONSTRAINT interview_processes_status_valid
        CHECK (status IN ('New','InProgress','Completed','Cancelled'))
);

CREATE INDEX interview_processes_intent_idx
    ON interview_processes (tenant_id, intent_id, status, created_at DESC);

-- interview_rounds: one per round per process.
CREATE TABLE interview_rounds (
    id               UUID         PRIMARY KEY,
    tenant_id        UUID         NOT NULL,
    process_id       UUID         NOT NULL,
    kind             TEXT         NOT NULL,
    sequence         INT          NOT NULL,
    status           TEXT         NOT NULL,
    questions        JSONB,
    attempt_count    INT          NOT NULL DEFAULT 0,
    last_error       TEXT         NOT NULL DEFAULT '',
    next_attempt_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT interview_rounds_kind_valid
        CHECK (kind IN ('screen','technical','system_design','behavioral','bar_raiser')),
    CONSTRAINT interview_rounds_status_valid
        CHECK (status IN ('Pending','QuestionsReady','Completed','Skipped','GenerationFailed')),
    CONSTRAINT interview_rounds_sequence_positive CHECK (sequence > 0),
    UNIQUE (tenant_id, process_id, sequence)
);

-- Worker poll index — claim next Pending round whose backoff has elapsed.
CREATE INDEX interview_rounds_pending_idx
    ON interview_rounds (next_attempt_at)
    WHERE status = 'Pending';

-- interview_feedback: append-only. Multiple rows per round allowed (panel).
CREATE TABLE interview_feedback (
    id                 UUID         PRIMARY KEY,
    tenant_id          UUID         NOT NULL,
    round_id           UUID         NOT NULL,
    interviewer_name   TEXT         NOT NULL,
    interviewer_email  TEXT         NOT NULL DEFAULT '',
    decision           TEXT         NOT NULL,
    notes              TEXT         NOT NULL DEFAULT '',
    submitted_by       UUID         NOT NULL,
    submitted_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT interview_feedback_decision_valid
        CHECK (decision IN ('strong_yes','yes','mixed','no','strong_no')),
    CONSTRAINT interview_feedback_interviewer_name_nonempty
        CHECK (length(interviewer_name) > 0)
);

CREATE INDEX interview_feedback_round_idx
    ON interview_feedback (tenant_id, round_id, submitted_at DESC);

-- interview_outbox: same shape as sourcing_outbox. Dispatched by the
-- interview context's own OutboxDispatcher.
CREATE TABLE interview_outbox (
    id             BIGSERIAL    PRIMARY KEY,
    event_name     TEXT         NOT NULL,
    aggregate_id   UUID         NOT NULL,
    tenant_id      UUID         NOT NULL,
    payload        JSONB        NOT NULL,
    occurred_at    TIMESTAMPTZ  NOT NULL,
    dispatched_at  TIMESTAMPTZ
);

CREATE INDEX interview_outbox_pending_idx
    ON interview_outbox (id)
    WHERE dispatched_at IS NULL;
