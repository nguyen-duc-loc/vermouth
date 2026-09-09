# 0003. Data model and data ownership per service

**Date**: 2026-08-22
**Status**: Accepted

## Summary

Every entity in Vermouth gets one owning service and one owning table, and every copied fact names
the event that carries it. Feature 4 lands that model through service migrations and typed queries,
with additive teaching and billing integrity migrations because the first model files already ran in
development. A projection is keyed by event identity and can be replayed, while an authoritative
record is never rebuilt. Nothing is deleted, and no projection stores a number it can count at read
time.

## Requirements

**User stories**:

- As the tutor, I want my classes, students, attendance, and invoices stored so that the month end
  bill comes out right and the same numbers come out again months later.
- As the engineer, I want one owning table per entity so that a later feature adds a column to a
  known place instead of inventing a second home for the same fact.
- As the engineer, I want every copied field traced to the event that carries it so that a wrong
  consumer is visible while reading the schema.

**Acceptance criteria** (the contract, each criterion is IDed and independently checkable):

- **AC-1**: Every entity in the ownership table below has exactly one owning service and one owning
  table, and no second service holds an authoritative home for the same entity.
- **AC-2**: `task migrate:up` applies every service's migrations cleanly from an empty database, and
  the `goose` down step of each new migration reverses it completely.
- **AC-3**: Every projection fact column is named with the event that writes it, in the migration
  comment above its table, and no fact column exists that no listed event writes. Projection
  bookkeeping timestamps follow the shared transaction clock rule below.
- **AC-4**: Every table carries `tutor_id`, either as a column or inside its primary key, and every
  statement in `db/queries/` that reads a tutor scoped table names `tutor_id` in its `WHERE` (INV-8).
  Every same service reference also matches `tutor_id`, through a composite foreign key for owned
  tables or an explicit tenant match for projections.
- **AC-5**: `sqlc generate` compiles every service's queries against that service's own migrations,
  and `task build` succeeds with no SQL outside `db/queries/` (STK-3).
- **AC-6**: No foreign key, view, or query crosses a service boundary. A projection row references
  another context's entity by id only (INV-2).
- **AC-7**: A redelivered or replayed event lands on the same projection row, because every
  projection table's primary key is the identifying key its events already carry, so the consumer
  upsert is idempotent (INV-5, INV-11).
- **AC-8**: A replay of `identity.events` and `teaching.events` from the beginning leaves every
  authoritative record untouched: `invoice_profiles` field values, `invoices`, `invoice_lines`,
  `invoice_number_counters`, `billing_runs`, and `digest_runs`.
- **AC-9**: No projection stores a derived or accumulated value. The roster count is counted at read
  time from membership rows, and the period a session bills in is a range over `local_date` rather
  than a stored month column (INV-7).
- **AC-10**: Money is `bigint`, every currency column is constrained to `VND`, every instant is
  `timestamptz`, and every calendar day is `date` (STK-7).
- **AC-11**: Nothing is deleted. Every entity that can end carries a nullable end timestamp
  (`removed_at`, `archived_at`, `cancelled_at`, `effective_to`, `voided_at`) and every list query
  filters on it being empty.
- **AC-12**: The invoice profile completeness gate exists once as the generated `is_complete`
  column, `GetInvoiceProfile` returns it, and the Go missing field list agrees with it for every
  required field. Later callers read this gate rather than testing their own field lists.

## Decision

**Chosen option**: Option 2: one owning table per entity, with projections keyed by the id their
events carry

Place every entity in the service spec 0001 gives it, key each projection table on the identifying
field its own events already carry so a replay is idempotent by construction, separate the
rebuildable copies from the authoritative records inside `billing`, forbid deletion in favour of a
nullable end timestamp, and land the whole confirmed model through one initial migration per service
plus additive teaching and billing integrity migrations where development already applied the first
files (basis: spec 0001's ownership map and local copies table, which this spec applies rather than
reopens).

**Implementation skills**: `go-goose` (`metalagman/agent-skills`, `.agents/skills/go-goose/`) ·
`golang-database` (`samber/cc-skills-golang`, `.agents/skills/golang-database/`) ·
`kafka-development` (`mindrally/skills`, `.agents/skills/kafka-development/`)

## Rationale

Reasoning, the options weighed, and the references: see [rationale.md](rationale.md).

## Feature design

### Ownership: one entity, one owning service

The nine entities named in the scope, plus the machinery entities the two hard flows need. A copy is
listed only where a service genuinely cannot ask at read time.

| Entity | Owning service | Owning table | Copies elsewhere, and the event that carries them |
|---|---|---|---|
| Tutor | `identity` | `tutors` | `notifications.recipients` (`email`, `display_name`, `timezone`, `language`) from `identity.tutor.registered` and `identity.tutor.profile.changed` |
| Student | `teaching` | `students` | `billing.students` (`name`) from `teaching.student.registered`, `.changed`, `.removed`. `notifications` keeps none |
| Class | `teaching` | `classes` | `billing.classes` and `notifications.classes` (`name`) from `teaching.class.created` and `teaching.class.changed` |
| Session | `teaching` | `sessions` | `billing.sessions` and `notifications.sessions` from `teaching.session.scheduled`, `.moved`, `.cancelled` |
| Attendance | `teaching` | `attendance` | `billing.attendance` from `teaching.attendance.marked` |
| Roster membership | `teaching` | `roster_periods` | `billing.roster_periods` and `notifications.roster_periods` from `teaching.roster.joined` and `teaching.roster.left` |
| Rate, current on a class | `teaching` | `classes.rate_amount` | none. Only the dated history crosses the line |
| Rate history, dated | `billing` | `class_rates` | none. It is `billing`'s own projection, appended from `teaching.class.created` and `teaching.class.rate.changed` |
| Invoice profile and bank details | `billing` | `invoice_profiles` | none. The row is seeded by `identity.tutor.registered`; every field is written by the tutor |
| Invoice | `billing` | `invoices` | none |
| Invoice line | `billing` | `invoice_lines` | none |
| Invoice number | `billing` | `invoice_number_counters`, plus `invoices.invoice_number` | none |
| Month end run | `billing` | `billing_runs` | none |
| Digest run | `notifications` | `digest_runs` | none |
| Document | a future `documents` service (feature 18) | none yet | none. Named here so nobody puts it in `teaching` |

