# 0009. Core teaching loop

**Date**: 2026-08-29
**Status**: Accepted

## Summary

The home screen becomes the first complete teaching path. A tutor creates a class and its first
session, adds a student, joins that student to the class, marks attendance, and sees the saved state
after reload. The same writes travel through the outbox and Redpanda into billing, while the gateway
keeps the teaching facts authoritative and treats billing progress as an optional panel.

## Requirements

**User stories**:

1. As a tutor, I want to set up one class and mark attendance from my phone so that today's teaching
   work lives in one place.
2. As a tutor, I want corrections and interrupted setup work to be safe so that a retry does not
   create duplicate records or lose entered values.
3. As the engineer, I want one teaching fact to reach a rebuildable billing projection so that the
   service boundary is proved before the money features depend on it.

**Acceptance criteria**:

1. **AC-1**: A signed in tutor can open one guided sheet from home, enter a class name, a VND rate,
   an optional class color choice, one local session date with start and end times, one student name
   with an optional phone, then complete roster membership without leaving the screen.
2. **AC-2**: Class creation writes the class, its first session, one command receipt, and the
   `teaching.class.created` plus `teaching.session.scheduled` outbox rows in one transaction. The
   rate effective date and stored `local_date` equal the submitted session date, currency is `VND`,
   identifiers are UUIDv7 values generated in Go, and no partial class survives a failed commit.
3. **AC-3**: A class stores one of the seven colors from spec 0008. The tutor may choose one during
   setup, or teaching derives the stored suggestion from the generated `class_id` with the same FNV
   1a contract. Home renders the stored value, so color remains stable across reloads.
4. **AC-4**: Student creation stores a trimmed name and an optional phone, writes a command receipt
   and `teaching.student.registered` in the same transaction, and never places the phone in an event
   or home response. Joining writes an open roster period effective on the first session
   `local_date` and `teaching.roster.joined`; repeating the same join returns the existing period and
   publishes nothing.
5. **AC-5**: Attendance starts unmarked and may become `Present` or `Absent` only when the student's
   roster period covers the session `local_date`. A correction updates the same row, the last
   committed state wins, each real state change writes `teaching.attendance.marked` in the same
   transaction, and a repeated identical state publishes nothing.
6. **AC-6**: The saved attendance state and `marked_at` survive a reload. Another tutor cannot read,
   join, or mark any class, session, or student, and a foreign tenant identifier returns the same
   `404` as a missing identifier.
7. **AC-7**: `GET /api/home` returns the tutor from identity, the verified request timezone plus the
   current local date and setup defaults from teaching, and active sessions for that date in pages
   of 50 ordered by `(starts_at, session_id)`. Active means the session is not cancelled, its class
   is not archived, each returned student is not removed, and the inclusive roster period covers
   `local_date`. Each session carries its stored class name and color, UTC times, covered students,
   nullable attendance state, and nullable `marked_at` from teaching.
8. **AC-8**: Billing consumes the five teaching facts used by this thread and writes its existing
   class, rate, session, student, roster, and attendance projections inside the shared handled event
   transaction. A duplicate delivery and a replay leave one correct projection row per event key and
   never touch billing's authoritative records.
9. **AC-9**: The home read fans out to identity and teaching as required panels and billing as an
   optional panel. Billing reports per tutor projection counts plus its latest update time. It is
   `waiting` until the first class, session, student, open roster period, and attendance row exist,
   then `active`; billing delay or failure never blocks setup or attendance.
10. **AC-10**: `POST /api/classes` and `POST /api/students` require `Idempotency-Key`. Retrying the
    same validated command returns the original resources with `200`, including after a lost
    response. Only the request that creates the rows returns `201`. Reusing the key for different
    input returns `409` and creates nothing.
11. **AC-11**: Inputs enforce the confirmed limits. Names contain 1 through 120 Unicode characters
    after outer whitespace is trimmed. Phone is empty or at most 40 characters and receives no
    format normalisation. Rate is an integer from 0 through 1,000,000,000 dong. Session end is later
    than start on the same local date. Missing or repeated local clock times are rejected rather
    than resolved silently. Dates use OpenAPI `format: date`; local clock inputs use 24 hour
    `HH:mm` strings without an offset.
