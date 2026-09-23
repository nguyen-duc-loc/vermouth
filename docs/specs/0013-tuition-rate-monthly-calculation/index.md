# 0013. Tuition rate and monthly calculation

**Date**: 2026-09-21
**Status**: Accepted

## Summary

You can correct a dated class rate, review one completed month, and issue one immutable invoice per
student from Present sessions only. Billing first proves that its projection includes a captured cut
of every published teaching event and contains no known unresolved failure, then gives you a detailed
preview. Issue succeeds only when the same facts are still current, and one database transaction
creates every money record or creates nothing.

## Requirements

**User stories**:

1. As a tutor, you want to maintain dated rates on a class so each session uses the rate that applied
   on its local date.
2. As a tutor, you want to review every session and total before issuing invoices so a silent error
   does not reach a parent.
3. As a tutor, you want a repeated or uncertain request to return the same invoices so a retry cannot
   charge a student twice.

**Acceptance criteria**:

1. **AC-1**: A signed in tutor can read the complete effective rate schedule for an owned class,
   newest effective date first, and can create or correct one rate with an effective date no later
   than today in the tutor's timezone. Same date corrections replace the effective value, while
   immutable receipts and events remain the audit history. The amount is an integer from 0 through
   1,000,000,000 dong. Another tutor receives no class or rate information.
2. **AC-2**: A rate command locks the owned class and is safe to retry through its
   `Idempotency-Key`. Existing classes begin at revision 0, new classes begin at 1, and every newly
   accepted command increments a monotonic `rate_revision` and emits the dated fact even when the
   amount happens to match. Only receipt replay is a no operation. The dated event
   carries it. Billing applies a rate only when its revision is newer than the stored revision for
   that date, so the last committed command wins. A backdated command changes the class current rate
   only when its effective date is at least as recent as the current effective date.
3. **AC-3**: An archived class accepts a correction from its earliest retained session date through
   its archive date. An archived class without a retained session returns
   `archived_class_has_no_sessions`. The class stays archived and the command creates no session or
   schedule work. Every successful rate command states that issued invoices remain unchanged.
4. **AC-4**: Billing metadata supplies the server derived most recent completed month and the verified
   tutor timezone. The screen keeps validated `year` and `month` search values in the URL, replaces
   missing or malformed values with that default, and allows any completed month from year 2000. A
   current or future month is refused.
5. **AC-5**: A preview groups all eligible rows into one prospective invoice per student across all
   classes. It shows each Present session with its local date, class, rate, and amount, each student
   total, and the grand total. Absent, cancelled, teaching superseded, and out of roster sessions are
   excluded. One candidate is one distinct `(student_id, session_id)` pair before attendance filtering.
6. **AC-6**: A rate is the newest `billing.class_rates` row on or before the session `local_date`.
   Money is `int64` dong, currency is VND, and no rounding occurs. A Present session at a zero rate
   remains a line, while a student with no Present session receives no invoice.
7. **AC-7**: Preview reports structured blockers for an incomplete invoice profile, every eligible
   roster attendance row that is still unmarked, and every Present session without a rate. Issue is
   disabled until every blocker is clear. An empty month returns an empty preview and writes no run,
   invoice, line, number, or event.
8. **AC-8**: Before preview or issue, billing captures the current next offset for every
   `teaching.events` partition and waits until its consumer group has committed a contiguous next
   offset at least that large. Included records satisfy `source_offset < captured_end`. Broker
   metadata and polling share one five second deadline and request cancellation. Timeout answers
   `503 projection_sync_pending` with `Retry-After: 1` and writes nothing.
9. **AC-9**: A parked event creates one durable unresolved `consumer_failures` row before its source
   offset commits. Preview and issue refuse when any relevant unresolved failure is visible in their
   database snapshot. For barrier membership, a record is earlier than the captured cut when
   `source_offset < captured_end`. A matching tutor failure blocks that tutor. A failure with no
   readable tutor blocks every billing run.
10. **AC-10**: A successful replay, including a safe duplicate or revision no operation, resolves its
    exact source failure in the same transaction as the handled event and projection work. Operator
    acknowledgement is limited to `decode_failed` records after repair and records an operating system
    identity, an enumerated resolution code, and a constrained repair reference. The table stores no
    event payload, raw error text, free text note, student name, bank detail, phone number, or amount.
11. **AC-11**: A structurally incomplete projection, including a session without its class or student
    label, returns `projection_incomplete` with safe record identifiers and a `request_id`. It never
    skips the row, substitutes a label, uses a zero rate, exposes raw data, or starts replay from the
    browser.
12. **AC-12**: Preview opens a PostgreSQL repeatable read transaction after the broker barrier. It
    takes the readiness share lock but writes no business data. Its versioned SHA 256 fingerprint
    covers a fixed typed canonical payload with the tutor,
    period, every candidate, attendance, selected rate and effective date, labels, complete profile
    values and revision, deterministic blockers, totals, and protocol version. Golden vectors fix JSON
    encoding and ordering. The preview is never stored and the browser treats the digest as opaque.
