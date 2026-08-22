-- billing's whole confirmed model (spec 0003), in two halves with a comment
-- marking the line between them. The line is what a replay must respect: every
-- table above it is rebuildable by `task replay:billing:<consumer>` and is
-- written only by internal/consumer; every table below it is an authoritative
-- record, written only by internal/handler, and no replay may touch it (AC-8).
--
-- Shape stated once: identifiers are uuid, money is bigint counting dong,
-- instants are timestamptz in UTC, a calendar day is date, and a state is text
-- with a CHECK. A projection carries recorded_at and updated_at; an owned
-- record carries its own creation instant. Nothing is ever deleted.
--
-- No projection here carries a foreign key, because two events with different
-- keys may arrive in either order (INV-6 guarantees order per key only): a
-- session may land before the class it names. A projection references another
-- context's entity by id and nothing more (INV-2, AC-6).
--
-- +goose Up

-- ---------------------------------------------------------------- projections
-- Rebuildable by a replay. Written only by internal/consumer, from the events
-- named above each table. No column here exists that no listed event writes
-- (AC-3), and no column here is derived or accumulated (AC-9, INV-7).

-- Written by teaching.student.registered, teaching.student.changed (name) and
-- teaching.student.removed (removed_at, from the envelope occurred_at, because
-- the event carries no timestamp of its own). billing keeps the name and never
-- the phone number: a past invoice must still print the name, which is why the
-- row is marked removed rather than deleted.
CREATE TABLE students (
    student_id  uuid        PRIMARY KEY,
    tutor_id    uuid        NOT NULL,
    name        text        NOT NULL,
    removed_at  timestamptz,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Written by teaching.class.created and teaching.class.changed. The label only:
-- the rate lives in class_rates below, because billing needs the dated history
-- teaching does not keep. An archived class is not projected here, because the
-- catalogue carries no archive event and billing still prints the label.
CREATE TABLE classes (
    class_id    uuid        PRIMARY KEY,
    tutor_id    uuid        NOT NULL,
    name        text        NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Written by teaching.session.scheduled, teaching.session.moved (starts_at,
-- ends_at, local_date) and teaching.session.cancelled (cancelled_at, from the
-- envelope occurred_at). There is no stored month column: the period a session
-- bills in is a range over local_date, because a session.moved would leave a
-- stored month stale (AC-9).
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

-- Written by teaching.attendance.marked, upserted on the key the event already
-- carries so a correction lands on the same row and a redelivery changes
-- nothing (AC-7). Only state = 'Present' bills; a missing row is unmarked,
-- which for money is the same answer as 'Absent'.
CREATE TABLE attendance (
    session_id  uuid        NOT NULL,
    student_id  uuid        NOT NULL,
    tutor_id    uuid        NOT NULL,
    state       text        NOT NULL,
    marked_at   timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, student_id),
    CONSTRAINT attendance_state_known CHECK (state IN ('Present', 'Absent'))
);

-- Written by teaching.roster.joined and teaching.roster.left, keyed by exactly
-- what those events carry. Coverage is the one predicate spec 0003 states for
-- all three services, inclusive at both ends:
--
--   effective_from <= $local_date AND (effective_to IS NULL OR $local_date <= effective_to)
--
-- ListBillableSessions and notifications' ListDigestSessions both use it
-- verbatim, because two services counting the same membership independently is
-- the one way they could disagree about who was in a class on a given day.
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

-- Seeded by teaching.class.created (its rate_effective_from) and appended by
-- teaching.class.rate.changed (its effective_from): two event fields writing
-- one column, which takes the name the ongoing event uses. This is billing's
-- own dated history, projected from teaching facts because teaching keeps only
-- the rate in force now.
--
-- The key is the pair rather than a surrogate id: the rate in force is the
-- newest row whose effective_from is on or before a session's local_date, so
-- two rows for one class on one date would make that question ambiguous, and a
-- corrected rate for the same date becomes an upsert. That key is also the only
-- index RateInForceOn needs, since the newest row on or before a date is a
-- backwards scan of it.
CREATE TABLE class_rates (
    class_id       uuid        NOT NULL,
    effective_from date        NOT NULL,
    tutor_id       uuid        NOT NULL,
    rate_amount    bigint      NOT NULL,
    currency       text        NOT NULL,
    recorded_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (class_id, effective_from),
    CONSTRAINT class_rates_currency_vnd  CHECK (currency = 'VND'),
    CONSTRAINT class_rates_rate_positive CHECK (rate_amount >= 0)
);

-- ------------------------------------------------------- authoritative records
-- Never rebuilt. Written only by internal/handler, never by a consumer, and no
-- replay may touch them (AC-8). Everything below this line is a record the
-- tutor or the month end run produced, not a copy of somebody else's fact.

-- The tutor's own invoice profile and bank details. It straddles the line and so
-- is called out: the consumer of identity.tutor.registered inserts the row with
-- ON CONFLICT DO NOTHING and writes no field, so a replay recreates a missing
-- row and can never overwrite bank details the tutor typed. No event carries
-- these fields, so they never reach the broker.
--
-- is_complete is the one completeness gate (AC-12), a generated column rather
-- than a rule in Go, so the month end refusal and the profile screen read the
-- same answer and Postgres recomputes it from the same row. All five fields are
-- required: the payment block is the whole point of the PDF, and an invoice
-- going out with a blank contact line is worse than a refusal the tutor can
-- clear in a minute. The Go side still lists which fields are missing, for the
-- profile_incomplete message.
CREATE TABLE invoice_profiles (
    tutor_id             uuid        PRIMARY KEY,
    legal_name           text,
    contact_line         text,
    bank_name            text,
    bank_account_number  text,
    bank_account_holder  text,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    is_complete          boolean     NOT NULL GENERATED ALWAYS AS (
            legal_name          IS NOT NULL AND legal_name          <> ''
        AND contact_line        IS NOT NULL AND contact_line        <> ''
        AND bank_name           IS NOT NULL AND bank_name           <> ''
        AND bank_account_number IS NOT NULL AND bank_account_number <> ''
        AND bank_account_holder IS NOT NULL AND bank_account_holder <> ''
    ) STORED
);

-- One counter per tutor and period year, bumped by one statement inside the
-- run's transaction. A row of its own rather than a max over live invoices,
-- because a voided invoice keeps its number spent. The year in the number is the
-- period_year, not the year the run happened, so a December period invoiced in
-- January still reads as a December year invoice.
CREATE TABLE invoice_number_counters (
    tutor_id      uuid        NOT NULL,
    period_year   integer     NOT NULL,
    last_sequence integer     NOT NULL,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tutor_id, period_year),
    CONSTRAINT invoice_number_counters_sequence_started CHECK (last_sequence >= 1)
);

