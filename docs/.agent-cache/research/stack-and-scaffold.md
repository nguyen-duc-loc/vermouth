# Landscape check: stack & scaffold (feature 2)

**Run**: 2026-08-22 · 5 searches, no page fetches beyond search results · for `/architect stack & scaffold`
**Reuse window**: 30 days (until 2026-09-21), then re-run.

## Message broker

- Kafka remains the reference log. Redpanda is the Kafka API compatible single binary with no JVM and no ZooKeeper, positioned exactly at "keep the Kafka client API, shed the operational weight"; recommended starting point for that motivation. Benchmark claims on both sides are contested (Confluent counter benchmarks found cases where Kafka wins), so treat the 10x latency claim as marketing, not a reason to pick.
- NATS JetStream: pick when sub millisecond latency and simplicity matter more than Kafka API compatibility.
- Pulsar: pick for hard multi tenant isolation or geo replication. Not relevant at one tutor on one VM.
- RabbitMQ sits in the queue family rather than the retained log family; retention long enough to rebuild a projection from zero (INV-11) is not its native model.
- All three log options share the model INV-11 needs: append only ordered durable log, retained, many independent consumers each tracking its own position, unlike a queue that deletes on consume.

Sources:
- https://markaicode.com/alternatives/kafka-alternatives/
- https://estuary.dev/blog/kafka-alternatives/
- https://designgurus.substack.com/i/206462324/apache-kafka-the-established-standard
- https://computingforgeeks.com/kafka-vs-redpanda-benchmarks/

## Object storage (invoice PDFs)

- **Freshness item that changes the default.** MinIO gutted its community edition: the console is now an object browser only, with account and policy management, bucket management, configuration, lifecycle and tiering, and site replication removed, pushing users to the commercial AIStor. Community reaction is openly hostile.
- Garage is the lightweight S3 compatible alternative most often named as the replacement, aimed at small to medium clusters. SeaweedFS and RustFS also appear. Ceph is the heavyweight, wrong size here.
- MinIO the server still works and is still widely embedded (Kubeflow keeps it supported), so it is not unusable, just no longer a free product with a working admin UI.

Sources:
- https://cloudian.com/blog/minios-ui-removal-leaves-organizations-searching-for-alternatives/
- https://github.com/minio/minio/discussions/21316
- https://docs.min.io/enterprise/aistor-object-store/upgrade-aistor-server/community-edition/
- https://glukhov.org/data-infrastructure/object-storage/garage-vs-minio-vs-s3/

## Outbox and idempotent consumer support per ecosystem

- The JVM has the deepest ready made support: `gruelbox/transaction-outbox` (and the `synaos` fork), `raedbh/spring-outbox`, plus Spring Boot 4 specific outbox + Kafka reference implementations covering idempotency, batch publishing and low poll relays.
- Go has working reference implementations but they are hand written per project (outbox + Postgres + broker + DLQ + tracing examples), not a library you install.
- Node and Python: the pattern is documented widely but there is no dominant library; you write the relay yourself. That is consistent with spec 0001's own framing (the outbox, relay and handled events table are yours to write, four times).
- Independent of ecosystem: a transactional outbox alone guarantees none of publish once, consume once, ordering, or retention. The relay, idempotency store and cleanup have to be designed as one system. Spec 0001 already does this (INV-3, INV-5, INV-11, INV-13).

Sources:
- https://github.com/KHolodilin/spring-transactional-outbox-kafka
- https://www.momentslog.com/development/spring-boot-transactional-outbox-in-production-design-the-relay-idempotency-and-cleanup-as-one-system
- https://github.com/gruelbox/transaction-outbox
- https://github.com/SagarMaheshwary/transactional-outbox-rabbitmq

## Gateway

- The mature open source field is APISIX, Kong, Envoy (and Envoy Gateway), Traefik, NGINX. APISIX is currently framed as the best all round fresh start on performance and plugin richness; Kong as the enterprise standard; Traefik as the lightweight Go reverse proxy with the best Kubernetes service discovery story.
- **Freshness item for feature 5.** Kubernetes SIG Network retired the `ingress-nginx` controller effective March 2026: no releases, no bug fixes, no security patches. Do not scaffold onto it.
- None of these proxies do read aggregation across two or more services (the fan out spec 0001 requires of the gateway). A proxy gives routing, TLS and token verification; the aggregating read has to be application code either way.

Sources:
- https://apisix.apache.org/learning-center/open-source-api-gateway-comparison
- https://zuplo.com/learning-center/best-api-gateways-2026/
- https://api7.ai/kong-vs-traefik
- https://community.replicated.com/t/ingress-nginx-is-retiring-how-to-choose-a-replacement/1611

## Framework and runtime freshness (as of 2026-08-22)

- Spring Boot 4.1 released 2026-06-10 (gRPC auto configuration, SSRF mitigation, Kotlin 2.3, async context propagation, better OpenTelemetry).
- Next.js 16.3 released around 2026-08 (largest update since 16.0 in November 2025; big dev memory and build time cuts). Note CVE-2026-23864, a React Server Components denial of service (CVSS 7.5) affecting Next.js and other React metaframeworks: stay patched if React Server Components are used.
- React Router v8 released 2026-06-17, ESM only; React Router v6 and Remix v2 are end of life.
- .NET: 9.0 and 8.0 LTS both reach end of life 2026-11-10, so a .NET scaffold must start on 10.
- FastAPI: no version signal found in this check; treat its currency as unverified.

Sources:
- https://www.infoq.com/news/2026/06/spring-boot-4-1/
- https://www.infoq.com/news/2026/08/vercel-next-js-16-3/
- https://www.netlify.com/changelog/2026-01-26-react-nextjs-dos-vulnerability/
- https://www.infoq.com/javascript/news/
- https://versionsof.net/