13. **AC-13**: Issue checks for an existing live run first. Otherwise it captures a new published
    broker barrier, opens a PostgreSQL repeatable read transaction, runs failure and structural checks,
    recomputes the canonical preview, and compares its fingerprint. Any previously ready input that
    becomes changed, blocked, empty, or too large returns `409 preview_stale`, writes nothing, and
    requires a fresh explicit decision.
14. **AC-14**: A successful issue writes one `billing_runs` row, at most one invoice per student in
    that run, every invoice line, every consumed invoice number, and one `billing.invoice.issued`
    outbox fact per invoice in one transaction. Any failure rolls back all of them, including invoice
    numbers.
15. **AC-15**: One invoice totals exactly the sum of its lines. Its number uses one never reused
    sequence per tutor and period year, with at least four digits and expansion beyond 9999. The
    invoice freezes the student name and payee values, and each line freezes its session date, class
    name, rate, and amount. Later changes never rewrite an issued row.
16. **AC-16**: The billing run is the issue consistency boundary for its invoices, lines, number
    allocation, and outbox rows. Feature 13 creates generation 1. The database enforces one live run
    per tutor and period, at most one invoice per student in a run, and at most one direct replacement
    for a voided invoice. A losing unique or serialization race rolls back, then reads the winner in a
    fresh transaction or retries within a bounded budget.
17. **AC-17**: A new issue answers `201`. A repeated issue answers `200` with the same run, invoice
    summaries, and frozen lines. If a response is lost, the browser reads the period first, shows the
    committed run when present, and otherwise offers the same explicit issue action again.
18. **AC-18**: When a period is already issued, its read and preview return `already_issued` with the
    live run and invoice summaries and no new fingerprint. Feature 13 shows issued numbers, students,
    lines, and totals, while stating that feature 14 adds PDFs. Feature 15 owns the deliberate void and
    replacement action.
19. **AC-19**: Existing issued period reads remain available when Redpanda is unavailable. Preview and
    issue fail safely. Teaching may commit a rate change with its outbox fact, and the class page shows
    the confirmed teaching result plus a syncing state until billing's projected revision reaches or
    passes the returned `rate_revision`.
20. **AC-20**: Preview and issue support at most 500 distinct students and 10,000 candidate roster and
    session rows for one tutor and month. Preview returns `422 period_too_large`. Issue returns
    `409 preview_stale` if a previously ready month grows beyond the bound. Both write nothing. The
    calculation remains synchronous inside billing.
21. **AC-21**: `api/openapi.yaml` defines every public operation, reusable schema, stable response, and
   `vermouth.APIError` detail shape. Generated gateway and browser types are current. Billing values
    are cached only in tutor scoped TanStack Query memory keys, related keys are invalidated after
    every affecting write, requests accept cancellation, and sign out cancels and removes every
    billing key. Every private rate and billing response sends `Cache-Control: no-store`.
22. **AC-22**: The billing and rate interfaces meet WCAG AA. Every action is reachable by keyboard,
    has a visible focus state and at least a 44 by 44 pixel touch target, announces loading and error
    changes, preserves the selected month through recovery, and works at phone width and 200 percent
    zoom.
23. **AC-23**: Structured logs record request, tutor, class, period, run and invoice identifiers,
    counts, outcome codes, and duration. They exclude names, bank data, phone numbers, amounts,
    preview fingerprints, event payloads, and raw consumer failure text. The invoice records, rate
    events, command receipts, and outbox rows provide the durable audit trail.
24. **AC-24**: Real Postgres and Redpanda tests prove rate revisions, replay resolution, dead letter
    blocking, the fixed broker barrier, stale preview refusal, every blocker, empty and zero rate
    months, transaction rollback, concurrent issue, lost response recovery, tenant isolation, the
    volume boundary, and unchanged issued invoices. Before issue is enabled, a full verified billing
    replay after the failure ledger migration certifies the current topic identity and partition set
    with a durable replay manifest and projection generation.
    Missing offsets, retention gaps during rebuild, topic recreation, and uncertified projection state
    fail closed.

## Decision

**Chosen option**: Reviewed synchronous issue from a certified published projection

Billing will calculate from its own replayable teaching projection after a fixed broker barrier and
an unresolved failure check. Preview is temporary. Issue repeats the proof and comparison inside one
repeatable read transaction before it writes immutable invoices.

**Implementation skills**: `golang-database` (`samber/cc-skills-golang`, `.agents/skills/golang-database/`) · `kafka-development` (`mindrally/skills`, `.agents/skills/kafka-development/`) · `go-goose` (`metalagman/agent-skills`, `.agents/skills/go-goose/`) · `openapi` (`oakoss/agent-skills`, `.agents/skills/openapi/`) · `golang-testing` (`samber/cc-skills-golang`, `.agents/skills/golang-testing/`) · `golang-stretchr-testify` (`samber/cc-skills-golang`, `.agents/skills/golang-stretchr-testify/`) · `tanstack-query-best-practices` (`deckard/tanstack-agent-skills`, `.agents/skills/tanstack-query-best-practices/`) · `tanstack-router-best-practices` (`deckard/tanstack-agent-skills`, `.agents/skills/tanstack-router-best-practices/`) · `tailwindcss-accessibility` (`josiahsiegel/claude-plugin-marketplace`, `.agents/skills/tailwindcss-accessibility/`)

## Feature design

### Data model sketch

