# 0010. Recurring sessions and exceptions

**Date**: 2026-09-18
**Status**: In Progress

## Summary

A tutor can define a weekly class schedule across any day from Monday through Sunday, inspect every
concrete session in a calendar, and change one session without disturbing the series. Rules are
versioned, but sessions are materialized as ordinary rows so attendance, billing, and notifications
keep using the event contract they already understand. A bounded date range and database overlap
constraints keep generation safe and predictable.

## Requirements

**User stories**:

1. As a tutor, I want one weekly schedule with different times on different days so that I do not
   create each class session by hand.
2. As a tutor, I want to move, cancel, or restore one occurrence so that a holiday or make up class
   does not rewrite the rest of the schedule.
3. As a tutor, I want one clear calendar across all classes so that I can see conflicts and find the
   session I need from a phone or desktop.
4. As the engineer, I want billing and notifications to keep receiving concrete session facts so
   that recurrence adds no new cross service interpretation.

**Acceptance criteria**:

1. **AC-1**: A signed in tutor can create a class with either the existing single first session or a
   weekly schedule. A weekly schedule selects one through seven ISO weekdays, including Saturday and
   Sunday, with one independent start and end time per selected day, an inclusive start date no more
   than 30 local dates before today, and an inclusive end date no more than two calendar years after
   `valid_from`. A 29 February anniversary clamps to 28 February when the target year has no leap
   day. Each session ends later on the same local date.
2. **AC-2**: Creating a weekly schedule materializes every occurrence through the inclusive end date
   in one teaching transaction. The transaction writes the rule, slots, concrete sessions, command
   receipt, and one `teaching.session.scheduled` outbox row per session, or writes nothing. The
   response returns the class, rule summary, candidate, created, and adopted counts, plus the
   nullable first upcoming session, rather than every generated row.
3. **AC-3**: A tutor can add a schedule to an existing class. An existing standalone session that
   is active and exactly matches a generated occurrence keeps its identifier and becomes attached to
   the rule without publishing a duplicate event. At most one row can match, it increments the
   adopted count rather than the created count, and cancelled standalone rows remain independent.
   Any other standalone session passes the same overlap check as generated sessions.
4. **AC-4**: A tutor can change a schedule from today or a future local date. The old rule closes on
   the preceding date and a new retained version begins on the chosen date. A future rule replaced
   on or before its start is retired without creating an invalid date range, its effective
   predecessor also closes when needed, and intentional gaps are allowed. Untouched future sessions
   whose origin is affected and whose start instant has not passed are marked as schedule replaced.
   A replacement on an active rule's `valid_from` retires that rule rather than creating an invalid
   range. Sessions that were moved or tutor cancelled remain as explicit exceptions, and matching
   origin dates from every retained rule version suppress regeneration.
5. **AC-5**: A tutor can end a schedule early with an inclusive last date that is today or later in
   the latest retained rule's stored time zone and no replacement rule. The expected class revision
   selects that latest rule. A future latest rule ended before it starts is retired, and an active
   predecessor is shortened when the same last date reaches it. Otherwise the selected rule's end is
   shortened and explicitly recorded. Eligible untouched later sessions become schedule replaced,
   while moved and tutor cancelled exceptions remain unchanged. Rule versions and session rows are
   retained rather than deleted.
6. **AC-6**: An active session can move repeatedly to any date within two years of its original rule
   start in either direction, including outside the rule date range. A standalone session uses its
   retained original local date as that anchor. Calendar year bounds are inclusive and clamp a leap
   day anniversary to the final valid day of February. Attendance stays attached. A move uses the
   source rule time zone for a recurring session and the current verified token time zone for a
   standalone session, writes `teaching.session.moved`, and updates billing and notifications by the
   existing session identifier.
7. **AC-7**: An active session can be cancelled even when attendance exists. Attendance remains as
   history, while the session is excluded from later billing and notifications through
   `teaching.session.cancelled`. A tutor cancelled session can be restored at its last stored time.
   Restoration clears tutor cancellation, retains any moved marker, and republishes
   `teaching.session.scheduled` for the same identifier so both projections become active again.
8. **AC-8**: Active sessions for one tutor never overlap. Adjacent boundaries are valid. The
   database enforces the rule under concurrent class creation, schedule change, move, and restore.
   A conflict returns `409` with a stable code plus the owned conflicting session identifier, class
   name, both UTC endpoints, and times in the verified token display zone. When several committed
   rows conflict, the earliest `(starts_at, session_id)` wins. If that row disappears after rollback,
   the command retries once from the beginning before returning a generic typed overlap conflict.
   Overlap inside one submitted schedule is invalid input with `400`. The attempted transaction
   writes no business row, receipt, or event.
9. **AC-9**: Rule date ranges for one class never overlap. A rule has one through seven unique
   weekday slots. A recurring session is unique by source rule and original occurrence date. Every
   teaching table and same service reference remains tutor scoped. An upgrade first adds the model
   without the overlap constraint, reports every existing owned conflict for deliberate correction,
   then adds the constraint only when none remain. The constraint migration fails closed if a
   conflict appears, and every migration applies from empty and reverses cleanly.
10. **AC-10**: A rule stores the IANA time zone from the verified token when its version is created.
    A missing local time moves forward by the zone gap while keeping the intended minutes. An
    ambiguous local time uses the earlier instant. Start and end are resolved separately, both UTC
    offsets are returned, and the whole command is rejected if resolved end is not later than start
    or either resolved date leaves its allowed window. A later profile time zone change does not move
    existing sessions, while a new rule version captures the then current token time zone.
11. **AC-11**: Schedule, move, cancel, restore, and end commands require `Idempotency-Key`. The same
    key and canonical explicit input return an immutable response plus captured command context before
    current state, token time zone, or clock validation, including after later edits or a lost
    response. Command identity includes the path resource, normalized body, sorted unique slots, and
    expected revision or version. A receipt stores the original success status, body, command instant,
    governing zone, and request display zone. Reusing a key for different explicit input returns
    `409`. Schedule commands name the expected class schedule revision, session commands name the
    expected session version, and stale state returns `409` with typed current owned details and no
    write.
