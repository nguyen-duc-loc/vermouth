# test

## Overview

The one infra stack, plus the scripts that run the skeleton. `compose.test.yaml` is started once and
serves both local development and the integration tests, because the failures worth catching (a
consumer that is not idempotent, a relay publishing twice, a projection rebuilt wrong) only appear
against a real broker and a real database (STK-15), and one stack started once is what 2 vCPU can
carry. The three shell scripts are what `Taskfile.yml` calls to start a service, stop it, and prove
the end to end thread.

## Key files

| File | Owns |
|---|---|
| `compose.test.yaml` | Four Postgres 18 instances and one Redpanda node, tuned small, under the compose project name `vermouth` |
| `start-service.sh` | Start one binary from `bin/`, record its pid under `.tmp/run`, and confirm it stayed up |
| `stop-service.sh` | Stop one service and wait until it is really gone, forcing it after 20 seconds |
| `thread.sh` | Register a tutor through the gateway, then poll `/api/thread` until `notifications` has recorded the event |
| `e2e/` | Empty. Playwright for the money path lands here when `/test` sets the runners up |

## Commands

```bash
task infra:up            # the four Postgres instances and Redpanda, waiting for health
task infra:ps            # what is running
task infra:logs          # follow the infra logs
task infra:down          # stop it, keeping the data
task infra:clean         # stop it and delete the data (it asks first)
task start               # every service in the background, logs under .tmp/logs
task svc:start -- identity
task svc:stop -- identity
task thread              # drive the end to end thread and watch it complete
```

Where things listen, all of it also in `.env.example`: Postgres on 5433 identity, 5434 teaching,
5435 billing, 5436 notifications. Redpanda on 19092 for the Kafka API and 19644 for its admin API.
The gateway on 8080, then the services on 8081 to 8084.

## Conventions

- One stack, started once. A test runs against `task infra:up` rather than starting containers of
  its own, which is what keeps the memory footprint predictable.
- Every port, role, and password here has a matching line in `.env.example`. Change the pair
  together, or a service will point at a database that is not there.
- The scripts take their paths as arguments (`.tmp/run`, `.tmp/logs`) instead of reading `.env`, so
  `Taskfile.yml` stays the only place that knows the layout.
- Redpanda creates no topic by itself (`auto_create_topics_enabled=false`), so `EnsureTopics` in each
  `main.go` owns creation with the right partition count and retention, and a forgotten call fails
  loudly at startup (STK-16, STK-17).

## Gotchas

- `task infra:clean` deletes every database and the whole event log. It asks first, and nothing
  brings the data back.
- `--set` and its value must stay two separate arguments in the Redpanda command. Written as
  `--set=key=value`, rpk passes it straight through, the binary rejects it, and the container
  restarts in a loop.
- Postgres 18 wants a single mount at `/var/lib/postgresql`, which then holds the data in a major
  version subdirectory. The older `/var/lib/postgresql/data` mount path does not work here.
- `start-service.sh` refuses a second copy of a service even when the pid file was lost: two members
  in one consumer group park the group in a rebalance, and `notifications` has to stay at exactly one
  replica (STK-23).
- `stop-service.sh` waits for the process to really exit rather than firing and forgetting, because a
  replay must not start while the consumer still holds its group (STK-22).
- Garage is not in this stack yet, so nothing here serves invoice PDFs. Feature 5 owns the cluster
  version of all of this, Garage included.

## Related specs

- [0002 stack and scaffold](../docs/specs/0002-stack-and-scaffold/index.md) (STK-15 to STK-17, STK-22, STK-23, and the scaffold target the thread proves)
- [0001 service boundaries and communication](../docs/specs/0001-service-boundaries-and-communication/index.md) (the thread `thread.sh` drives, browser to gateway to service to broker to consumer)

_Drafted by /audit from the repo, worth a quick human pass. Edit freely: once a line stops matching this draft, later runs treat it as curated and will flag rather than overwrite it._