The accepted model remains the base and gains only the confirmed revision, readiness, and uniqueness
constraints. Identifiers are UUIDv7 values. Money is `bigint` in
Postgres and `int64` in Go. Dates are `date`, instants are UTC `timestamptz`, and currency is VND.

| Entity | Key and required fields | Nullable fields and relationships |
|---|---|---|
| `teaching.classes` | `class_id`, `tutor_id`, current `rate_amount`, `currency`, `rate_effective_from`, `rate_revision bigint` | `archived_at`; existing rows start at 0, new rows start at 1, accepted rate commands increment it |
| `billing.students` | `student_id`, `tutor_id`, `name` | `removed_at`; retained labels support past invoice issue and rendering |
| `billing.classes` | `class_id`, `tutor_id`, `name` | No end field because the event catalogue carries no class archive fact |
| `billing.class_rates` | `(class_id, effective_from)`, `tutor_id`, `rate_amount`, `currency`, `rate_revision bigint` | Nullable source partition and offset support legacy replay; positive revisions apply only when newer |
| `billing.sessions` | `session_id`, `class_id`, `tutor_id`, `local_date` | `cancelled_at`; one session contributes at most one line per student |
| `billing.attendance` | `(session_id, student_id)`, `tutor_id`, `state`, `marked_at` | One current attendance fact per student and session |
| `billing.roster_periods` | `(class_id, student_id, effective_from)`, `tutor_id` | `effective_to`; inclusive coverage on both dates |
| `billing.invoice_profiles` | `tutor_id`, `revision`, `is_complete` | Editable profile and bank fields; complete values are frozen at issue |
| `billing.billing_runs` | `billing_run_id`, `tutor_id`, `period_year`, `period_month`, `generation` | `superseded_at`; unique generation and partial unique live `(tutor_id, period_year, period_month)`; one run has many invoices |
| `billing.invoices` | `invoice_id`, `tutor_id`, `billing_run_id`, `student_id`, number, period, total, currency, issued and frozen render fields | PDF, paid, void, and replacement fields; unique `(tutor_id, billing_run_id, student_id)`; unique nonnull `(tutor_id, replaces_invoice_id)` |
| `billing.invoice_lines` | `invoice_line_id`, `invoice_id`, `tutor_id`, `session_id`, date, class name, rate, amount | One invoice has many lines; `(invoice_id, session_id)` stays unique |
| `billing.invoice_number_counters` | `(tutor_id, period_year)`, `last_sequence` | One counter supplies numbers inside the issue transaction |
| `consumer_failures` | `(consumer_name, source_topic, source_partition, source_offset)`, `failure_category`, `failed_at` | `event_id`, `tutor_id`, `resolved_at`, `resolution`, `resolved_by`, `repair_reference`; unresolved rows gate projection reads |
| `consumer_readiness` | `consumer_name`, current `projection_generation uuid`, state, `updated_at` | The issue transaction takes a share lock; replay reset and certification take an update lock |
| `consumer_replay_manifests` | `(consumer_name, projection_generation)`, source topic, topic identity, partition set, earliest offsets, captured ends, completed offsets, start and completion times, operator identity | A completed retained-history replay is the only source of certification; manifests remain as audit history |

`consumer_failures` is canonical shared DDL in `pkg/vermouth`. Because existing
`00001_vermouth_kit.sql` migrations have already run, one additive canonical file is copied by the
kit sync task into the next migration number for each service. The billing constraint changes follow
in their own additive migration. No applied migration is rewritten.

`consumer_failures.failure_category` is `decode_failed`, `version_unknown`, or `handler_failed`.
Resolution is `replayed` or `acknowledged`. Unresolved rows have no resolution fields. A replayed row
has no operator identity or repair reference. Only a `decode_failed` row may be acknowledged, and it
requires both. The repair reference is 1 through 200 ASCII letters, digits, dots, underscores, colons,
slashes, or hyphens. A partial index on consumer, source topic, partition, offset, and tutor covers
unresolved barrier checks. Repeated parking of the same source coordinate keeps one row.

### State transitions

```text
rate command
  received -> class locked -> receipt claimed -> revision incremented -> dated fact committed with outbox
  same idempotency key and same input -> original response
  same idempotency key and different input -> conflict

consumer failure
  absent -> unresolved after dead letter park -> resolved by successful replay
  unresolved unreadable record -> acknowledged only by an operator after repair

consumer readiness
  uncertified -> replaying with manifest -> certified for topic identity and partitions
  certified -> topic identity changed, partitions changed, database rebuilt, or replay reset -> uncertified

billing period
  unissued -> preview ready -> issued generation 1
  unissued -> preview blocked -> unissued
  live generation N -> repeated issue -> same generation N
  live generation N -> superseded by feature 15 -> replacement generation N plus 1

invoice
  prospective preview only -> issued immutable record
  issued -> PDF attached by feature 14
  issued -> voided and directly replaced by feature 15
```

### Projection readiness protocol

1. Billing verifies that `billing.teaching` is certified for the live topic identity and exact
   partition set. Certification names a completed replay manifest for the current projection
   generation. Missing group offsets, a recreated topic, an uncertified database, or a changed
   partition set fails closed.
2. Billing asks Redpanda for the current next offset of every `teaching.events` partition and captures
   those values once. It does not chase a moving topic tip.
