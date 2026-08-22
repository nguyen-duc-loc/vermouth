# 0002. Rationale: Go services on Postgres and Redpanda, in one repository

Reasoning, the stacks weighed, and the sources behind [0002](index.md). `/develop` does not read this file.

## Context

> ⚠️ Premise note: the surface area of this stack, not any single choice in it, is the risk to the 7 November 2026 target. Spec 0001 already committed to four services with hand written outbox, relay, and idempotency code, and this decision adds four databases, a broker, S3 semantics, two code generators, a design system, and Kubernetes still ahead, all at 10 to 15 hours a week solo. The failure mode is not picking a wrong tool, it is spending the whole budget on foundations and never reaching the money loop the target is measured on. The framing that protects against it: the shared module `pkg/vermouth` and the one thread through the pipe are what must be excellent, and everything else in features 2, 3, 5, and 6 is allowed to stay thin until the money loop works end to end. If the schedule slips, thin those out further rather than dropping a service boundary, since the boundaries are the part that cannot be added back cheaply.

Spec 0001 fixed the architecture and deliberately named no tools. It left a capability table instead: a broker with per key ordering, independent consumer groups, retention long enough to rebuild a projection from zero, offset reset, and a dead letter destination with replay; one relational database per service with transactions strong enough for an outbox; a gateway that verifies a signed token, forwards a request id, and fans out reads in parallel; object storage for invoice PDFs; an in process scheduler with a database lock; and a language that makes an outbox, a relay, and an idempotent consumer cheap to write four times. Nothing can be scaffolded until those are turned into products, and every later feature is blocked behind it.

The forces are unusually concrete. The machine is measured, not assumed: 2 vCPU, 7.8 GiB total memory with 5.0 GiB available, and 23 GB free disk, with Go 1.27, Node 24.19, pnpm 11.22, Docker 29.7.2, Python 3.14.5, and git installed, and no Java, no .NET, and no Kubernetes. That same 5.0 GiB must later hold four services, four databases, a broker, a gateway, a dev server, and a Kubernetes control plane, which turns footprint from a preference into a constraint. The builder is one person at 10 to 15 hours a week, with the money loop due for friend testing by 7 November 2026 and the project's stated purpose being to learn this architecture properly rather than to ship the least code. There is no `AGENTS.md`, no source file, and no installed technology skill, so nothing in the repository constrains the choice and nothing will guide implementation afterwards.

Three findings from the landscape check changed the space rather than confirming it. MinIO's community edition removed its administration console during 2026 and directs users to a paid product, so it is no longer the obvious lightweight object store. Kubernetes SIG Network retired `ingress-nginx` in March 2026 with no further security patches, which rules out scaffolding onto it in feature 5. And no broker in the Kafka family provides bounded retry with a dead letter destination as a feature, so INV-13 is application code on Kafka or Redpanda, while NATS JetStream and RabbitMQ provide it natively. The last of those cuts against the otherwise strongest broker family, so it had to be weighed openly rather than assumed away.

The consequence of not deciding is simply that nothing else can start: features 3 through 20 all rest on this.

## Options considered

Full stacks, not individual tools, since the layers constrain each other.

### Option 1: Go services and gateway, four Postgres, Redpanda, one repository

Five static Go binaries over `net/http`, `pgx` with `sqlc` and `goose`, four Postgres instances, Redpanda through `franz-go`, Garage for object storage, and a React and Vite web app typed from one OpenAPI document. The outbox, relay, handled events store, retry, and dead letter code lives once in a shared module.

**Pros**:
- Smallest runtime footprint of any option that keeps four separate databases, which is what leaves room for the Kubernetes control plane feature 5 needs.
- The transaction containing both the business write and the outbox insert is visible in one function, so INV-3 is legible rather than implied.
- Static `FROM scratch` images make features 5 and 16 nearly free, with no runtime to install anywhere.
- Kafka vocabulary transfers to any future employer's Kafka, and `franz-go` needs no cgo, so the small image promise holds.
- One language across services means no context switching for a solo builder, and one shared module means one fix instead of four.

**Cons**:
- No maintained transactional outbox library exists for Go, so the machinery is hand written, and its bugs are the builder's own.
- Redpanda has no native dead letter path, so bounded retry, the failure record, and the park are also hand written.
- Two code generators (`sqlc`, `oapi-codegen`) mean a generate step that will be forgotten at least once.
- More code per feature than a batteries included framework would need, especially for validation and error mapping.