Two rows that look like copies and are not. `billing.invoice_profiles` is `billing`'s own record: the
`identity` event only causes the empty row to exist. `billing.class_rates` is a projection of
`teaching` rate facts, but the dated history itself is a thing `billing` owns, because `teaching`
keeps only the current rate.

### Projection versus authoritative record

The line a replay must respect. `task replay:<service>:<consumer>` (STK-22) may rewrite everything in
the left column and must never touch anything in the right.

| Rebuildable by replay | Never rebuilt, and no consumer writes it |
|---|---|
| `notifications`: `recipients`, `classes`, `sessions`, `roster_periods` | `notifications`: `digest_runs` |
| `billing`: `students`, `classes`, `sessions`, `attendance`, `roster_periods`, `class_rates` | `billing`: `invoice_profiles` field values, `invoices`, `invoice_lines`, `invoice_number_counters`, `billing_runs` |

`invoice_profiles` straddles the line, which is why it is called out: the consumer of
`identity.tutor.registered` inserts the row with `ON CONFLICT DO NOTHING` and writes no field. A
replay therefore recreates a missing row and cannot overwrite bank details the tutor typed.

**Data model sketch**:

Shared shape, so it is stated once rather than repeated per table. Identifiers are `uuid` (UUIDv7
generated in Go), money is `bigint` counting dong, instants are `timestamptz` in UTC, a calendar day
is `date`, and a state is `text` with a `CHECK` rather than a Postgres enum, because the enum's
migration cost buys nothing when the check also lives in Go. An owned table carries `created_at` and
`updated_at`; a projection table carries `recorded_at` and `updated_at`, the way `recipients` already
does. Both projection timestamps come from the consumer transaction clock. `recorded_at` is the
first insert time and never changes. `updated_at` changes on an applied upsert. They are bookkeeping
rather than event facts, so replay equality compares keys and business columns and does not require
these timestamps to match. Every table carries `tutor_id`, as a column or inside its key.

#### `identity`, migration `00003_tutor_timestamps.sql`

Completes the table the scaffold started. It applies cleanly as plain `NOT NULL` because no tutor row
exists in any environment yet: registration arrives with feature 7.

Corrected by spec [0004](../0004-tutor-sign-in-google-oauth/index.md): this migration also carried `password_hash text NOT NULL`, which is
what `verify.md` recorded as an open item. Spec 0004 removed passwords, so feature 7 rewrites this
file as `updated_at` only and renames it, and the Google account link that replaces the hash lands in
its own `00004` migration owned by feature 7.

| Table | Column | Type | Notes |
|---|---|---|---|
| `tutors` | `updated_at` | `timestamptz NOT NULL DEFAULT now()` | new, for `identity.tutor.profile.changed` |

Existing columns stay as they are: `tutor_id` primary key, `email` unique, `display_name`,
`timezone`, `language`, `created_at`. `identity` holds a copy of nothing and gains no other table.

#### `teaching`, migrations `00002_teaching_model.sql` and `00003_tenant_references.sql`

| Table | Key columns | Other columns | Constraints and indexes |
|---|---|---|---|
| `students` | `student_id uuid PK` | `tutor_id NOT NULL`, `name text NOT NULL`, `phone text` (nullable, stored exactly as typed), `created_at`, `updated_at`, `removed_at` | `UNIQUE (tutor_id, student_id)`, index `(tutor_id) WHERE removed_at IS NULL` |
| `classes` | `class_id uuid PK` | `tutor_id NOT NULL`, `name text NOT NULL`, `rate_amount bigint NOT NULL`, `currency text NOT NULL`, `rate_effective_from date NOT NULL`, `created_at`, `updated_at`, `archived_at` | `UNIQUE (tutor_id, class_id)`, `CHECK (currency = 'VND')`, `CHECK (rate_amount >= 0)`, index `(tutor_id) WHERE archived_at IS NULL` |
| `sessions` | `session_id uuid PK` | `class_id NOT NULL`, `tutor_id NOT NULL`, `starts_at NOT NULL`, `ends_at NOT NULL`, `local_date date NOT NULL`, `schedule_rule_id uuid` (nullable, no table yet), `created_at`, `updated_at`, `cancelled_at` | `UNIQUE (tutor_id, session_id)`, composite foreign key `(tutor_id, class_id)` to `classes`, `CHECK (ends_at > starts_at)`, index `(tutor_id, local_date) WHERE cancelled_at IS NULL` |
| `roster_periods` | `PK (class_id, student_id, effective_from)` | `tutor_id NOT NULL`, `effective_to date`, `created_at`, `updated_at` | composite foreign keys `(tutor_id, class_id)` to `classes` and `(tutor_id, student_id)` to `students`, `UNIQUE (class_id, student_id) WHERE effective_to IS NULL`, `CHECK (effective_to IS NULL OR effective_to >= effective_from)` |
| `attendance` | `PK (session_id, student_id)` | `tutor_id NOT NULL`, `state text NOT NULL`, `marked_at timestamptz NOT NULL`, `created_at`, `updated_at` | composite foreign keys `(tutor_id, session_id)` to `sessions` and `(tutor_id, student_id)` to `students`, `CHECK (state IN ('Present', 'Absent'))` |