12. **AC-12**: Every route requires the existing bearer token and returns the existing `APIError`
    shape. Invalid input is `400`, an invalid token is `401`, a missing owned resource is `404`, a
    state or idempotency conflict is `409`, and a required upstream failure or timeout at the
    gateway is `502`. A billing timeout degrades only its optional panel.
13. **AC-13**: The home screen follows `web/design.md`. It places the local date and setup action
    first, chronological session cards and attendance next, then billing status in the wide
    contextual panel or after sessions on phones. Loading, empty, pending, unavailable, success, and
    field error states remain keyboard usable, work at 200 percent zoom, expose visible focus and
    names, use 44 pixel targets, and announce dynamic results without relying on color alone.
14. **AC-14**: The guided draft, current step, idempotency keys, and created identifiers survive a
    reload in the same tab through tutor scoped `sessionStorage`. Completion, sign out, or a tutor
    change clears them. A mutation failure keeps values and the current step. A future session shows
    its local date in the success message and does not appear in today's session list.
15. **AC-15**: Home invalidates after each successful mutation, polls billing once per second for at
    most ten seconds through `GET /api/home/billing-projection` while it is waiting, and then offers
    manual retry. It refetches when the window regains focus and at the next tutor local midnight.
    Attendance remains usable during every projection state.
16. **AC-16**: `api/openapi.yaml` is the source for every public request and response type. Generated
    Go and browser types are current, the old `/api/thread` contract is removed, `task thread` drives
    the new teaching path, and the feature adds no environment variable, secret, or external
    account.

## Decision

**Chosen option**: Option 1: guided home flow with explicit teaching resources and an observable
billing projection

Build one phone first setup flow over small resource commands, then read identity and authoritative
teaching facts through an aggregated home response while billing exposes only its own projection
progress. Keep partial business facts instead of inventing a distributed rollback, and make class
and student creates safe through durable command receipts.

**Implementation skills**: `openapi` (`oakoss/agent-skills`, `.agents/skills/openapi/`) ·
`golang-database` (`samber/cc-skills-golang`, `.agents/skills/golang-database/`) ·
`kafka-development` (`mindrally/skills`, `.agents/skills/kafka-development/`) ·
`tanstack-query-best-practices` (`deckardger/tanstack-agent-skills`,
`.agents/skills/tanstack-query-best-practices/`) · `tailwindcss-accessibility`
(`josiahsiegel/claude-plugin-marketplace`, `.agents/skills/tailwindcss-accessibility/`)

## Rationale

Reasoning and options: see [rationale.md](rationale.md).

## Feature design

### User flow and page composition

1. Home opens with the tutor's local date and a primary `Create a class` action.
2. `TeachingSetupSheet` collects class name, rate, and either `Suggest for me` or one of the seven
   stored colors. The next step collects local date, start, and end. The final step collects student
   name and optional phone, then joins that student to the class.
3. Every completed server step remains committed. The sheet stores only the draft and command
   identifiers needed to continue. A failed step stays open with its boundary error and the same
   action available again.
4. `SessionAttendanceCard` renders one session, its class marker, local time, rostered students, and
   a labelled radio group with unmarked, `Present`, and `Absent` states. A polite live region reports
   a saved correction.
5. `BillingProjectionPanel` is secondary. It lives in the existing wide contextual panel and after
   the session list on phones. The screen uses existing primitives and Lucide icons, with no image
   asset.
6. Visible copy is English in this slice and stays in callers. Feature 20 adds translation state and
   Vietnamese as the default language.

### Data model sketch

The existing teaching and billing tables from spec 0003 remain authoritative. One additive teaching
migration extends them as follows.

