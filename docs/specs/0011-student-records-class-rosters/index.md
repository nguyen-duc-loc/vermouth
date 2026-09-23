# 0011. Student records and class rosters

**Date**: 2026-09-20
**Status**: Accepted

## Summary

Student records become a complete working area instead of a setup step hidden on Home. A tutor can
manage students, keep dated class membership history, and save one whole roster attendance pass in
one transaction. The design extends the existing teaching model and event catalogue, then replaces
the narrow roster and attendance routes with atomic resource commands.

## Requirements

**User stories**:

1. As a tutor, I want to create, find, edit, and remove student records so that my contact list stays
   useful as classes change.
2. As a tutor, I want to manage one class roster with effective dates so that current work and past
   billing agree.
3. As a tutor, I want to mark a whole session roster in one pass so that I can finish attendance
   safely from my phone.
4. As an engineer, I want retries, concurrent edits, and event publication to preserve one coherent
   teaching truth.

**Acceptance criteria**:

1. **AC-1**: A signed in tutor can create a student, list active students, open one student, edit
   name or phone, and archive the student. Names are trimmed and contain 1 through 160 Unicode
   characters. Phone is nullable, trimmed at its outside edges, at most 40 characters, and preserves
   punctuation. Duplicate names and phone values are allowed.
2. **AC-2**: `GET /api/students` searches active students by case insensitive name or literal phone
   fragment, rejects a trimmed query longer than 160 characters, returns 50 rows ordered by
   `(lower(name), name, student_id)`, and uses an opaque cursor. An unchanged result set has no
   duplicate or missing row across its cursor pages. A student mutation resets browser pagination
   because an edited ordering field may move a row. Each row contains name, nullable phone, active
   class count for the tutor's current local date, and `updated_at`.
3. **AC-3**: Student detail returns the active record, active classes first, then past membership
   periods. Each period is a separate item containing class identifier, name, color, inclusive first
   date, and nullable inclusive last date. An archived student is absent from normal student reads,
   while historical roster and attendance reads retain the name with an `Archived` flag and never
   expose the phone.
4. **AC-4**: Student edit requires the `updated_at` value the client read. A stale value returns
   `409 student_changed` with the current identifier and timestamp, but no phone. The browser reads
   the current owned record separately before asking the tutor to resolve the edit. A changed name publishes
   `teaching.student.changed` in the same transaction. A phone only edit stays inside teaching.
5. **AC-5**: Student removal is blocked while any roster period is active on the tutor's current
   local date. The `409 active_memberships` details contain safe class identifiers and names. A
   successful removal sets `removed_at`, publishes `teaching.student.removed`, keeps every old row,
   and provides no restore action in this feature.
6. **AC-6**: `GET /api/classes/{class_id}/roster` returns the owned class summary, the requested
   local date or the tutor's current local date by default, and every student whose membership
   covers that date. A requested date after today is rejected. Current means the resolved date is
   today and the student is not archived, and only those rows may show phone. Past and archived rows
   show name, membership dates, and an optional `Archived` badge but no phone.
7. **AC-7**: One roster command accepts one `change_date`, disjoint addition and removal identifier
   lists, and no more than 200 changed students. `change_date` is today or earlier in the token
   timezone and means the first date with the new roster. An addition starts on that date. A removal
   stores the previous date as its inclusive `effective_to`.
8. **AC-8**: One class may cover at most 500 students on any local date. Membership periods for one
   class and student never overlap, a period always covers at least one day, and leaving then
   rejoining creates another period. A delta is evaluated against committed periods after it obtains
   the class lock. Valid concurrent disjoint changes may both commit. An invalid addition or removal
   returns `409 roster_conflict` with a phone free roster summary and changes nothing.
9. **AC-9**: A roster delta locks the owned class, then every referenced student in stable order,
   validates every owned active student and period, writes every membership change, command receipt, and one catalogue event per change in one
   transaction, or writes nothing. Events use the existing `class_id` key and carry the dates named
   by spec 0001. Exact retries return the original response. Reusing the key with another command
   returns `409 idempotency_conflict`.
10. **AC-10**: A backdated roster correction never deletes attendance. A student no longer covered
    by a past session disappears from that session roster and future billing calculations, while the
    retained attendance row remains history. Already issued invoices remain unchanged under INV-9.
11. **AC-11**: `GET /api/sessions/{session_id}/attendance` returns the owned session summary, its
    roster on `local_date`, nullable saved state per student, one opaque revision, and eligibility
    with an exhaustive refusal reason.
    Attendance becomes eligible at `starts_at` and remains correctable afterward while the session
    is active, not cancelled, and not replaced.