### Option 2: TypeScript everywhere, four Postgres, Redpanda, one repository

Node 24 with a framework such as Fastify or NestJS for the services and the gateway, an ORM with migrations, Redpanda through a Node client, and the same React web app. The event envelope becomes shared types used by publisher, consumer, and screen.

**Pros**:
- One language across the whole system including the browser, so the shared kit and the event types are literally the same code everywhere.
- Fastest to write per line, with the largest volume of examples for every layer.
- Already installed, with pnpm workspaces covering the entire repository as one workspace tool.

**Cons**:
- Five Node processes cost several times the memory of five Go binaries, which is the constraint that decides this spec, and images are hundreds of megabytes rather than tens.
- The Node Kafka clients are either librdkafka bindings, which fights the small image goal, or pure JavaScript with thinner coverage.
- An ORM tends to hide the outbox transaction, exactly the mechanism this project is meant to see clearly.
- No dominant outbox library either, so the hand written machinery cost is not avoided, only relabelled.

### Option 3: Kotlin or Java on Spring Boot 4.1, four Postgres, Kafka

The JVM stack with the deepest ready made support for this architecture: real transactional outbox libraries, mature Kafka integration with idempotent consumer patterns, Flyway or Liquibase migrations, and Spring Cloud Gateway in front.

**Pros**:
- The only option where the outbox, the relay, and idempotent consumption can be taken off the shelf rather than written.
- Deepest documentation and the largest body of production experience for precisely this design.
- Strongest transfer to a job market where this architecture is common, which matters given the project's learning goal.
- Kafka itself rather than a compatible reimplementation, so no compatibility question ever arises.

**Cons**:
- Four JVM services plus a JVM broker will not sit comfortably in 5.0 GiB alongside a Kubernetes control plane, and nothing is installed on this machine today.
- Slowest of the four to get to a first running thread, at a writing pace that is wrong for a 7 November target at 10 to 15 hours a week.
- Framework magic is at its densest exactly where the learning goal wants visibility: what the annotation did is the thing being studied.
- Container images and startup times are the largest of any option, which makes local iteration slower on 2 vCPU.

### Option 4: Python on FastAPI, four Postgres, NATS JetStream

FastAPI services with SQLAlchemy and Alembic, NATS JetStream as the broker, and the same React web app.

**Pros**:
- Lightest broker by a wide margin, at tens of megabytes, with bounded redelivery and a native dead letter path, so INV-13 becomes configuration instead of code.
- SQLAlchemy with Alembic is excellent for the per service migrations INV-2 requires.
- Fast to write, and Python is already installed with `uv` available.

**Cons**:
- Python processes are heavier than Go binaries and images are larger, though lighter than the JVM.
- NATS vocabulary does not transfer to Kafka, and the ecosystem around event driven patterns is smaller, so there is less to borrow when something goes wrong.
- Type checking is optional and after the fact, which is a real cost on a system whose correctness lives in event field names.
- The gateway's parallel read fan out is more awkward in an async framework than `errgroup` makes it in Go.

## Rationale

The deciding force is the memory measurement, not taste. Feature 5 has to fit a Kubernetes control plane into the same 5.0 GiB that already holds four services, four databases, a broker, and a dev server, and only Option 1 and Option 4 leave that room while keeping four physically separate databases. Option 3 is, on the merits of this architecture alone, the best engineering answer available: it is the only stack where the outbox and idempotent consumer are libraries rather than homework, and it would transfer best to a job. It loses on operational reality, which is the rule this project has to obey more than most, since the whole system lives on one small VM (basis: state the operational reality of every recommendation, not just the name). Option 2 loses for the same reason with less compensation.

Between the two that fit, Option 1 wins on what transfers and Option 4 wins on what is free. The engineer chose Kafka vocabulary and typed correctness over a native dead letter path, and that is the right call here for one specific reason: INV-13 is written once in a shared module and then used four times, so its cost is paid once, while a broker vocabulary is carried for the life of the project and beyond it. Redpanda rather than Kafka itself keeps that vocabulary while shedding the JVM, which is what makes the Kafka family affordable on 2 vCPU at all. The dead letter code being hand written is recorded as this decision's largest single cost, and it is the first thing to review if the shared module proves shaky.