The `00002` migration creates the tables. The additive `00003` migration adds the composite unique
keys and foreign keys after checking existing rows for tenant mismatches.

Three choices inside this schema that a reader would otherwise question.

- `sessions.schedule_rule_id` is nullable and carries no foreign key, because feature 10 owns the
  recurrence rule table and adds the reference then. What this spec fixes is that a session is always
  a concrete row, never a shape computed from a rule at read time, so a cancelled or moved session is
  an ordinary update to an ordinary row.
- A roster period is identified by `(class_id, student_id, effective_from)`, which is exactly what the
  roster events carry, so no service needs a `roster_period_id` that no event carries.
  `teaching.roster.left` closes the one open row for the pair, and the partial unique index is what
  makes "the one open row" true rather than hoped for. Rejoining a class opens a new row.
- `attendance` does not store `class_id`, even though `teaching.attendance.marked` carries it. It is
  the session's class, and a stored copy of a derivable value is one more thing that can disagree. The
  publisher reads it from the session row in the same transaction.

**What a roster period covers, stated once here for all three services.** `effective_from` is the
first day of membership and `effective_to` is the last day, both inclusive, so coverage is one
predicate copied verbatim wherever it is needed:

```sql
effective_from <= $local_date AND (effective_to IS NULL OR $local_date <= effective_to)
```

Inclusive rather than half open because the `CHECK (effective_to >= effective_from)` above allows the
two being equal: read inclusively that is a one day membership, read half open it is a period that
could never bill. And if attendance says `Present` on the leave date, that session has to bill
someone. `ListBillableSessions` in `billing` and `ListDigestSessions` in `notifications` both use this
exact predicate, which matters because they are two services counting the same membership
independently, and a boundary each picked for itself is the one way they could disagree about who was
in a class on a given day.

#### `billing`, migrations `00002_billing_model.sql` and `00003_model_integrity.sql`

The six projections first, then the five authoritative tables, in one file with a comment marking the
line between them.

Projections, written only by `internal/consumer`:

| Table | Key columns | Other columns | Written by |
|---|---|---|---|
| `students` | `student_id uuid PK` | `tutor_id NOT NULL`, `name text NOT NULL`, `removed_at`, `recorded_at`, `updated_at` | `teaching.student.registered`, `.changed`, `.removed` |
| `classes` | `class_id uuid PK` | `tutor_id NOT NULL`, `name text NOT NULL`, `recorded_at`, `updated_at` | `teaching.class.created`, `teaching.class.changed` |
| `sessions` | `session_id uuid PK` | `class_id NOT NULL`, `tutor_id NOT NULL`, `starts_at`, `ends_at`, `local_date date NOT NULL`, `cancelled_at`, `recorded_at`, `updated_at`. Index `(tutor_id, local_date) WHERE cancelled_at IS NULL` | `teaching.session.scheduled`, `.moved`, `.cancelled` |
| `roster_periods` | `PK (class_id, student_id, effective_from)` | `tutor_id NOT NULL`, `effective_to date`, `recorded_at`, `updated_at`. `UNIQUE (class_id, student_id) WHERE effective_to IS NULL` | `teaching.roster.joined`, `teaching.roster.left` |
| `attendance` | `PK (session_id, student_id)` | `tutor_id NOT NULL`, `state text NOT NULL CHECK (state IN ('Present', 'Absent'))`, `marked_at`, `recorded_at`, `updated_at` | `teaching.attendance.marked`, upserted so a correction lands on the same row |
| `class_rates` | `PK (class_id, effective_from)` | `tutor_id NOT NULL`, `rate_amount bigint NOT NULL`, `currency text NOT NULL CHECK (currency = 'VND')`, `recorded_at`, `updated_at` | `teaching.class.created` seeds the first row, `teaching.class.rate.changed` appends or corrects one dated row |

`class_rates` is append only in the sense that a new date adds a row and never edits an older one.
The primary key is the pair, not a surrogate, for a reason worth stating: the rate in force is the
newest row whose `effective_from` is on or before a session's `local_date`, so two rows for one class
on one date would make that question ambiguous. The key turns a corrected rate for the same date into
an upsert instead. That primary key is also the only index `RateInForceOn` needs: its leading column
is `class_id` and its second is `effective_from`, so the newest row on or before a date is a backwards
scan of one index. No separate descending index is worth adding.

Authoritative records, written only by `internal/handler`:

| Table | Key columns | Other columns | Constraints |
|---|---|---|---|
| `invoice_profiles` | `tutor_id uuid PK` | `legal_name text`, `contact_line text`, `bank_name text`, `bank_account_number text`, `bank_account_holder text`, all nullable, plus `created_at`, `updated_at`, plus the generated `is_complete` gate below | none beyond the key. The row is seeded empty |
| `invoice_number_counters` | `PK (tutor_id, period_year)` | `last_sequence integer NOT NULL`, `updated_at` | `CHECK (last_sequence >= 1)` |
| `billing_runs` | `billing_run_id uuid PK` | `tutor_id NOT NULL`, `period_year integer NOT NULL`, `period_month integer NOT NULL`, `generation integer NOT NULL`, `created_at`, `superseded_at` | `UNIQUE (tutor_id, billing_run_id)`, `UNIQUE (tutor_id, period_year, period_month, generation)`, `CHECK (period_month BETWEEN 1 AND 12)`, `CHECK (generation >= 1)` |
| `invoices` | `invoice_id uuid PK` | `tutor_id NOT NULL`, `billing_run_id NOT NULL`, `student_id NOT NULL`, `invoice_number text NOT NULL`, `period_year`, `period_month`, `total_amount bigint NOT NULL`, `currency text NOT NULL`, `issued_at NOT NULL`, `pdf_location text` (nullable), `paid_at`, `voided_at`, `void_reason text`, `replaces_invoice_id uuid`, the frozen render block below, `updated_at` | `UNIQUE (tutor_id, invoice_id)`, composite foreign keys `(tutor_id, billing_run_id)` to `billing_runs` and `(tutor_id, replaces_invoice_id)` to `invoices`, `UNIQUE (tutor_id, invoice_number)`, `CHECK (currency = 'VND')`, `CHECK ((voided_at IS NULL) = (void_reason IS NULL))`, `CHECK (void_reason IS NULL OR void_reason <> '')`, index `(tutor_id, period_year, period_month)` |
| `invoice_lines` | `invoice_line_id uuid PK` | `invoice_id NOT NULL`, `tutor_id NOT NULL`, `session_id uuid NOT NULL`, `session_date date NOT NULL`, `class_name text NOT NULL`, `rate_amount bigint NOT NULL`, `amount bigint NOT NULL` | composite foreign key `(tutor_id, invoice_id)` to `invoices`, `UNIQUE (invoice_id, session_id)`, index `(invoice_id)` |