12. **AC-12**: The attendance sheet starts every new row unmarked and offers an explicit Mark all
    present action. Saving requires exactly one `Present` or `Absent` item for every student in the
    returned roster. Duplicate, missing, or extra identifiers reject the whole request.
13. **AC-13**: Attendance save requires the revision that was read, locks the owned class, session,
    and roster students in the shared order, then writes every attendance row, command receipt, and `teaching.attendance.marked` event in one
    transaction. Every row and event in one accepted pass uses one server `marked_at`. The response
    returns the complete saved marks in roster order. A stale revision returns
    `409 attendance_changed` with the fresh sheet and changes nothing.
14. **AC-14**: `api/openapi.yaml` defines every request, response, detail object, parameter, and
    error for the feature with reusable OpenAPI 3.1 components and unique operation identifiers.
    Generated gateway and browser types are current. The old single student roster join and single
    student attendance routes are removed after their callers move to the atomic routes.
15. **AC-15**: Every route requires the existing bearer token. `tutor_id` and timezone come only
    from verified claims, every query filters by tutor, and a foreign tutor identifier receives the
    same `404` as a missing record. Phone values never enter events, logs, metrics, error details,
    class history shown for an archived student, attendance surfaces, or conflict details.
16. **AC-16**: The app adds `Students` as a primary destination, `/students` for the searchable
    responsive list, `/students/{student_id}` for record and class history, and
    `/classes/{class_id}` for a class summary and dated roster. Home, Schedule, student membership,
    and roster links use real TanStack Router links and survive reloads.
17. **AC-17**: The class detail page opens a Manage roster sheet that searches active students,
    stages checked additions and removals, preserves its draft while creating a missing student,
    then saves one delta. The student detail page edits in a sheet and keeps archive as a separate
    destructive action. A blocked archive shows links to every active class.
18. **AC-18**: An eligible Home or Schedule session opens one attendance sheet. It shows student
    name and state controls only, keeps input after a network failure, reuses the same idempotency
    key while input is unchanged, and requires review after a stale response. An empty roster shows
    a link to class roster management and no Save action.
19. **AC-19**: Every screen follows `web/design.md`, works phone first and as a wide table or detail
    layout, remains keyboard usable at 200 percent zoom, has visible focus, uses at least 44 pixel
    controls, traps and restores sheet focus, announces success politely, and exposes blocking
    errors as alerts. No new image asset or color only meaning is introduced.
20. **AC-20**: The feature adds structured logs containing request, command, entity identifiers,
    and counts only. It adds no environment variable, secret, external account, feature flag,
    service, or formal privacy workflow. Store, handler, gateway, generated contract, browser,
    Postgres, Redpanda, and replay tests prove the complete path.

## Decision

**Chosen option**: Option 1: extend the temporal teaching model with atomic resource commands

Keep `students`, `roster_periods`, `attendance`, and `command_receipts` as the only entities. Add the
constraints and reads that make their existing meanings complete, then expose student resources,
one class roster delta, and one session attendance pass through teaching and the gateway.

**Implementation skills**: `golang-database` (`samber/cc-skills-golang`,
`.agents/skills/golang-database/`) · `go-goose` (`metalagman/agent-skills`,
`.agents/skills/go-goose/`) · `openapi` (`oakoss/agent-skills`, `.agents/skills/openapi/`) ·
`frontend-design` (`anthropics/skills`, `.agents/skills/frontend-design/`) ·
`tailwindcss-accessibility` (`josiahsiegel/claude-plugin-marketplace`,
`.agents/skills/tailwindcss-accessibility/`) · `tanstack-query-best-practices`
(`deckardger/tanstack-agent-skills`, `.agents/skills/tanstack-query-best-practices/`) ·
`tanstack-router-best-practices` (`deckardger/tanstack-agent-skills`,
`.agents/skills/tanstack-router-best-practices/`)

## Rationale

Reasoning and options: see [rationale.md](rationale.md).

## Feature design

### Current gap and replacement boundary

The core teaching loop already creates one student, opens one roster period, and writes one
attendance row at a time. It has no student list or detail read, no edit or archive command, no
roster close command, and no whole roster transaction. This feature keeps the proven entities and
events. It directly replaces only the browser facing single membership and single mark routes,
because the gateway and browser ship from the same repository.

### Page composition