3. Billing waits until the `billing.teaching` consumer group's contiguous committed next offset
   reaches each captured end. A source offset commits only after its projection transaction succeeds
   or its dead letter and durable failure row both exist.
4. Billing opens a repeatable read transaction and takes a share lock on the current
   `consumer_readiness` row. It rechecks certification and queries every unresolved failure visible
   for the tutor, not only failures before the barrier. A matching tutor row or a row with no
   `tutor_id` fails the proof. The same snapshot supplies structure checks and every calculation read.
5. Preview returns without writing. Issue compares the supplied fingerprint and writes authoritative
   records in its transaction. Replay reset or certification needs an update lock on the same
   readiness row, so it cannot cross an active calculation.

A teaching fact published after the captured barrier may already appear in the database snapshot, so
the barrier is a minimum published cut rather than a frozen stream snapshot. Every unresolved failure
visible in that snapshot still blocks. A teaching transaction whose outbox row has not yet reached
Redpanda is outside this proof. Browser initiated teaching writes remain visibly syncing until their
returned causal revision or version appears in the relevant projection.

### Monthly calculation

The period is the closed date range for `period_year` and `period_month`. Eligibility begins with
every projected session whose `cancelled_at` is empty and every roster period covering its
`local_date`. Teaching represents both tutor cancellation and schedule supersession through
`teaching.session.cancelled`, so billing needs no separate superseded field. The roster predicate
stays verbatim:

```sql
effective_from <= $local_date
AND (effective_to IS NULL OR $local_date <= effective_to)
```

One candidate is one distinct student and session pair after inclusive roster coverage and before
attendance filtering. Overlapping coverage for the same pair is `projection_incomplete`, never a
duplicate line. Structure queries begin from sessions and roster rows and use outer joins or explicit
missing row checks, so an absent label cannot disappear.

Every candidate row retains its attendance state for blockers and the fingerprint. Only `Present`
becomes an invoice line. Its rate is the newest class rate whose `effective_from` is not later than
the session date. Rows are sorted by student identifier, session date, and session identifier before
canonical JSON encoding. Student groups are sorted by student identifier.

The issue transaction inserts the run first at the current allowed generation, takes one number only
for each nonempty student group, inserts the invoice and its lines, and inserts its issued event into
the outbox. A unique or serialization loser rolls back fully. It then uses a fresh transaction to
read the winner, or retries a serialization failure at most three times when no winner exists. No row
from the preview itself is persisted.

### API surface

All operations require the existing bearer token. The gateway takes `tutor_id` from its verified
`sub` claim and never accepts it as input. `api/openapi.yaml` is the only public contract, and every
response below sends `Cache-Control: no-store`.

| Endpoint | Method | Key inputs | Key outputs | Auth | Key errors |
|---|---|---|---|---|---|
| `/api/classes/{class_id}/rates` | `GET` | owned `class_id` | teaching current rate and revision, allowed date range, billing effective schedule newest first when available, projected revision, history state | bearer | `401`, `404` |
| `/api/classes/{class_id}/rates/{effective_date}` | `PUT` | path date, `rate_amount`, `Idempotency-Key` | saved dated rate and revision, resulting current rate and date, allowed range, immutable invoice warning, `pending` history state | bearer | `400`, `401`, `404`, `409 idempotency_conflict`, `409 archived_class_has_no_sessions` |
| `/api/billing-periods/default` | `GET` | none | server date, verified timezone, default completed year and month, minimum year | bearer | `401 invalid_token` |
| `/api/billing-periods/{year}/{month}` | `GET` | completed year and month | `unissued` or `already_issued`, current run, invoice summaries and frozen lines | bearer | `400`, `401` |
| `/api/billing-periods/{year}/{month}/preview` | `POST` | completed year and month | `ready`, `blocked`, `empty`, or `already_issued`; lines, totals, blockers, fingerprint when ready | bearer | `400`, `401`, `409 projection_failed`, `409 projection_incomplete`, `409 projection_uncertified`, `422 period_too_large`, `503 projection_sync_pending`, `503 projection_unavailable` |
| `/api/billing-periods/{year}/{month}/issue` | `POST` | completed year and month, `preview_fingerprint` | `201` new or `200` existing run, invoices, frozen lines | bearer | `400`, `401`, `409 preview_stale`, `409 projection_failed`, `409 projection_incomplete`, `409 projection_uncertified`, `503 projection_sync_pending`, `503 projection_unavailable` |

Preview returns stable blocker codes `profile_incomplete`, `attendance_incomplete`, and
`rate_missing`, ordered by code, student, date, session, then field. Safe details contain only stable
field codes and owned record identifiers plus recovery destinations. An empty result exists only
after every attendance obligation is marked and no Present line remains. Profile completeness is then
irrelevant because no invoice can be issued.

Both POST operations first validate the period and read an existing live run. An existing run returns
without broker access, fingerprint work, or number allocation. Otherwise broker certification and
availability precede calculation. `projection_failed` takes precedence over structure, blockers, and
fingerprint comparison. Its details contain only failure category, safe source coordinates,
`request_id`, and operator guidance. Preview reports calculation blockers. Issue accepts only a previously ready
fingerprint, so a newly blocked, empty, oversized, or changed result is `preview_stale` and contains no
replacement preview. The browser fetches the new state separately.

