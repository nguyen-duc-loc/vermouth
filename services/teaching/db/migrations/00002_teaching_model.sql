-- teaching's whole confirmed model (spec 0003). Everything here is teaching's
-- own truth: it publishes these facts and holds a copy of nothing. Other
-- services keep projections of some of these rows, fed only by the events in
-- spec 0001's catalogue, and no foreign key or query crosses that line (INV-2).
--
-- Shape stated once: identifiers are uuid (UUIDv7 generated in Go), money is
-- bigint counting dong, instants are timestamptz in UTC, a calendar day is
-- date, and a state is text with a CHECK rather than a Postgres enum. Nothing
-- is ever deleted: what can end carries a nullable end timestamp, and every
-- list filters on that being empty (AC-11).
--
-- +goose Up

-- Owned by teaching: the student record, which is a name and a phone number.
-- The phone number is stored exactly as typed and is deliberately absent from
-- teaching.student.registered, so it never crosses a service boundary: no
-- invoice and no digest needs it.
CREATE TABLE students (
    student_id uuid        PRIMARY KEY,
    tutor_id   uuid        NOT NULL,
    name       text        NOT NULL,
    phone      text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    removed_at timestamptz
);

CREATE INDEX students_live_idx ON students (tutor_id) WHERE removed_at IS NULL;

-- Owned by teaching: the class, including the one rate in force on it now.
-- Dated rate history belongs to billing, appended from
-- teaching.class.rate.changed, because teaching never bills anything.
CREATE TABLE classes (
    class_id            uuid        PRIMARY KEY,
    tutor_id            uuid        NOT NULL,
    name                text        NOT NULL,
    rate_amount         bigint      NOT NULL,
    currency            text        NOT NULL,
    rate_effective_from date        NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    archived_at         timestamptz,
    CONSTRAINT classes_currency_vnd  CHECK (currency = 'VND'),
    CONSTRAINT classes_rate_positive CHECK (rate_amount >= 0)
);

CREATE INDEX classes_live_idx ON classes (tutor_id) WHERE archived_at IS NULL;

-- Owned by teaching: the session, always a concrete row rather than a shape
-- computed from a rule at read time, so cancelling or moving one is an ordinary
-- update to an ordinary row. local_date is computed here once, in the tutor's
-- timezone, and carried on the event so no consumer recomputes it.
--
-- schedule_rule_id is nullable and carries no foreign key on purpose: feature
-- 10 owns the recurrence rule table and adds the reference then.
CREATE TABLE sessions (
    session_id       uuid        PRIMARY KEY,
    class_id         uuid        NOT NULL REFERENCES classes (class_id),
    tutor_id         uuid        NOT NULL,
    starts_at        timestamptz NOT NULL,
    ends_at          timestamptz NOT NULL,
    local_date       date        NOT NULL,
    schedule_rule_id uuid,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    cancelled_at     timestamptz,
    CONSTRAINT sessions_ends_after_starts CHECK (ends_at > starts_at)
);

CREATE INDEX sessions_day_idx   ON sessions (tutor_id, local_date) WHERE cancelled_at IS NULL;
CREATE INDEX sessions_class_idx ON sessions (class_id);

-- Owned by teaching: roster membership as a dated period, keyed by exactly
-- what the roster events carry, so no service needs a roster_period_id no
-- event names. teaching.roster.left closes the one open row for the pair, and
-- rejoining opens a new row rather than reopening the closed one.
--
-- effective_from is the first day of membership and effective_to the last, both
-- inclusive, so effective_to = effective_from is a one day membership. The one
-- coverage predicate every service copies verbatim:
--
--   effective_from <= $local_date AND (effective_to IS NULL OR $local_date <= effective_to)
--
-- The partial unique index is what makes "the one open row" true rather than
-- hoped for.
CREATE TABLE roster_periods (
    class_id       uuid        NOT NULL REFERENCES classes (class_id),
    student_id     uuid        NOT NULL REFERENCES students (student_id),
    effective_from date        NOT NULL,
    tutor_id       uuid        NOT NULL,
    effective_to   date,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (class_id, student_id, effective_from),
    CONSTRAINT roster_periods_ends_after_starts CHECK (effective_to IS NULL OR effective_to >= effective_from)
);

CREATE UNIQUE INDEX roster_periods_one_open_idx
    ON roster_periods (class_id, student_id) WHERE effective_to IS NULL;

-- Owned by teaching: attendance truth, one row per session and student, so a
-- correction updates it in place and per key ordering on session_id means the
-- last mark is the last one billing sees (INV-6).
--
-- class_id is deliberately absent even though teaching.attendance.marked
-- carries it: it is the session's class, and a stored copy of a derivable value
-- is one more thing that can disagree. The publisher reads it from the session
-- row in the same transaction.
CREATE TABLE attendance (
    session_id uuid        NOT NULL REFERENCES sessions (session_id),
    student_id uuid        NOT NULL REFERENCES students (student_id),
    tutor_id   uuid        NOT NULL,
    state      text        NOT NULL,
    marked_at  timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, student_id),
    CONSTRAINT attendance_state_known CHECK (state IN ('Present', 'Absent'))
);

-- +goose Down
DROP TABLE attendance;
DROP TABLE roster_periods;
DROP TABLE sessions;
DROP TABLE classes;
DROP TABLE students;