1. `/students` uses `AppShell`, a page header with Create student, search, active student results,
   and cursor controls. `ResponsiveTable` renders cards on phones and a semantic table on wide
   screens. Each result shows name, nullable phone, and active class count.
2. `/students/{student_id}` shows the record first, Edit and Archive actions next, active classes,
   then dated past memberships. Edit uses a sheet. Archive uses a confirmation dialog and stays
   separate from ordinary editing.
3. `/classes/{class_id}` shows class identity with its stored color marker, a validated roster date,
   roster rows, then Manage roster. The sheet searches active students, stages one delta, and may
   open Create student without dropping that draft.
4. Home and Schedule session cards open one attendance sheet after the session start. The sheet
   shows one labelled Present or Absent choice per student, Mark all present, and Save attendance.
   New rows have no selected state.
5. Existing Vermouth components and Lucide icons are enough. There are no photos, avatars, new
   primitives, decorative metrics, or page specific motion.

### Data model

| Entity | Key | Fields used here | Constraints and relationships |
|---|---|---|---|
| `students` | `student_id` | `tutor_id`, `name`, nullable `phone`, `created_at`, `updated_at`, nullable `removed_at` | name length 1 through 160 after trim; nullable phone length at most 40; duplicates allowed; one tutor owns many students |
| `classes` | `class_id` | existing class fields | the owned row is locked for a roster delta; one class covers at most 500 students on a date |
| `roster_periods` | `(class_id, student_id, effective_from)` | `tutor_id`, nullable inclusive `effective_to`, timestamps | tenant foreign keys remain; date ranges for one pair do not overlap; one student may join many classes and may rejoin one class in another period |
| `sessions` | `session_id` | existing session fields | the owned row is locked for attendance; eligibility comes from stored timestamps and active state |
| `attendance` | `(session_id, student_id)` | `tutor_id`, `state`, `marked_at`, timestamps | one state per session and student; state remains `Present` or `Absent`; roster coverage is validated at write time |
| `command_receipts` | `(tutor_id, operation, idempotency_key)` | existing request hash, resource identifiers, context, response status, response snapshot | add `update_student`, `remove_student`, `change_roster`, and `save_attendance`; receipts remain immutable |

`students.created_at` and the initial `updated_at` come from the Postgres transaction clock. A real
student edit sets `updated_at` to the later of the command time and the prior value plus one
microsecond, so each accepted change advances strictly. The API serialises the stored UTC value as
RFC 3339 with microsecond precision, and the next edit compares that exact instant. An accepted no
operation does not advance it.

Teaching migration `00007_student_records_class_rosters.sql` is additive. It extends the student
checks and command operation check, adds the student list index on
`(tutor_id, lower(name), name, student_id)` for active rows, and adds a GiST exclusion constraint
using the existing `btree_gist` extension so membership date ranges for one tutor, class, and student
cannot overlap. Before adding each constraint, the migration reports and refuses invalid existing
rows rather than rewriting history.

Phone fragment search has no special index because literal substring matching cannot use the name
index and the agreed tutor scale does not justify another extension. The runner up was a stored
normalised phone value, rejected because it duplicates private data only to optimise an unmeasured
problem.

### State transitions

```text
student
  active -> edited -> active
  active with no active memberships -> archived
  archived -> retained history only

roster period
  absent -> open on effective_from
  open -> closed on effective_to
  closed -> another open period on a later effective_from

attendance pass
  unmarked roster -> complete Present or Absent set
  complete set -> corrected complete set
  stale revision -> fresh sheet required before another save
```

### API surface

All public operations live in `api/openapi.yaml`. The gateway verifies the bearer token and proxies
commands without owning business rules. Teaching applies the same paths without the public `/api`
prefix.