`GET /api/classes/{class_id}/rates` is the gateway's allowed read fan out. Teaching proves ownership
and supplies authoritative current state. Billing supplies the effective schedule and its greatest
projected revision plus the revision on each effective date. A requested correction is synced only
when that exact effective date row reaches or passes the command revision. A higher revision on a
different date is not proof. A missing or older billing projection returns `history_state: syncing`;
a billing outage returns the current teaching rate with `history_state: unavailable` and no claimed
history. For an active class, the date range begins at its earliest retained session date, or its
current `rate_effective_from` when no session remains, and ends today. An archived class uses its
earliest retained session date through its archive date.

### Operator surface

The shared `consumerfailures` command has `list`, `acknowledge`, and `certify` operations. `list` takes
a service and shows safe unresolved coordinates and categories. `acknowledge` takes the service,
consumer, topic, partition, offset, an enumerated resolution code, and a constrained repair reference.
It accepts only `decode_failed`, with resolution code `source_repaired` or `projection_restored`. It
records the current operating system user and refuses an absent, resolved, or readable failure.
`certify` records the live topic identity and exact partition set only
after a ledger aware replay from every earliest retained offset reaches the captured ends with no
unresolved failure. The manifest records earliest, captured end, and completed next offsets for every
partition plus start, completion, and operator identity. Any earliest offset above zero fails
certification. Trusted snapshot restore is not designed here, so a retained-history gap remains
blocked. The replay command takes an update lock, creates a new projection generation and manifest,
and clears certification before it resets offsets.

The task wrapper uses the selected service's existing database environment variable. The command
never reads or prints a dead letter payload.

### Value sourcing

| Action | Value produced or displayed | Source |
|---|---|---|
| Validate a rate date | Today and archive local date | Server time resolved in the timezone claim of the verified token; `teaching.classes.archived_at` |
| Validate a correction | Allowed date range | Active class: minimum retained session date or current rate date through today. Archived class: minimum retained session date through archive date; a null minimum produces `archived_class_has_no_sessions` |
| Apply a rate command | Current rate, date, and revision | Locked `teaching.classes`; revision increments for every newly accepted command; current fields update only when the date is at least the stored date |
| Publish a rate fact | Identifier, amount, currency, date, revision, tutor | Owned class, validated request, fixed VND, incremented revision, verified token; event name and key from specs 0001 and 0013 |
| Show rate history | Current and dated rows, revisions, history state | Teaching current class read plus owned `billing.class_rates`; the exact effective date row reaching the command revision ends syncing |
| Choose the initial month | Server date, timezone, most recent completed month, minimum year | `GET /api/billing-periods/default`, using one server UTC clock resolved in the verified IANA timezone claim |
| Prove certification | Projection generation, topic identity, partition set, and replay evidence | Share locked `consumer_readiness`, its completed `consumer_replay_manifests` row, and live Redpanda metadata |
| Prove published progress | Fixed partition barriers | Redpanda next offsets and contiguous committed `billing.teaching` group next offsets through existing `franz-go` and `kadm` support |
| Refuse an unresolved failure | Tenant and source position | Every unresolved `consumer_failures` row visible in the repeatable read snapshot for that tutor or with no tutor |
| Build candidate rows | Sessions and roster membership | `billing.sessions` in the period joined to `billing.roster_periods` by the accepted inclusive predicate |
| Decide attendance | Present, Absent, or unmarked | `billing.attendance`; a missing row is unmarked and blocks issue |
| Select a rate | Rate and currency in force | Latest owned `billing.class_rates` row on or before session `local_date` |
| Show names | Student and class labels | Owned `billing.students.name` and `billing.classes.name` projections |
| Show profile blockers | Completeness and missing fields | A missing profile row is incomplete; required fields are `legal_name`, `contact_line`, `bank_code`, derived `bank_name`, `bank_account_number`, and `bank_account_holder`; stable codes come from the same row |
| Compute line and totals | Integer VND amounts | One rate per Present session; sums use checked `int64` arithmetic and the 10,000 row bound |
| Compute the fingerprint | Stable digest | Version 1 typed canonical payload defined below and encoded with explicit nulls and fixed array order |
| Select generation | Generation 1 or existing run | Existing live run read first; feature 15 is the only feature that may open a later generation |
| Allocate invoice identity | Run, invoice, line, and event identifiers | UUIDv7 values generated in billing inside the issue command |
| Allocate invoice number | Year and sequence | Counter row locked by its upsert inside the transaction; one sequence per tutor and period year, formatted with at least four digits and allowed to expand |
| Set issue time | Run creation, invoice issue, outbox occurrence | One PostgreSQL `transaction_timestamp()` read and reused for the whole run |
| Freeze invoice values | Period, student, payee, line and total values | Requested completed period and the recomputed canonical input inside the issue transaction |
| Publish an issued event | Invoice identifier, number, tutor, student, period, total, currency, issue time, PDF location | Frozen invoice row; `pdf_location` is JSON null in feature 13 because feature 14 owns rendering |
| Resolve ownership | `tutor_id` | Verified token `sub` for handlers, event envelope for consumers |
| Recover a lost issue response | Whether issue committed | `GET /api/billing-periods/{year}/{month}` from billing authoritative records |