The `00002` migration creates the tables. The additive `00003` migration adds the composite unique
keys and foreign keys after checking existing rows for tenant mismatches, then adds
`class_rates.updated_at` for same date corrections.

**The invoice number.** `invoice_number_counters` is bumped by one statement inside the run's
transaction, returning the number it just took:

```sql
INSERT INTO invoice_number_counters (tutor_id, period_year, last_sequence)
VALUES ($1, $2, 1)
ON CONFLICT (tutor_id, period_year)
DO UPDATE SET last_sequence = invoice_number_counters.last_sequence + 1, updated_at = now()
RETURNING last_sequence;
```

The year in the number is the `period_year`, not the year the run happened, so a December period
invoiced in January still reads as a December year invoice. `<year>-<4 digit sequence>` is formatted
in Go from that pair. A voided invoice keeps its number spent, which is exactly why the counter is a
row of its own rather than a `max` over live invoices.

**The completeness gate**, so the month end refusal and the profile screen cannot disagree
(AC-12). It lives in the schema as a generated column, which Postgres recomputes from the same row
and so can never go stale:

```sql
is_complete boolean NOT NULL GENERATED ALWAYS AS (
    legal_name           IS NOT NULL AND legal_name           <> ''
AND contact_line         IS NOT NULL AND contact_line         <> ''
AND bank_name            IS NOT NULL AND bank_name            <> ''
AND bank_account_number  IS NOT NULL AND bank_account_number  <> ''
AND bank_account_holder  IS NOT NULL AND bank_account_holder  <> ''
) STORED
```

All five are required, because the payment block is the whole point of the PDF and an invoice going
out with a blank contact line is worse than a refusal the tutor can clear in a minute. The `Go` side
still lists which fields are missing, for the `profile_incomplete` message; one test asserts that
list being empty is the same answer as `is_complete`, so the two cannot drift.

**The frozen render block on `invoices`**, so a re render years later prints the file the tutor
already sent: `student_name text NOT NULL`, `payee_legal_name`, `payee_contact_line`,
`payee_bank_name`, `payee_bank_account_number`, `payee_bank_account_holder`, all `text NOT NULL`,
copied from `invoice_profiles` and `students` at issue. This extends the freezing rule from the
invoice line to the payee block for the same reason: the run refuses unless the profile is complete,
so these can never be empty at issue, and a later bank account change must not reach backwards into
an issued invoice.

`invoice_lines` keeps both the frozen values and the `session_id` they came from. The id is for
tracing during a dispute and is never joined at render time. `amount` is stored beside `rate_amount`
even though they are equal today, because it is frozen money on an issued document rather than a
cached calculation.

#### `notifications`, migration `00003_digest_model.sql`

`recipients` already exists and is unchanged. Everything added here is a projection except
`digest_runs`.

| Table | Key columns | Other columns | Written by |
|---|---|---|---|
| `classes` | `class_id uuid PK` | `tutor_id NOT NULL`, `name text NOT NULL`, `recorded_at`, `updated_at` | `teaching.class.created`, `teaching.class.changed` |
| `sessions` | `session_id uuid PK` | `class_id NOT NULL`, `tutor_id NOT NULL`, `starts_at`, `ends_at`, `local_date date NOT NULL`, `cancelled_at`, `recorded_at`, `updated_at`. Index `(tutor_id, local_date) WHERE cancelled_at IS NULL` | `teaching.session.scheduled`, `.moved`, `.cancelled` |
| `roster_periods` | `PK (class_id, student_id, effective_from)` | `tutor_id NOT NULL`, `effective_to date`, `recorded_at`, `updated_at`. `UNIQUE (class_id, student_id) WHERE effective_to IS NULL` | `teaching.roster.joined`, `teaching.roster.left` |
| `digest_runs` | `PK (tutor_id, local_date)` | `state text NOT NULL CHECK (state IN ('Pending', 'Sent', 'Failed'))`, `attempts integer NOT NULL DEFAULT 0`, `last_error text NOT NULL DEFAULT ''`, `sent_at timestamptz`, `created_at`, `updated_at` | the scheduler only, never a consumer |

There is no stored roster count, which is a deliberate refinement of spec 0001's wording rather than a
contradiction of it: 0001 grants `notifications` the copy and its own follow up asks for the shape to
be revisited. An integer bumped up and down is the accumulate in arrival order shape INV-7 forbids, so
a redelivery double counts and a replay from the beginning multiplies the number, neither of which
announces itself. One membership row per pair makes both harmless, and the digest counts the rows
whose period covers its `local_date`. `notifications` stores no student name, because the digest
renders a count and never a name.

Nothing prunes `sessions`. A tutor may schedule weeks ahead, so a table holding only today would need
someone to put tomorrow in it and no event does that. Retention is a follow up, not an invented rule.