Several smaller choices follow from spec 0001 rather than from preference. One topic per publishing service, partitioned by the envelope key, is the only arrangement that makes INV-6 true for two differently named events about the same class, so a topic per event name was rejected. `sqlc` over an ORM follows from INV-3: the value of the outbox pattern is that the reader can see the business write and the event in one transaction, and an ORM that manages transactions for you removes exactly that. Four Postgres instances rather than one with four databases follows from what spec 0001 exists to demonstrate, since a shared instance means one restart takes every service's storage down at once and the demonstration quietly stops being true. The named fallback is kept explicit because the memory estimate might be wrong, and a build should never make that call silently.

Two things had to be reconciled against spec 0001 rather than simply chosen. Token verification was discussed as the gateway fetching a JWKS document, but INV-14 says a service verifies with a key it already holds and asks `identity` nothing, so configuration is the authoritative source here and the JWKS endpoint exists only for a rotation step. That ordering matters: it is what lets a service verify a token while `identity` is down, which is the property the invariant was written to protect. The second is the gateway itself. The landscape check confirms that no proxy product performs a read fan out across two services, so the gateway spec 0001 describes is application code either way, and reaching for APISIX or Kong would have added a second thing to operate without removing the code that had to be written.

Object storage was decided rather than asked, because the answer is narrow. Invoice PDFs need S3 semantics and must not live in a database, and MinIO stopped being the easy default when its community edition lost administration. Garage is the lightweight replacement now most often named, and coding against the S3 API through `aws-sdk-go-v2` means feature 16 can point at cloud object storage by changing configuration rather than code.

Five mechanics were pinned after a cross check read the draft cold, because each was a decision a build would otherwise have invented on the spot. The partition count is the one worth explaining: three rather than one, even though a single partition would order everything and make INV-6 trivially true. That triviality is the trap. A handler that wrongly assumed order across two different keys would pass every test on this machine and fail only where partitions are many, which is precisely the mistake INV-7 was written to prevent, so three partitions make it visible here instead. The relay polls rather than waiting on `LISTEN/NOTIFY` for a related reason: the outbox row is the durable fact and a notification is not, so a design that waits for one loses events when a connection drops, while a poll that later gains notifications loses nothing. The shared module's own tables are the single exception written into STK-3, because `outbox` and `handled_events` live in four databases while their code lives once, and four generated packages for a schema that must never differ is four chances to differ. Topic creation and the offset reset pair were both promised by spec 0001's capability table with nobody named to do them; `EnsureTopics` and one replay target turn those from intentions into code. Migrations moved outside `main.go` for the same class of reason, since two replicas starting together would race `goose`, and feature 5 inherits an init container for free.

## References

**Project sources** (verifiable, in this repo):
- `docs/specs/0001-service-boundaries-and-communication/index.md`, the capability table under "What this requires of feature 2": every required broker, database, gateway, storage, scheduling, and language property this spec is checked against.
- The same spec's INV-2, INV-3, INV-5, INV-6, INV-11, INV-13, INV-14, INV-15, which decide the database layout, the data access style, the topic and consumer group naming, the retry and dead letter design, and where verification keys come from.
- `docs/scope/scope.md`, feature 2 "Done when": every tool choice recorded with a reason, and a skeleton that builds and starts locally with a service answering a health check through the gateway.
- `docs/scope/scope.md`, the "Decided up front" block: VND only, Vietnamese and English, local Kubernetes on an Azure VM with a cloud deployment later.
- `docs/scope/scope.md`, header: Tracer Bullet build approach, Beta workflow tier, 10 to 15 hours a week, money loop by 7 November 2026.
- Measured on this machine on 2026-08-22: 2 vCPU, 7.8 GiB memory with 5.0 GiB available, 23 GB free disk, Go 1.27.0, Node 24.19.0, pnpm 11.22.0, npm 11.17.0, Docker 29.7.2, Python 3.14.5, git 2.43.0; no Java, no .NET, no `kubectl`, no k3s, no kind, no Helm, no Task. `bash -lc 'pnpm --version'` fails, because `PNPM_HOME` is exported only from `~/.bashrc`.
- No `AGENTS.md` and no installed technology community skill, so no project convention constrained this choice.
- `docs/.agent-cache/research/stack-and-scaffold.md`, the landscape check summarised below.

