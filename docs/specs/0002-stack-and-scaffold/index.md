# 0002. Go services on Postgres and Redpanda, in one repository

**Date**: 2026-08-22
**Status**: Accepted

## Summary

Vermouth is built in Go for the four services and the gateway, in TypeScript for the web app, on four separate Postgres databases, with Redpanda as the message broker (a Kafka compatible log that carries facts between services). Everything lives in one repository, so the outbox, the relay, and the idempotent consumer are written once in a shared Go module and used four times instead of four times over. The choice is driven by two forces: spec 0001 forbids service to service calls, so this boilerplate is unavoidable, and the machine has about 5 GiB of memory free that must later also hold a Kubernetes control plane, so footprint decides several ties. The one real cost is that Redpanda has no built in dead letter path, so bounded retry and parking a bad message are code in the shared module rather than configuration.

## Requirements

This is a decision spec. It carries no build tasks and no acceptance criteria of its own: `$develop stack & scaffold` derives the scaffold steps from the stack below, and the scope's "Done when" for feature 2 is the contract it is checked against (every tool choice recorded with a reason, and the skeleton building and starting locally with at least one service answering a health check through the gateway).

What it carries instead is the set of rules every later feature obeys, in the same spirit as spec 0001's invariants. Where an `INV-n` is named, that rule comes from [spec 0001](../0001-service-boundaries-and-communication/index.md) and this one only says how it is met.

- **STK-1**: Go is the only language for the four services and the gateway. TypeScript appears only in `web/`. No third runtime enters the system without superseding this spec.
- **STK-2**: Every piece of cross service machinery lives once in the shared module `pkg/vermouth` (event envelope, outbox writer, relay, handled events store, retry and dead letter, config loading, logging, token verification, health and readiness). A service never keeps its own copy of any of it.
- **STK-3**: SQL is written by hand in `db/queries/` and compiled to typed Go by `sqlc`. No ORM, and no SQL assembled by string concatenation at runtime. One exception, because STK-2 outranks it: the shared module's own tables (`outbox`, `handled_events`) keep their canonical DDL in `pkg/vermouth`, copied into each service's `db/migrations/` by a task target so the four cannot drift, and the shared module's own queries against them use `pgx` directly rather than a per service `sqlc` package.
- **STK-4**: The business write and its outbox insert happen in the same `pgx.Tx`, visible in the same function (INV-3). No code path publishes to the broker directly from a handler.
- **STK-5**: Each service owns its own `db/migrations/` directory, its own database URL, and its own database role. No migration and no query reaches another service's database (INV-2).
- **STK-6**: Every Go binary builds with `CGO_ENABLED=0` and ships `FROM scratch`. This rules out any library needing cgo, including the librdkafka based Kafka client.
- **STK-7**: Money is an `int64` count of dong. Never a float, never a decimal string inside Go. Formatting happens only at the edges, in the PDF and in the browser.
- **STK-8**: Configuration arrives from the environment only, parsed once at startup into a typed struct per service. A missing or unparseable required variable stops startup with a named error, rather than defaulting silently.
- **STK-9**: Logging is `log/slog` in JSON to stdout. Every line carries `service` and the `request_id` from `X-Request-Id` (INV-15).
- **STK-10**: `api/openapi.yaml` is the single description of the gateway surface. The gateway's request and response types come from it through `oapi-codegen`, and the browser's types come from it through `openapi-typescript`. A hand written client type for a gateway endpoint is not allowed.
- **STK-11**: One topic per publishing service (`identity.events`, `teaching.events`, `billing.events`), partitioned by the envelope `key`, with the event name inside the envelope. This is what makes per key ordering real (INV-6): two events about the same `class_id` land on the same partition in the order they were published, whatever their names. Five kinds of key (`tutor_id`, `class_id`, `session_id`, `student_id`, `invoice_id`) share those three topics, which is safe only because INV-7 stores facts and derives at read time, so no consumer depends on order across two different keys.
- **STK-12**: A consumer group is named `<consuming service>.<consumer name>` and matches the `consumer_name` in its `handled_events` table (INV-5), so a reset and replay is one visible pair.
- **STK-13**: A failing message is retried in process a bounded number of times with growing delay, then published to `<topic>.dlq` with the failure reason and the original envelope, and the offset moves on (INV-13). Retry budget and delay are configuration, not literals in a handler.
- **STK-14**: Token verification uses an Ed25519 public key the service already holds in configuration, keyed by `kid` (INV-14). No verification path performs a network call.
- **STK-15**: Integration tests for a relay, a consumer, a projection, or a month end run go against the real Postgres and the real Redpanda in `test/compose.test.yaml`. A mocked broker or database is not an acceptable substitute for those.
- **STK-16**: Topic creation belongs to `EnsureTopics` in `pkg/vermouth`, which every `main.go` calls at startup before it starts its relay or its consumers. A service creates its own publish topic plus that topic's `.dlq`, with `retention.ms=-1` (INV-11). Redpanda runs with automatic topic creation turned off, so a forgotten call fails loudly at startup instead of quietly producing a topic with default retention.
- **STK-17**: Every topic and every `.dlq` topic is created with 3 partitions, fixed at creation. Partition count is the least changeable property a topic has, because changing it remaps keys to partitions and breaks per key ordering (INV-6) for every key whose events span the change. 3 rather than 1 is deliberate: a single partition orders every event globally, so a handler that wrongly assumed order across keys would pass on this machine and fail only somewhere never tested.
- **STK-18**: The relay is one goroutine inside each service binary, not a separate process. It wakes on a ticker every 500ms to 1s, claims unsent outbox rows with `SELECT ... FOR UPDATE SKIP LOCKED` ordered by creation, and publishes and marks one row at a time rather than acknowledging a batch. `LISTEN/NOTIFY` may later shorten the wait on top of the poll, never replace it, because a dropped connection loses a notification while the outbox row survives (see `## Follow-up`).
- **STK-19**: The envelope constructor in `pkg/vermouth` takes `key` as a required typed argument, so the partition key is chosen at the call site next to the business write rather than derived somewhere later. Every event has one test asserting its key is the field spec 0001's catalogue names for it.
- **STK-20**: A consumer declares the `event_version` values it accepts, and the shared decode function checks that set before it decodes any field. An unrecognised version parks the message (INV-12); an unrecognised field is ignored. Those two cases are decided in one place rather than per handler.
- **STK-21**: Migrations run only through `task migrate:up`, before services start, never from inside `main.go`. Two replicas starting together would otherwise race `goose`, and feature 5 turns this target into an init container unchanged.
- **STK-22**: A replay is one task target, `task replay:<service>:<consumer>`, which stops the consumer, resets its group to the earliest offset through `franz-go`'s `kadm` package, deletes that consumer's `handled_events` rows in the same invocation, then starts it again. Either half alone is a silent no operation, which is why this is a target and not a runbook.
- **STK-23**: `notifications` runs as exactly one replica while its scheduler is an in process ticker. The unique constraint on `(tutor_id, local_date)` makes the digest idempotent but cannot serialise two replicas ticking together, so a second replica needs a Postgres advisory lock around the tick first (feature 5 owns that).
- **STK-24**: `go.work` is development tooling only. Every service image builds with the repository root as its Docker build context, because `pkg/vermouth` resolves through the workspace rather than through a published version. The shared module carries no independent version: it moves in lockstep with the services importing it, which is the reason for one repository.