12. **AC-12**: `GET /api/schedule` reads an inclusive window of at most 42 local dates across all
    classes in the verified token display time zone, with optional repeated class filters. UTC
    boundaries select sessions by their start instant. It returns active and tutor cancelled
    sessions normally. Replaced history is opt in, limited to 50 rows, and cursor paged. Returned
    rules include versions whose plain calendar date ranges intersect the requested dates, the latest
    class schedule revision, and every source rule referenced by a returned session. Replaced history
    is selected by `origin_local_date`; its opaque cursor is bound to tutor, window, sorted filters,
    order, and limit. The window is the bounded calendar page unit and retained history has its own
    bound.
13. **AC-13**: Every endpoint requires the existing bearer token. `tutor_id` comes only from its
    verified `sub` claim. A missing or foreign class, rule, or session returns the same `404`, and no
    response or conflict detail reveals another tutor's data. Invalid input is `400`, an invalid
    token is `401`, and state, overlap, stale write, and idempotency conflicts are `409` in the
    existing `APIError` shape. Foreign identifiers in list filters simply match no owned row.
14. **AC-14**: The protected Schedule page shows all classes with optional filters. Desktop opens in
    Week view with a compact month picker, quiet page actions, a time grid, and class colored session
    cards inspired by the supplied calendar reference. Day and Month views are also available, and
    every week starts on Monday. Phones open in a chronological Day agenda with a compact date
    strip. Search, statistics cards, world calendar, AI controls, and unrelated dashboard chrome are
    not copied. Add schedule is a page action, and every class filter exposes Manage schedule with
    the latest revision already loaded.
15. **AC-15**: Selecting a session opens an accessible details sheet with rule origin, current time,
    time zone, state, and valid Move, Cancel, or Restore actions. The calendar and forms follow
    `web/design.md`, work by keyboard and screen reader at 200 percent zoom, preserve visible focus,
    use targets at least 44 pixels, announce results and errors, and never use color alone for class
    or exception meaning. Empty, loading, stale conflict, validation, and retry states tell the tutor
    what action is available.
16. **AC-16**: Billing and notifications converge through the existing scheduled, moved, and
    cancelled events with duplicate delivery and replay safety. A change after invoice issue never
    rewrites an issued invoice. The tutor must use the existing void and rerun correction flow when
    that later feature is present.
17. **AC-17**: `api/openapi.yaml` remains the source for every public request and response type. Go
    and browser types regenerate cleanly. Weekly expansion uses the Go standard library, database
    enforcement uses Postgres `btree_gist`, and the feature adds no recurrence package, calendar
    package, environment variable, secret, provider, or external account.

## Decision

**Chosen option**: Option 1: versioned weekly rules with materialized concrete sessions

Store normalized rule versions and weekday slots in `teaching`, then create every bounded occurrence
as an ordinary session row. Keep one global calendar read, explicit exception commands, and the
existing session events as the only facts that cross service boundaries. When the same command
creates a class, a weekly schedule uses its explicit `valid_from` as the class
`rate_effective_from`; the single session path keeps using that session's `local_date`.

**Implementation skills**: `golang-database` (`samber/cc-skills-golang`,
`.agents/skills/golang-database/`) · `go-goose` (`metalagman/agent-skills`,
`.agents/skills/go-goose/`) · `kafka-development` (`mindrally/skills`,
`.agents/skills/kafka-development/`) · `openapi` (`oakoss/agent-skills`,
`.agents/skills/openapi/`) · `frontend-design` (`anthropics/skills`,
`.agents/skills/frontend-design/`) · `tailwindcss-accessibility`
(`josiahsiegel/claude-plugin-marketplace`, `.agents/skills/tailwindcss-accessibility/`) ·
`tanstack-query-best-practices` (`deckardger/tanstack-agent-skills`,
`.agents/skills/tanstack-query-best-practices/`)

## Rationale

Reasoning and options: see [rationale.md](rationale.md).

## Feature design

### User flow and page composition

1. Class creation keeps the current single session path and adds a recurring schedule choice. The
   recurring form asks for a start date, inclusive end date, and one time range for each selected
   weekday. It previews the candidate date count as a provisional estimate. The committed response
   replaces that estimate with authoritative candidate, created, and adopted counts. The schedule
   start date also becomes the first class rate effective date, even when the first matching weekday
   occurs later.
2. The protected `/schedule` route opens one tutor wide calendar. Its URL search values carry the
   view, visible date, and selected class identifiers so reload and browser navigation preserve the
   same calendar.
3. Desktop uses the supplied reference as a composition guide: a compact month picker and class
   filters on the left, restrained page actions above, then the calendar surface. The existing
   Vermouth shell, Montserrat type, semantic tokens, seven class colors, spacing, and dark theme stay
   authoritative.
4. Week is the desktop default. Day shows one time column, Week shows seven day columns, and Month
   shows compact session buttons. Phone width defaults to a chronological Day agenda and compact date
   strip rather than squeezing or horizontally scrolling a seven column grid.
5. A session button opens `SessionDetailsSheet`. The sheet shows its class, current date and time,
   source rule and time zone, exception state, and only the actions valid in that state. Move fields
   are ordinary labelled inputs. Drag and drop is out of scope.
6. `Add schedule` asks for an existing class before opening the rule form. Every class filter item
   exposes `Manage schedule`, which loads the latest revision, rule, and slots. Replacement and ending
   confirmations explain that explicit exceptions survive. The success summary reports the exact
   preserved and superseded counts from the command.
7. After a successful mutation, the browser renders the canonical response, invalidates every
   filtered and unfiltered schedule query for the affected class, and announces the result. It does
   not invent an optimistic time because the server may reject overlap or stale state.