### Rate command and fingerprint contracts

A rate receipt key is scoped by trusted tutor and operation `put_class_rate`. Its request hash covers
the operation, owned class identifier, ISO `effective_date`, integer `rate_amount`, and fixed `VND`.
After authentication and basic request parsing, receipt lookup happens before mutable class, archive,
or allowed range validation. Equal normalized input returns the stored status and response even when
the class later changes. Different input returns `idempotency_conflict`. A new key then passes current
validation before any state change. Receipts are immutable and retained with the class audit history.

New `teaching.class.created` and `teaching.class.rate.changed` facts carry `rate_revision`. Retained
version 1 events may omit it and project as revision 0. Billing stores source partition and offset on
each dated row. A legacy event updates another revision 0 row only when it comes from the same
partition and has a greater source offset, or when the row has no backfilled source coordinate during
the required first full replay. It never overwrites a positive revision. New events update a same
date row only when their positive revision is greater. The shared replay preserves original topic,
partition, and offset from the source record or dead letter record. A safe no operation still inserts
`handled_events` and resolves that exact failure coordinate atomically.

Fingerprint schema version 1 is a Go struct tree, never a map. JSON fields appear in declared order.
UUID values use lowercase canonical text, dates use `YYYY-MM-DD`, integers use JSON integers, optional
values are explicit `null`, and UTF 8 labels use their stored normalized values. The root contains:

1. `schema_version`, fixed at 1, `readiness_version`, fixed at 1, `tutor_id`, `period_year`, and
   `period_month`.
2. `profile`, containing revision plus legal name, contact line, bank code, bank name, account number,
   and account holder, with explicit nulls.
3. `candidates`, ordered by student identifier, session date, and session identifier. Each contains
   student identifier and name, session identifier, class identifier and name, local date, attendance
   state including `Unmarked`, and either an explicit null rate or rate amount, currency, effective
   date, and revision.
4. `blockers`, ordered by code, student identifier, date, session identifier, then field code.
5. `student_totals`, ordered by student identifier, plus `grand_total`.

Cancelled rows are absent, so a cancellation changes the digest. The digest is
`base64.RawURLEncoding` of SHA 256 over the encoded bytes. Golden vectors cover nulls, Vietnamese text,
zero rates, several students, and every blocker. Only the server creates or verifies this payload.

### Key invariants

1. Teaching owns the current class rate and publishes dated facts. Billing owns dated rate history and
   every money calculation. Neither service calls the other.
2. Every rate write and its outbox fact share one teaching transaction. Every issue record and issued
   outbox fact share one billing transaction.
3. A broker barrier proves a minimum published cut, not that every committed teaching outbox row is
   published. It is ready only when certification matches live topic metadata, contiguous committed
   offsets reach the captured ends, and no applicable unresolved failure is visible in the calculation
   snapshot.
4. Preview is advice, not a record. Only issue creates authoritative money state.
5. Preview and issue each use one repeatable read snapshot. A supplied fingerprint must equal the
   recomputed snapshot.
6. Only Present bills. Unmarked blocks. Absent, projected cancelled, and out of roster rows never
   become lines.
7. Every invoice has at least one line. A zero amount line is valid. Total equals the checked sum of
   line amounts.
8. One partial unique constraint permits one live run per tutor and period. That run absorbs every
   retry. Only feature 15 may supersede it after a deliberate void.
9. An invoice and its lines never change after issue. A later rate, name, profile, attendance, move,
   or cancellation changes only a future deliberate replacement.
10. No replay writes authoritative billing tables. A replay may resolve only its matching shared
    consumer failure row while it applies or safely rejects the projection fact.
11. Every database read and write matches the trusted `tutor_id`. A request never supplies it.
12. No page, API detail, log, or failure row exposes data outside the minimum its recovery action
    needs.
13. The billing run is the aggregate and consistency boundary for issue. Its per year counter row is
    supporting transactional allocation, not a separately writable business aggregate.

### Security model

The feature is private to the single signed in tutor. It is a tuition statement and payment request,
not a statutory tax or electronic invoice. Student names and financial amounts remain private
business data. Student phone numbers never enter billing. Bank values stay in the invoice profile and
frozen invoice columns, and preview exposes only completeness and missing field codes. Every real
profile change increments `revision`; an identical retry does not. The frozen payee columns are legal
name, contact line, bank name, bank account number, and bank account holder.

Browser data lives only in tutor scoped TanStack Query memory keys. Sign out cancels in flight reads
and removes rate and billing keys before another tutor can render. The selected month may remain in
the URL because it contains no private value. Mutation logs follow **AC-23**, while immutable money
records, command receipts, rate events, and outbox rows form the audit trail.

### Browser behavior

The existing class detail page gains current rate, dated history, and an accessible rate dialog. A
confirmed teaching response renders immediately, while history announces that it is syncing and
refetches until billing's exact effective date row reaches the returned revision. A greater revision
on another date does not finish the wait. A greater revision on the same date means a newer correction
replaced this one and the page says so. It never invents an optimistic projection row.

