-- Hand written SQL for billing, compiled to typed Go by sqlc (STK-3). No ORM,
-- and no SQL assembled by string concatenation at runtime.
--
-- Every statement here names tutor_id (INV-8, AC-4), which comes from the sub
-- claim of the verified token and never from an input. The file keeps the two
-- halves of 00002_billing_model.sql in the same order, with the same line
-- between them: the projection statements above it are run only by
-- internal/consumer, the authoritative ones below it only by internal/handler.

-- ---------------------------------------------------------------- projections
-- Every one of these is an upsert on the identifying key the event already
-- carries, so a redelivery and a full replay both land on the same row and
-- change nothing the second time (AC-7, INV-5, INV-11).

-- name: UpsertStudent :exec
INSERT INTO students (student_id, tutor_id, name)
VALUES ($1, $2, $3)
ON CONFLICT (student_id) DO UPDATE
SET name       = excluded.name,
    updated_at = now()
WHERE students.tutor_id = excluded.tutor_id;

-- MarkStudentRemoved stamps the end from the envelope occurred_at, because
-- teaching.student.removed carries no timestamp of its own (INV-4). The first end
-- wins, so a replay leaves the value it already recorded.
-- name: MarkStudentRemoved :exec
UPDATE students
SET removed_at = $3,
    updated_at = now()
WHERE tutor_id = $1
  AND student_id = $2
  AND removed_at IS NULL;

-- name: UpsertClass :exec
INSERT INTO classes (class_id, tutor_id, name)
VALUES ($1, $2, $3)
ON CONFLICT (class_id) DO UPDATE
SET name       = excluded.name,
    updated_at = now()
WHERE classes.tutor_id = excluded.tutor_id;

-- UpsertSession serves teaching.session.scheduled and teaching.session.moved
-- alike: a move is the same three columns at new values.
-- name: UpsertSession :exec
INSERT INTO sessions (session_id, class_id, tutor_id, starts_at, ends_at, local_date)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (session_id) DO UPDATE
SET starts_at  = excluded.starts_at,
    ends_at    = excluded.ends_at,
    local_date = excluded.local_date,
    updated_at = now()
WHERE sessions.tutor_id = excluded.tutor_id;

-- name: MarkSessionCancelled :exec
UPDATE sessions
SET cancelled_at = $3,
    updated_at   = now()
WHERE tutor_id = $1
  AND session_id = $2
  AND cancelled_at IS NULL;