| Endpoint | Method | Key inputs | Key outputs | Auth | Key errors |
|---|---|---|---|---|---|
| `/api/students` | `GET` | optional `q`, optional opaque `cursor` | 50 active summaries, nullable `next_cursor` | bearer | `400`, `401` |
| `/api/students` | `POST` | `Idempotency-Key`, name, nullable phone | `201` created or `200` exact replay, student record | bearer | `400`, `401`, `409` |
| `/api/students/{student_id}` | `GET` | student identifier | active record with ordered membership history | bearer | `401`, `404` |
| `/api/students/{student_id}` | `PATCH` | `Idempotency-Key`, `expected_updated_at`, name or phone | updated record | bearer | `400`, `401`, `404`, `409 student_changed`, `409 idempotency_conflict` |
| `/api/students/{student_id}` | `DELETE` | `Idempotency-Key` | `204` exact success or replay | bearer | `401`, `404`, `409 active_memberships`, `409 idempotency_conflict` |
| `/api/classes/{class_id}/roster` | `GET` | optional `date` | class summary, resolved date, ordered roster | bearer | `400`, `401`, `404` |
| `/api/classes/{class_id}/roster` | `PUT` | `Idempotency-Key`, `change_date`, additions, removals | canonical roster on `change_date` | bearer | `400`, `401`, `404`, `409 roster_conflict`, `409 idempotency_conflict` |
| `/api/sessions/{session_id}/attendance` | `GET` | session identifier | session, eligibility, roster marks, revision | bearer | `401`, `404` |
| `/api/sessions/{session_id}/attendance` | `PUT` | `Idempotency-Key`, revision, complete mark array | batch `marked_at`, complete saved marks | bearer | `400`, `401`, `404`, `409 attendance_changed`, `409 session_not_eligible`, `409 idempotency_conflict` |

Every operation uses named component schemas, path parameters marked required, the existing bearer
security scheme, and the shared `APIError`. Recovery data uses the existing `details` object:

1. `student_changed` contains the current student.
2. `active_memberships` contains the active class identifiers and names.
3. `roster_conflict` contains the current dated roster.
4. `attendance_changed` contains the fresh attendance sheet.
5. `session_not_eligible` contains one of `replaced`, `cancelled`, `class_archived`, or
   `not_started`, in that precedence order.

Invalid shape, date, query, cursor, or roster coverage is `400`. Missing and cross tutor resources
are `404`. A valid command blocked by current state or stale input is `409`.

### Response components and ordering

| Component | Exact fields | Ordering or disclosure rule |
|---|---|---|
| `StudentSummary` | `student_id`, `name`, nullable `phone`, `active_class_count`, `updated_at` | student page order is `(lower(name), name, student_id)` |
| `StudentRecord` | `student_id`, `name`, nullable `phone`, `created_at`, `updated_at` | returned only for an active owned student |
| `StudentMembership` | `class_id`, `class_name`, `class_color`, `effective_from`, nullable `effective_to`, `active` | periods covering today first, then past periods by `effective_from` descending, `class_name`, `class_id`; one item per period |
| `ClassSummary` | `class_id`, `name`, `color` | values come from the owned class row |
| `RosterStudent` | `student_id`, `name`, nullable `phone`, `archived`, `effective_from`, nullable `effective_to` | `(lower(name), name, student_id)`; phone is nonnull only for an active student when resolved date equals today |
| `SessionSummary` | `session_id`, `class_id`, `class_name`, `class_color`, `starts_at`, `ends_at`, `local_date`, `state` | state is `active`, `cancelled`, `replaced`, or `class_archived` from owned rows |
| `AttendanceStudent` | `student_id`, `name`, `archived`, nullable `state`, nullable `marked_at` | `(lower(name), name, student_id)`; phone never appears |
| `StudentPage` | `students`, nullable `next_cursor` | contains at most 50 `StudentSummary` values |
| `StudentDetail` | `student`, `memberships` | `student` is `StudentRecord`; memberships use the rule above |
| `ClassRoster` | `class`, `resolved_date`, `students` | `class` is `ClassSummary`; students use the roster rule above |
| `AttendanceSheet` | `session`, `eligible`, nullable `ineligible_reason`, `revision`, `students` | session and students use the attendance rules above |
| `AttendanceSave` | `session_id`, `marked_at`, `marks` | every mark carries `student_id`, `state`, and the common `marked_at`, in attendance order |

Blocking classes use `(lower(class_name), class_name, class_id)`. Every recovery payload uses its own
phone free component rather than reusing `StudentRecord` or the ordinary current roster response.
`student_changed` details contain only `student_id` and `updated_at`. `roster_conflict` details use
`ClassSummary` plus phone free `RosterStudent` values. The browser follows either response with the
ordinary authenticated read when it needs the full current resource.

### Command behavior

Student create keeps its current receipt behavior. Edit hashes the validated patch plus
`expected_updated_at`. Archive hashes the owned student identifier and requires no request body.
Both lock the owned student before comparing state. An exact receipt replay wins before current
state validation, so a lost response always returns the original result.