The new Billing route validates `year` and `month`, defaults them as **AC-4** states, and includes both
values and the tutor identifier in hierarchical query keys. Its loader uses the shared query options,
and every request passes the cancellation signal to `openapi-fetch`. Rate mutation invalidates the
owned class, rate, and every billing period key for that tutor. Attendance invalidates its session
month. A session move invalidates its old and new month. Roster, student label, profile, and rate
changes invalidate every unissued billing period key for that tutor. Issue invalidates the selected
period and related billing keys. Sign out cancels and removes their common tutor scoped prefixes.

Preview separates billable lines from blockers. The Issue button remains disabled while blocked or
pending, but the reason is visible text and an announced status rather than color alone. A stale
preview keeps the selected month, explains that facts changed, loads a new preview, and requires a
new button press. Controls use semantic elements, visible focus, 44 pixel targets, and live regions
for syncing, success, and error changes.

Blocker recovery uses typed destinations. `profile_incomplete` links to `/profile`.
`attendance_incomplete` links to `/` with validated `date` and `session` search values and opens the
owned attendance sheet. `rate_missing` links to `/classes/{class_id}` with validated `rateDate` and
opens the rate dialog. `projection_incomplete` offers retry and operator guidance only.

### Configuration required

No new environment variable, credential, provider, or feature flag is required. The five second
barrier timeout and one second retry hint are versioned application constants. Existing Postgres and
Redpanda configuration remains authoritative.

### Critical test scenarios

1. Happy path: change one class rate, consume its event, preview a completed month with one Present
   session, issue once, and read the same frozen invoice again, verifies **AC-1**, **AC-2**, **AC-5**,
   **AC-6**, **AC-12** to **AC-18**.
2. Rate history: insert older, same date, and latest changes concurrently, retry each receipt, and
   prove existing revision 0 and new revision 1 baselines converge, exact date syncing works, a
   matching receipt replays after archive, and source offsets or positive revisions prevent an older
   event from replacing a newer correction, verifies
   **AC-1** to **AC-3**, **AC-19**.
3. Eligibility table: cover Present, Absent, unmarked, cancelled, superseded, roster boundary, rejoin,
   zero rate, missing rate, removed student label, and several classes for one student, verifies
   **AC-5** to **AC-7**, **AC-15**.
4. Readiness: hold one partition behind, capture fixed next offsets while new events continue, reach
   the original barrier, and prove the snapshot includes at least that published cut without waiting
   for the later topic tip, verifies **AC-8**.
5. Dead letter and certification: deploy the ledger against existing offsets, prove issue remains
   uncertified, replay from every earliest retained offset, certify the topic identity and partitions,
   then park decoded and unreadable events and prove scoped blocking, exact replay resolution, strict
   acknowledgement, topic recreation refusal, and private data exclusion, verifies **AC-9** to
   **AC-11**, **AC-23**, **AC-24**.
6. Snapshot and stale preview: mutate rows between individual preview reads and prove one repeatable
   snapshot, then change attendance, rate, label, session date, or profile revision before issue and
   prove each returns `preview_stale` with no write, verifies **AC-12**, **AC-13**.
7. Atomicity: fail after run, number, invoice, line, and outbox steps in turn and prove each rollback
   leaves no record or spent number, verifies **AC-14**, **AC-15**.
8. Concurrency: issue the same period concurrently, force unique and serialization failures, and prove
   rollback plus a fresh winner read yields one live run, one invoice per student, stable numbers, and
   the same response, verifies **AC-16**, **AC-17**.
9. Recovery: lose the successful issue response, disable Redpanda, reload the period, and prove the
   existing run stays readable and no duplicate is issued, verifies **AC-17**, **AC-19**.
10. Bounds: exercise exactly 500 students and 10,000 candidates, then exceed each bound and prove the
    larger preview returns `period_too_large`, a formerly ready issue returns `preview_stale`, and both
    write nothing, verifies **AC-20**.
11. Contract and browser: regenerate Go and browser types, test all states and stable errors, sign out
    during an in flight preview, assert `Cache-Control: no-store`, exact invalidations and recovery
    links, and verify keyboard, focus, announcements, 44 pixel targets, phone width, and 200 percent
    zoom, verifies **AC-4**, **AC-21**, **AC-22**.
12. Ownership and replay: attempt cross tutor reads and references, then replay all teaching facts
    after issue and prove projection equality, resolved failure behavior, and byte for byte unchanged
    authoritative business columns, verifies **AC-9**, **AC-15**, **AC-21**, **AC-23**, **AC-24**.

## Build plan

Tracer Bullet means the first slice proves one dated rate through Redpanda into one reviewed and
issued invoice. Later slices widen that working thread rather than building every backend layer before
the browser can exercise it.

1. Add the canonical additive `consumer_failures` and `consumer_readiness` goose migration under
   `pkg/vermouth/ddl`, including durable replay manifests and projection generations, extend
   `task migrate:sync-kit` with each service's next local migration number, and add drift, up, down,
   tenant scope, category, resolution, and certification tests. Extend the shared consumer so park
   records the failure before offset commit and every successful or safe no operation replay resolves
   the exact source coordinate with the projection transaction. Extend replay to take the readiness
   lock, create its manifest, clear certification before reset, and record completed offsets. Add
   strict list, acknowledge, and certify operator commands, satisfies
   **AC-9**, **AC-10**, **AC-23**, **AC-24**.