| Table | Key | Fields used or added here | Constraints and relationships |
|---|---|---|---|
| `classes` | `class_id` | Existing fields plus required `color text` | `color` is one of `red`, `rose`, `orange`, `green`, `blue`, `yellow`, `violet`; existing rows are backfilled to `blue`, and no database default remains |
| `sessions` | `session_id` | `class_id`, `tutor_id`, `starts_at`, `ends_at`, `local_date` | Existing composite tenant foreign key to class; one class may have many sessions |
| `students` | `student_id` | `tutor_id`, `name`, nullable `phone` | Existing tutor scoped key; one student may join many classes |
| `roster_periods` | `(class_id, student_id, effective_from)` | `tutor_id`, nullable `effective_to` | Existing tenant foreign keys; at most one open period for the pair; coverage is inclusive |
| `attendance` | `(session_id, student_id)` | `tutor_id`, `state`, `marked_at` | Existing tenant foreign keys; state is `Present` or `Absent`; coverage is checked before write |
| `command_receipts` | `(tutor_id, operation, idempotency_key)` | `request_hash bytea`, `primary_resource_id uuid`, nullable `related_resource_id uuid`, `created_at timestamptz` | `operation` is `create_class` or `create_student`; key length is 1 through 128; immutable and retained |
| Billing projection tables | Existing event keys | Existing classes, class rates, sessions, students, roster periods, and attendance | Written only by `internal/consumer`; no new billing column or migration |

`command_receipts` has no polymorphic foreign key. The owning transaction writes it beside the
business rows and outbox rows. The request hash is SHA 256 over the operation's validated canonical
command struct. A concurrent loser rolls back its generated business identifiers, reads the winning
receipt, and returns the owned resources named there.

Class color is teaching truth only. No billing or notification behavior uses it, so it is absent
from the event catalogue and never copied across a service boundary.

The class aggregate includes the class, its current rate, and its first concrete session during
initial creation. Later session commands may operate on an existing class, but the first session is
part of the initial class invariant and commits with it. Student is a separate aggregate, and roster
membership is a separate class command.

### State transitions

```text
setup in this tab
  class and session pending -> class and session saved -> student pending
  student saved -> roster pending -> complete -> draft cleared
  any failed step -> same step with values and command key retained

attendance
  unmarked -> Present
  unmarked -> Absent
  Present <-> Absent
  repeated current state -> no write and no event

billing projection panel
  waiting -> active
  unavailable is a response condition, not persisted state
```

### API surface

Public schemas and operations live in `api/openapi.yaml`. Gateway routes map them to constant service
paths and pass the verified bearer token plus `X-Request-Id` unchanged.

| Endpoint | Method | Key inputs | Key outputs | Auth | Key errors |
|---|---|---|---|---|---|
| `/api/classes` | `POST` | `Idempotency-Key`; name; optional color; `rate_amount`; `first_session.local_date` as `date`; `start_time` and `end_time` as `HH:mm` | `201` created or `200` replay; class with stored color and VND rate; first session with UTC times and local date | bearer | `400`, `401`, `409` |
| `/api/students` | `POST` | `Idempotency-Key`; name; nullable phone | `201` created or `200` replay; student including phone | bearer | `400`, `401`, `409` |
| `/api/classes/{class_id}/roster` | `POST` | `student_id`, `effective_from` as `date` | `201` new period or `200` exact existing period | bearer | `400`, `401`, `404`; `409` for another open effective date |
| `/api/sessions/{session_id}/attendance/{student_id}` | `PUT` | state | `200`; canonical attendance row | bearer | `400`, `401`, `404`, `409` |
| `/api/home` | `GET` | optional opaque cursor | tutor, local day data, at most 50 sessions, next cursor, optional billing projection | bearer | `400`, `401`, `502` |
| `/api/home/billing-projection` | `GET` | none | current billing projection or unavailable state | bearer | `401` |
| `/projections/teaching/status` | `GET`, billing internal | none | state, five counts, nullable latest update | bearer verified locally | `401` |

The gateway calls identity `/me` and teaching `/home` as required reads, and billing
`/projections/teaching/status` as the optional read. The public billing projection endpoint calls
only the same billing status path, so browser polling never reloads identity or teaching.

The opaque session cursor is base64url encoded JSON containing `local_date`, RFC 3339 `starts_at`,
and `session_id`. Teaching rejects malformed cursors and cursors whose date differs from the current
request date with `400`. It fetches 51 rows, returns at most 50, and emits `next_cursor` only when the
extra row proves another page exists. The next query applies the same ordered
`(starts_at, session_id)` tuple, so equal start times neither duplicate nor skip a session.