### Data model sketch

Two additive teaching migrations create the target safely. The first creates the rule tables and
extends classes, sessions, and receipts. A read only conflict report then exposes any overlapping
existing sessions for deliberate correction. The second enables `btree_gist` and adds the exclusion
constraints only after a migration guard proves no conflict remains. All identifiers are UUIDv7
values generated in Go. Calendar days are `date`, local clocks are `time without time zone`, and
instants are `timestamptz` in UTC.

| Table | Key | Required fields | Nullable fields | Constraints and relationships |
|---|---|---|---|---|
| `classes` | existing `class_id uuid` | existing fields plus `schedule_revision bigint` | none added | Revision starts at 0 and increments for every successful put or end schedule command. It is the optimistic guard even when no rule is currently active. |
| `schedule_rules` | `schedule_rule_id uuid` | `tutor_id`, `class_id`, `revision bigint`, `valid_from date`, `valid_through date`, `time_zone text`, `created_at`, `updated_at` | `replaced_at timestamptz`, `ended_at timestamptz`, `retired_at timestamptz` | Composite tutor and class foreign key to `classes`. Unique `(tutor_id, schedule_rule_id)`, `(tutor_id, class_id, schedule_rule_id)`, and `(tutor_id, class_id, revision)`. Inclusive range, at most two years. A GiST exclusion constraint prevents overlapping nonretired date ranges for one tutor and class. |
| `schedule_slots` | `(schedule_rule_id, weekday)` | `tutor_id`, `weekday smallint`, `start_time time`, `end_time time`, `created_at`, `updated_at` | none | Composite tutor and rule foreign key. ISO weekday is 1 through 7. End is later on the same local date. One through seven rows exist per rule, enforced by the handler inside the rule transaction. |
| `sessions` | existing `session_id uuid` | existing class, tutor, UTC start, UTC end, local date, timestamps, new `origin_local_date date`, new `version bigint` | existing `schedule_rule_id`, new `moved_at timestamptz`, existing `cancelled_at timestamptz`, new `superseded_at timestamptz` | The first migration backfills `origin_local_date` from existing `local_date` and starts version at 1. A recurring row has a composite `(tutor_id, class_id, schedule_rule_id)` foreign key and a unique `(schedule_rule_id, origin_local_date)`. A standalone row leaves the rule empty. A partial GiST exclusion constraint rejects overlap only where `cancelled_at IS NULL AND superseded_at IS NULL`. |
| `command_receipts` | existing `(tutor_id, operation, idempotency_key)` | existing request hash, primary resource, creation time, new `context_snapshot jsonb`, new `response_snapshot jsonb`, new `response_status integer` | existing related resource and all three response fields only for old receipt rows | Add operation values `put_schedule`, `end_schedule`, `move_session`, `cancel_session`, and `restore_session`. Every new receipt stores the command instant, governing zone, request display zone, original success status, and exact response body. Receipts remain immutable and retained. |

The overlap range is half open, `[starts_at, ends_at)`, so one session may start exactly when another
ends. Rule date ranges are closed, `[valid_from, valid_through]`, because both dates may generate an
occurrence. The session overlap constraint covers standalone and recurring sessions equally.

Slots are immutable after rule creation. A change increments `classes.schedule_revision`, closes or
retires the old rule, and inserts a new rule plus slots at that revision. A replacement on or before
a future rule start retires it. A later replacement closes an effective rule on the preceding date.
A replacement beyond its end leaves an intentional gap. The old rule row stays linked to the
sessions it generated. A change identifies future work by `origin_local_date`, not the current moved
date.

There is at most one future latest rule. When it is replaced by a rule that starts earlier, the
future rule is retired and its nonretired predecessor is also closed on the day before the new start
when their ranges would overlap. Ending before a future latest rule starts retires that rule and
shortens its nonretired predecessor when the requested last date is earlier than the predecessor end.
Every affected rule and session is handled inside the same class transaction.

An explicit exception is a session where `moved_at` or `cancelled_at` is present. A rule change sets
`superseded_at` only on untouched future sessions. The generator gathers matching origin dates from
every retained explicit exception for the class, not just the previous version. Those dates suppress
new occurrences and define `preserved_exception_count`. An active moved exception may also cause the
normal tutor overlap constraint to reject the new rule.

An active standalone row may be adopted only when it has no `moved_at`, `cancelled_at`, or
`superseded_at` and exactly one row matches the generated class, UTC start, UTC end, and local date.
The active overlap invariant makes a second active match impossible. Adoption fills its rule and
origin fields, increments its version, counts as adopted rather than created, and publishes no event
because the concrete session fact did not change.

The first migration supports a read only `task schedule:conflicts` target, backed by a small teaching
command that prints owned class and session identifiers plus UTC endpoints in stable JSON order by
tutor, start, and session identifier. It uses the existing `TEACHING_DATABASE_URL` and writes
nothing. Existing conflicts are never changed automatically. The schema phase may deploy beside the
old application. An operator deliberately reconciles every reported conflict before the second
migration runs. The second migration repeats the query and aborts when any row remains. Recurring
write routes are deployed only after that constraint migration succeeds.

### State transitions

```text
rule
  absent at class revision N -> planned or active rule at revision N+1
  future rule -> retired old rule plus replacement rule at next revision
  active rule replaced on its valid_from -> retired old rule plus replacement rule at next revision
  active rule -> closed old rule plus replacement rule at next revision
  completed or ended rule -> replacement rule after an allowed gap at next revision
  future rule -> retired with no replacement at next class revision
  active rule -> explicitly ended with no replacement at next class revision

session
  active generated version N -> moved active version N+1
  moved active version N -> moved active version N+1
  active or moved active version N -> tutor cancelled version N+1
  tutor cancelled version N -> restored at last stored time version N+1
  untouched future active version N -> schedule replaced version N+1

terminal rule
  schedule replaced sessions are immutable
```