Student patch distinguishes field presence. Omitted phone means unchanged, explicit null or a value
that trims empty means clear, and a nonempty value replaces it. An empty patch, duplicate JSON field,
or unknown field is `400 invalid_input`. A valid patch whose canonical values equal the stored row
returns `200`, records the receipt, advances no timestamp, and emits no event. A real phone change
advances `updated_at` without an event. A real name change advances `updated_at` and emits one
`teaching.student.changed`.

Roster save locks the owned class, sorts each identifier list, checks that the lists are disjoint,
rejects repeated identifiers and an empty delta, locks referenced student rows in identifier order,
then evaluates all periods and the 500 student coverage limit.
For change date `D`, an addition writes `effective_from = D` and a removal writes
`effective_to = D minus one calendar day`. A removal targets the unique period that covers `D`
before the change and requires `effective_from < D`. That period may be open or closed and may belong
to an archived student, so a backdated correction can shorten retained history. An archived student
may never be added. An addition requires an active student and no retained period on or after `D`
that an open period would overlap. Adding before a later retained period, adding an already covered
student, removing a student not covered on `D`, a zero day result, and capacity overflow are
`409 roster_conflict`. The transaction writes events in stable student identifier order.
`teaching.roster.joined` carries the new first date, while `teaching.roster.left` carries the stored
inclusive last date.

The date model deliberately cannot erase a mistaken same day addition. The earliest legal removal
uses the following day as `change_date`, and future changes are not accepted, so the correction is
available on that following day and preserves a one day period. The Manage roster confirmation
states this before save. The runner up was deleting an unused period, rejected because the system
retains teaching history and downstream consumers have no deletion fact.

Attendance read builds a canonical snapshot ordered by `(lower(name), name, student_id)`. Its opaque
revision is SHA 256 over the owned session state, ordered roster identities and labels, and current
attendance states and update timestamps. Save checks the receipt first, locks the class, session,
and roster students in the shared order, rebuilds the snapshot, compares the revision, then checks
that the submitted identifiers equal the roster.
It upserts every row with one UTC command time and emits one `teaching.attendance.marked` event per
row using that time. An exact receipt retry returns the stored response without emitting again.

Eligibility is false when the session has `superseded_at`, then when it has `cancelled_at`, then when
the class has `archived_at`, then when the service clock is before `starts_at`. Those checks map to
`replaced`, `cancelled`, `class_archived`, and `not_started` in that order. Eligibility is true only
when none applies. An attendance read still returns the sheet and refusal reason when false.

The chosen class and session row locks use Postgres default `READ COMMITTED` isolation. The runner up
was serializable isolation, rejected because one aggregate row already gives every competing command
the same lock order and a database exclusion constraint remains the final membership guard.

### Lock order and coherent reads

Every write that can affect roster or attendance uses one lock order: owned class row, owned session
rows in identifier order when present, then owned student rows in identifier order when present.
Roster commands take class then students. Attendance and session cancellation, restore, move, or
replacement take class then session, with attendance then taking roster students. Student edit and
archive take only their student row and never request a class lock. This asymmetric rule has no lock
cycle: if archive wins its student lock, a waiting roster command later sees an archived student and
fails; if roster wins, archive later sees the committed active membership and is blocked.

After those locks are held, a command reloads every value used for validation in the same
transaction. The exclusion constraint remains the final roster overlap guard. Attendance may correct
a retained historical mark for an archived student, but it cannot add that student to a new roster.

Student list and count use one SQL statement. Student detail, dated roster, and attendance sheet use
one SQL statement where practical, otherwise a read only `REPEATABLE READ` transaction so every row
and revision comes from one Postgres snapshot. Attendance save takes the locks above, then rebuilds
its snapshot before comparing the revision. These rules prevent a response or revision assembled
from states that never existed together.

### Receipt identity

Canonical request hashes include the operation, path resource identifier, and validated body with
field presence preserved. Student patch also includes `expected_updated_at`. Roster hashes include
`class_id`, `change_date`, and sorted unique addition and removal identifiers. Attendance hashes
include `session_id`, the revision, and marks sorted by `student_id`. Duplicate identifiers fail
validation before hashing. A changed canonical command requires a new browser key.

Create keeps the existing contract: the creator receives `201`, while an exact replay receives the
same resource with `200`. Every new mutation in this feature stores and replays its original status
and body, which is `200` for edit, roster, and attendance and `204` for archive.

### Error mapping

1. `400 invalid_input`: malformed JSON or cursor, unknown or repeated JSON field, empty patch or
   roster delta, duplicate identifier, intersecting addition and removal lists, more than 200
   changes, invalid date, future roster date, or incomplete attendance marks.
