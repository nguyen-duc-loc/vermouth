-- What the daily digest needs, per spec 0003. recipients (00002) is unchanged.
-- Everything added here is a projection except digest_runs, which is the one
-- record notifications owns and no replay may touch.
--
-- notifications owns no business truth: it never generates a schedule and never
-- decides invoice content. It keeps no student name either, because the digest
-- renders a count and never a name, and no foreign key or query crosses a
-- service boundary (INV-2, AC-6).
--
-- +goose Up

-- ---------------------------------------------------------------- projections
-- Written only by internal/consumer, from the events named above each table, and
-- rebuildable by `task replay:notifications:<consumer>`.

-- Written by teaching.class.created and teaching.class.changed. The label only:
-- the digest prints the class name and nothing else about the class.
CREATE TABLE classes (
    class_id    uuid        PRIMARY KEY,
    tutor_id    uuid        NOT NULL,
    name        text        NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Written by teaching.session.scheduled, teaching.session.moved (starts_at,
-- ends_at, local_date) and teaching.session.cancelled (cancelled_at, from the
-- envelope occurred_at).
--
-- Nothing prunes this table. A tutor may schedule weeks ahead, so a table
-- holding only today would need someone to put tomorrow in it and no event does
-- that. Retention is a follow up in spec 0003, not an invented rule here.
CREATE TABLE sessions (
    session_id   uuid        PRIMARY KEY,
    class_id     uuid        NOT NULL,
    tutor_id     uuid        NOT NULL,
    starts_at    timestamptz NOT NULL,
    ends_at      timestamptz NOT NULL,
    local_date   date        NOT NULL,
    cancelled_at timestamptz,
    recorded_at  timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_day_idx ON sessions (tutor_id, local_date) WHERE cancelled_at IS NULL;

-- Written by teaching.roster.joined and teaching.roster.left, keyed by exactly
-- what those events carry.
--
-- There is no stored roster count, which is a deliberate refinement of spec
-- 0001's wording rather than a contradiction of it. An integer bumped up and
-- down is the accumulate in arrival order shape INV-7 forbids: a redelivery
-- double counts and a replay from the beginning multiplies the number, and
-- neither announces itself. One membership row per pair makes both harmless, and
-- the digest counts the rows whose period covers its local_date, by the one
-- coverage predicate spec 0003 states for all three services, inclusive at both
-- ends:
--
--   effective_from <= $local_date AND (effective_to IS NULL OR $local_date <= effective_to)
CREATE TABLE roster_periods (
    class_id       uuid        NOT NULL,
    student_id     uuid        NOT NULL,
    effective_from date        NOT NULL,
    tutor_id       uuid        NOT NULL,
    effective_to   date,
    recorded_at    timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (class_id, student_id, effective_from),
    CONSTRAINT roster_periods_ends_after_starts CHECK (effective_to IS NULL OR effective_to >= effective_from)
);

CREATE UNIQUE INDEX roster_periods_one_open_idx
    ON roster_periods (class_id, student_id) WHERE effective_to IS NULL;

-- ------------------------------------------------------- authoritative record
-- Written by the scheduler only, never by a consumer, and never rebuilt: a
-- replay must not resend a morning's email.

-- One digest per tutor per local day, which the primary key is what makes true.
-- It is also the lock the single replica leans on: a repeated tick is a unique
-- violation rather than a second email (STK-23). Feature 5 owns the advisory
-- lock that would let a second replica exist at all.
CREATE TABLE digest_runs (
    tutor_id   uuid        NOT NULL,
    local_date date        NOT NULL,
    state      text        NOT NULL,
    attempts   integer     NOT NULL DEFAULT 0,
    last_error text        NOT NULL DEFAULT '',
    sent_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tutor_id, local_date),
    CONSTRAINT digest_runs_state_known CHECK (state IN ('Pending', 'Sent', 'Failed'))
);

-- +goose Down
DROP TABLE digest_runs;
DROP TABLE roster_periods;
DROP TABLE sessions;
DROP TABLE classes;