Restoration clears `cancelled_at`. It retains `moved_at` and the current UTC times. Every session
mutation increments `version`, including adoption and schedule replacement. A fresh command that
requests an invalid transition returns `invalid_session_state`. Invalid rule replacement, ending,
or revision state returns `invalid_schedule_state`. Only a retry carrying the original command key
replays its earlier response snapshot.

Rule state is derived in this order. `retired_at` means retired. `ended_at` means explicitly ended.
`replaced_at` means replaced. A rule with no transition timestamp is planned before `valid_from`,
active inside its inclusive range, and completed after `valid_through`. A naturally completed rule
therefore stays completed when a later rule begins after a gap. The effective rule for a date is the
one nonretired range containing that date. The latest rule and the effective rule may differ while a
future change is waiting to begin. Planned, active, and completed are computed from the one request
clock, converted separately through each rule's stored zone.

### Recurrence and time rules

1. Weekly expansion uses `time.Date` and `time.Location` from the Go standard library. No general
   recurrence grammar is stored or accepted.
2. Candidate dates are walked from `valid_from` through `valid_through`. A matching slot produces at
   most one session for that original local date. A valid rule produces at least one occurrence and
   at most 732.
3. A new rule may begin no earlier than 30 local dates before the current date in its stored zone.
   A schedule change begins today or later. Ending a rule uses an inclusive last date of today or
   later. A new or replacement rule uses its newly captured token zone. An end command uses the
   latest retained rule's stored zone. The current local date comes from the one teaching transaction
   clock resolved through that governing zone.
4. A missing wall clock time moves forward by the exact zone transition gap. The resolved local day
   becomes `local_date`, while `origin_local_date` retains the candidate occurrence day.
5. An ambiguous wall clock time chooses the earlier UTC instant. Start and end resolve separately.
   The command returns both numeric offsets and rejects `invalid_local_time` when resolved end is not
   later than resolved start, the two resolved endpoints leave one local date, or a resolved date is
   outside the command bounds. A whole day zone jump can therefore reject the rule rather than
   silently collide with the next occurrence.
6. A recurring session always uses its source rule zone for later moves. A standalone session uses
   the current verified token zone. The action sheet displays the zone before confirmation.
7. A recurring move target lies inside the inclusive interval from two calendar years before through
   two calendar years after its source rule `valid_from`. A standalone move uses the same interval
   around its immutable `origin_local_date`. An anniversary of 29 February clamps to 28 February in
   a year without that date.
8. Single session class creation keeps spec 0009 behavior and rejects missing or ambiguous wall clock
   input. The shift and earlier instant policy applies only to weekly expansion and moves governed by
   this feature.
9. Rule maximum dates and move bounds use the same calendar year arithmetic. `AddDate` style elapsed
   duration is not used. A 29 February bound clamps to 28 February before the inclusive comparison.
10. The resolver identifies the zone offsets on both sides of a transition. It shifts a missing wall
    time by the exact gap and chooses the earlier UTC candidate for an ambiguous wall time. Plain
    `time.Date` normalization is not accepted as proof of either rule.

### API surface

Public schemas and operations live in `api/openapi.yaml`. The gateway only authenticates and proxies
these commands and the teaching read. It holds no schedule rule.

| Endpoint | Method | Key inputs | Key outputs | Auth | Key errors |
|---|---|---|---|---|---|
| `/api/classes` | `POST` | `Idempotency-Key`; existing class fields; exactly one of `first_session` or `schedule` | `201` or replay `200`; class with schedule revision, nullable first upcoming session, nullable rule summary, candidate, created, and adopted counts | bearer | `400`, `401`, `409` |
| `/api/classes/{class_id}/schedule` | `PUT` | `Idempotency-Key`; `expected_revision`; `effective_from`; `valid_through`; one through seven slots | `201` for first rule or `200` for change or replay; new class revision, rule, candidate, created, adopted, superseded, and preserved counts, nullable first upcoming session | bearer | `400`, `401`, `404`, `409` |
| `/api/classes/{class_id}/schedule/end` | `POST` | `Idempotency-Key`; `expected_revision`; inclusive `last_date` | `200`; new class revision, ended or retired rule, superseded count, preserved count | bearer | `400`, `401`, `404`, `409` |
| `/api/sessions/{session_id}/move` | `POST` | `Idempotency-Key`; `expected_version`; local date, start time, end time | `200`; canonical session with new version, state, rule origin, source zone, display zone, both offsets, and update time | bearer | `400`, `401`, `404`, `409` |
| `/api/sessions/{session_id}/cancel` | `POST` | `Idempotency-Key`; `expected_version` | `200`; canonical cancelled session with new version | bearer | `401`, `404`, `409` |
| `/api/sessions/{session_id}/restore` | `POST` | `Idempotency-Key`; `expected_version` | `200`; canonical active session with new version | bearer | `401`, `404`, `409` |
| `/api/schedule` | `GET` | inclusive display zone `from` and `through`; zero or more `class_id`; maximum 42 dates; optional `include_replaced`, `history_limit`, `history_cursor` | tutor and display zone, class summaries with revisions, applicable rules, active and cancelled sessions, optional replaced history and next cursor | bearer | `400`, `401` |

`api/openapi.yaml` defines these response objects exactly:

| Object | Required fields and source contract |
|---|---|
| Class summary | `class_id`, `name`, `color`, and `schedule_revision` from the owned class row |
| Rule summary | `schedule_rule_id`, `class_id`, `revision`, `valid_from`, `valid_through`, stored `time_zone`, derived `state`, ordered slots, and nullable transition timestamps |
| Canonical session | `session_id`, `class_id`, UTC endpoints, stored `local_date`, `origin_local_date`, nullable `schedule_rule_id`, nullable source zone, `version`, derived state, nullable move, cancellation, and replacement timestamps, and `updated_at` |
| Calendar session | Canonical session plus retained class name, color, archive flag, display date and clocks in the request zone, and separate numeric start and end offsets formatted as `+HH:MM` or `-HH:MM` |
| Create or change result | Owned class summary, nullable rule summary, nullable first upcoming canonical session, and integer candidate, created, adopted, superseded, and preserved counts as applicable to that operation |
| Schedule read | Trusted `tutor_id`, request display zone, inclusive `from` and `through`, available active classes, included rules, ordered normal sessions, ordered replaced history, and nullable next history cursor |