All three home upstream calls use one code constant timeout, with no environment variable.
Identity or teaching timeout fails home with `502`. Billing timeout sets
`billing_projection_unavailable`. The billing only public endpoint returns that unavailable state
rather than failing the teaching screen.

The home response contains:

1. `tutor`, using the existing tutor schema from identity.
2. `request_time_zone`, `local_date`, `next_local_midnight_at`, and `setup_defaults` from teaching.
3. `sessions`, where each row contains `session_id`, `class_id`, `class_name`, `class_color`,
   `starts_at`, `ends_at`, `local_date`, and covered students with `student_id`, `name`, nullable
   `attendance_state`, and nullable `marked_at`.
4. Nullable `next_cursor`.
5. `billing_projection`, with `state`, `class_count`, `session_count`, `student_count`,
   `open_roster_count`, `attendance_count`, and nullable `latest_updated_at`.
6. Nullable `billing_projection_unavailable`, set by the gateway when only billing fails.

All public operations use reusable OpenAPI 3.1 component schemas, unique `operationId` values, the
existing bearer security scheme, and the existing error responses. The old thread operation and
schemas are removed rather than deprecated because the only client and driver move in the same
change.

### Value sourcing

| Action | Value produced or displayed | Source |
|---|---|---|
| Home read | tutor identity and profile timezone | Identity `/me`, reached by the gateway; never a projection |
| Home read | request timezone | Teaching returns the `tz` claim from the token it verified for this request |
| Home read | tutor current `local_date` | Teaching converts its UTC clock through that same request timezone |
| Home read | next local midnight instant | Teaching resolves the first instant of the next calendar date in the token timezone |
| Setup defaults | default date, start, and end | Teaching rounds its current local time to the next half hour and adds one hour; when that span leaves today it starts at the first valid half hour tomorrow |
| Setup form | edited local date and times | Tutor inputs, initially populated from `setup_defaults` |
| Create class | `tutor_id` | Verified token `sub` claim |
| Create class | class and session identifiers | UUIDv7 values generated in the teaching handler |
| Create class | class name and rate | Validated request fields |
| Create class | stored class color | Valid optional request color, else FNV 1a suggestion from the generated `class_id` |
| Create class | currency | Fixed `VND` |
| Create class | `rate_effective_from` and session `local_date` | Validated `first_session.local_date` |
| Create class | `starts_at` and `ends_at` | Submitted local date and times resolved in the token timezone, then stored as UTC instants |
| Create class | event keys and fields | Spec 0001 catalogue, using `class_id` for class and `session_id` for session |
| Create student | identifier, name, and phone | UUIDv7 from Go plus validated request fields; empty phone becomes null |
| Create student | published student facts | `student_id`, trusted `tutor_id`, and stored name; phone is excluded by spec 0001 |
| Join roster | class, student, and effective date | Path `class_id`, body `student_id`, and body `effective_from`; the setup uses its saved first session `local_date` |
| Mark attendance | class identifier | Teaching reads it from the owned session inside the attendance transaction |
| Mark attendance | state and `marked_at` | Validated state plus teaching's UTC transaction clock |
| Every event | `event_id`, `occurred_at`, `request_id`, version, topic, and key | Shared envelope and outbox code, with `request_id` from gateway context and catalogue version 1 |
| Create retry | command identity | Browser UUID in `Idempotency-Key`, scoped by trusted tutor and operation |
| Create retry | class request equality | SHA 256 over trimmed name, explicit nullable requested color, rate, local date, local start, local end, and verified token timezone; generated ids and derived color are excluded |
| Create retry | student request equality | SHA 256 over trimmed name and explicit nullable phone; generated id is excluded |
| Create retry | original resources | Receipt resource identifiers followed by tutor scoped reads of teaching owner tables |
| Create retry | response status | `201` when this request created the receipt, otherwise `200` when it read an equal existing receipt |
| Home session | class name and color | Teaching `classes` joined by matching `tutor_id` and `class_id` |
| Home session | student name and attendance | Covered teaching roster periods joined to teaching students and left joined to teaching attendance |
| Home session | active rows | Noncancelled sessions, nonarchived classes, nonremoved students, and spec 0003's inclusive roster coverage predicate |
| Home session | displayed local time | Stored UTC instant formatted with teaching `request_time_zone` and explicit English locale |
| Home page | class marker | Stored `class_color`, always paired with class name |
| Home pagination | next cursor | Base64url JSON of current `local_date` plus the last returned `(starts_at, session_id)`, emitted only after a 51st row is found |
| Billing status | five counts | Tutor scoped counts over billing classes, sessions, active students, open roster periods, and attendance projections |
| Billing status | latest update | Greatest nonnull `updated_at` across those projection tables and class rates |
| Billing status | `waiting` or `active` | `active` when all five displayed counts are greater than zero, otherwise `waiting` |
| Billing status | unavailable message | Gateway generated optional panel failure after a billing transport or non 200 response |
| Home failure | upstream deadline | One code constant applied to each identity, teaching, and billing call |
| Setup storage | tutor scope and draft key | Home tutor identifier plus versioned `vermouth.teaching-setup.v1` session storage key |
| Setup storage | idempotency keys | `crypto.randomUUID()` once per create step, reused until that step completes |
| Projection refresh | stop condition | Billing state becomes `active`, ten seconds elapse, or the query unmounts |
| Date refresh | timer instant | Teaching `next_local_midnight_at`; window focus is the second trigger |
| Visible copy | English labels, hints, errors, and announcements | Caller owned constants in feature components until feature 20 supplies translation state |