**State transitions**:

```
session (teaching, and both projections)
  scheduled ──(session.moved, an update to starts_at, ends_at, local_date)──> scheduled
  scheduled ──(session.cancelled)──> cancelled_at set, and it is never billable again

roster period
  open (effective_to IS NULL) ──(roster.left)──> closed (effective_to set)
  a later roster.joined for the same pair opens a new row, never reopens the closed one

invoice
  issued ──(tutor marks paid)──> paid_at set ──(tutor unmarks)──> paid_at cleared
  issued ──(void with a reason)──> voided_at and void_reason set, forever
  a void stamps superseded_at on the newest billing_runs row for the period, which raises the
  allowed generation by exactly one, and the reissue links back through replaces_invoice_id

billing run
  generation N live (superseded_at IS NULL) ──(a void)──> superseded, and generation N+1 allowed

digest run
  Pending ──(send succeeds)──> Sent, sent_at set
  Pending ──(retry budget exhausted)──> Failed, last_error set, alert raised
  Failed ──(a deliberate replay, feature 17)──> Pending
```

**API surface**:

This feature adds no HTTP endpoint and no change to `api/openapi.yaml`. The surface it defines is each
service's `db/queries/` set, which `sqlc` compiles to typed Go, so the load bearing queries are listed
in that spirit. Every one of them filters by `tutor_id` (AC-4).

| Query | Service | Key inputs | Key outputs | Auth | Key failures |
|---|---|---|---|---|---|
| `ListSessionsForLocalDate` | `teaching` | `tutor_id`, `local_date` | session rows with `class_id` | `tutor_id` from the token | none. An empty list is a valid answer |
| `UpsertAttendance` | `teaching` | `session_id`, `student_id`, `tutor_id`, `state`, `marked_at` | the stored row | same | foreign key violation when the session or student is unknown |
| `FindOpenRosterPeriod` | `teaching` | `tutor_id`, `class_id`, `student_id` | the one open row | same | none. Absent means never joined or already left |
| `ListBillableSessions` | `billing` | `tutor_id`, `local_date` range | session, student, rate rows for the period | same | none |
| `RateInForceOn` | `billing` | `tutor_id`, `class_id`, `local_date` | newest `class_rates` row on or before the date | same | absent, which the run turns into `rate_missing` |
| `TakeNextInvoiceNumber` | `billing` | `tutor_id`, `period_year` | `last_sequence` | same | none. The upsert always returns a fresh number |
| `CurrentBillingRunGeneration` | `billing` | `tutor_id`, `period_year`, `period_month` | the highest `generation` for the period and whether it is superseded | same | absent, which means no run yet, so the next generation is 1 |
| `InsertBillingRun` | `billing` | `tutor_id`, `period_year`, `period_month`, `generation` | the run row | same | unique violation, which means a concurrent press already won |
| `GetInvoiceProfile` | `billing` | `tutor_id` | fields plus `is_complete` | same | identity event lag. The later handler first calls the idempotent profile seed, then reads an incomplete empty row |
| `ListDigestSessions` | `notifications` | `tutor_id`, `local_date` | sessions with class name and a counted roster size | the scheduler, not a request | none |
| `InsertDigestRun` | `notifications` | `tutor_id`, `local_date` | the run row | same | unique violation, which means the digest already went out |

`ListBillableSessions` and `ListDigestSessions` both join `roster_periods` on the one coverage
predicate written above, and neither may write its own. `CurrentBillingRunGeneration` is a plain read
that needs no lock and no `SELECT ... FOR UPDATE`: it only tells the handler which number to attempt,
and `UNIQUE (tutor_id, period_year, period_month, generation)` is what actually decides a race
(spec 0001, flow 1 step 3). A loser reads the winner's run and returns its invoices.

Every join between tenant scoped tables matches `tutor_id` as well as the entity id. Current list
rules are explicit: `ListSessionsForLocalDate` excludes `cancelled_at`, `ListBillableSessions`
excludes removed students and cancelled sessions and applies roster coverage, and
`ListDigestSessions` excludes cancelled sessions and applies the same roster coverage. A later list
query must name the end timestamp for every entity it can expose.

**Value sourcing**:

Only the values whose source was not already obvious. Every row here is a value some action must
produce whose source the build would otherwise have to invent, including the gaps the cross check
found.

These rows are design obligations for the action that eventually uses them. Feature 4 verifies a row
only when its Build plan builds the producing or reading path. A later feature must carry each
remaining row into its own acceptance criteria and `verify.md` when it builds that action. An unbuilt
future action does not block feature 4.