2. `404 not_found`: missing or foreign student, class, or session.
3. `409 student_changed` or `active_memberships`: stale student edit or blocked archive.
4. `409 roster_conflict`: already covered addition, absent removal target, archived addition,
   overlap, zero day result, later retained period, or 500 student capacity.
5. `409 attendance_changed` or `session_not_eligible`: stale sheet, replaced session, cancelled
   session, archived class, or request before `starts_at`.
6. `409 idempotency_conflict`: one key names another canonical command.

### Value sourcing

| Action | Value produced or displayed | Source |
|---|---|---|
| Every request | `tutor_id`, timezone, language | verified token claims, never request fields |
| Student create | identifier | UUIDv7 generated in Go |
| Student create or edit | name and phone | trimmed validated request fields; empty phone becomes null |
| Student create | `created_at`, initial `updated_at` | Postgres transaction clock, stored in UTC |
| Student edit | next `updated_at` | later of service command time and stored `updated_at` plus one microsecond |
| Student list | search text | trimmed `q`, empty when omitted |
| Student list | ordering and cursor | stored `lower(name)`, stored name, and `student_id`; cursor also binds the normalised query |
| Student list | active class count | roster periods covering current date from the service clock in token timezone |
| Student detail | active and past classes | owned class rows joined to all retained roster periods, one item per period, ordered by the response component rule |
| Student edit | concurrency value | request `expected_updated_at` compared with `students.updated_at` |
| Student archive | blocking classes | roster periods covering current local date joined to owned classes |
| Student archive | `removed_at` | one UTC transaction clock value |
| Roster read | resolved date and current versus historical disclosure | validated query `date`, else current date from service clock in token timezone; a date after today is invalid and only a today row for an active student carries phone |
| Roster read | class identity and rows | owned `classes`, `roster_periods`, and `students` rows covering the resolved date |
| Roster change | first date with new roster | validated request `change_date` |
| Roster change | removal last date | one calendar day before validated `change_date` |
| Roster events | names, keys, and fields | spec 0001 catalogue plus committed membership rows |
| Attendance read | session summary and eligibility | owned session and class columns plus service UTC clock, evaluated by the fixed refusal precedence |
| Attendance read | roster | periods covering stored session `local_date` joined to retained students |
| Attendance read | states | `attendance` rows for the session and roster student identifiers |
| Attendance read | revision | SHA 256 of the canonical session, roster, and attendance snapshot |
| Attendance save | state per student | complete request mark array validated against the fresh roster |
| Attendance save | `marked_at` | one UTC transaction clock value shared by every row and event |
| Every event | envelope and partition key | shared vermouth envelope plus the event key fixed in spec 0001 |
| Historical screen | archived label and name | retained `students.removed_at` and `students.name`; phone is deliberately omitted |
| Browser route | search, cursor, and roster date | validated TanStack Router search values |
| Browser mutation | retry key | `crypto.randomUUID()` retained while validated input is unchanged |

### Key invariants

1. One command transaction touches one student, one class roster, or one session attendance
   aggregate, never more than one aggregate.
2. A business change and every outbox row it causes commit together. The handler never publishes.
3. All owned reads, locks, writes, joins, and receipt lookups include trusted `tutor_id`.
4. Student phone is teaching only. Events carry name when the catalogue requires it and never phone.
5. Membership coverage remains inclusive at both ends. Different services use the predicate fixed
   by spec 0003.
6. One class command serialises all roster changes. The exclusion constraint, not handler care,
   prevents overlapping history.
7. Attendance save is a complete session aggregate replacement for the roster at that session date.
   Attendance rows no longer covered after a dated roster correction remain stored and untouched.
8. Exact command retries return the stored status and body. A key with another canonical input is a
   conflict.
9. Student and attendance reads use stable ordering. Browser caches never invent teaching state.
10. Issued invoices never change. Later projection correction affects only a deliberate void and
    reissue flow.
11. Canonical multirow reads come from one SQL statement or one `REPEATABLE READ` snapshot. Commands
    lock dependencies in class, session, student order before rebuilding their validation snapshot.

### Security and privacy

1. The gateway and teaching require and verify the existing token. No endpoint accepts `tutor_id`.
2. A foreign identifier returns `404`. Structured conflict details contain only owned recovery data.
3. Phone appears only in active student and current roster responses for the owning tutor. It is
   excluded from events, logs, metrics, attendance, archived history, and errors.