## Decision

**Chosen option**: Option 1: Go services and gateway, four Postgres instances, Redpanda, one repository with a shared module

Build the four services and the gateway as static Go binaries over `net/http`, each with its own Postgres instance reached through `pgx` and `sqlc`, communicating only through Redpanda using `franz-go`, with a React and Vite web app talking to the gateway through types generated from one OpenAPI document, all in a single repository whose shared Go module carries the outbox, relay, idempotency, retry, and dead letter machinery once (basis: spec 0001's capability table, and the measured footprint of this machine).

**Implementation skills**: none installed. No technology community skills exist in this project yet; see `## Follow-up`.

## Proposed stack

Every row is a choice with its reason. Layers this product does not need yet (cache, search engine, metrics store) are absent on purpose and named in `## Follow-up`.

| Layer | Choice | Reason |
|---|---|---|
| Language, services and gateway | Go 1.27 (installed) | Static binaries in the tens of megabytes leave room for a Kubernetes control plane in the same 5 GiB; the outbox and consumer code is explicit rather than hidden in a framework, which is the thing this project exists to learn |
| Language, web app | TypeScript on Node 24.19 (installed) | The only sensible browser language, and the gateway contract crosses the boundary as generated types (STK-10) |
| HTTP layer | Standard library `net/http` with `ServeMux` pattern routing | Go 1.27 routes `GET /classes/{id}` natively, so a framework would hide exactly the request path being studied; the gateway's parallel read fan out is `errgroup` plus goroutines |
| Data access | `pgx` v5 with `sqlc` generated queries | Real SQL, checked at build time; the outbox insert sits in the same visible transaction as the business write (STK-4), which an ORM would bury |
| Migrations | `goose`, one directory per service, run by `task migrate:up` before services start (STK-21) | Four separate migration sets make one database per service physical rather than aspirational (INV-2), and running them outside `main.go` means two replicas can never race each other |
| Primary database | Postgres 18, four instances, one per service | Restarting one service's storage must not take the others down, since proving that month end billing runs while `teaching` is restarting is the point of spec 0001 |
| Broker | Redpanda, single node, tuned small | Kafka API compatible so the partition key is INV-6 and offset reset is INV-11 exactly, with no JVM and no ZooKeeper, which is what makes it fit on 2 vCPU |
| Broker client | `franz-go` | Pure Go, so `CGO_ENABLED=0` static binaries hold (STK-6); complete consumer group and offset reset support, and Redpanda's own examples use it |
| Object storage | Garage, reached through the S3 API with `aws-sdk-go-v2` | Invoice PDFs need S3 semantics, not a database blob; MinIO's community edition lost its administration console in 2026 and pushes users to a paid product, and coding against the S3 API keeps a move to cloud object storage a configuration change |
| Gateway implementation | A Go binary written in this repository, not a proxy product | No off the shelf gateway (APISIX, Kong, Traefik, Envoy) performs the read fan out across two services that spec 0001 requires; routing and TLS at the cluster edge belong to feature 5 |
| Token issuing and verification | Ed25519 (`EdDSA`) tokens minted by `identity` with `golang-jwt/jwt/v5`, verified locally against a JWKS document supplied in configuration and keyed by `kid` | Asymmetric keys mean a verifying service cannot mint a token, and configuration as the source keeps verification working with `identity` down (INV-14). `identity` also serves `/.well-known/jwks.json`, used by a rotation step, never on a request path |
| Tutor authentication | Google OAuth 2.0 Authorization Code with PKCE, run server side by `identity` through `golang.org/x/oauth2` and `google.golang.org/api/idtoken` | Corrected by spec [0004](../0004-tutor-sign-in-google-oauth/index.md). This row read `argon2id` through `golang.org/x/crypto/argon2` until 0004 removed passwords altogether, so there is no hash to store and Google carries strength, breach checks, two factor, and recovery. The token rows above are unchanged |
| Scheduler | A `time.Ticker` loop inside a single replica of `notifications`, ticking every 15 minutes, with the unique constraint on `(tutor_id, local_date)` as the lock (STK-23) | Spec 0001 asks for an in process scheduler with a database lock rather than a cluster cron, so the 6:00 digest stays testable locally; a constraint is a stronger lock than a lease |
| Identifiers | UUIDv7, generated in Go with `google/uuid` | Time ordered so indexes stay healthy, and generated in the application so an `event_id` can be written into the outbox row inside the transaction that produced it |
| Web app | React 19 with Vite, TanStack Router, TanStack Query v5 | Static files, so nothing is added to the runtime memory budget and no second backend blurs INV-10; TanStack Query's cache invalidation is how eventually consistent reads are made honest on screen rather than confusing |
| Styling and components | Tailwind CSS v4 with `shadcn/ui` | Phone first sizing is where utility classes earn their place, and the accessible primitives (dialog, select, date picker) arrive as code in the repository rather than as a dependency to fight |
| API contract | `api/openapi.yaml`, with `oapi-codegen` for Go types and `openapi-fetch` plus `openapi-typescript` for the browser | One document is authoritative on both sides, it doubles as the gateway's documentation, and it describes error shapes and status codes properly. Request validation at the gateway reads the same document |
| Configuration | Environment variables only, with `.env.example` committed and `.env` ignored | These same variables become a ConfigMap and a Secret in feature 5, so no code changes when the system moves onto the cluster |
| Logging | `log/slog`, JSON, stdout | Standard library, no dependency, and the `request_id` on every line is what makes feature 9 possible across the broker |
| Testing, Go | `testing` with `testify/require`, against one shared `test/compose.test.yaml` stack (four Postgres, one Redpanda) | The interesting failures (a consumer that is not idempotent, a relay publishing twice, a projection rebuilt wrong) only appear against a real broker and a real database; one stack started once is what 2 vCPU can carry |
| Testing, web | Vitest for units, Playwright for the money path end to end | The standard pairing for a Vite project, and the money loop is the flow that must never silently break |
| Repository layout | One repository, `go.work` over the service modules plus the shared module, `pnpm-workspace.yaml` for the web app | A fix to the outbox kit lands once instead of four times, and one commit can change an event and both its consumers together |
| Task runner | `Taskfile.yml` (go-task), installed with `go install` | One command style over both halves of the repository, itself a Go binary, so no new ecosystem enters |
| Package manager, web | pnpm 11.22.0 (installed), pinned by the `packageManager` field | Already chosen and installed; the pinned field means corepack keeps every later checkout on the same version |
| Containers | `FROM scratch` images from `CGO_ENABLED=0` builds | Images in the tens of megabytes on a disk with 23 GB free, and the payoff for choosing Go and a pure Go Kafka client |
| Localisation | `react-i18next` with plain JSON message files, Vietnamese as default | Widest ecosystem, and it handles the plural and date cases the invoice needs (feature 20 owns the full pass) |
| Observability | `log/slog` structured logs now; a trace collector deferred to feature 9 | A collector plus a trace store does not fit the current memory budget, and feature 9 exists precisely to decide that |

### How the stack meets spec 0001's capability table

Spec 0001 named required properties rather than products. Each is met as follows, and a row failing here means this spec, not that one, is wrong.

| Required property (spec 0001) | Met by | Residual gap |
|---|---|---|
| Ordering per key | One topic per publishing service, partitioned by the envelope `key` (STK-11), 3 partitions fixed at creation (STK-17) | None, as long as the partition count is never changed afterwards |
| Independent consumer groups per service | Redpanda consumer groups named `<service>.<consumer>` (STK-12) | None |
| Retention long enough to rebuild a projection from zero | Topics created by `EnsureTopics` with `retention.ms=-1` and automatic creation off (STK-16), so nothing is deleted by age at this data volume | Disk is finite; revisit if the log outgrows the 23 GB free |
| Consumer offset reset | `task replay:<service>:<consumer>`: stop the consumer, reset the group to the earliest offset with `kadm`, delete that consumer's `handled_events` rows, restart (STK-22) | A replay rebuilds projections only. `billing_run` rows, invoices, numbers, voids, and paid state are authoritative records and no replay may rewrite them |
| Dead letter destination with replay | `<topic>.dlq` topics created alongside their topic (STK-16), plus the bounded retry and park code in `pkg/vermouth` (STK-13) | Written by hand, because Redpanda has no native dead letter. This is the largest single cost of the broker choice |
| One relational database per service with transactions strong enough for an outbox | Four Postgres 18 instances, `pgx` transactions, unique constraints for idempotency | None |
| Gateway verifies a signed token, creates and forwards `X-Request-Id`, fans out reads in parallel | The Go gateway binary: `golang-jwt` verification, request id middleware, `errgroup` fan out | None |
| Object storage for invoice PDFs, reachable only through the owning service | Garage over the S3 API, credentials held only by `billing` | Feature 5 must actually run it; feature 14 consumes it |
| In process scheduler with a database lock | `time.Ticker` in `notifications` plus the unique digest constraint | Holds only at one replica (STK-23). The constraint makes a repeated tick harmless but cannot serialise two replicas, which needs an advisory lock |
| A language making outbox, relay, and idempotent consumer cheap four times | Go, with the machinery written once in `pkg/vermouth` and imported by four services | Go has no maintained outbox library, so the first write of it is real work |

### Conventions this stack fixes

These are settled here so feature 3 records them and feature 4 does not reopen them. Feature 4 still owns every entity and field.

- Identifiers are UUIDv7, generated in Go, stored as `uuid`.
- Timestamps are `timestamptz` stored in UTC; a `local_date` is a `date`, computed once by `teaching` as spec 0001 requires.
- Money is `bigint` in the database and `int64` in Go, counting dong (STK-7).
- Event names, envelope fields, and topic names use the exact names in spec 0001's catalogue, so one name means one value everywhere.
- The Go module path is `github.com/OWNER/vermouth`, with `OWNER` replaced at scaffold time (see `## Follow-up`).
- A replay rebuilds projections and nothing else. `billing`'s copies of sessions, attendance, roster periods, class labels, student labels, and rate history are rebuilt from events; its `billing_run` rows, invoices, invoice numbers, voids, and paid state are authoritative records rather than projections, and no replay rewrites them (spec 0001, flow 1).

### The scaffold target

The ordered steps belong to `$develop stack & scaffold`, not here. What this spec fixes is the shape they arrive at, and the single thread the skeleton must demonstrate, in the project's Tracer Bullet spirit: a request from the browser, through the gateway, into one service, out of its outbox, through the relay into Redpanda, and consumed by a second service that records it. Health checks alone would prove the tools resolve; this thread proves the architecture does, and it is what feature 8 then thickens into real behaviour.

```
vermouth/
  Taskfile.yml            one entry point over both halves
  go.work                 the Go modules below
  pnpm-workspace.yaml     the web app
  api/openapi.yaml        the gateway contract, authoritative (STK-10)
  pkg/vermouth/           shared module: envelope, outbox, relay, handled events,
                          retry and dlq, config, slog, token verify, health
  gateway/                cmd/gateway, internal/{route,aggregate,auth}
  services/
    identity/  teaching/  billing/  notifications/
      cmd/<service>/main.go
      internal/{http,handler,store,consumer}/
      db/migrations/      goose, this service only (STK-5)
      db/queries/         sqlc input
      sqlc.yaml
      go.mod
  web/                    React 19, Vite, TanStack, Tailwind v4
  test/
    compose.test.yaml     four Postgres plus one Redpanda (STK-15)
    e2e/                  Playwright
  deploy/                 empty here; feature 5 owns its contents
```

**Where the shared module resolves from.** The five service modules and `pkg/vermouth` sit under one `go.work`, and that workspace is development tooling only (STK-24). A service's `Dockerfile` therefore builds with the repository root as its context and copies both its own module and `pkg/vermouth`, because the import cannot resolve from the service directory alone. This is the first thing that breaks a `FROM scratch` build, so it is fixed here rather than discovered.

### The memory budget this stack has to live inside

Measured on this machine today: 2 vCPU, 7.8 GiB total memory with 5.0 GiB available, 23 GB free disk. The figures below are rough estimates, not measurements, and they exist because feature 5 has to fit a Kubernetes control plane into the same space.

| Component | Rough estimate |
|---|---|
| Four Postgres instances | 600 MB to 1 GiB |
| Redpanda, one node, tuned | 1 GiB to 1.5 GiB |
| Five Go binaries | 200 MB to 400 MB |
| Garage | under 100 MB |
| Vite dev server | 300 MB to 600 MB |
| Kubernetes control plane (feature 5) | 800 MB to 1.5 GiB |
| **Total** | **about 3.0 GiB at the low end, about 5.1 GiB at the high end** |

The arithmetic matters more than either figure. Against 5.0 GiB available, the high end does not fit, so the fallback named below is the expected path at the high end rather than a remote edge case. Two things move the number. The Vite dev server line leaves the budget entirely once feature 5 serves the web app as built static files, which is where most of the real headroom comes from. And Redpanda is the component that must be told to be small: a single node in development mode, with `--smp 1`, `--overprovisioned`, an explicit `--memory` ceiling around 1 GiB, and `--reserve-memory 0M`. Its defaults assume a dedicated machine and will take far more. If the total still does not fit once feature 5 lands, the named fallback is collapsing to one Postgres instance holding four databases with one role each, which keeps INV-2 in substance and costs one set of memory instead of four. That fallback is a change to this spec, not a decision for a build to make quietly.

## Consequences

**Positive**:
- The lightest combination available for this architecture, which is what leaves room for feature 5 on the same machine.
- The outbox, relay, idempotency, retry, and dead letter code is written once and reviewed once, then used by four services.
- A wrong column name or a changed query shape fails the build, because `sqlc` generates from real SQL against the real schema.
- Static binaries and `FROM scratch` images make deployment in features 5 and 16 close to trivial, with no runtime to install.
- One repository means an event and both its consumers change in one commit, which is the failure mode most likely to bite a solo builder across four services.
- The web app is static files, so INV-10 stays structurally true: there is no second backend that could quietly read another service's data.

**Negative / tradeoffs**:
- Go has no maintained transactional outbox library, so `pkg/vermouth` is real work before any feature exists, and its bugs are yours.
- Redpanda has no native dead letter path, so bounded retry, the failure record, and the park to `<topic>.dlq` are hand written (STK-13). NATS JetStream would have given this as configuration.
- Four Postgres instances, a broker, five binaries, and a dev server on 2 vCPU means local work will feel slow at times, and an accidental parallel test run can thrash the machine.
- The memory estimate has no comfortable margin: about 5.1 GiB at the high end against 5.0 GiB available. Feature 5 either lands in the headroom freed by stopping the dev server, or it triggers the named fallback.
- The relay polls, so an event reaches its consumer 500ms to 1s after the write commits (STK-18). Tests must wait for a projection rather than assert immediately, which spec 0001 already anticipated.
- `sqlc` and `oapi-codegen` add a generate step that must be run and committed, and stale generated code is a confusing class of error until the habit sets in.
- The OpenAPI document is maintained by hand. Nothing forces it to match the gateway's real behaviour except the generated types and the tests.
- Short lived asymmetric tokens cannot be revoked before they expire, so a compromised access token stays valid for its lifetime. Feature 7 owns that window and may need a deny list.
- Choosing Go over the JVM knowingly gives up the ecosystem with the deepest ready made support for exactly this architecture.
- The stack is wide for one person: Go, four Postgres, Redpanda, S3 semantics, React, Tailwind, two code generators, and Kubernetes still ahead.

**Neutral**:
- New tools to learn in the first weeks: `sqlc`, `goose`, `franz-go` consumer groups, `oapi-codegen`, Task, and Garage's S3 surface.
- `pnpm` is installed but only on the interactive shell path, so a task target that shells out would fail today (see `## Follow-up`).
- The repository is not a git repository yet, so the freshness checks every workflow skill runs have nothing to read until it is.
- Avoiding React Server Components sidesteps CVE-2026-23864 (a denial of service in React metaframeworks) entirely, as a side effect of the single page app choice rather than a reason for it.
- Feature 3 records these conventions in `AGENTS.md`; until then, this spec is the only place they exist.

## Follow-up

- [ ] Replace `OWNER` in the Go module path `github.com/OWNER/vermouth` with the real account before the first push. `$develop` should ask once at scaffold time rather than guess.
- [ ] `git init` plus a `.gitignore` is part of the scaffold. Until it exists, every skill's freshness check is blind, and nothing is recoverable.
- [ ] Add the `PNPM_HOME` export to `~/.profile` as well as `~/.bashrc`. Verified today: `bash -lc 'pnpm --version'` fails, so anything the task runner shells out to will not find pnpm.
- [ ] Feature 5 must not scaffold onto `ingress-nginx`: Kubernetes SIG Network retired it in March 2026, with no releases and no security patches. Choose a maintained controller there (Traefik and Envoy Gateway are the current candidates).
- [ ] Feature 5 must run Redpanda with automatic topic creation off, so `EnsureTopics` owns creation with the right retention and partition count (STK-16, STK-17), plus Garage, plus the tuned memory flags above. A minimal in memory broker mode breaks INV-11 and INV-13.
- [ ] Feature 5 owns the Postgres advisory lock that would let `notifications` run more than one replica (STK-23). Until it exists, a second replica means two digests on the same morning.
- [ ] `LISTEN/NOTIFY` on the outbox table is a deliberate later optimisation on top of the relay poll (STK-18), worth doing only if the wait proves visible on screen. Never a replacement for the poll: a dropped connection loses the notification while the row survives.
- [ ] Feature 5 should measure the real total against the estimate table above. If it does not fit, apply the named fallback (one Postgres instance, four databases, four roles) by superseding this spec rather than improvising.
- [ ] Feature 3 (or `$audit`) records in root `AGENTS.md`: the service contract, the shared module's role, the `sqlc` and `goose` workflow, the generate step, and the event naming convention.
- [ ] No technology community skills are installed. Consider installing skills covering Go backend conventions, Postgres, React with TanStack, and Tailwind v4, so later features get specific implementation guidance instead of generic advice.
- [ ] Feature 4 inherits the identifier, timestamp, and money conventions above and should not decide them again.
- [ ] Feature 7 owns token lifetime, refresh rotation, and browser storage. The recommendation carried into it: access token in memory lasting about fifteen minutes, refresh token in a cookie marked `HttpOnly`, `Secure`, and `SameSite=Strict`. It also owns the revocation window named in Consequences.
- [ ] Feature 9 decides tracing. This spec deliberately ships structured logs only, because a collector does not fit the memory budget yet.
- [ ] No cache, no search engine, and no metrics store are in this stack. Postgres full text search is the intended starting point for feature 19, and a cache should wait for a measured problem.
- [ ] The landscape check behind several of these choices is cached at `docs/.agent-cache/research/stack-and-scaffold.md` with a reuse window to 2026-09-21. Re run it before feature 16 (cloud deployment), where the tooling moves fastest.

## Rationale

Reasoning, the full stacks weighed, and the references: see [rationale.md](rationale.md).