| Action | Value produced or displayed | Source |
|---|---|---|
| Any consumer upsert | The projection row's identity | The identifying id the event already carries: `student_id`, `class_id`, `session_id`, `(session_id, student_id)`, `(class_id, student_id, effective_from)`, or `(class_id, effective_from)` |
| Schedule or move a session | `teaching.sessions.local_date` | `teaching` derives the calendar day from `starts_at` in the verified token's timezone. Feature 10 must store the recurrence timezone before a background scheduler can create sessions without a token |
| Close a roster period | Which period to close | The one row for the pair with `effective_to IS NULL`, held true by the partial unique index. `teaching.roster.left` carries no `effective_from`, so this is the only well defined target |
| Mark a projection ended | `removed_at`, `cancelled_at` | The envelope `occurred_at` (INV-4). `teaching.student.removed` and `teaching.session.cancelled` carry no timestamp of their own, and the envelope already says when the fact happened |
| Archive a class | `classes.archived_at` | `teaching` only, from its own transaction clock. No event carries it because the catalogue has none, and no consumer renders it: `billing` still prints the label, and an archived class simply stops producing sessions |
| Change a class rate | `teaching.classes.rate_amount`, `currency`, `rate_effective_from` | `teaching`'s own rate change handler, which updates the class row and inserts the `teaching.class.rate.changed` outbox row in the same transaction (INV-3). `teaching.class.created` seeds the same three columns at creation. Nothing else writes them, and `billing` never writes back |
| Append a rate | `class_rates.effective_from` | Two event fields write this one column: `rate_effective_from` on `teaching.class.created` and `effective_from` on `teaching.class.rate.changed`. The column takes the name the ongoing event uses |
| Month end run | The period a session belongs to | A range over `billing.sessions.local_date`. There is no stored month column, because a `session.moved` event would leave one stale |
| Month end run | Whether a session bills for a student | The `billing.attendance` row for `(session_id, student_id)`: only `state = 'Present'` bills. No row means unmarked, so unmarked and `Absent` produce the same money and differ only on screen |
| Month end run | Whether a student is invoiced at all | The billable sessions in the period, never the roster. The run groups over `Present` sessions, so a student on the roster with no `Present` session in the period gets no invoice and burns no invoice number |
| Month end run | The invoice number's year part | `period_year`, the period being billed, not the year the run happened |
| Month end run | Payee block on the invoice | `invoice_profiles`, frozen onto `invoices` at issue |
| Month end run | Student and class name on a line | `billing.students.name` and `billing.classes.name`, frozen onto `invoices` and `invoice_lines` at issue |
| Month end refusal | Whether the profile is complete | `invoice_profiles.is_complete`, the one generated gate. The missing field list for the message comes from the same row's nulls |
| Read or update an invoice profile | The profile row when the registration event is still in flight | The billing handler calls the same idempotent empty profile seed used by the identity consumer before reading or updating |
| A void, then a reissue | The generation to attempt | `CurrentBillingRunGeneration`, plus one. A void stamps `superseded_at`, which is what raises the allowed generation |
| A reissue | The rate and the names on each line | Re evaluated from the current projections, including the `class_rates` row in force on the session's current `local_date`, which a `session.moved` may have changed. The voided invoice keeps its own frozen values forever (INV-9); the replacement's job is to be right, not to match |
| Digest | Roster size per class | Counted at read time from `notifications.roster_periods` covering the digest `local_date`, by the one coverage predicate above. Never a stored integer |
| Digest | The tutor's local date | `recipients.timezone`, resolved with `time.LoadLocation` and the `_ "time/tzdata"` blank import every `main.go` already carries, because a `FROM scratch` image ships no system zone database while a name like `Asia/Ho_Chi_Minh` still has to resolve (STK-6) |
| Handler write | `tutor_id` | The `sub` claim of the verified token, never a request input (INV-8) |
| Consumer projection write | `tutor_id` | The event envelope `tutor_id`, never a payload field chosen independently |
| Digest scheduler write | `tutor_id` | The `recipients.tutor_id` row selected for that scheduled run |

**Key invariants**:

- One entity, one owning table. A copy is only ever written by `internal/consumer`, an authoritative
  record only ever by `internal/handler`.
- Every projection table's primary key is the identifying key its events carry, so a consumer upsert
  is idempotent and a replay lands on the same rows (INV-5, INV-7, INV-11).
- At most one open roster period per `(class_id, student_id)`, held by a partial unique index.
- A roster period covers a day inclusively at both ends, by the one predicate above, and every service
  that counts membership uses that predicate rather than its own.
- At most one attendance row per `(session_id, student_id)`. A correction updates it in place, and per
  key ordering on `session_id` means the last mark is the last one `billing` sees (INV-6).
- Only `state = 'Present'` bills. A missing attendance row is unmarked, which for money is the same
  answer as `Absent`.
- An invoice always has at least one line, because the run groups over billable sessions and never
  over the roster. A student with no `Present` session in the period is not invoiced and spends no
  invoice number. This is deliberately not a `CHECK (total_amount > 0)`, because a rate of zero is
  legal under `CHECK (rate_amount >= 0)` and a genuinely free lesson should still produce a line.
- `invoices.total_amount` equals the sum of its lines' `amount`, written in the run's one transaction.
- `UNIQUE (tutor_id, invoice_number)`, and a number is never reused, including by a voided invoice.
- `UNIQUE (tutor_id, period_year, period_month, generation)` is what makes a double press safe, not
  the lookup above it (spec 0001, flow 1 step 3).
- `UNIQUE (tutor_id, local_date)` on `digest_runs` is what makes one digest per tutor per day true.
- Nothing is deleted, ever. A list filters on the end timestamp being empty.
- No foreign key crosses a service boundary. Inside a service, a foreign key is used only where both
  tables are authoritative and in the same context, and it includes `tutor_id` so a child cannot
  point at another tutor's row. Projections reference by id without a foreign key because events for
  two different keys may arrive in either order, but their writes and joins match `tutor_id` too.
- Handler writes take `tutor_id` from the verified token, consumer writes from the envelope, and
  scheduler writes from the selected recipient row. No request or event payload may replace it.
- Projection bookkeeping timestamps describe consumer processing. Replay safety compares keys and
  business columns, not `recorded_at` or `updated_at`.
- Money is `bigint` dong and currency is `VND` by `CHECK`.

**Security model**:

- A handler takes `tutor_id` from the verified token, a consumer from the event envelope, and the
  digest scheduler from the selected recipient row. Every query filters on that trusted value and
  every reference matches it (INV-8). There is no cross tutor read or reference path.
- `identity` holds no password at all (spec [0004](../0004-tutor-sign-in-google-oauth/index.md)). What it holds instead, the Google account
  link and the refresh token hashes, never leaves it and no event carries either. The `identity`
  events carry `email`, `display_name`, `timezone`, `language` only.