4. Logs contain request, operation, entity identifiers, counts, status, and duration only. Command
   receipt hashes and bodies are not logged.
5. No formal compliance program applies in this feature. Data minimisation, tenant isolation, and
   permanent teaching history remain the agreed boundary.

No configuration, secret, credential, external provider, or feature flag is required.

### Browser data flow

1. Add validated TanStack routes for `/students`, `/students/$studentId`, and
   `/classes/$classId`. Student `q` and `cursor`, and class roster `date`, live in validated search
   values. Links use `Link`, not click handlers or raw anchors.
2. Query keys are hierarchical arrays for student pages, student detail, dated class roster, and
   session attendance. Cursor reads use `useInfiniteQuery` with `next_cursor` as the next page
   parameter.
3. Student mutations invalidate the affected detail, every student list, and dated rosters that may
   show the label, plus attendance sheets that may show the name or archived state. Roster changes invalidate that class roster, affected student details and lists,
   Home, Schedule, and the affected session attendance queries. Attendance invalidates its session,
   Home, and Schedule. Invalidation is targeted by key family, never the whole cache.
4. Do not apply optimistic teaching writes. Display the canonical response, then refetch. Network
   failure keeps the form and key. A student edit conflict keeps the local draft, reads the current
   record, and shows both without merging. A roster conflict shows the fresh roster and keeps the
   intended delta visibly: additions already present and removals already absent are marked
   satisfied, while remaining changes require confirmation. An attendance conflict replaces the
   server baseline, retains local states by `student_id` for students still present, adds new
   students unmarked, drops removed students with a notice, and refreshes renamed labels. Every row
   must be reviewed before another save. Any canonical input changed during recovery gets a new
   idempotency key.
5. Sheets trap focus and restore it to their trigger. Radio groups and checked student controls use
   labelled fieldsets, visible focus, logical reading order, polite success announcements, and alert
   errors. Every normal target is at least 44 by 44 CSS pixels.

### Critical test scenarios

1. Student lifecycle: create two students with the same name and phone, search them across a cursor,
   edit one name and one phone, reload detail, then archive after leaving every class, verifies
   **AC-1** through **AC-5**.
2. Lost response and concurrency: retry each command after commit, reuse a key with changed input,
   race two student edits, race two roster deltas, and race two attendance saves, verifies **AC-4**,
   **AC-9**, **AC-13**.
3. Roster history: add, remove, and rejoin one student, reject overlap and zero day periods, enforce
   500 covered students, and read current plus historical rosters including an archived label,
   verifies **AC-6** through **AC-10**.
4. Whole roster attendance: leave rows unmarked, use Mark all present, change one to Absent, save one
   complete pass, retry it, then correct the pass and prove one timestamp per accepted command,
   verifies **AC-11** through **AC-13**.
5. Attendance refusal: reject a future, cancelled, replaced, stale, incomplete, duplicate, or extra
   mark request with no row, receipt, or outbox write, verifies **AC-11** through **AC-13**.
6. Projection thread: apply roster and attendance events through real Postgres and Redpanda, deliver
   duplicates, replay, and prove billing and notifications converge without changing issued
   invoices, verifies **AC-9**, **AC-10**, **AC-20**.
7. Tenant and privacy: try every read and command with another tutor's identifiers, then inspect
   events, structured logs, error details, and historical responses for phone leakage, verifies
   **AC-14**, **AC-15**, **AC-20**.
8. Contract: regenerate Go and browser types, assert old narrow routes are absent, and drive the
   student, roster, and attendance path through the gateway, verifies **AC-14**, **AC-20**.
9. Browser recovery: fail every mutation once, preserve drafts and keys, return each stale conflict,
   and require review before resubmission, verifies **AC-16** through **AC-18**.
10. Accessibility: complete every route, sheet, dialog, search page, and attendance pass at phone and
    wide widths with keyboard only, screen reader output, 200 percent zoom, long Unicode labels,
    visible focus, and no color only meaning, verifies **AC-19**.

## Build plan

Tracer Bullet means the first milestone proves one student record from migration through a generated
contract to a real page. Later milestones thicken that same teaching path with class membership and
session attendance rather than building each layer in isolation.

1. Add the guarded teaching migration, student list, create, detail, and optimistic edit commands,
   their OpenAPI schemas, teaching and gateway routes, generated types, typed client functions, and
   the minimal Students page. Prove one created and edited record survives reload, satisfies
   **AC-1** through **AC-4**, **AC-14** through **AC-16**, **AC-20**.
