# pkg/vermouth (the shared module)

## Overview

Every piece of cross service machinery lives here once and is imported by the four services and the
gateway (STK-2). Spec 0001 forbids service to service calls, so an outbox, a relay, an idempotent
consumer, bounded retry, and a dead letter park are unavoidable, and writing them four times is what
this module exists to prevent. It carries no independent version: it moves in lockstep with the
services importing it (STK-24), which is the reason for one repository.

## Stack

- **Module**: `github.com/nguyen-duc-loc/vermouth/pkg/vermouth`, Go 1.27
- **Key dependencies**: `pgx` v5, `franz-go` (plus `kadm` for offset resets), `golang-jwt/jwt/v5`, `google/uuid`
- No `sqlc` here. This module's own queries use `pgx` directly, the one exception STK-3 names.

## Key files

| File | Owns |
|---|---|
| `envelope.go` | The event envelope and its constructor, which takes the partition `key` as a required typed argument (STK-19) |
| `catalogue.go` | Spec 0001's topic and event names as constants, so one name means one value everywhere |
| `outbox.go` | The outbox writer, called inside the caller's `pgx.Tx` next to the business write (STK-4) |
| `relay.go` | The relay goroutine: a ticker, `SELECT ... FOR UPDATE SKIP LOCKED`, publish and mark one row at a time (STK-18) |
| `consumer.go` | The consumer loop, the `handled_events` idempotency check, version gating (STK-20), bounded retry, and the `.dlq` park (STK-13) |
| `broker.go` | Producer and consumer construction, plus `EnsureTopics` with 3 partitions and `retention.ms=-1` (STK-16, STK-17) |
| `config.go` | The typed configuration loaded once from the environment, `MissingEnvError` when a required variable is absent (STK-8) |
| `token.go` | Ed25519 verification against the public keys held in configuration, keyed by `kid`, never over the network (STK-14) |
| `db.go`, `serve.go`, `health.go`, `logging.go`, `errors.go` | The pool, the HTTP server lifecycle, health and readiness, `log/slog` in JSON with `request_id`, and the one `APIError` shape |
| `ddl/00001_vermouth_kit.sql` | The canonical DDL for `outbox` and `handled_events`, copied into each service by `task migrate:sync-kit` |
| `cmd/devkeys`, `cmd/replay` | The development key pair generator, and the replay tool behind `task replay:<service>:<consumer>` |

## Conventions

- A change here lands in four services at once. Read the callers before changing a signature.
- The DDL is canonical here and copied outward. Edit `ddl/00001_vermouth_kit.sql`, then run
  `task migrate:sync-kit`, never a service's copy directly.
- Consumer group names are `<consuming service>.<consumer name>` and match the `consumer_name` in
  `handled_events` (STK-12), so a reset and a row delete are one visible pair.
- Every event gets one test asserting its key is the field spec 0001's catalogue names for it (STK-19).

## Gotchas

- Partition count is fixed at creation. Changing it remaps keys to partitions and breaks per key
  ordering (INV-6) for every key whose events span the change.
- A replay rebuilds projections only. Authoritative records (invoices, numbers, voids, paid state)
  are never rewritten by one.
- Redpanda has no native dead letter path, so the retry budget, the failure reason, and the park are
  code here rather than broker configuration. Budget and delay are configuration, never literals.
- `LISTEN/NOTIFY` may later shorten the relay wait on top of the poll, never replace it: a dropped
  connection loses a notification while the outbox row survives (STK-18).

## Agent skills

The repo wide skills in the root file all apply here. These are the ones that earn their keep in this area:

- [kafka-development](../../.agents/skills/kafka-development/): `mindrally/skills`, producer, consumer group, and partitioning practice
- [golang-database](../../.agents/skills/golang-database/): `samber/cc-skills-golang`, `pgx` pools and transaction patterns
- [golang-concurrency](../../.agents/skills/golang-concurrency/): `samber/cc-skills-golang`, the relay and consumer goroutines
- [golang-testing](../../.agents/skills/golang-testing/): `samber/cc-skills-golang`, table tests, as `envelope_test.go` does

## Related specs

- [0001 service boundaries and communication](../../docs/specs/0001-service-boundaries-and-communication/index.md) (INV-1 to INV-15)
- [0002 stack and scaffold](../../docs/specs/0002-stack-and-scaffold/index.md) (STK-2, STK-13, STK-16 to STK-20, STK-24)

_Drafted by $audit from the repo, worth a quick human pass. Edit freely: once a line stops matching this draft, later runs treat it as curated and will flag rather than overwrite it._