2. Add teaching and billing rate revision migrations, legacy revision 0 replay compatibility, and
   existing class revision 0 and new class revision 1 baselines, legacy source coordinates, and
   billing's additive live run, invoice, and replacement uniqueness constraints. Add typed SQL for
   certification, unresolved failure checks,
   candidate calculation, current run reads, invoice insertion, lines, numbers, and outbox work. Keep
   every query tenant scoped and regenerate committed `sqlc` output, satisfies **AC-5** to **AC-7**,
   **AC-9** to **AC-16**, **AC-20**, **AC-24**.
3. Prove the first vertical thread. Add the teaching dated rate command and event, billing projection
   handling, the fixed Redpanda barrier, canonical preview, repeatable read issue transaction, the six
   public OpenAPI operations, gateway routes, generated types, a minimal class rate control, and a
   minimal Billing page for one student and one Present session, satisfies **AC-1**, **AC-2**, **AC-4**
   to **AC-8**, **AC-12** to **AC-17**, **AC-21**, **AC-23**.
4. Complete rate management with archived corrections, same date replacement, concurrency and receipt
   replay, full history syncing, validation, stable errors, and class page accessibility, satisfies
   **AC-1** to **AC-3**, **AC-19**, **AC-21** to **AC-24**.
5. Complete monthly eligibility and review with all classes, roster boundaries, attendance blockers,
   profile blockers, missing rates, zero rates, empty months, checked totals, the 500 student and
   10,000 candidate limits, and detailed phone first preview states, satisfies **AC-4** to **AC-7**,
   **AC-11**, **AC-12**, **AC-20** to **AC-24**.
6. Harden readiness and recovery with topic certification, full rollout replay, fixed partition
   barriers, exact next offset arithmetic, five second timeout, Redpanda outage, metadata changes,
   decoded and unreadable dead letters, replay resolution, strict operator actions, stale preview
   refresh, lost responses, and privacy safe logs, satisfies **AC-8** to **AC-13**, **AC-17**,
   **AC-19**, **AC-23**, **AC-24**.
7. Close the money transaction with injected failures at every write, concurrent issue, duplicate
   requests, fresh winner reads, live run, invoice and replacement uniqueness, immutable snapshot
   tests, outbox assertions, and
   existing period reload, satisfies **AC-14** to **AC-18**, **AC-23**, **AC-24**.
8. Regenerate every SQL and public type, extend `task thread` through rate, preview, issue, and reload,
   run the real Postgres and Redpanda suite, add Vitest component coverage, and record GA verification
   evidence for every acceptance criterion, satisfies **AC-1** to **AC-24**.

## Consequences

**Positive**:

1. A tutor sees the exact inputs before money becomes immutable.
2. Retries, concurrent presses, event lag, and dead letter records fail safely without duplicate or
   partial invoices.
3. The design reuses existing services, tables, broker, database, contracts, and browser libraries.
4. The shared failure ledger and certification make known projection failures visible to every future
   projection reader.

**Negative and tradeoffs**:

1. Preview and issue each wait for a broker barrier and calculate the month, so review adds latency
   and duplicate read work.
2. Repeatable read is weaker than serializable isolation. Correctness therefore also depends on the
   fixed broker cut, immutable preview comparison, row locks, and database uniqueness constraints.
3. A failure without a readable tutor blocks all billing runs until an operator repairs or explicitly
   acknowledges it. This is disruptive by design because guessing would risk wrong money.
4. Rate history is unpaged. This is acceptable for one independent tutor and infrequent class rate
   changes, but the endpoint must gain cursor pagination before the product supports bulk imported or
   institutional rate schedules.
5. Feature 13 creates invoice records without PDFs and without the void action. The following roadmap
   features must finish those user journeys without changing the frozen money model.
6. The barrier proves consumption of published events, not publication of every committed teaching
   outbox row. Known browser writes therefore remain visibly syncing until their causal revision or
   version arrives, and the tutor still reviews the complete calculated input before issue.

**Neutral**:

1. Additive migrations land for shared consumer failure and certification state, teaching and billing
   rate revisions, and billing uniqueness constraints.
2. A zero total invoice remains valid when it contains at least one genuinely free Present session.
3. No external service, secret, environment variable, background job, or new JavaScript package is
   introduced.

## Follow-up

1. Feature 9 should alert on unresolved `consumer_failures`, sustained consumer lag, repeated
   `projection_sync_pending`, and money transaction failures.
2. Feature 14 should render and store PDFs only from the frozen invoices and lines defined here.
3. Feature 15 should implement the deliberate void with reason and the one direct replacement flow,
   using the generation contract defined here and in spec 0001.
4. Cursor pagination should be designed before rate histories can be bulk imported or managed by a
   teaching center.
5. Feature 14 should decide whether consumers need a new PDF ready fact. The immutable
   `billing.invoice.issued` fact created here records `pdf_location` as null at issue time and cannot
   be rewritten later.
6. If friend testing shows that review plus visible causal syncing is not enough, design a teaching
   outbox fence before promising that billing includes every committed but not yet published teaching
   write. A broker end offset alone can never make that stronger promise.

## Rationale

Reasoning and options: see [rationale.md](rationale.md).