-- One month end run per tutor, period and generation. The unique constraint is
-- what makes a double press safe, not the lookup above it: two concurrent runs
-- attempt the same generation and exactly one commits, and the loser reads the
-- winner's invoices. A void stamps superseded_at on the newest row for the
-- period, which raises the allowed generation by exactly one.
CREATE TABLE billing_runs (
    billing_run_id uuid        PRIMARY KEY,
    tutor_id       uuid        NOT NULL,
    period_year    integer     NOT NULL,
    period_month   integer     NOT NULL,
    generation     integer     NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    superseded_at  timestamptz,
    CONSTRAINT billing_runs_one_per_generation UNIQUE (tutor_id, period_year, period_month, generation),
    CONSTRAINT billing_runs_month_known       CHECK (period_month BETWEEN 1 AND 12),
    CONSTRAINT billing_runs_generation_start  CHECK (generation >= 1)
);

-- The invoice, and the frozen render block that makes a re render years later
-- print the file the tutor already sent (INV-9). student_name and the five
-- payee_ columns are copied from students and invoice_profiles at issue: the run
-- refuses unless the profile is complete, so they can never be empty here, and a
-- later bank account change must not reach backwards into an issued invoice.
--
-- There is deliberately no CHECK (total_amount > 0): a rate of zero is legal
-- under class_rates_rate_positive, and a genuinely free lesson should still
-- produce a line.
CREATE TABLE invoices (
    invoice_id                uuid        PRIMARY KEY,
    tutor_id                  uuid        NOT NULL,
    billing_run_id            uuid        NOT NULL REFERENCES billing_runs (billing_run_id),
    student_id                uuid        NOT NULL,
    invoice_number            text        NOT NULL,
    period_year               integer     NOT NULL,
    period_month              integer     NOT NULL,
    total_amount              bigint      NOT NULL,
    currency                  text        NOT NULL,
    issued_at                 timestamptz NOT NULL,
    pdf_location              text,
    paid_at                   timestamptz,
    voided_at                 timestamptz,
    void_reason               text,
    replaces_invoice_id       uuid        REFERENCES invoices (invoice_id),
    student_name              text        NOT NULL,
    payee_legal_name          text        NOT NULL,
    payee_contact_line        text        NOT NULL,
    payee_bank_name           text        NOT NULL,
    payee_bank_account_number text        NOT NULL,
    payee_bank_account_holder text        NOT NULL,
    updated_at                timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT invoices_number_once     UNIQUE (tutor_id, invoice_number),
    CONSTRAINT invoices_currency_vnd    CHECK (currency = 'VND'),
    CONSTRAINT invoices_void_has_reason CHECK ((voided_at IS NULL) = (void_reason IS NULL)),
    CONSTRAINT invoices_void_reason_said CHECK (void_reason IS NULL OR void_reason <> '')
);

CREATE INDEX invoices_period_idx ON invoices (tutor_id, period_year, period_month);

-- One line per billable session on an invoice. It keeps both the frozen values
-- and the session_id they came from: the id is for tracing during a dispute and
-- is never joined at render time. amount sits beside rate_amount even though
-- they are equal today, because it is frozen money on an issued document rather
-- than a cached calculation.
CREATE TABLE invoice_lines (
    invoice_line_id uuid   PRIMARY KEY,
    invoice_id      uuid   NOT NULL REFERENCES invoices (invoice_id),
    tutor_id        uuid   NOT NULL,
    session_id      uuid   NOT NULL,
    session_date    date   NOT NULL,
    class_name      text   NOT NULL,
    rate_amount     bigint NOT NULL,
    amount          bigint NOT NULL,
    CONSTRAINT invoice_lines_session_once UNIQUE (invoice_id, session_id)
);

CREATE INDEX invoice_lines_invoice_idx ON invoice_lines (invoice_id);

-- +goose Down
DROP TABLE invoice_lines;
DROP TABLE invoices;
DROP TABLE billing_runs;
DROP TABLE invoice_number_counters;
DROP TABLE invoice_profiles;
DROP TABLE class_rates;
DROP TABLE roster_periods;
DROP TABLE attendance;
DROP TABLE sessions;
DROP TABLE classes;
DROP TABLE students;