Rule origin in the details sheet means the source rule identifier, original occurrence date, stored
rule zone, and slot weekday plus clocks. Current time means the stored UTC endpoints formatted into
the request display zone. Source zone and stored billing date remain separate labelled facts.

Typed `APIError.error.details` objects are selected by the stable error code:

| Code | Required details |
|---|---|
| `stale_schedule` | `class_id`, current `schedule_revision`, and nullable latest rule summary |
| `stale_session` | Current canonical session |
| `invalid_schedule_state` | `class_id`, current revision, latest rule state, and allowed actions |
| `invalid_session_state` | `session_id`, current state, current version, and allowed actions |
| `session_overlap` | Nullable conflicting session identifier and class label, both UTC endpoints, request display zone, and nullable formatted display endpoints. The fields are null only after the one bounded race retry cannot reload the vanished row. |
| `idempotency_conflict` | Operation and path resource identifier only. The key and request body are never echoed. |

The teaching service mirrors the same paths without the `/api` prefix. Every command response comes
from committed teaching rows and is copied into its receipt with its success status before commit.
An exact retry returns both snapshots before current clock, revision, state, or overlap validation.
`PUT /schedule` takes the current `classes.schedule_revision`, which is 0 before the first rule. A mismatch returns
`stale_schedule` with typed class revision and latest rule details. Session version mismatches return
`stale_session` with the current typed session details.

The schedule read is windowed pagination by calendar range. Day asks for one date, Week for seven,
and Month for its visible calendar cells, never more than 42. Teaching resolves the start of `from`
and the start after `through` in the verified token time zone, then selects session start instants in
that half open UTC range. It returns all active class summaries for filter controls even when a class
has no session in the window. Repeated `class_id` values filter rules and sessions, but not the
available class list. A foreign filter value matches nothing and reveals nothing.
No `class_id` parameter and zero repeated values both mean all owned classes. An empty string value is
invalid input. Mixed owned and foreign identifiers return only rows for the owned identifiers.

Normal calendar rows include active and tutor cancelled sessions. `include_replaced=true` adds at
most `history_limit` replaced rows, where the default and maximum are 50, ordered by
`(origin_local_date, session_id)` whose origin is inside the requested plain date window. The opaque
cursor encodes tutor, window, sorted filters, limit, ordering version, and the last pair. A mismatch
returns `400`. The rule list compares stored rule ranges with the request range as plain dates. It
includes versions whose local range intersects the requested dates, the latest rule for each returned class, and every source
rule referenced by a returned session. A returned session for an archived class carries the retained
class label and color plus `class_archived=true`, but archived classes do not appear as available
filters.

Classes order by normalized name then identifier. Rules order by class identifier then revision.
Normal sessions order by `(starts_at, session_id)`. Replaced history uses the cursor order above.

Conflict codes are `session_overlap`, `stale_schedule`, `stale_session`,
`invalid_session_state`, `invalid_schedule_state`, and `idempotency_conflict`.
`session_overlap` carries the committed owned session identifier, class label, UTC endpoints, and
endpoints formatted in the request display zone. `stale_schedule` and `stale_session` carry their
separate typed current state details. Overlap among candidates inside one request returns
`invalid_input` with the conflicting slot and dates because no committed session identifier exists.

### Value sourcing

