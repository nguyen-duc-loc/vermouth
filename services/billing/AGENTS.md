# services/billing

## Overview

Owns money: dated rate history, the billable session projection, the tutor's invoice profile and bank
details, invoices, invoice lines, invoice numbers, voids, paid state, and the rendered invoice PDF. It
must never own attendance truth, the schedule, student management, or email sending. This is the one
service where the difference between a projection and an authoritative record decides correctness.

The teaching projection path, versioned invoice profile editor, and committed bank catalog exist.
Features 13 to 15 build the month end calculation, invoice records, PDF rendering, sharing, and paid
state.

## Stack

- **Module**: `github.com/nguyen-duc-loc/vermouth/services/billing`, Go 1.27
- **Database**: its own Postgres 18 instance, `BILLING_DATABASE_URL` only
- **Object storage**: Garage over the S3 API, with the credentials held by this service alone
- **Publishes**: `billing.events`. **Consumes**: `identity.events` and `teaching.events`

## Key files

| File | Owns |
|---|---|
| `cmd/billing/main.go` | Startup, including `EnsureTopics` and the relay |
| `internal/http/routes.go` | Routing only |
| `internal/http/profile_routes.go` | Private profile and committed bank catalog endpoints |
| `internal/consumer/identity.go` | Idempotent empty profile seeding from tutor registration facts |
| `internal/consumer/teaching.go` | The only writer of teaching projections, including students, roster periods, attendance, and session state |
| `internal/store/store.go`, `internal/handler/projection.go`, `internal/handler/profile.go`, `internal/handler/banks.go` | Typed database access, projections, invoice profile rules, and the stable bank catalog |
| `db/migrations/00001_vermouth_kit.sql` through `00004_tutor_profile_bank_details.sql` | Shared machinery, billing entities, integrity constraints, and versioned bank details |

## Conventions

- Money is `int64` dong in Go and `bigint` in the database. Never a float, never a decimal string
  inside Go, and formatted only in the PDF and the browser.
- Rate history is append only. A `teaching.class.rate.changed` event adds a row, never updates one.
- A student label is marked inactive, never deleted, because a past invoice must still print the name.
- The month end run is synchronous inside this service: every input is already local, so it is a local
  read rather than a call to anyone.
* A scheduled or moved session upsert clears `cancelled_at`, so restoring the same session identifier
  reactivates its projection. A cancelled fact sets it inactive.
* Invoice profile saves normalize and validate the whole resource before revision guarded updates.
  An identical valid retry succeeds without increasing the revision.
* Billing derives `bank_name` from the committed catalog. Requests supply only `bank_code`, and a
  retired saved code remains readable until the tutor selects an active replacement.

## Gotchas

- **Projections versus authoritative records.** Sessions, attendance, roster periods, class labels,
  student labels, and rate history are projections and a replay rebuilds them. `billing_run` rows,
  invoices, invoice numbers, voids, and paid state are authoritative records: no replay may rewrite
  them, and nothing outside this service may produce them.
- An already issued invoice keeps the values it was rendered with. A later label or rate change does
  not reach backwards into it.
- The invoice PDF belongs here, not to the future `documents` service, because the PDF is the rendered
  form of an invoice.

## Agent skills

The repo wide skills in the root file all apply here. These are the ones that earn their keep in this area:

- [golang-database](../../.agents/skills/golang-database/): `samber/cc-skills-golang`, transactions across the run and its writes
- [kafka-development](../../.agents/skills/kafka-development/): `mindrally/skills`, consuming two topics idempotently
- [go-goose](../../.agents/skills/go-goose/): `metalagman/agent-skills`, this service's migrations

## Related specs

- [0001 service boundaries and communication](../../docs/specs/0001-service-boundaries-and-communication/index.md) (flow 1, month end invoice generation)
- [0002 stack and scaffold](../../docs/specs/0002-stack-and-scaffold/index.md) (STK-7, and the replay rule under conventions)
* [0010 recurring sessions and exceptions](../../docs/specs/0010-recurring-sessions-exceptions/index.md) (session move, cancellation, restoration, and invoice immutability)
* [0011 student records and class rosters](../../docs/specs/0011-student-records-class-rosters/index.md) (student labels, dated membership, attendance, and issued invoice immutability)
* [0012 tutor profile and bank details](../../docs/specs/0012-tutor-profile-bank-details/index.md) (versioned profile saves, bank catalog, privacy, and replay safe seeding)

_Drafted by $audit from the repo, worth a quick human pass. Edit freely: once a line stops matching this draft, later runs treat it as curated and will flag rather than overwrite it._