### Key invariants

1. One transaction writes one aggregate, its command receipt when required, and all outbox facts
   caused by that command. No handler publishes to Redpanda.
2. Teaching is the only source for class, session, student, roster, and attendance values displayed
   on home. Billing returns projection progress only.
3. Every teaching query and receipt lookup filters by trusted `tutor_id`. Same service joins match it
   on both sides. A consumer takes it from the event envelope.
4. A class and first session commit together. Student creation and roster joining are separate
   commands, and completed facts are never compensated or deleted after a later step fails.
   The first session is inside the initial class aggregate invariant.
5. A receipt is immutable. One `(tutor_id, operation, idempotency_key)` names one validated command
   forever. Class equality includes its validated local inputs and request timezone. Student equality
   includes its validated name and nullable phone. Generated values are excluded. A different hash
   is a conflict.
6. Local input resolves through the token timezone. The server rejects ambiguous or nonexistent
   clock readings and verifies the resulting end instant follows the start instant.
7. Roster coverage uses spec 0003's inclusive predicate. Attendance cannot create a relationship
   that the roster does not contain.
8. An identical attendance state is a successful no operation. A different state updates the row
   and publishes one correction event.
9. Billing handles each event inside the same transaction as `handled_events`, accepts only version
   1, ignores unknown fields, and commits its broker offset only after the projection transaction.
   Projection tables have no cross event foreign keys, so `teaching.session.scheduled` may arrive
   before `teaching.class.created` without failing or losing data.
10. Projection counts are diagnostic progress, not teaching truth and not proof that the latest
    later correction has arrived.
11. Home session pagination is stable on `(starts_at, session_id)`. Its cursor also binds the current
    `local_date`. A date change clears pages and begins at the first cursor.
12. The web does not optimistically invent attendance. It displays the canonical mutation response,
    then invalidates the home query.

### Security model

1. The existing access token is required at the gateway and verified again in identity, teaching,
   and billing. No request accepts `tutor_id`.
2. Missing and cross tutor resources share `404`. Logs may include request, operation, and resource
   identifiers, but never phone, form bodies, access tokens, or idempotency hashes.
3. Phone remains in teaching storage, the immediate create response, and the temporary setup draft
   in `sessionStorage`. It never reaches home, an event, billing, logs, or persistent browser
   storage. The draft is cleared on completion, sign out, or tutor change.