| Action | Value produced or displayed | Source |
|---|---|---|
| Create or change rule | trusted tutor | Verified token `sub`, never request input |
| Create or change rule | rule time zone | Verified token `tz`, validated by `time.LoadLocation` and stored on the new rule version |
| Create or change rule | rule and session identifiers | UUIDv7 values generated in the teaching handler |
| Create or change rule | date range and weekly slots | Validated request dates, ISO weekdays, and `HH:mm` clock values |
| Create class with a weekly rule | class `rate_effective_from` and `teaching.class.created.rate_effective_from` | Validated request `schedule.valid_from`; the single session path continues to use `first_session.local_date` |
| Create, change, or end rule | current local date boundary | Teaching transaction clock resolved through the stored or newly captured rule zone |
| Create, change, or end rule | optimistic state and next revision | Request `expected_revision` compared with locked `classes.schedule_revision`, then incremented by one |
| Generate occurrence | original occurrence date | Candidate date matching one stored slot inside the inclusive rule range, stored as `origin_local_date` |
| Generate occurrence | UTC start, UTC end, billing local date, and offsets | Candidate date plus slot clocks resolved separately in the stored rule zone by the confirmed gap and ambiguity rules |
| Generate occurrence | exception suppression | Every retained class session with the same `origin_local_date` and `moved_at` or `cancelled_at` present |
| Attach existing session | matching session | The one owned active standalone row with equal class, UTC start, UTC end, and local date |
| Create or change result | `candidate_count` | Number of unique resolved slot dates in the requested inclusive rule range before exception suppression or adoption |
| Create or change result | `created_count` and `adopted_count` | New session rows inserted and eligible standalone rows attached, two disjoint sets |
| Change or end result | `superseded_count` | Untouched recurring rows whose origin is affected and whose `starts_at >= command_time`, updated by this transaction |
| Change or end result | `preserved_count` | Retained moved or tutor cancelled session rows whose origin lies in the affected set, counted as rows rather than distinct dates |
| Create or change result | first upcoming session | Earliest active owned class session with `starts_at >= command_time`, ordered by `(starts_at, session_id)`, including standalone and preserved exception rows |
| Browser form | provisional candidate count | Client count of selected weekdays inside the entered inclusive range, labelled as an estimate until the committed response replaces it |
| Change or end rule | latest rule state | Highest retained rule revision for the class plus its `ended_at` or `retired_at`, read after the class revision lock |
| Change or end rule | sessions to replace | Recurring rows with no move or cancellation marker, affected `origin_local_date`, and `starts_at >= command_time` |
| Change or end rule | exceptions to preserve | Every retained explicit exception for the class whose origin lies in the affected set |
| Move session | input zone | Source rule `time_zone` when linked, otherwise verified token `tz` |
| Move session | allowed date interval | Source rule `valid_from` for a recurring session, otherwise immutable session `origin_local_date`, with confirmed clamped calendar year arithmetic |
| Session mutation | current version guard | Request `expected_version` compared with the owned session `version`, which every mutation increments |
| Cancel or restore | state transition time | Teaching transaction clock in UTC |
| Restore projection | active session fact | Existing `teaching.session.scheduled` with stored current values and the same session identifier |
| Replace projection | inactive session fact | Existing `teaching.session.cancelled` for each newly superseded untouched row |
| Any event | envelope, topic, key, and payload | Shared outbox contract plus spec 0001 catalogue. The key remains `session_id` |
| Retry command | command identity | `Idempotency-Key` scoped by trusted tutor and operation |
| Retry command | request equality | SHA 256 over operation, path resource identifier, normalized explicit body, sorted unique slots, and expected revision or version, excluding inferred clock, token values, and generated identifiers |
| Retry command | original inferred values | Immutable `command_receipts.context_snapshot` containing command time, governing time zone, and verified token request display zone |
| Retry command | exact response after later edits | Immutable success status plus `command_receipts.response_snapshot`, read with context before time zone, time dependent, or current state validation |
| Calendar read | visible classes | Active teaching `classes` rows for the trusted tutor |
| Calendar read | returned tutor | Verified token `sub` used for the owned query |
| Calendar read | UTC selection window | `from` and day after `through` resolved at local midnight in verified token `tz` |
| Calendar read | visible sessions | Teaching sessions whose `starts_at` falls inside that UTC window, optionally filtered by owned class identifiers |
| Calendar read | replaced history page | Replaced rows ordered by `(origin_local_date, session_id)` after the decoded opaque cursor, limited to at most 50 |
| Calendar read | included rule versions | Date intersecting rules, each returned class latest rule, and source rule identifiers from returned sessions |
| Calendar read | response ordering | Classes by normalized name and identifier, rules by class and revision, sessions by start and identifier, replaced history by origin and identifier |
| Calendar card | class label and color | Owned teaching class row joined by matching tutor and class identifiers |
| Calendar card | state | `cancelled_at`, then `superseded_at`, otherwise active |
| Calendar card | display date, times, and offsets | Stored UTC endpoints formatted in verified token `tz`, with start and end offsets returned separately |
| Calendar card | source zone and billing date | Source rule `time_zone` when present plus stored session `local_date`, both labelled as secondary facts when they differ from display values |
| Calendar filters and view | selected classes, Day, Week, Month, and visible date | Validated TanStack Router URL search values, with Monday as the first day |
| Calendar cache | exact server data window | TanStack Query key `['schedule', tutorId, requestTimeZone, { from, through, classIds, includeReplaced, historyCursor }]` |
| Overlap conflict | committed conflicting session details | Earliest owned `(starts_at, session_id)` found after rollback, formatted in the context snapshot request display zone; one vanished row causes one full command retry |
| Stale conflict | current schedule or session details | Locked owned class or session row that failed its revision or version comparison |
| Any transition | transition timestamps, row `updated_at`, and event `occurred_at` | One teaching transaction instant captured before validation and reused for every write and envelope caused by the command |

### Key invariants

1. Every schedule command holds the owned class row lock before reading or changing any rule,
   session, receipt, or outbox row for that aggregate. A session command first reads its owned class
   identifier without a lock, locks that class, then locks and rechecks the owned session. This is the
   one lock order.
2. The rule, slots, session rows, receipt, and every outbox row caused by one command commit in one
   `pgx.Tx`. No handler publishes to Redpanda.
3. The Postgres exclusion constraint, not a prior query, decides a concurrent time overlap. The
   handler may query first for a friendly response, but it must translate the constraint violation
   through a tutor scoped read when a race wins at commit.
4. Rule version ranges for a class never overlap. Sessions from different versions may coexist only
   when their active time ranges do not overlap.
5. A concrete session remains the only session fact seen by billing and notifications. Neither
   consumer stores or interprets a schedule rule.
6. A cancelled or schedule replaced session is never billable. Attendance remains attached and does
   not reactivate it.
7. Issued invoices remain immutable after a later move, cancellation, restoration, or rule change.
8. Every schedule query, join, conflict lookup, and receipt lookup filters by trusted `tutor_id`.
   Same service foreign keys include it.
9. Class schedule revision and session version are monotonic integers. Every successful relevant
   mutation increments exactly once, and no timestamp is used as an optimistic guard.
10. An idempotency receipt plus its context and response snapshots are immutable. A different
    canonical explicit request under the same key is a conflict. Canonical input includes the path
    resource and sorted slots. Generated identifiers, inferred time zone, and transaction timestamps
    are not part of the request hash. An exact replay reads the original status and snapshots before
    current token time zone, clock, or state validation.
11. A stale request never rebases itself onto current state. The current owned summary is returned
    so the tutor can refresh and decide again.
12. Calendar color is always paired with class text. Cancelled and replaced states also carry text
    and an icon.
13. Normal calendar queries use one request display zone and UTC instant bounds. Stored billing local
    dates and source rule zones are facts shown separately, never alternative grouping keys.
14. Replaced history is never unbounded. It requires an explicit request, a limit no greater than 50,
    and a stable opaque cursor bound to the complete owned query.
15. Every mutation uses one captured teaching transaction instant for transition timestamps, row
    update times, receipt context, and event occurrence time.
