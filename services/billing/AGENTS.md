# services/billing

## Overview

Owns money: dated rate history, the billable session projection, the tutor's invoice profile and bank
details, invoices, invoice lines, invoice numbers, voids, paid state, and the rendered invoice PDF. It
must never own attendance truth, the schedule, student management, or email sending. This is the one
service where the difference between a projection and an authoritative record decides correctness.

Only the skeleton exists today. Feature 4 decides the entities, features 13 to 15 build the behaviour.

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
| `internal/consumer/doc.go` | Empty until feature 4. It will be the only writer of the projections |
| `internal/store/doc.go`, `internal/handler/doc.go` | Empty until the entities exist |
| `db/migrations/00001_vermouth_kit.sql` | The shared kit tables, copied from `pkg/vermouth/ddl` |

## Conventions

- Money is `int64` dong in Go and `bigint` in the database. Never a float, never a decimal string
  inside Go, and formatted only in the PDF and the browser.
- Rate history is append only. A `teaching.class.rate.changed` event adds a row, never updates one.
- A student label is marked inactive, never deleted, because a past invoice must still print the name.
- The month end run is synchronous inside this service: every input is already local, so it is a local
  read rather than a call to anyone.

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

_Drafted by /audit from the repo, worth a quick human pass. Edit freely: once a line stops matching this draft, later runs treat it as curated and will flag rather than overwrite it._