- Bank details live only in `billing.invoice_profiles` and in the frozen payee block on issued
  invoices. No event carries them, so they never reach the broker.
- A student's phone number lives only in `teaching.students`. It is deliberately absent from
  `teaching.student.registered`, because no invoice and no digest needs it, so it never crosses a
  boundary.
- No compliance regime applies. The data is one tutor's own students and their own bank details, on
  their own machine.
- Each service reaches only its own database, through its own role and its own
  `<SERVICE>_DATABASE_URL` (STK-5).

**Configuration required**:

None. Every service already has its own `<SERVICE>_DATABASE_URL`, and this feature adds no
environment variable, secret, or third party credential.

**Critical test scenarios**:

- Happy path: `task migrate:up` on empty databases, then `sqlc generate` and `task build` clean in
  every module, verifies **AC-2**, **AC-5**.
- Migration reversal: `goose down` on each new migration returns the schema to the kit tables plus
  what was there before, verifies **AC-2**.
- Idempotent projection: the same `teaching.session.scheduled` delivered twice, then a full replay of
  `teaching.events` from the earliest offset, leaves one row per session and the same roster counts,
  against the real Postgres and Redpanda in `test/compose.test.yaml` (STK-15), verifies **AC-7**,
  **AC-9**.
- Replay safety: a replay of both consumed topics after an invoice and digest run exist leaves
  `invoices`, `invoice_lines`, `invoice_number_counters`, `billing_runs`, `digest_runs`, and the
  `invoice_profiles` field values unchanged, verifies **AC-8**.
- Failure case: two month end runs for the same `(tutor_id, period_year, period_month, generation)`
  inserted concurrently, where exactly one commits and the loser returns the winner's invoices, and
  two concurrent `TakeNextInvoiceNumber` calls never return the same sequence, verifies **AC-1**.
- Rejoin case: join, leave, rejoin the same class produces two roster period rows with exactly one
  open, and a session on a date between the two periods bills nobody, verifies **AC-11**.
- Roster boundary: a student who leaves on the day a session runs, and is marked `Present` for it, is
  billed for that session, and `ListBillableSessions` and `ListDigestSessions` return the same
  membership for that day. A one day period, where `effective_to` equals `effective_from`, covers
  exactly that one day, verifies **AC-11**.
- Nothing to bill: a student on the roster for the whole period whose sessions are all `Absent` or
  unmarked produces no invoice row and leaves `invoice_number_counters` for that tutor and year
  exactly where it was, verifies **AC-1**, **AC-8**.
- Ownership guard: a test walking every `db/queries/*.sql` asserts each statement on a tutor scoped
  table names `tutor_id`, and that no statement names a table the service does not own, verifies
  **AC-4**, **AC-6**.
- Schema guard: a test reading each service's `information_schema` asserts every money column is
  `bigint`, every currency column carries the `VND` check, every instant is `timestamptz`, and every
  projection table's primary key matches the event key listed in the ownership table, verifies
  **AC-3**, **AC-10**.
- Completeness gate: a table of profile rows where the `Go` missing field list is empty exactly when
  `is_complete` is true, verifies **AC-12**.
- Auth and permission: a query run with another tutor's `tutor_id` returns nothing rather than someone
  else's row, for one table per service, verifies **AC-4**.
- Tenant reference: a teaching or billing child written with tutor A and a parent id owned by tutor B
  is rejected, while projection joins cannot match across those tutors, verifies **AC-4**.

## Build plan

Tracer Bullet, read for a schema feature: the migrations land in the direction the facts flow, so the
publisher's table exists before the projection that copies it, and the guard tests that prove the
whole model come last as the thread's closing check. One initial model migration per service lands
the coherent schema. Teaching and billing then receive one additive integrity migration because the
initial files already ran in development and must not be rewritten.

1. `identity` migration `00003_tutor_timestamps.sql`: `updated_at` on `tutors` (this step read
   `password_hash` and `updated_at` until spec [0004](../0004-tutor-sign-in-google-oauth/index.md) removed passwords),
   satisfies **AC-1**, **AC-2**, **AC-10**
2. `teaching` migration `00002_teaching_model.sql`: `students`, `classes`, `sessions`,
   `roster_periods`, `attendance`, with the comment above each table naming what it owns, satisfies
   **AC-1**, **AC-2**, **AC-10**, **AC-11**
3. `billing` migration `00002_billing_model.sql`: the six projections, a comment marking the line,
   then `invoice_profiles` with its generated gate, `invoice_number_counters`, `billing_runs`,
   `invoices`, `invoice_lines`. Each projection's comment names the events that write it, satisfies
   **AC-1**, **AC-2**, **AC-3**, **AC-6**, **AC-10**, **AC-11**, **AC-12**
4. `notifications` migration `00003_digest_model.sql`: `classes`, `sessions`, `roster_periods`,
   `digest_runs`, with the comment on `roster_periods` recording why there is no stored count,
   satisfies **AC-1**, **AC-2**, **AC-3**, **AC-9**
5. `sqlc.yaml` for `teaching` and `billing`, copied from `services/identity/sqlc.yaml` so the `uuid`
   and `timestamptz` overrides match, satisfies **AC-5**
6. The first `db/queries/` file per service, covering the load bearing queries in the API surface
   table above, every one filtering by `tutor_id` and matching it on projection joins, replacing the
   two `db/queries/README.md` placeholders, satisfies **AC-4**, **AC-5**
7. `task migrate:up` from empty databases, then `sqlc generate` and `task build` across every module,
   satisfies **AC-2**, **AC-5**
8. The query guard test: every statement on a tutor scoped table names `tutor_id`, every projection
   join matches it, and no statement names a table its service does not own, satisfies **AC-4**,
   **AC-6**