**Practices & standards**:
- Boring and proven over new and exciting, and never recommend a technology you would not operate at 2am.
- State the operational reality of a recommendation, not only its name.
- Transactional outbox with a publishing relay, and an idempotent consumer with a handled message store.
- Per key partition ordering, with one topic per publishing aggregate rather than per event name.
- Dead letter destination with bounded retry and growing delay, plus replay.
- Object storage for files, never blobs in the primary database.
- Asymmetric token signing so a verifier cannot mint, with local verification and no network call on the request path.
- Argon2id as the current default for password hashing (no longer applies: spec [0004](../0004-tutor-sign-in-google-oauth/index.md) removed passwords, so Vermouth stores no hash).
- Environment only configuration, parsed once at startup and failing loudly.
- Structured logging with a correlation identifier from day one.
- Integration testing against real infrastructure for anything whose correctness depends on the broker or the database.
- Time ordered identifiers (UUIDv7) for index health, generated in the application.
- Integer minor units for money, never floating point.

**Links** (web verified during the landscape check on 2026-08-22, for a human to follow):
- Kafka alternatives, where Redpanda sits and why: https://estuary.dev/blog/kafka-alternatives/ and https://markaicode.com/alternatives/kafka-alternatives/
- Kafka and Redpanda benchmark claims, and why they are contested: https://computingforgeeks.com/kafka-vs-redpanda-benchmarks/
- MinIO community edition console removal: https://github.com/minio/minio/discussions/21316 and https://docs.min.io/enterprise/aistor-object-store/upgrade-aistor-server/community-edition/
- Garage compared with MinIO and S3: https://glukhov.org/data-infrastructure/object-storage/garage-vs-minio-vs-s3/
- `ingress-nginx` retirement and how to choose a replacement: https://community.replicated.com/t/ingress-nginx-is-retiring-how-to-choose-a-replacement/1611
- Open source gateway comparison (APISIX, Kong, Traefik, Envoy, NGINX): https://apisix.apache.org/learning-center/open-source-api-gateway-comparison
- Designing the outbox relay, idempotency, and cleanup as one system: https://www.momentslog.com/development/spring-boot-transactional-outbox-in-production-design-the-relay-idempotency-and-cleanup-as-one-system
- The JVM outbox libraries this decision gives up: https://github.com/gruelbox/transaction-outbox
- Spring Boot 4.1, the Option 3 baseline: https://www.infoq.com/news/2026/06/spring-boot-4-1/
- React metaframework denial of service (CVE-2026-23864), avoided by the single page app choice: https://www.netlify.com/changelog/2026-01-26-react-nextjs-dos-vulnerability/
- .NET 9 and 8 end of life dates, which ruled out a .NET start on anything but 10: https://versionsof.net/

## Landscape check (evidence)

Run on 2026-08-22 in a read only helper, five searches, no page fetches beyond search results, with a 30 day reuse window to 2026-09-21. Full notes: `docs/.agent-cache/research/stack-and-scaffold.md`. What it changed:

- **Broker.** Redpanda is positioned exactly at "keep the Kafka client API, shed the operational weight", which is the motivation here. The often quoted latency multiple is contested by counter benchmarks and was not treated as a reason to pick. Pulsar was set aside as multi tenant and geo replication machinery irrelevant at one tutor on one VM. RabbitMQ sits in the queue family, so retention long enough to rebuild a projection from zero is not its native model, which is what INV-11 needs.
- **Object storage.** The MinIO community edition change is the one finding that reversed a default outright, and Garage is the replacement most often named at this size.
- **Outbox support per ecosystem.** The JVM has real libraries, Go has reference implementations written per project, and Node and Python have neither. That confirmed spec 0001's own framing that this machinery is written by hand, and it is why the shared module exists rather than four copies.
- **Gateway.** No proxy product does read aggregation across services, so the aggregating gateway is application code in every option. The `ingress-nginx` retirement is carried into feature 5 as a follow up rather than acted on here.
- **Framework and runtime freshness.** Spring Boot 4.1 dated the Option 3 baseline, the .NET end of life dates removed a fifth option before it was written, and the React metaframework advisory is noted as sidestepped rather than solved. FastAPI's currency could not be verified in this check and is recorded as unverified.
