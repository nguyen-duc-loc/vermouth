# services/teaching

## Overview

Owns what is taught: classes, schedules, sessions, students, roster membership, attendance, and the
current tuition rate on a class. It publishes most of the facts in the system, so it is the busiest
publisher and the one whose event changes ripple furthest. It must never own invoices, money
arithmetic, rate history for billing, or email sending, and it holds copies of nothing.

Only the skeleton exists today: the layers are in place with package comments, and feature 8 gives it
its first real behaviour.

## Stack

- **Module**: `github.com/nguyen-duc-loc/vermouth/services/teaching`, Go 1.27
- **Database**: its own Postgres 18 instance, `TEACHING_DATABASE_URL` only
- **Publishes**: `teaching.events`, keyed by `class_id`, `session_id`, or `student_id`
- No `sqlc.yaml` yet. It appears with the first query, next to `db/queries/`

## Key files

| File | Owns |
|---|---|
| `cmd/teaching/main.go` | Startup, including `EnsureTopics` and the relay |
| `internal/http/routes.go` | Routing only |
| `internal/handler/doc.go` | Empty until feature 8. The write and its outbox insert belong in one `pgx.Tx` |
| `internal/store/doc.go` | Empty until feature 4 decides the entities |
| `db/migrations/00001_vermouth_kit.sql` | The shared kit tables, copied from `pkg/vermouth/ddl` |

## Conventions

- Event names, keys, and fields come from spec 0001's catalogue verbatim. Do not invent one here.
- `local_date` is computed once by this service, in the tutor's timezone, and carried on the event, so
  no consumer has to recompute it.
- The rate on a class is the current one here. Dated rate history belongs to `billing`, fed by
  `teaching.class.rate.changed`.

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

_Drafted by /audit from the repo, worth a quick human pass. Edit freely: once a line stops matching this draft, later runs treat it as curated and will flag rather than overwrite it._