-- name: UpsertAttendance :exec
INSERT INTO attendance (session_id, student_id, tutor_id, state, marked_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (session_id, student_id) DO UPDATE
SET state      = excluded.state,
    marked_at  = excluded.marked_at,
    updated_at = now()
WHERE attendance.tutor_id = excluded.tutor_id;

-- name: OpenRosterPeriod :exec
INSERT INTO roster_periods (class_id, student_id, effective_from, tutor_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (class_id, student_id, effective_from) DO NOTHING;

-- name: CloseRosterPeriod :exec
UPDATE roster_periods
SET effective_to = $4,
    updated_at   = now()
WHERE tutor_id = $1
  AND class_id = $2
  AND student_id = $3
  AND effective_to IS NULL;

-- UpsertClassRate appends the dated history billing owns. teaching.class.created
-- seeds the first row from its rate_effective_from and
-- teaching.class.rate.changed appends from its effective_from: two event fields
-- writing one column. A corrected rate for the same date is an upsert, never a
-- second row, because two rows for one class on one date would make "the rate in
-- force" ambiguous.
-- name: UpsertClassRate :exec
INSERT INTO class_rates (class_id, effective_from, tutor_id, rate_amount, currency)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (class_id, effective_from) DO UPDATE
SET rate_amount = excluded.rate_amount,
    currency    = excluded.currency
WHERE class_rates.tutor_id = excluded.tutor_id;

-- ListBillableSessions is the month end run's one read of the projections. The
-- period is a range over local_date, because there is no stored month column a
-- session.moved could leave stale (AC-9). Only state = 'Present' bills: a
-- missing attendance row is unmarked, which for money is the same answer as
-- 'Absent'. Membership uses the one coverage predicate spec 0003 states for all
-- three services, inclusive at both ends, so this and notifications'
-- ListDigestSessions can never disagree about who was in a class on a day.
--
-- The rate joins LATERAL and LEFT, and rate_known says whether it was there: a
-- session whose class has no rate row on or before its date comes back with
-- rate_known false rather than vanishing, so the run refuses with rate_missing
-- instead of silently billing less. It is a flag rather than a null rate because
-- a rate of zero is legal (a genuinely free lesson), so zero and missing have to
-- be two different answers. The rate itself comes back coalesced, so the type
-- stays plain int64 dong and rate_known is the only place the question "was there
-- a rate at all" is answered.
-- name: ListBillableSessions :many
SELECT s.session_id,
       s.local_date,
       s.class_id,
       c.name AS class_name,
       a.student_id,
       st.name AS student_name,
       (r.rate_amount IS NOT NULL)::boolean AS rate_known,
       coalesce(r.rate_amount, 0)::bigint AS rate_amount,
       coalesce(r.currency, '')::text     AS currency
FROM sessions s
JOIN attendance a
  ON a.session_id = s.session_id
 AND a.tutor_id = s.tutor_id
JOIN students st
  ON st.student_id = a.student_id
 AND st.tutor_id = s.tutor_id
JOIN classes c
  ON c.class_id = s.class_id
 AND c.tutor_id = s.tutor_id
JOIN roster_periods rp
  ON rp.class_id = s.class_id
 AND rp.student_id = a.student_id
 AND rp.tutor_id = s.tutor_id
 AND rp.effective_from <= s.local_date
 AND (rp.effective_to IS NULL OR s.local_date <= rp.effective_to)
LEFT JOIN LATERAL (
    SELECT cr.rate_amount, cr.currency
    FROM class_rates cr
    WHERE cr.class_id = s.class_id
      AND cr.tutor_id = s.tutor_id
      AND cr.effective_from <= s.local_date
    ORDER BY cr.effective_from DESC
    LIMIT 1
) r ON true
WHERE s.tutor_id = sqlc.arg(tutor_id)
  AND s.local_date >= sqlc.arg(period_start)
  AND s.local_date <= sqlc.arg(period_end)
  AND s.cancelled_at IS NULL
  AND a.state = 'Present'
ORDER BY a.student_id, s.local_date, s.session_id;

-- RateInForceOn is the newest row on or before the date, which the primary key
-- already orders: a backwards scan of one index, no extra descending index. An
-- absent row is what the run turns into rate_missing.
-- name: RateInForceOn :one
SELECT class_id, effective_from, tutor_id, rate_amount, currency
FROM class_rates
WHERE tutor_id = $1
  AND class_id = $2
  AND effective_from <= $3
ORDER BY effective_from DESC
LIMIT 1;

-- ------------------------------------------------------- authoritative records
-- No replay may rewrite anything below this line (AC-8).

-- SeedInvoiceProfile is the one statement a consumer runs on an authoritative
-- table, and it writes no field: identity.tutor.registered only causes the empty
-- row to exist. That is what lets a replay recreate a missing row without ever
-- overwriting bank details the tutor typed.
-- name: SeedInvoiceProfile :exec
INSERT INTO invoice_profiles (tutor_id)
VALUES ($1)
ON CONFLICT (tutor_id) DO NOTHING;

-- SaveInvoiceProfile is the tutor's own write, from the profile screen. Every
-- field here is theirs; no event carries any of it, so bank details never reach
-- the broker.
-- name: SaveInvoiceProfile :one
INSERT INTO invoice_profiles (tutor_id, legal_name, contact_line, bank_name, bank_account_number, bank_account_holder)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (tutor_id) DO UPDATE
SET legal_name          = excluded.legal_name,
    contact_line        = excluded.contact_line,
    bank_name           = excluded.bank_name,
    bank_account_number = excluded.bank_account_number,
    bank_account_holder = excluded.bank_account_holder,
    updated_at          = now()
WHERE invoice_profiles.tutor_id = excluded.tutor_id
RETURNING tutor_id, legal_name, contact_line, bank_name, bank_account_number,
          bank_account_holder, created_at, updated_at, is_complete;

-- GetInvoiceProfile returns is_complete beside the fields, so the month end
-- refusal and the profile screen read the one gate rather than each testing its
-- own field list (AC-12).
-- name: GetInvoiceProfile :one
-- The column order is the table's own, so sqlc hands back the one
-- invoice_profiles row type rather than a second shape of the same row.
SELECT tutor_id, legal_name, contact_line, bank_name, bank_account_number,
       bank_account_holder, created_at, updated_at, is_complete
FROM invoice_profiles
WHERE tutor_id = $1;

-- TakeNextInvoiceNumber bumps the counter inside the run's transaction and
-- returns the number it just took. A row of its own rather than a max over live
-- invoices, because a voided invoice keeps its number spent. Two concurrent
-- takes serialise on the row and can never return the same sequence.
-- name: TakeNextInvoiceNumber :one
INSERT INTO invoice_number_counters (tutor_id, period_year, last_sequence)
VALUES ($1, $2, 1)
ON CONFLICT (tutor_id, period_year) DO UPDATE
SET last_sequence = invoice_number_counters.last_sequence + 1,
    updated_at    = now()
RETURNING last_sequence;

-- name: GetInvoiceNumberCounter :one
SELECT tutor_id, period_year, last_sequence, updated_at
FROM invoice_number_counters
WHERE tutor_id = $1
  AND period_year = $2;

-- CurrentBillingRunGeneration only tells the handler which number to attempt. It
-- needs no lock and no SELECT ... FOR UPDATE: the unique constraint on
-- (tutor_id, period_year, period_month, generation) is what actually decides a
-- race, and the loser reads the winner's invoices. An absent row means no run
-- yet, so the next generation is 1.
-- name: CurrentBillingRunGeneration :one
SELECT billing_run_id, generation, (superseded_at IS NOT NULL)::boolean AS superseded
FROM billing_runs
WHERE tutor_id = $1
  AND period_year = $2
  AND period_month = $3
ORDER BY generation DESC
LIMIT 1;

-- name: InsertBillingRun :one
INSERT INTO billing_runs (billing_run_id, tutor_id, period_year, period_month, generation)
VALUES ($1, $2, $3, $4, $5)
RETURNING billing_run_id, tutor_id, period_year, period_month, generation,
          created_at, superseded_at;

-- name: GetBillingRun :one
SELECT billing_run_id, tutor_id, period_year, period_month, generation,
       created_at, superseded_at
FROM billing_runs
WHERE tutor_id = $1
  AND billing_run_id = $2;

-- InsertInvoice freezes the render block at issue: the student name and the five
-- payee fields are copied in, so a re render years later prints the file the
-- tutor already sent and a later bank account change cannot reach backwards into
-- it (INV-9).
-- name: InsertInvoice :one
INSERT INTO invoices (invoice_id, tutor_id, billing_run_id, student_id, invoice_number,
                      period_year, period_month, total_amount, currency, issued_at,
                      replaces_invoice_id, student_name, payee_legal_name, payee_contact_line,
                      payee_bank_name, payee_bank_account_number, payee_bank_account_holder)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
RETURNING invoice_id, tutor_id, billing_run_id, student_id, invoice_number, period_year,
          period_month, total_amount, currency, issued_at, pdf_location, paid_at, voided_at,
          void_reason, replaces_invoice_id, student_name, payee_legal_name, payee_contact_line,
          payee_bank_name, payee_bank_account_number, payee_bank_account_holder, updated_at;

-- name: GetInvoice :one
SELECT invoice_id, tutor_id, billing_run_id, student_id, invoice_number, period_year,
       period_month, total_amount, currency, issued_at, pdf_location, paid_at, voided_at,
       void_reason, replaces_invoice_id, student_name, payee_legal_name, payee_contact_line,
       payee_bank_name, payee_bank_account_number, payee_bank_account_holder, updated_at
FROM invoices
WHERE tutor_id = $1
  AND invoice_id = $2;

-- name: ListInvoicesForPeriod :many
SELECT invoice_id, tutor_id, billing_run_id, student_id, invoice_number, period_year,
       period_month, total_amount, currency, issued_at, paid_at, voided_at, student_name
FROM invoices
WHERE tutor_id = $1
  AND period_year = $2
  AND period_month = $3
ORDER BY invoice_number;

-- InsertInvoiceLine keeps both the frozen values and the session_id they came
-- from. The id is for tracing during a dispute and is never joined at render
-- time.
-- name: InsertInvoiceLine :one
INSERT INTO invoice_lines (invoice_line_id, invoice_id, tutor_id, session_id, session_date,
                           class_name, rate_amount, amount)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING invoice_line_id, invoice_id, tutor_id, session_id, session_date, class_name,
          rate_amount, amount;

-- name: ListInvoiceLines :many
SELECT invoice_line_id, invoice_id, tutor_id, session_id, session_date, class_name,
       rate_amount, amount
FROM invoice_lines
WHERE tutor_id = $1
  AND invoice_id = $2
ORDER BY session_date, session_id;