2. Add student membership history and archive with active class conflict details. Complete student
   cards, wide table, detail page, edit sheet, archive dialog, search cursor, recovery states, and
   privacy checks, satisfies **AC-2** through **AC-5**, **AC-14** through **AC-17**, **AC-19**,
   **AC-20**.
3. Add dated class roster read and atomic roster delta under the class row lock. Publish existing
   events, verify billing and notifications, replace the old join caller and route, then build class
   detail and Manage roster with nested student creation, satisfies **AC-6** through **AC-10**,
   **AC-14** through **AC-17**, **AC-19**, **AC-20**.
4. Add session attendance read, canonical revision, atomic complete save under the session row lock,
   receipt replay, and existing events. Replace the old per student caller and route, then change
   Home and Schedule to open the attendance sheet, satisfies **AC-11** through **AC-15**,
   **AC-18** through **AC-20**.
5. Add validated routes, standard links, exact query keys, cursor continuation, targeted
   invalidation, every loading, empty, error, stale, success, phone, wide, and accessibility state,
   satisfies **AC-2**, **AC-3**, **AC-6**, **AC-11**, **AC-16** through **AC-19**.
6. Regenerate and commit SQL and public types, remove obsolete narrow schemas and routes, extend the
   real infrastructure thread, and run format, lint, type, build, integration, replay, component,
   and migration reversal checks, satisfies **AC-8** through **AC-15**, **AC-19**, **AC-20**.

## Migration plan

**Strategy**: additive data constraints, then direct internal contract replacement

**Phases**:

1. Scan existing students for invalid lengths and roster periods for overlaps. Stop and report exact
   identifiers if any row violates the target model.
2. Apply migration `00007`, regenerate SQL, and deploy the new teaching reads and commands while the
   old narrow routes still exist.
3. Move the setup, Home, Schedule, and new records pages to the resource routes. Remove the old
   single join and single mark routes only after every repository caller and driver has moved.

**Rollback**: before any new operation receipt exists, callers may revert and the Goose down step may
drop the new checks, index, exclusion constraint, and command operation additions. The down step
checks for `update_student`, `remove_student`, `change_roster`, and `save_attendance` receipts and
refuses if any exists. After first use, roll back application code while leaving migration `00007`
in place, then ship a forward fix. No rollback deletes a receipt, roster period, attendance row, or
student record automatically.

**Risks**: invalid development history blocks the constraint migration. Direct route replacement is
safe only because the web app and gateway are the sole clients and ship from this repository. A
separately deployed client would require a deprecation window. A same day mistaken roster addition
cannot be erased, and a backdated addition before a later retained period is rejected rather than
fitted around history.

## Consequences

**Positive**:

1. Student records, membership history, and attendance become real product areas without another
   service or another source of truth.
2. Database constraints and aggregate row locks make concurrent roster and attendance commands
   deterministic.
3. One complete attendance command removes the partial save state that the current per student route
   permits.
4. Historical records remain available for billing and disputes while current pages stay quiet.

**Negative and tradeoffs**:

1. Complete attendance save rewrites every current roster row and publishes one event per student,
   even when only one state changed. This buys one timestamp and one atomic pass at the cost of more
   outbox traffic.
2. Dated roster correction can change future billing projections without changing an already issued
   invoice. The tutor still needs the later void and reissue flow for a sent invoice.
3. Cursor search, stale revisions, and structured conflict details add contract and browser state
   beyond simple create, read, update, and archive screens.
4. Receipt rows retain response snapshots that may contain phone values inside the teaching
   database. They never leave teaching, but they are another retained copy of the private value.
5. Phone substring search may scan one tutor's active rows. This is deliberate until measured data
   justifies a normalised search field or another Postgres extension.
6. A same day mistaken roster addition remains a one day historical membership. The model favours
   retained history over an undo path, and the confirmation must make that cost visible.

**Neutral**:

1. Student restore, CSV import, spreadsheet paste, photos, notes, guardian contacts, and future
   roster changes remain outside this feature.
2. Existing billing and notification consumers need stronger integration tests, not new event names.
3. Visible copy remains English until feature 20 adds translation state.

## Follow-up

1. [ ] If student restoration becomes a real need, amend the event catalogue before adding it. A
   local restore would leave billing's removed projection wrong.
2. [ ] Revisit phone search only after query measurements show the tutor scoped scan is material.
3. [ ] Feature 20 should translate the caller owned student, roster, and attendance copy with
   Vietnamese as the default language.