4. Stored class color is a closed enum, never an arbitrary CSS class or style value.
5. No new unauthenticated route exists. No new compliance regime applies beyond protecting the tutor's own
   student contact data under the existing tenant rules.

No new environment variable, secret, feature flag, provider, or external account is required.

### Browser data flow

1. Use React Hook Form with one Zod schema and `@hookform/resolvers` for the multi step draft. Each
   step reveals its own fields but validates through the same typed schema. Field errors use
   `aria-invalid`, stable descriptions, and alerts where they block progress.
2. Use array query keys, with `['home', cursor]` as the base shape. A date change or mutation resets
   the cursor chain and invalidates the exact home key family.
3. Do not retry mutations automatically. The sheet preserves the command key and exposes the same
   action, so a deliberate retry reaches the server receipt.
4. Poll only the optional billing state. Stop automatically after ten seconds, preserve the pending
   result, and expose a manual retry. Every poll calls `/api/home/billing-projection`, never the full
   home endpoint.
5. The setup sheet traps focus and returns it to its trigger. Attendance radio targets are at least
   44 by 44 pixels, have a group label and visible focus, and retain a logical reading order with
   long labels and 200 percent zoom.

### Critical test scenarios

1. Happy path: create a suggested color class for today, create and join a student, mark `Present`,
   reload, then observe the same teaching state and an eventually active billing panel, verifies
   **AC-1** through **AC-9**.
2. Color override: choose each allowed color, reject an unknown value, and prove the stored home
   color matches the class response and survives reload, verifies **AC-3**, **AC-11**.
3. Lost response: submit class and student creates twice with the same keys, including concurrent
   requests, then prove one set of rows and events exists; change a body under the same key and get
   `409`, change the token timezone under a class key and get `409`, and confirm create returns `201`
   while replay returns `200`, verifies **AC-2**, **AC-4**, **AC-10**.
4. Roster retry: repeat the exact effective date and receive `200` with no event, then retry the pair
   with a different open effective date and receive `409`, verifies **AC-4**, **AC-12**.
5. Attendance correction: mark `Present`, race `Absent` with another `Present`, and prove the last
   committed state is authoritative, an identical repeat emits no event, and billing converges to
   the same final state, verifies **AC-5**, **AC-6**, **AC-8**.
6. Tenant isolation: use tutor A's token with tutor B's class, session, and student identifiers and
   receive `404` with no write or event, verifies **AC-6**, **AC-12**.
7. Time boundary: create a valid local session with `date` and `HH:mm` fields, reject reversed, cross midnight, missing, and
   repeated clock times, then confirm stored UTC instants and `local_date`, verifies **AC-2**,
   **AC-11**.
8. Projection failure: stop billing while home, setup, and attendance continue; prove billing timeout
   does not delay required panels past the shared deadline, then restart it and wait
   for replay to produce one row per event key and an active panel, verifies **AC-8**, **AC-9**,
   **AC-15**.
9. Browser recovery: reload at each setup step, fail each mutation once, sign out, and switch tutors;
   prove draft retention and clearing rules with no token or phone left in persistent storage,
   verifies **AC-14**.
10. Pagination and date change: return more than 50 same day sessions including equal start times,
    walk every cursor without a duplicate or gap, reject malformed and prior date cursors, then cross
    local midnight and refocus the window, verifies **AC-7**, **AC-11**, **AC-15**.
11. Timezone consistency: use a token timezone that differs from both the browser and a newly changed
    identity profile, then prove home filtering, setup defaults, and displayed times all use teaching
    `request_time_zone`, verifies **AC-7**, **AC-15**.
12. Accessibility: complete setup and attendance at phone width using keyboard only and a screen
    reader, test 200 percent zoom, visible focus, 44 pixel targets, error announcements, and billing
    status without color, verifies **AC-13**.
13. Contract and driver: regenerate both clients, build every module, and run `task thread` through
    the gateway, Postgres, outbox, Redpanda, billing consumer, aggregated home read, and web contract,
    verifies **AC-16**.

## Build plan

Tracer Bullet means the first milestone crosses every runtime boundary with the smallest truthful
class and session fact. Later milestones thicken the same path with student, roster, attendance, and
the complete working screen.

