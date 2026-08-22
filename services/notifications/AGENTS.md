# services/notifications

## Overview

Owns delivery: sending email, digest run records, alert delivery. It owns no business truth, so it
never generates a schedule or decides invoice content. Everything it reads about tutors, classes, and
today's sessions is a projection kept current by its consumers and rebuildable by a replay.

## Stack

- **Module**: `github.com/nguyen-duc-loc/vermouth/services/notifications`, Go 1.27
- **Database**: its own Postgres 18 instance, `NOTIFICATIONS_DATABASE_URL` only
- **Key dependencies**: `pgx` v5 with `sqlc`, `franz-go` through the shared module
- **Consumes**: `identity.events` today (the `recipients` consumer); teaching facts follow later
- **Scheduler**: a `time.Ticker` inside the single replica, with a unique constraint as the lock

## Key files

| File | Owns |
|---|---|
| `cmd/notifications/main.go` | Startup, plus starting the consumer alongside the server |
| `internal/consumer/recipients.go` | The recipient projection, and the only writer of it. `RecipientsConsumerName` fixes the group name |
| `internal/http/routes.go` | Routing only |
| `internal/store/`, `internal/store/sqlcgen/` | This service's database, generated queries included |
| `db/queries/recipients.sql` | Hand written SQL for the projection |

## Commands

```bash
task replay:notifications:recipients   # stop, reset the group, delete handled_events rows, restart
task svc:start -- notifications
task logs -- notifications
```

## Conventions

- `internal/consumer` is the only writer of a projection table. A handler never writes one.
- A consumer reads tolerantly: it takes only the fields it needs, so a field added later is ignored
  rather than demanded (INV-12).
- The consumer name in code, the group name, and the `consumer_name` in `handled_events` are the same
  string (STK-12), which is what makes one replay target correct.

## Gotchas

- Exactly one replica while the scheduler is an in process ticker (STK-23). The unique constraint on
  `(tutor_id, local_date)` makes a repeated tick harmless but cannot serialise two replicas, so a
  second one means two digests on the same morning. Feature 5 owns the advisory lock that fixes this.
- A replay is only correct as the pair: reset the offsets and delete that consumer's
  `handled_events` rows. Either half alone is a silent no operation.

## Agent skills

The repo wide skills in the root file all apply here. These are the ones that earn their keep in this area:

- [kafka-development](../../.agents/skills/kafka-development/): `mindrally/skills`, consumer groups and offset handling
- [golang-context](../../.agents/skills/golang-context/): `samber/cc-skills-golang`, cancellation through the consumer loop
- [go-goose](../../.agents/skills/go-goose/): `metalagman/agent-skills`, this service's migrations

## Related specs

- [0001 service boundaries and communication](../../docs/specs/0001-service-boundaries-and-communication/index.md) (flow 2, the daily digest)
- [0002 stack and scaffold](../../docs/specs/0002-stack-and-scaffold/index.md) (STK-12, STK-22, STK-23)

_Drafted by /audit from the repo, worth a quick human pass. Edit freely: once a line stops matching this draft, later runs treat it as curated and will flag rather than overwrite it._