9. The schema guard test against the real Postgres: money is `bigint`, currency carries the `VND`
   check, instants are `timestamptz`, and each projection table's primary key matches its event key,
   satisfies **AC-3**, **AC-7**, **AC-10**
10. The replay and idempotency test against the real Postgres and Redpanda: a duplicate delivery and a
    full replay leave projection business fields identical and billing and notification authoritative
    records untouched, satisfies **AC-7**, **AC-8**, **AC-9**
11. The remaining scenario tests: concurrent run insert and concurrent number take, cross tenant
    reference rejection, the rejoin case, and the completeness gate table, satisfies **AC-1**,
    **AC-4**, **AC-11**, **AC-12**
12. Add `00003_tenant_references.sql` to teaching and `00003_model_integrity.sql` to billing. Add the
    composite unique keys and foreign keys in the Data model sketch without rewriting an already
    applied migration. Add `class_rates.updated_at` and extend the schema guards with cross tenant
    child writes, satisfies **AC-2**, **AC-3**, **AC-4**, **AC-6**
13. Make consumers take `tutor_id` from the event envelope, then extend replay tests to preserve
    `digest_runs`, reject a conflicting payload tenant, compare projection business fields separately
    from bookkeeping timestamps, and assert the timestamp clock rule, satisfies **AC-3**, **AC-4**,
    **AC-7**, **AC-8**

## Migration plan

**Strategy**: additive constraints, no data rewrite

**Phases**:

1. Before adding constraints, query teaching and billing for child rows whose `tutor_id` differs from
   the referenced parent. Stop with the exact rows if any mismatch exists.
2. Apply `00003_tenant_references.sql` in teaching and `00003_model_integrity.sql` in billing. Each
   migration adds the composite unique keys first, then the composite foreign keys, and validates
   every constraint. The billing migration also adds `class_rates.updated_at`.
3. Regenerate SQL, run the schema guards, then prove migration down and up on temporary databases.

**Rollback**: each Goose down step drops only the new composite foreign keys and unique constraints.
The original primary keys and data remain intact.

**Risks**: an existing cross tenant mismatch blocks the migration. That is an intentional refusal,
because choosing a parent or tenant automatically would hide corruption.

## Consequences

**Positive**:

- Every entity has one home, so a later feature adds a column to a known table rather than inventing a
  second place for the same fact. The most expensive mistake in this architecture is now closed.
- Every projection is idempotent by its key, so a redelivery and a replay are both harmless without any
  care taken in a handler. `handled_events` becomes a second line of defence rather than the only one.
- The month end run reads only local tables and produces the same numbers when run again, because the
  frozen render block and the dated rate history both survive later edits.
- The projection versus authoritative split is written down per table, so `task replay:` is safe to run
  without reading any handler code.
- Spec 0001's weakest copy, the roster count, is resolved rather than deferred, and its follow up
  closes.

**Negative / tradeoffs**:

- The schema reaches ahead of the features that use it. Tables for the month end run land now while
  features 13 to 15 are still planned, so a later spec may still need its own migration and this one
  will look partly unused for weeks.
- Real duplication to maintain: `classes` exists in three databases and `sessions` in three, so a new
  consumer need means a new event field and a migration in each copy that wants it.
- Freezing the payee block onto every invoice stores the same five values on every row of a tutor's
  invoices. That is deliberate waste, bought for the guarantee that a re render matches the file the
  tutor already sent.
- Counting the roster at read time is one more join in the digest query than an integer column would
  be, on every tick.
- Nothing is deleted, so every list query carries a `WHERE ... IS NULL` and a forgotten one silently
  shows removed data. The partial indexes help but do not enforce it.
- Composite tenant keys add redundant unique indexes, but they make a cross tutor reference
  impossible at the database boundary rather than relying on handler discipline.
- `sessions` in `notifications` grows without bound until someone writes a retention rule.

**Neutral**:

- Two places where this spec refines spec 0001's wording rather than applying it, both flagged in
  Follow-up: the roster count's shape, and `billing_run` written as the plural `billing_runs` to match
  every other table in the repo.
- New patterns to learn: a generated column as a gate, a partial unique index as a real invariant, and
  a composite primary key taken from event fields rather than a surrogate id.
- Three services gain their first business table, so `internal/store` and `internal/consumer` stop
  being empty packages.

## Follow-up

- [ ] Spec 0001's follow up "revisit the `notifications` roster count if it drifts" is answered here by
      storing membership rows instead of an integer. Worth a line in 0001 pointing at this spec, so the
      two records do not disagree.
- [ ] Spec 0001's flow 1 step 3 writes the run table as `billing_run`; this spec names it
      `billing_runs` to match every other table in the repo. Same table, and 0001's prose is worth
      tidying.
- [ ] The catalogue has no event for archiving a class, and `teaching.student.removed` and
      `teaching.session.cancelled` carry no timestamp. This spec resolves both without touching the
      catalogue, through the envelope `occurred_at` and by keeping `archived_at` local to `teaching`.
      If a later feature genuinely needs consumers to know a class was archived, that is a new event
      and an amendment to spec 0001, not a column added quietly here.
- [ ] `teaching.class.created` carries `rate_effective_from` while `teaching.class.rate.changed`
      carries `effective_from`, for the same value. Worth one name in a later catalogue tidy.
- [ ] Feature 10 owns the recurrence rule table and the foreign key from `sessions.schedule_rule_id`.
      The column is nullable and unreferenced until then.
- [ ] Feature 12 may narrow the `is_complete` predicate if requiring `contact_line` proves annoying in
      practice. That is a migration and a reason, not an edit to the handler.
- [ ] Retention for `notifications.sessions` and for old `digest_runs` is unspecified. Worth deciding
      when the tables are big enough to notice, rather than inventing a rule now.
- [ ] Feature 18's `documents` service owns the `document` entity. No table for it exists in any
      service, on purpose.
