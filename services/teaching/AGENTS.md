# services/teaching

## Overview

Owns what is taught: classes, schedules, sessions, students, roster membership, attendance, and the
current tuition rate on a class. It publishes most of the facts in the system, so it is the busiest
publisher and the one whose event changes ripple furthest. It must never own invoices, money
arithmetic, rate history for billing, or email sending, and it holds copies of nothing.

The service provides the core teaching loop, recurring schedule commands, session exceptions,
complete student records, dated class rosters, whole roster attendance, and calendar reads. The
handler keeps each aggregate write and its outbox facts inside one transaction.

## Stack

- **Module**: `github.com/nguyen-duc-loc/vermouth/services/teaching`, Go 1.27
- **Database**: its own Postgres 18 instance, `TEACHING_DATABASE_URL` only
- **Publishes**: `teaching.events`, keyed by `class_id`, `session_id`, or `student_id`
* **Queries**: hand written in `db/queries/*.sql`, then generated through `sqlc` into
  `internal/store/sqlcgen/`

## Key files

| File | Owns |
|---|---|
| `cmd/teaching/main.go` | Startup, including `EnsureTopics` and the relay |
| `cmd/scheduleconflicts/main.go` | Read only report for active session overlaps before the guarded constraint migration |
| `internal/http/routes.go` | Routing only |
| `internal/handler/commands.go`, `schedule*.go`, `session_commands.go` | Teaching commands and reads, including immutable command replay and schedule state guards |
| `internal/handler/student_records.go`, `class_rosters.go`, `session_attendance.go` | Student resources, atomic dated roster deltas, and whole roster attendance passes |
| `internal/store/store.go`, `db/queries/*.sql` | The only database path and its hand written queries |
| `db/migrations/00002_teaching_model.sql` through `00007_student_records_class_rosters.sql` | Teaching entities, tenant references, the core loop, recurring rules, overlap constraints, and complete student and roster integrity |

## Commands

```bash
task schedule:conflicts   # report active overlaps in stable JSON without writing
```

## Conventions

- Event names, keys, and fields come from spec 0001's catalogue verbatim. Do not invent one here.
- `local_date` is computed once by this service, in the tutor's timezone, and carried on the event, so
  no consumer has to recompute it.
- The rate on a class is the current one here. Dated rate history belongs to `billing`, fed by
  `teaching.class.rate.changed`.
* Weekly rules are versioned by `classes.schedule_revision`, but every occurrence is a concrete
  session row. Billing and notifications still react only to concrete session facts.
* An active session has both `cancelled_at` and `superseded_at` empty. Tutor cancellation can be
  restored for the same identifier, while schedule replacement is terminal.
* Schedule and session commands store immutable response and context snapshots in
  `command_receipts`, so a retry returns the original result after later state changes.
* Student phone stays inside teaching and appears only on current owned student surfaces. Events,
  logs, conflicts, attendance, and historical roster responses never carry it.
* A roster delta locks one class and its students, while an attendance pass locks one class, one
  session, and its students. Each command writes its rows, receipt, and outbox facts atomically.

## Gotchas

- A key choice is not editable later: two events about the same `class_id` must share a partition to
  stay ordered (INV-6, STK-19). Take the key from the catalogue.
- `teaching.attendance.marked` carries `Present` or `Absent`, and only `Present` is billable. Deciding
  billability here would take work that belongs to `billing`.

## Agent skills

The repo wide skills in the root file all apply here. These are the ones that earn their keep in this area:

- [golang-database](../../.agents/skills/golang-database/): `samber/cc-skills-golang`, `pgx` transactions for the write plus outbox pair
- [go-goose](../../.agents/skills/go-goose/): `metalagman/agent-skills`, this service's migrations

## Related specs

- [0001 service boundaries and communication](../../docs/specs/0001-service-boundaries-and-communication/index.md) (the event catalogue)
- [0002 stack and scaffold](../../docs/specs/0002-stack-and-scaffold/index.md) (STK-11, STK-19)
* [0010 recurring sessions and exceptions](../../docs/specs/0010-recurring-sessions-exceptions/index.md) (weekly rules, concrete occurrences, exceptions, overlap guards, and calendar reads)
* [0011 student records and class rosters](../../docs/specs/0011-student-records-class-rosters/index.md) (student privacy, dated membership, and whole roster attendance)

_Drafted by $audit from the repo, worth a quick human pass. Edit freely: once a line stops matching this draft, later runs treat it as curated and will flag rather than overwrite it._