16. Recurring write routes are not deployed until the guarded `btree_gist` constraint migration is
    live. The additive schema alone is not a safe release of schedule generation.

### Security model

1. The gateway and teaching service require and verify the existing bearer token. There is one tutor
   role and no public schedule surface.
2. Every identifier is resolved together with token `sub`. Missing and cross tutor resources share
   `404`. List filters are predicates rather than resource lookups, so a foreign filter matches no
   row instead of changing the response shape.
3. Conflict details are returned only after the conflicting row is loaded with the same tutor scope.
4. Logs may carry request, operation, rule, class, and session identifiers plus stable error codes.
   They never carry tokens, request bodies, idempotency keys, class names, or student and attendance
   data.
5. The feature adds no regulated data category and no new compliance regime. Existing student and
   attendance protections continue to apply.

No new environment variable, secret, feature flag, third party account, or runtime service is
required.

### Browser behavior and accessibility

1. TanStack Router validates `view`, `date`, and repeated class filters. Invalid values fall back to
   Week on desktop or Day on phone and the tutor's current local date. Week columns and compact month
   rows always begin with Monday.
2. TanStack Query keys include tutor identifier, request time zone, the full visible window, sorted
   class filters, history state, and history cursor. A tutor or token time zone change clears the
   schedule cache. Successful mutations place their canonical session in view when applicable, then
   invalidate all filtered and unfiltered schedule queries that can include the affected class,
   because a move can leave one window and enter another.
3. Mutations do not retry automatically. The form retains its command key and values, so deliberate
   retry reaches the same receipt.
4. The calendar uses semantic day sections and session buttons in chronological document order. It
   does not claim `role="grid"` unless full arrow key behavior is implemented. Day, Week, and Month
   use the existing accessible Tabs primitive.
5. The details and form sheets trap focus, return focus to their trigger, use labelled controls and
   error descriptions, and keep primary targets at least 44 by 44 pixels.
6. Session cards use the existing seven class color tokens with readable foregrounds. Text, state
   labels, and icons carry the same meaning. Motion is limited to the existing page entrance and
   sheet behavior and respects reduced motion.
7. `Add schedule` and `Manage schedule` read `schedule_revision` from the class summary. Replacement
   and end confirmations state that explicit exceptions remain. The committed result announces exact
   created, adopted, preserved, and superseded counts. Stale results keep the draft and replace its
   expected revision only after the tutor chooses to refresh.

### Critical test scenarios

1. Happy path: create a Monday and Sunday rule during class creation, observe all concrete sessions
   in the global calendar, move one, cancel another, restore it, and observe billing and notifications
   converge through the real broker, verifies **AC-1**, **AC-2**, **AC-6**, **AC-7**, **AC-12**,
   **AC-16**.
2. Rule change: change a schedule from a future date where one later session is moved and one is
   cancelled across older rule versions, then replace before a future rule starts, at its start, after
   an active start, and after its end. Prove retirement, closing, allowed gaps, monotonic class
   revisions, preserved exceptions, no resurrected occurrence, and readable old rules, verifies
   **AC-4**, **AC-9**, **AC-11**.
3. End rule: end a series early and prove no replacement rule exists, untouched later sessions are
   inactive, exceptions remain, and consumers receive one cancellation per newly replaced session,
   verifies **AC-5**, **AC-16**.
4. Existing class: attach an exact standalone match without changing its identifier, retain a
   nonmatching or cancelled standalone session, publish no event for adoption, return separate
   created and adopted counts, and reject a generated overlap, verifies **AC-3**, **AC-8**.
5. Concurrent conflict: race two schedule creates, a move, and a restore into the same time range.
   Exactly one valid set commits, every loser returns its owned conflict detail, and no loser leaves
   a receipt or event. Submit two overlapping candidates together and receive `400` without a fake
   conflicting identifier, verifies **AC-8**, **AC-9**.
6. Lost response and stale tab: retry every command with the same key, change input under a used key,
   mutate the returned rule and session, change the token time zone, then retry the old keys and
   receive the exact original context and response snapshots. Issue a fresh key against stale class
   revision and session version and receive typed current details, verifies **AC-11**.
7. Time boundaries: accept a new start exactly 30 local dates ago, reject 31, generate across leap
   day, a forward clock gap, a whole day gap, and a backward repeated clock, and reject an end action
   before today. Prove inclusive dates, clamped move anniversaries, shifted gap time, earlier
   ambiguous instants, distinct start and end offsets, rejection after inverted normalization, and
   stable UTC values after a profile time zone change, verifies **AC-1**, **AC-5**, **AC-6**,
   **AC-10**.
8. Tenant isolation: use tutor A's token with tutor B's class, rule, session, and filter identifiers.
   Every operation returns `404` or an empty filtered read, with no leaked conflict detail, write, or
   event, verifies **AC-13**.
9. Window boundary: read Day, Week, and a six week Month window, reject a reversed or 43 date range,
   change the token zone across a date boundary, filter several classes including a foreign one, and
   prove UTC selection matches displayed dates. Request more than 50 replaced rows and walk the
   history cursor without a duplicate or gap, verifies **AC-12**, **AC-13**.
10. Projection replay: deliver scheduled, moved, cancelled, and restoration scheduled facts twice,
    then replay from the beginning. Each projection ends with one correct row and authoritative
    invoice records remain unchanged, verifies **AC-7**, **AC-16**.
11. Browser and accessibility: use the Monday first calendar at desktop and phone widths by keyboard
    and screen reader, test Add schedule, Manage schedule, all views, class filters, confirmation and
    session sheets, conflicts, exact result counts, empty states, 200 percent zoom, visible focus, 44
    pixel targets, and meaning without color, verifies **AC-14**, **AC-15**.