1. Add the teaching color and command receipt migration, the OpenAPI class, session, home, and
   billing status schemas, then build class plus first session as one teaching transaction. Consume
   `teaching.class.created` and `teaching.session.scheduled` in billing, replace `/api/thread` with
   the initial identity plus teaching plus billing home aggregation, regenerate both clients, and
   extend `task thread` through this vertical path, satisfies **AC-2**, **AC-3**, **AC-7**,
   **AC-8**, **AC-9**, **AC-10**, **AC-11**, **AC-12**, **AC-16**.
2. Add student creation and roster joining through teaching store, handler, service routes, gateway
   routes, outbox events, billing projections, receipt recovery, and home roster rows, satisfies
   **AC-1**, **AC-4**, **AC-6**, **AC-8**, **AC-10**, **AC-11**, **AC-12**.
3. Add attendance validation, idempotent state replacement, event publication, billing projection,
   and canonical home reads for unmarked and marked rows, satisfies **AC-5**, **AC-6**, **AC-8**,
   **AC-11**, **AC-12**.
4. Replace the development thread page with the home composition. Add React Hook Form, Zod, and
   `@hookform/resolvers`, then build `TeachingSetupSheet`, `SessionAttendanceCard`, and
   `BillingProjectionPanel` from existing primitives with tutor scoped draft recovery and explicit
   English caller copy, satisfies **AC-1**, **AC-3**, **AC-4**, **AC-5**, **AC-13**, **AC-14**.
5. Add cursor paging, exact TanStack Query invalidation, bounded billing polling, window focus and
   local midnight refresh, the billing only public read, shared upstream deadlines, future session
   feedback, and every loading, empty, error, pending, and unavailable state, satisfies **AC-7**,
   **AC-9**, **AC-12**, **AC-13**, **AC-14**, **AC-15**.
6. Regenerate and commit SQL plus API types, remove every old thread type and route, run the build and
   repository checks, and keep the critical scenarios ready for `$check verify` and `$test`,
   satisfies **AC-11**, **AC-12**, **AC-13**, **AC-16**.

## Consequences

**Positive**:

1. The first business slice proves the database, outbox, broker, consumer, projection, gateway, and
   browser as one path.
2. Teaching remains authoritative while billing can restart or lag without taking attendance down.
3. Command receipts make the two irreversible create operations safe after a lost response.
4. The home screen begins as a useful daily workspace rather than an architecture demonstration.

**Negative and tradeoffs**:

1. One feature changes teaching, billing, gateway, OpenAPI, the thread driver, and the web app. The
   Tracer Bullet order limits the risk but does not make the diff small.
2. `command_receipts` grows forever. That is acceptable at one tutor scale and avoids a cleanup job,
   but retention may need a later decision if the product changes shape.
3. Billing counts prove the first complete path, not whether every later correction is current.
   Presenting them as business truth would be wrong.
4. A tab closed between student creation and roster joining can leave an unrostered student. Feature
   11 will expose existing students for reuse; this slice preserves the fact rather than deleting it.
5. The class color suggestion algorithm exists in Go and TypeScript. Contract tests must keep the
   two implementations identical.
6. Cursor paging adds contract and cache complexity to a daily list that will normally fit in one
   response.

**Neutral**:

1. Class color stays inside teaching because no consumer needs it.
2. React Hook Form, Zod, and `@hookform/resolvers` become web dependencies. No new runtime service or
   configuration appears.
3. The old thread endpoint and architecture proof page disappear. `task thread` keeps the concept but
   now drives the real teaching flow.

## Follow-up

1. [ ] Feature 11 should let the tutor select an existing unrostered student, which provides the
   recovery path after a tab closes between student creation and roster joining.
2. [ ] Feature 20 should replace the caller owned English copy with Vietnamese default translation
   state and English switching.
3. [ ] A later class editing feature may expose color changes. It needs no event until a real
   consumer requires class color.
4. [ ] `$sync` should record that React Hook Form and Zod Agent Skills and the broad React skills MCP
   were declined in `web/AGENTS.md`, so later workflow steps do not offer them again.