12. Contract and migration: migrate up and down from empty Postgres, regenerate sqlc plus both API
    clients, build every module, and drive the first recurring session through gateway, teaching,
    outbox, Redpanda, both projections, and the browser contract. Seed an existing overlap, prove the
    report names both owned rows and the constraint migration refuses it, reconcile deliberately,
    then apply the constraint. Prove recurring write routes are deployed only after that migration,
    verifies **AC-9**, **AC-17**.

## Build plan

Tracer Bullet means the first milestone proves one truthful weekly occurrence through the database,
event pipe, public contract, and browser before the recurrence engine grows wider.

1. [x] Add the schema phase teaching migration with class schedule revision, normalized rule and slot
   rows, session origin plus version fields, receipt snapshots, and the read only overlap report. Add
   the guarded `btree_gist` session and rule constraints after that report is empty, before recurring
   write routes are deployed. Extend class creation with one weekly slot, materialize its bounded occurrences in the class
   transaction, publish existing scheduled events, update both projection consumers for scheduled
   upsert, add the display zone 42 date schedule read, regenerate contracts, and render one
   accessible Week card through the gateway and web route, satisfies **AC-1**, **AC-2**, **AC-8**,
   **AC-9**, **AC-10**, **AC-12**, **AC-13**, **AC-16**, **AC-17**.
2. [x] Complete all seven independent weekday slots, two year bounds, active standalone adoption,
   separate authoritative counts, forward gap and backward ambiguity handling, postresolution
   duration checks, stored rule zones, immutable retry snapshots, class revision locking, and first
   schedule creation for an existing class, satisfies **AC-1**, **AC-3**, **AC-9**, **AC-10**,
   **AC-11**.
3. [x] Add future schedule replacement and explicit end schedule commands. Close rule versions, preserve
   all retained explicit exceptions, handle future retirement and allowed gaps, suppress regenerated
   origin dates, mark untouched sessions as replaced, publish one cancelled fact per replaced session
   atomically, and return typed stale and schedule state details, satisfies **AC-4**, **AC-5**,
   **AC-11**, **AC-16**.
4. [x] Add move, cancel, and restore commands with expected state guards, canonical responses, existing
   class then session lock ordering, version increments, event publication, projection reactivation
   on scheduled upsert, detailed owned conflicts, attendance plus invoice correction behavior, and
   `task schedule:conflicts`, satisfies **AC-6**, **AC-7**, **AC-8**, **AC-9**, **AC-11**,
   **AC-13**, **AC-16**.
5. [x] Build the complete Schedule page from existing design tokens and primitives. Add URL backed Day,
   Week, and Month views, the compact month picker, class filters, Add and Manage schedule entry
   points, confirmation and mutation sheets, provisional previews plus committed summaries, tutor and
   zone aware cache identity, class wide invalidation, cursor paged replaced history, phone agenda,
   and all accessible loading, empty, error, conflict, and announcement states, satisfies **AC-12**,
   **AC-14**, **AC-15**.
6. [x] Extend `task thread` through recurrence and one exception, add real Postgres and Redpanda tests for
   concurrent overlaps, rule changes, retries, time transitions, projection replay, and tenant
   isolation, then regenerate and commit every typed artifact and pass repository checks, satisfies
   **AC-1** through **AC-17**.

## Consequences

**Positive**:

1. Billing and notifications stay simple because they continue to consume concrete session facts.
2. One session row remains the stable home for attendance and individual corrections.
3. Database constraints protect schedule correctness even when requests race.
4. The calendar makes the generated facts visible and useful rather than hiding recurrence behind a
   form.

**Negative and tradeoffs**:

1. A two year daily rule writes as many as 732 sessions and outbox rows in one transaction. That is
   intentionally bounded, but the command is heavier than ordinary class creation.
2. A rule change emits one cancellation per replaced occurrence and one scheduled event per new
   occurrence. Projection lag will be visible briefly after a large change.
3. Keeping rule history, replaced sessions, and command receipts forever grows teaching storage.
   Receipt response snapshots add more retained JSON for the same reason.
4. `btree_gist` becomes a required Postgres extension and must be available to the migration role.
   Existing deployments need a schema phase, conflict reconciliation, and a later constraint phase.
5. Class and session revision columns add explicit concurrency state to every relevant response and
   command.
6. Three calendar views plus phone behavior are more web work than a list, and accessibility rules
   rule out drag and drop as the only interaction.
7. A restored or moved session after invoice issue changes future calculation facts but never the
   invoice already sent. Correction remains a deliberate void and rerun.

**Neutral**:

1. Spec 0003's session transition that described cancellation as permanent is refined here. Tutor
   cancellation becomes reversible by republishing the existing scheduled fact, while schedule
   replacement remains terminal.
2. A profile time zone change affects new rule versions and standalone moves, not existing recurring
   session instants.
3. The calendar reference supplies composition, not a second design system. Vermouth tokens and
   components remain authoritative.
4. The shared calendar groups by the tutor's current token time zone. A recurring source zone and
   stored billing date remain visible secondary facts when they differ.

## Migration plan

**Strategy**: additive two phase migration before route deployment

**Phases**:

1. Deploy the additive schema while the old application continues serving. Run
   `task schedule:conflicts` and reconcile every owned conflict deliberately.
2. Apply the guarded `btree_gist` constraint migration. Only after it succeeds, deploy the application
   version that registers recurring write routes and reads the new required columns.

**Rollback**: Before route deployment, reverse the additive schema after confirming no new application
uses it. After route deployment, roll back the application first. Retain generated rule and session
rows while deciding whether the schema can be reversed without data loss.

**Risks**: A new conflict between the report and the guarded migration stops phase 2. An unavailable
`btree_gist` extension also stops phase 2. Neither failure exposes recurring writes or changes
existing rows automatically.

## Follow-up

1. [ ] Feature 13 should show when a session correction affects a period with an issued invoice and
   lead the tutor to the existing void and rerun flow. Teaching must not call billing to enforce it.
2. [ ] Feature 17 should verify that its digest consumer treats a repeated
   `teaching.session.scheduled` as reactivation after cancellation.
