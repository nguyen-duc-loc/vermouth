# 0005. Local Kubernetes platform and one command startup

**Date**: 2026-08-25
**Status**: In Progress

## Summary

Vermouth will run locally on your Apple silicon Mac in one k3d cluster. One daily command builds the current images, runs migrations, starts every workload, waits for health, and exposes the app at `http://vermouth.localhost`. Local and production use the same k3s generation, packaged Traefik edge, chart, and image set. The existing Compose stack stays as the integration test platform, while a separate image command proves every deployable image supports both `linux/arm64` and `linux/amd64`.

## Requirements

**User stories**:

1. As the developer, I want one command to start the whole system so I can work on product behavior instead of operating each process.
2. As the future operator, I want the same application images to support Apple silicon and an `amd64` Azure VM so moving hosts does not require new Dockerfiles.
3. As the developer, I want failures to preserve the last usable release and its data so recovery starts from evidence rather than a clean slate.

**Acceptance criteria**:

1. **AC-1**: On macOS with Colima, k3d, kubectl, Helm, Docker Buildx, Task, Go, Node, and pnpm installed, `task platform:bootstrap` starts Colima when needed, validates at least 4 CPUs and 8 GiB, creates missing local configuration and signing keys without overwriting existing values, installs web dependencies, creates the `vermouth` k3d cluster and its local registry, keeps packaged Traefik, and reconciles the pinned Traefik configuration. Running it again changes nothing when the desired state already exists.
2. **AC-2**: `task dev` builds native `linux/arm64` images, pushes immutable content tags to the local registry, records their digests, creates content named application Secrets from `.env`, installs or upgrades the `vermouth-foundation` Helm release, runs Garage initialization and four bounded migration Jobs, upgrades the `vermouth` application release, waits for every required condition, and prints `http://vermouth.localhost` only after the platform is ready. Both releases render from the same chart.
3. **AC-3**: The ready platform contains the web app, gateway, identity, teaching, billing, notifications, four separate Postgres 18 instances, one tuned Redpanda node, one Garage node, one packaged Traefik replica, and the local registry. Every application workload has one replica, and `notifications` cannot be configured above one replica in this feature.
4. **AC-4**: `http://vermouth.localhost` serves the web app, routes `/api`, `/health`, and `/ready` through one Kubernetes Ingress to the gateway, routes `/config.json` and `/` to web, preserves every path, and keeps browser API calls on the same origin. Gateway readiness reports every Go service ready.
5. **AC-5**: Identity, teaching, billing, notifications, Redpanda, and Garage data survive pod replacement, service redeploy, `task platform:stop`, and the next `task dev`. `task platform:clean` and `task platform:recreate` both show the target and require the typed value `vermouth` because deleting the k3d node deletes its local path data. Clean stops there. Recreate then bootstraps an empty cluster.
6. **AC-6**: `test/compose.test.yaml` and the existing `task infra:*` commands remain the real Postgres and Redpanda integration test platform. Kubernetes becomes the normal development platform without making `task test` depend on a cluster.
7. **AC-7**: `task dev:redeploy -- <workload>` accepts `gateway`, `identity`, `teaching`, `billing`, `notifications`, or `web`. It builds and pushes only that native image, runs the named service migration Job when the target owns a database, upgrades only the affected image digest, and waits for that workload.
8. **AC-8**: `task images:multi` builds the gateway, four services, web runtime, Garage initializer, and four service migration images for `linux/arm64` and `linux/amd64`, pushes one manifest list per image to the local registry, and fails unless Buildx inspection finds both platforms. Go builds use `TARGETOS` and `TARGETARCH`, and the web runtime uses a pinned two architecture base. The local only development token image is not a deployment artifact and is excluded.
9. **AC-9**: The `vermouth` namespace starts with deny all NetworkPolicies. The committed traffic matrix names every allowed source selector, destination selector, namespace, port, and protocol for packaged Traefik, web, gateway, all services, all Jobs, four Postgres instances, Redpanda, Garage, DNS, and identity outbound OAuth traffic. A rendered policy outside that matrix fails chart validation.
10. **AC-10**: Every container matches the committed workload matrix for requests, limits, probes, numeric user and group, ServiceAccount, token mounting, writable mounts, and any read only root filesystem exception. All Pods use `RuntimeDefault` seccomp, drop Linux capabilities, and prevent privilege escalation. Vermouth and web images run as nonroot users.
11. **AC-11**: A build failure changes no registry, migration, Secret reference, or Helm state. A partial registry push may leave unreferenced content but changes no migration or release. A migration failure leaves the previous application release and its content named Secrets running, retains failed Job logs, and never runs a down migration. A failed application readiness check rolls only the `vermouth` application release back while preserving the foundation release and any forward migration already applied.
12. **AC-12**: `task platform:doctor`, `platform:status`, `platform:logs`, `platform:stop`, `platform:recreate`, and `platform:clean` verify the exact `k3d-vermouth` context before mutation, use bounded deadlines, and point a failure to the next useful status or log command. Bootstrap refuses an unknown or drifted cluster, and only `platform:recreate` may replace it after typed confirmation.
13. **AC-13**: `task dev:token` runs the development token program as a short lived Kubernetes Job whose image is not a production service image. `task thread` then reaches the gateway through `http://vermouth.localhost` and observes the identity event in notifications.
14. **AC-14**: When Google OAuth credentials are absent, explicit local configuration keeps the platform ready, the web app shows Google sign in as unavailable with a clear message, and the development token thread still works. Enabling Google OAuth requires configuration only, not different manifests.
15. **AC-15**: On the 4 CPU and 8 GiB Colima profile, steady declared requests stay at or below 1 CPU and 2.5 GiB, the measured ready platform working set including k3s and the registry stays at or below 7 GiB, and five minute CPU use stays below 3.5 CPUs. Redpanda remains at one core and 1 GiB of application memory. The result is recorded. The build never collapses the four databases without a later spec change.
16. **AC-16**: An idempotent Garage initialization Job reconciles zone `local`, 5 GiB capacity, replication factor one, bucket `vermouth-invoices`, fixed key name `billing`, the key values sourced from the billing foundation Secret, and bucket read plus write grants. It inspects before every change, resumes after interruption, refuses a conflicting key, and creates no duplicate layout, bucket, key, or grant.
17. **AC-17**: `deploy/platform/versions.yaml` pins compatible host tools, k3s, and the packaged Traefik image and chart generation. `deploy/images.lock.yaml` pins every base and infrastructure image by tag and digest with supported architecture metadata. The chart, local registry, migration targets, and development token image all resolve through those files. `platform:doctor` reports drift instead of silently upgrading.

## Decision

**Chosen option**: Option 2: k3d development beside the Compose test platform

Add a one node k3d platform for daily development, package Vermouth in one Helm chart, use the packaged k3s Traefik release through one Kubernetes Ingress, distribute local images through a k3d managed registry, and preserve Compose for integration tests (basis: the existing Task and Compose workflow in `AGENTS.md`, specs 0002 and 0006, the strangler pattern, and official k3s packaged Traefik guidance).

**Implementation skills**: `kubernetes-specialist` (`jeffallan/claude-skills`, `.agents/skills/kubernetes-specialist/`) · `helm-chart-scaffolding` (`wshobson/agents`, `.agents/skills/helm-chart-scaffolding/`) · `docker-buildx` (`full-stack-skills/docker-skills`, `.agents/skills/docker-buildx/`) · `docker-security` (`full-stack-skills/docker-skills`, `.agents/skills/docker-security/`) · `s3-storage` (`terminalskills/skills`, `.agents/skills/s3-storage/`) · `go-goose` (`metalagman/agent-skills`, `.agents/skills/go-goose/`)

### Platform choices

| Layer | Choice | Build contract |
|---|---|---|
| Local container runtime | Existing Colima profile | Bootstrap may start and validate it, but may not install it or overwrite its profile |
| Local Kubernetes | k3d `v5.9.0` with k3s `v1.36.3-k3s1`, one server, no agents | The committed k3d config names the cluster `vermouth`, keeps packaged Traefik, maps local port 80, and attaches the managed registry |
| Application packaging | One Helm chart under `deploy/helm/vermouth`, rendered as `vermouth-foundation` and `vermouth` releases | The chart has `Chart.yaml`, documented defaults, `values.schema.json`, `values-local.yaml`, component gates, helpers, NOTES, and lint plus render checks. Separate releases make cold start ordering and application rollback real without creating a second chart (basis: the installed Helm chart skill and Helm chart practices) |
| Edge routing | Packaged Traefik image `rancher/mirrored-library-traefik:3.7.8`, chart `40.1.4+up40.1.0`, one replica | A committed `HelmChartConfig` fixes resources and JSON access logs. Vermouth owns one standard Kubernetes Ingress. Local uses HTTP only; spec 0006 adds production ACME and HTTPS values (basis: official k3s Helm and networking guidance) |
| Local image distribution | k3d managed registry, bound for local use only | Daily builds push native immutable tags and deploy registry digests. The registry is not a production artifact store (basis: https://k3d.io/v5.0.0/usage/registries/) |
| Multiplatform builds | Docker Buildx for `linux/arm64` and `linux/amd64` | Builder stages run on `$BUILDPLATFORM`; Go receives `TARGETOS` and `TARGETARCH`; the six application images and four migration images use registry manifest lists (basis: https://docs.docker.com/build/building/multi-platform/ and the installed Buildx skill) |
| Web runtime | Repository image derived from `nginx:stable-alpine3.24`, pinned by digest | It serves `web/dist`, uses SPA fallback, listens on 8080, writes temporary files under writable `emptyDir` mounts, and runs as a nonroot user (basis: https://hub.docker.com/_/nginx and the installed Docker security skill) |
| Stateful storage | Vermouth specific static local PVs with `ReadWriteOnce`, `Retain`, node affinity, and deterministic paths under `/var/lib/vermouth` | One Docker named volume mounts `/var/lib/vermouth` into the k3d server. Eight fixed directories hold four Postgres instances, Redpanda, Garage metadata and data, and Traefik ACME state. Dynamic local path directory IDs are not used (basis: spec 0006 and Kubernetes local volume practice) |
| Migrations | One uniquely named Kubernetes Job per service, orchestrated by Task before the application upgrade | Each migration image contains goose and only that service migration directory. Jobs use `restartPolicy: Never`, `backoffLimit: 1`, and a deadline. Task removes successful Jobs and retains failed Jobs for diagnosis (basis: STK-5, STK-21, https://kubernetes.io/docs/concepts/workloads/controllers/job/, and the installed goose skill) |
| Object storage | Garage `v2.2.0` in one node mode | One init Job owns the local layout, private bucket, fixed billing key, and bucket grant (basis: spec 0002 and https://www.mintlify.com/deuxfleurs-org/garage/installation/docker) |
| Broker | Redpanda `v25.3.5`, one node, no host listener | It keeps the exact Compose memory, partition, retention, and automatic topic creation behavior. Services still own topic creation through `EnsureTopics` |
| Secrets | Stable foundation Secrets plus content named application Secrets, created directly from ignored local values | Foundation credential drift requires confirmed recreate. Application release references change atomically with the release. Secret values never appear in Helm arguments, committed values files, image layers, or logs |
| Observability | Existing JSON application logs plus JSON Traefik access logs to stdout | `platform:logs` reads current and previous containers. Prometheus, tracing, log aggregation, and alerts stay with feature 9 (basis: spec 0002 and packaged Traefik configuration) |

### Packaged Traefik contract

K3s owns one Traefik release. Vermouth never installs a second controller. The committed files are `deploy/platform/traefik/values-common.yaml`, `values-local.yaml`, and `values-production.yaml`. Task renders the common file plus exactly one environment file into the k3s `HelmChartConfig` named `traefik` in `kube-system`.

Common values fix one replica, image `rancher/mirrored-library-traefik:3.7.8`, chart `40.1.4+up40.1.0`, CPU request and limit `25m` and `250m`, memory request and limit `64Mi` and `256Mi`, official probes, JSON access logs to stdout, entrypoints `web` and `websecure`, and container log rotation. Local values expose only entrypoint `web` on host port 80 and configure no certificate resolver or redirect. Production values come from spec 0006 and add persistent ACME plus HTTPS.

The application chart renders one Ingress. Local annotations select IngressClass `traefik` and entrypoint `web`. Production annotations select `websecure`, TLS, and the final production certificate resolver. The route inventory and destination Services stay identical across environments.

Bootstrap desired state comes only from `deploy/platform/versions.yaml`, `deploy/images.lock.yaml`, `deploy/k3d/vermouth.yaml`, the three committed Traefik values files, `deploy/helm/vermouth/values-local.yaml`, and `.env.example`. The existing Colima profile is the runtime source and must already satisfy the CPU and memory values in `versions.yaml`; bootstrap never rewrites it. Missing `.env` is copied from `.env.example`, then only empty generated values are filled. Any other local file or live default is drift, not configuration.

### Startup order and release ownership

| Step | Owner | Completion gate | Failure result |
|---|---|---|---|
| 1. Validate host and desired state | `platform:doctor` | Version, capacity, context, ports, config hash, and image locks pass | No cluster or release mutation |
| 2. Build and push | Buildx and local registry | Every required native digest is recorded | No Secret, Job, or release mutation. Partial unreferenced registry content may remain |
| 3. Prepare Secrets | Task | Stable foundation Secret hashes match, new content named application Secrets exist | Old release keeps old Secret references |
| 4. Reconcile foundation | `vermouth-foundation` release | Postgres, Redpanda, Garage, Services, and PVCs are ready | Application release is untouched |
| 5. Initialize Garage | Task owned Job | Desired layout, bucket, key, and grants match | Application release is untouched, failed Job remains |
| 6. Run migrations | Four Task owned Jobs in parallel | All four Jobs complete | Application release is untouched, failed Jobs remain, no down migration runs |
| 7. Upgrade applications | `vermouth` release | Deployments, Gateway, routes, policies, and probes reach their current revision | Only this release rolls back to its prior revision and Secret references |
| 8. Verify entry path | Task | Traefik is Available, the Ingress exists, and `/ready` is ready through `vermouth.localhost` | Command fails with status and log targets |

The foundation release owns the namespace identity, StatefulSets, their Services, PVC templates, NetworkPolicies, and references to stable bootstrap credentials. Task creates the actual Secrets. The application release owns the web app, gateway, four service Deployments and Services, the Ingress, application ConfigMaps, and content named runtime Secret references. K3s owns packaged Traefik. Migration, Garage initialization, and development token Jobs are rendered from the chart but created and waited on by Task, so they do not race a Helm release.

### Compatibility and image inventories

`deploy/platform/versions.yaml` is the only source for host and controller compatibility.

| Tool or component | Accepted value |
|---|---|
| Host | macOS on `arm64` |
| Colima | `0.10.x`, minimum `0.10.3` |
| Docker Engine | `29.x`, minimum `29.7.2` |
| Docker Buildx | `0.36.x`, minimum `0.36.1` |
| k3d | `5.9.x`, minimum `5.9.0` |
| k3s live version | Exact `v1.36.3+k3s1`, as reported by Kubernetes |
| k3s node image tag | Exact `v1.36.3-k3s1`, as used by `rancher/k3s` and k3d |
| kubectl | `1.35.x` or `1.36.x` |
| Helm | `4.2.x`, minimum `4.2.4` |
| Task | `3.53.x`, minimum `3.53.1` |
| Go | `1.27.x`, minimum `1.27.0` |
| Node | `24.19.x`, minimum `24.19.0` |
| pnpm | Exact `11.22.0` from `packageManager` |
| Packaged Traefik | Image `3.7.8`, chart `40.1.4+up40.1.0`, inherited from the exact k3s release |

`deploy/images.lock.yaml` records repository, selected tag, resolved digest, and supported platforms for every external image. For images built from this repository it records the Dockerfile, named target, and required platforms. Their per worktree runtime digests live in ignored `.tmp/platform/images.json`, because committing a digest for every local edit would make the lock meaningless. A missing external digest, build target, or required architecture fails doctor and chart rendering. The lock contains at least these entries:

| Entry | Selected tag or source | Required platforms |
|---|---|---|
| Go builder | `golang:1.27-alpine` | `linux/arm64`, `linux/amd64` |
| Web runtime | `nginx:stable-alpine3.24` | `linux/arm64`, `linux/amd64` |
| Postgres | `postgres:18.3-alpine` | `linux/arm64`, `linux/amd64` |
| Redpanda | `docker.redpanda.com/redpandadata/redpanda:v25.3.5` | `linux/arm64`, `linux/amd64` |
| Garage | `dxflrs/garage:v2.2.0` | `linux/arm64`, `linux/amd64` |
| Local registry | `registry:2.8.3` | `linux/arm64`, `linux/amd64` |
| k3s node | `rancher/k3s:v1.36.3-k3s1` | `linux/arm64`, `linux/amd64` |
| Packaged Traefik | `rancher/mirrored-library-traefik:3.7.8` | `linux/arm64`, `linux/amd64` |
| Gateway, services, web | Repository Dockerfiles and web Dockerfile | `linux/arm64`, `linux/amd64` |
| Four migration targets | Service Dockerfiles, migration stage | `linux/arm64`, `linux/amd64` |
| Garage initializer | `deploy/images/garage-init/Dockerfile`, runtime target | `linux/arm64`, `linux/amd64` |
| Development token | Identity Dockerfile, local development stage | `linux/arm64` only |

The bundled local path provisioner is not used by Vermouth PVCs. Static PVs keep deterministic component directories for safe export and restore.

### Registry and generated state

| Value | Fixed source |
|---|---|
| Registry name | `vermouth-registry` in the committed k3d configuration |
| Host endpoint | `k3d-vermouth-registry.localhost:5111` |
| Cluster endpoint | `k3d-vermouth-registry:5000` |
| Storage | Docker named volume `vermouth-registry-data`, stopped but not removed by `platform:stop` |
| Transport | Plain HTTP marked insecure only in the generated k3d containerd registry configuration |
| Application tag | `dev-<first 12 characters of the relevant build input SHA256>` |
| Digest record | Ignored `.tmp/platform/images.json`, schema version 1 |
| Runtime values | Ignored `.tmp/platform/runtime-values.yaml`, containing image digests and Secret names but no Secret values |
| Cleanup | Normal startup never deletes registry content. Confirmed clean and recreate delete the registry volume. No automatic garbage collection is promised |

The build input hash covers the Dockerfile plus every copied file for that target, including `pkg/vermouth` for Go images and lockfiles for the web image. A tag is never overwritten. The digest record contains `schema_version`, `workload`, `repository`, `tag`, `digest`, `platform`, `source_revision`, `dirty`, and `built_at` for every native image.

### Resource and storage matrix

| Workload | Replicas | CPU request and limit | Memory request and limit | Storage |
|---|---:|---|---|---|
| Each Go application | 1 | `25m`, `250m` | `32Mi`, `128Mi` | None |
| Web | 1 | `10m`, `100m` | `32Mi`, `64Mi` | Writable `emptyDir` mounts only |
| Each Postgres | 1 | `50m`, `300m` | `128Mi`, `384Mi` | `2Gi` PVC |
| Redpanda | 1 | `250m`, `1000m` | `768Mi`, `1536Mi` | `4Gi` PVC |
| Garage | 1 | `25m`, `250m` | `64Mi`, `256Mi` | `1Gi` metadata PVC and `5Gi` data PVC |
| Packaged Traefik | 1 | `25m`, `250m` | `64Mi`, `256Mi` | None locally, ACME PVC in production |
| Each migration Job | At most 1 | `25m`, `250m` | `32Mi`, `128Mi` | None |
| Garage initialization Job | At most 1 | `25m`, `100m` | `32Mi`, `64Mi` | Garage PVCs through its API only |
| Development token Job | On demand | `25m`, `250m` | `32Mi`, `128Mi` | None |

Four migration Jobs may run in parallel. Steady declared requests remain under the AC-15 ceiling. Limits may be oversubscribed, but the five minute measured CPU and total working set ceilings still decide whether the platform passes. `task platform:measure` samples `/proc/meminfo` inside Colima, `docker stats`, and `kubectl top` every 5 seconds for 5 minutes. It writes schema version 1 JSON to `.tmp/platform/reports/resource-<run-id>.json` with tool versions, timestamps, maxima, averages, per workload samples, and the pass decision. `$check verify` records the durable verdict in its review artifact.

### Probe and deadline matrix

| Target | Readiness | Liveness | Deadline or retry |
|---|---|---|---|
| Go application | `GET /ready`, every 5 seconds, 2 second timeout, 12 failures | `GET /health`, every 10 seconds, 2 second timeout, 3 failures | Deployment 5 minutes |
| Web | `GET /`, every 5 seconds, 2 second timeout, 6 failures | Same path every 10 seconds, 3 failures | Deployment 3 minutes |
| Postgres | `pg_isready`, every 5 seconds, 3 second timeout, 12 failures | Same command every 10 seconds, 3 failures | Foundation 8 minutes |
| Redpanda | `rpk cluster health`, every 10 seconds, 5 second timeout, 12 failures | Same command every 20 seconds, 3 failures | Foundation 8 minutes |
| Garage | Pinned image health or `garage status`, every 10 seconds, 5 second timeout, 12 failures | Same check every 20 seconds, 3 failures | Foundation 8 minutes |
| Packaged Traefik | Official chart readiness and liveness probes | Official chart probes | Reconcile 5 minutes |
| Ingress | Route exists and HTTP `/ready` succeeds through `vermouth.localhost` | Not applicable | 2 minutes |
| Migration Job | Completion condition | Not applicable | `activeDeadlineSeconds: 300`, `backoffLimit: 1` |
| Garage initialization | Completion condition | Not applicable | `activeDeadlineSeconds: 300`, `backoffLimit: 1` |
| Development token | Completion condition | Not applicable | `activeDeadlineSeconds: 120`, `backoffLimit: 0` |
| End to end thread | Recorded projection | Not applicable | 40 seconds |

Wait loops poll every 2 seconds and check current generation or current release revision, not a stale Ready condition. Cold `task dev` has a 20 minute outer deadline. Warm `task dev` and service redeploy have an 8 minute outer deadline. Builds do not retry. Registry pushes retry twice after 2 and 5 seconds. Task deletes a successful Job after capturing its summary. It leaves every failed Job and Pod until the next confirmed clean or an explicit named cleanup. No Job uses `ttlSecondsAfterFinished`.

One `task dev` invocation creates a lowercase run ID from the first 12 hexadecimal characters of a new UUIDv7 generated by the existing Go UUID dependency. Migration names are `migrate-<service>-<run-id>`. The four independent Jobs start in parallel, and Task waits for all of them. Rerunning after success creates new Jobs and goose reports no pending migration. Rerunning after partial success creates a new run, with completed databases remaining at their recorded goose version and only pending databases applying work.

## Feature design

### Platform topology

| Resource | Kind | Replicas | Persistent storage | Exposed outside namespace |
|---|---|---:|---|---|
| `web` | Deployment | 1 | None | Through Ingress only |
| `gateway` | Deployment | 1 | None | Through Ingress only |
| `identity`, `teaching`, `billing`, `notifications` | Deployment | 1 each | None | No |
| Four Postgres services | StatefulSet | 1 each | One PVC each | No |
| `redpanda` | StatefulSet | 1 | One PVC | No |
| `garage` | StatefulSet | 1 | Metadata PVC and data PVC | No |
| Four migration runs | Job | One per startup or redeploy | None | No |
| Garage initialization | Job | One idempotent run | Garage PVCs | No |
| Development token | Job | On demand | None | No |
| Packaged Traefik | K3s packaged Deployment and Service | 1 locally | None | Local port 80 |

There is no application schema change. Existing Postgres schemas remain authoritative. Kubernetes resources map the eight fixed PVC names to deterministic component directories through StorageClass `vermouth-local`. The local node name is `k3d-vermouth-server-0`.

| PV and claim | Directory under `/var/lib/vermouth` | Request |
|---|---|---:|
| `postgres-identity` | `postgres-identity` | `2Gi` |
| `postgres-teaching` | `postgres-teaching` | `2Gi` |
| `postgres-billing` | `postgres-billing` | `2Gi` |
| `postgres-notifications` | `postgres-notifications` | `2Gi` |
| `redpanda` | `redpanda` | `4Gi` |
| `garage-metadata` | `garage-metadata` | `1Gi` |
| `garage-data` | `garage-data` | `5Gi` |
| `traefik-acme` | `traefik-acme` | `256Mi` |

Every PV uses `local.path`, reclaim policy `Retain`, volume mode `Filesystem`, access mode `ReadWriteOnce`, StorageClass `vermouth-local`, and node affinity for `k3d-vermouth-server-0`. Bootstrap creates the directories with the numeric owner required by the matching workload before applying PVs. Local clean removes the Docker named data volume only after typed confirmation.

### State transitions

1. Platform lifecycle: `absent` to `bootstrapped` to `running` to `stopped` to `running`.
2. Destructive lifecycle: `stopped` or `running` to typed confirmation to `absent`.
3. Release lifecycle: `source` to `images pushed` to `foundation ready` to `Garage ready` to `migrations complete` to `application ready`.
4. Failure before migrations leaves both releases unchanged except for a safe foundation reconciliation. Migration failure leaves the old application and Secret references running. Readiness failure rolls application resources back and keeps the foundation plus forward schema.

### Command surface

| Command | Inputs | Success output | Important failures |
|---|---|---|---|
| `task platform:bootstrap` | Committed platform config, installed tools, `.env` | Cluster, registry, packaged Traefik, Ingress prerequisites | Missing tool, low Colima resources, context collision, version drift |
| `task dev` | Worktree, `.env`, local values | Ready workloads and `http://vermouth.localhost` | Build, registry, Secret, migration, Helm, probe, or timeout failure |
| `task dev:redeploy -- <workload>` | Allowed workload name | New digest and ready workload | Invalid name, build, migration, or rollout failure |
| `task images:multi` | All Dockerfiles | Verified two platform manifest for every image | Builder lacks platform, build failure, missing manifest entry |
| `task platform:doctor` | Host and cluster state | Version, capacity, context, registry, controller report | Any unsupported or drifted prerequisite |
| `task platform:status` | Exact context | Both Helm releases, Gateway, every supported log target, Job, PVC, registry, and readiness summary | Wrong context or unreachable cluster |
| `task platform:logs -- <target> [--previous|--follow|--since <duration>]` | Named target and optional mode | Current or requested logs | Invalid target or missing workload |
| `task platform:stop` | Exact context | Stopped cluster and registry with their data retained | Wrong context or stop timeout |
| `task platform:recreate` | Exact context and typed `vermouth` | Empty cluster recreated from committed configuration | Wrong confirmation or target |
| `task platform:clean` | Exact context and typed `vermouth` | Removed cluster and persistent data | Wrong confirmation or target |
| `task platform:jobs:clean -- <job>` | Exact failed Job name | Named Job and Pod removed | Unknown or running Job |
| `task dev:token` | Optional `--email`, `--name`, `--timezone`, `--language` flags | JSON with `tutor_id`, `email`, `access_token`, `access_expires_at` | Invalid flags, Job, database, or signer failure |
| `task thread` | Ready platform | Recorded identity event through notifications | Gateway, relay, broker, or consumer timeout |

The log and status target inventory is fixed: `web`, `gateway`, `identity`, `teaching`, `billing`, `notifications`, `postgres-identity`, `postgres-teaching`, `postgres-billing`, `postgres-notifications`, `redpanda`, `garage`, `traefik`, `registry`, `garage-init`, `devtoken`, and `migrate-<service>-<run-id>`. `all` visits every applicable target. Failed Jobs remain addressable until the named cleanup or cluster clean.

`dev:token` defaults `name` to `Tracer Tutor`, `timezone` to `Asia/Ho_Chi_Minh`, `language` to `vi`, and generates a unique `tutor-<nanoseconds>@example.com` email when none is supplied. Its schema version 1 JSON requires `tutor_id`, `email`, `access_token`, and `access_expires_at`. It prints no other stdout content, so `task thread` can consume it directly.

### HTTP surface

| Host and path | Destination | Access | Source of behavior |
|---|---|---|---|
| `PathPrefix /api` | Gateway service | Local host | Prefix is preserved, with no rewrite |
| `Exact /health` | Gateway service | Local host | Path is preserved |
| `Exact /ready` | Gateway service | Local host | Path is preserved |
| `Exact /config.json` | Web service | Local host | Mounted ConfigMap response with `Cache-Control: no-store` |
| `PathPrefix /` | Web service | Local host | Final catch all rule, with Nginx SPA fallback |

All rules use hostname `vermouth.localhost` on Traefik entrypoint `web` at port 80. Exact matches outrank prefixes, and longer prefixes outrank `/`. No route rewrites a path. `/config.json` is exactly `{"schemaVersion":1,"environment":"local","googleAuthEnabled":<boolean>,"apiBasePath":"/api"}`.

### Value sourcing

| Action | Value produced | Source |
|---|---|---|
| Bootstrap | Cluster name, k3s version, port map, registry name | Committed k3d configuration |
| Bootstrap | Traefik image, chart, replicas, resources, and log format | Exact k3s version plus committed `HelmChartConfig` |
| Bootstrap | static PV names, paths, node affinity, and reclaim policy | Fixed eight component inventory, root `/var/lib/vermouth`, node `k3d-vermouth-server-0`, and StorageClass `vermouth-local` |
| Doctor | Minimum CPU, memory, and compatible tool versions | Committed platform requirements |
| Native build | Image tag and digest | Buildx content tag plus local registry push result |
| Multiplatform build | Required platform entries | Fixed list `linux/arm64,linux/amd64` and Buildx manifest inspection |
| Multiplatform build | Required image set | Gateway, identity, teaching, billing, notifications, web, Garage initializer, and four matching migration targets from the committed image inventory |
| Secret creation | Application credentials and signing keys | Existing `.env`, plus generated missing local secrets written only to ignored `.env` |
| Secret naming | Immutable application Secret suffix | First 12 characters of the SHA256 over canonical sorted key and value bytes |
| Migration | Database target and SQL set | The named service Secret and its `db/migrations/` directory |
| Helm upgrade | Image references | Digest file produced by the completed build stage |
| Web startup | Google sign in availability | Public `googleAuthEnabled` value in the local ConfigMap, derived from explicit local auth configuration |
| Web startup | schema version, environment, API path | Fixed runtime config schema version 1, local values, and same origin `/api` contract |
| Platform readiness | Component state | Kubernetes conditions, Traefik availability, Ingress existence, Job completion, PVC binding, and existing `/health` plus `/ready` responses |
| Local address | `http://vermouth.localhost` | Committed Ingress and k3d port mapping |
| Garage initialization | Node layout, bucket, S3 key, grants | Fixed Garage desired state in this spec and `billing-s3-bootstrap` |
| Clean | Exact deletion target | Verified `k3d-vermouth` context, cluster identity, and typed `vermouth` confirmation |
| Drift decision | Reconcile or require recreate | Committed immutable field list and the live `vermouth-platform-identity` ConfigMap |

### Secret lifecycle

Stable foundation Secrets are `postgres-<service>-bootstrap`, `garage-bootstrap`, and `billing-s3-bootstrap`. Redpanda has no local authentication Secret. Bootstrap annotates each Secret with the SHA256 of its canonical source values. If a live foundation Secret hash differs from `.env`, doctor refuses normal startup because changing a bootstrap password or imported storage key does not rotate stored data. Confirmed recreate is the local recovery path.

Application Secrets are immutable and content named, such as `identity-runtime-<hash>`. Task creates the new names before migrations, passes the service migration Job its new database URL, and changes Deployment references only inside the application Helm upgrade. Old Pods keep their process environment and old Secret references. After a successful release, Task keeps the Secrets referenced by the current and previous Helm revisions and removes older unreferenced application Secrets. It never changes stable foundation Secrets.

`.tmp/platform/runtime-values.yaml` contains Secret names only. Database URLs are derived from committed service DNS names, database names, roles, and the foundation password source. They are not copied from the host port URLs in `.env`.

### Cluster identity and drift

Bootstrap writes ConfigMap `vermouth-platform-identity` with schema version, cluster name, k3s live version, k3s image tag, server count, agent count, packaged Traefik image and chart, static storage root and node name, host port map, registry endpoints, k3d config SHA256, Traefik config SHA256, and image lock SHA256. Doctor requires current context `k3d-vermouth`, matching k3d ownership labels, the expected namespace identity, and the matching ConfigMap.

Changes to k3s version, node count, port map, Traefik state, registry endpoints, or local path layout require confirmed recreate. Changes to application images, resource values, probes, routes, policies, or nonsecret ConfigMaps reconcile through Helm. A busy local port 80 or 5111, an unknown same name cluster, or a foundation Secret hash mismatch stops before mutation.

### Redpanda desired state

Redpanda uses the pinned image and these exact cluster only settings:

```text
--node-id=0
--smp=1
--overprovisioned
--memory=1G
--reserve-memory=0M
--check=false
--default-log-level=warn
--kafka-addr=internal://0.0.0.0:9092
--advertise-kafka-addr=internal://redpanda:9092
--rpc-addr=0.0.0.0:33145
--advertise-rpc-addr=redpanda:33145
--set redpanda.auto_create_topics_enabled=false
```

Kafka port 9092 and admin port 9644 are cluster only. The data PVC mounts `/var/lib/redpanda/data`. Identity, teaching, and billing services call the existing `EnsureTopics` path before their relay or consumers start. The resulting inventory is `identity.events`, `teaching.events`, `billing.events`, and each matching `.dlq`, all with three partitions and `retention.ms=-1`. No separate topic Job may replace STK-16.

### Garage desired state

The committed desired state is zone `local`, capacity `5G`, replication factor one, metadata directory `/var/lib/garage/meta`, data directory `/var/lib/garage/data`, S3 port 3900, RPC port 3901, admin port 3903, bucket `vermouth-invoices`, key name `billing`, and private bucket access. Bootstrap generates missing fixed access and secret key values into ignored `.env`; the billing foundation Secret is their source.

The initializer reads the live node ID, layout version, bucket, key, and grants before changing anything. It assigns the node only when its zone or capacity is absent, applies exactly the next layout version only when the layout differs, creates the bucket only when absent, imports the fixed key only when absent, and grants read plus write only when missing. Matching state is success. A different secret for the same access key, an unexpected bucket owner, or a conflicting layout fails without overwriting. The next `task dev` resumes from the first missing step after interruption.

### Key invariants

1. Platform commands mutate only the exact `k3d-vermouth` context and never switch context silently.
2. Image digests, not mutable tags, select the running application bits.
3. Build, push, foundation readiness, Garage initialization, migration, and application upgrade happen in that order. A later stage never begins after an earlier failure.
4. Database down migrations never run automatically. Every forward migration must remain compatible with the previous application revision.
5. Each service and migration Job receives only its own database URL. Only billing receives Garage S3 credentials.
6. Redpanda keeps automatic topic creation off, unlimited time retention, three partitions, one core, and its existing memory tuning.
7. `notifications` remains exactly one replica. Its advisory lock belongs to the digest feature, where scheduled sending begins.
8. The local registry, databases, Redpanda, Garage, and service ports are not exposed through Ingress.
9. Compose remains the integration test platform. Kubernetes manifests may not replace real broker and database tests with mocks.
10. No startup path installs tools through Homebrew, deletes data, chooses an unpinned latest version, or rewrites an existing `.env` value.
11. Application Deployments use RollingUpdate with `maxUnavailable: 0` and `maxSurge: 1`. A replacement Pod receives traffic only after readiness at the current revision.
12. A failed deployment may leave unreferenced registry content and new unreferenced Secrets. It may not change the running application release, its Secret references, or the migration history after a failed migration.

### Security model

The platform is local only and serves plain HTTP on `vermouth.localhost`. Public access, TLS, production secret storage, image signing, backups, and Azure network policy belong to feature 16.

Every application Pod uses a named ServiceAccount with no RBAC permissions and `automountServiceAccountToken: false`. Jobs follow the same rule. Packaged Traefik keeps the RBAC owned by k3s in `kube-system`. Default deny NetworkPolicies enforce service ownership, including one allowed database client set per Postgres instance. The local registry is reachable only by the host and cluster network.

Standard Kubernetes NetworkPolicy cannot filter an external destination by DNS name. The identity exception therefore allows outbound TCP port 443 to external addresses when Google auth is enabled. Application level OAuth configuration still fixes the actual Google endpoints. A later CNI with DNS policy support would be a feature 16 decision, not a hidden local dependency.

The chart renders this exact traffic matrix. Selectors use `app.kubernetes.io/name` and `app.kubernetes.io/component`. Cross namespace rules also require the standard namespace name label.

| Source | Destination | Ports | Reason |
|---|---|---|---|
| Every Vermouth Pod and Job | CoreDNS Pods in `kube-system` | UDP and TCP 53 | Service discovery |
| Traefik Pods in `kube-system` selected by `app.kubernetes.io/name=traefik` | `web` and `gateway` | TCP 8080 | Public local entry |
| `gateway` | `identity`, `teaching`, `billing`, `notifications` | TCP 8081, 8082, 8083, 8084 respectively | Existing gateway upstreams and readiness |
| Each Go service | Its own Postgres | TCP 5432 | Service owned database |
| Each `migrate-<service>` Job | Its own Postgres | TCP 5432 | Service owned migration |
| `devtoken` Job | Identity Postgres | TCP 5432 | Local development tutor creation |
| `identity`, `teaching`, `billing`, `notifications` | Redpanda | TCP 9092 | Publish and consume events |
| `billing` | Garage | TCP 3900 | S3 API |
| `garage-init` | Garage | TCP 3903 | Administrative reconciliation |
| `identity`, only when Google auth is enabled | Public IPv4 except RFC 1918 and link local ranges | TCP 443 | OAuth and Google token verification |

Every other ingress and egress path is denied. Postgres, Redpanda, Garage, and internal service ports have no Ingress route. The identity external rule excludes `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, and `169.254.0.0/16`.

The workload security matrix is also fixed. A pinned image must pass its stated numeric identity before its digest can enter `deploy/images.lock.yaml`.

| Workload | ServiceAccount | User and group | Root filesystem and writable paths | Token mount |
|---|---|---|---|---|
| Go applications | Matching workload name, no RBAC | `65534:65534` | Read only, writable memory backed `/tmp` only when required | Off |
| Migration and devtoken Jobs | Matching Job family, no RBAC | `65534:65534` | Read only, no writable path beyond bounded `/tmp` | Off |
| Web | `web`, no RBAC | `101:101` | Read only, `emptyDir` at `/var/cache/nginx`, `/var/run`, and `/tmp` | Off |
| Postgres | Matching database name, no RBAC | `70:70`, `fsGroup: 70` | Read only, PVC at `/var/lib/postgresql`, `emptyDir` at `/var/run/postgresql` and `/tmp` | Off |
| Redpanda | `redpanda`, no RBAC | `101:101`, `fsGroup: 101` | Read only where the pinned image permits, PVC at `/var/lib/redpanda/data`, `emptyDir` at `/tmp` | Off |
| Garage | `garage`, no RBAC | `1000:1000`, `fsGroup: 1000` | Read only, PVCs at the configured metadata and data paths, `emptyDir` at `/tmp` | Off |
| Packaged Traefik | K3s packaged account and RBAC | Pinned image identity | Official writable paths, explicit requests and limits | Only packaged controller permissions |

All entries set `allowPrivilegeEscalation: false`, `capabilities.drop: [ALL]`, and `seccompProfile.type: RuntimeDefault`. If an upstream image cannot run with its listed identity and mounts, implementation stops and returns to this matrix instead of silently granting root or a writable root filesystem.

Secrets are base64 encoded Kubernetes Secrets, not encrypted application storage. That is acceptable only because the cluster is local and disposable. Secret values stay out of Helm release values and command output. Production must replace this source before any public deployment.

When `VERMOUTH_GOOGLE_AUTH_ENABLED=false`, identity does not require the three Google variables. Auth start and callback return the standard `APIError` with HTTP 503 and code `auth_unavailable`. `/config.json` returns `{"googleAuthEnabled":false}` with `Cache-Control: no-store`. The disabled button uses `aria-describedby` and this fixed message in the active language: English, `Google sign in is not configured for this local environment. Use task dev:token for the development thread.` Vietnamese, `Đăng nhập Google chưa được cấu hình cho môi trường cục bộ này. Hãy dùng task dev:token để chạy luồng phát triển.`

When the flag is true, all three Google variables are required and the redirect is exactly `http://vermouth.localhost/api/auth/google/callback`. The OpenAPI document records the 503 response, and the browser runtime config changes without rebuilding the web image.

### Configuration required

1. `VERMOUTH_GOOGLE_AUTH_ENABLED`: public local runtime flag. `false` lets the platform start without OAuth secrets.
2. `IDENTITY_GOOGLE_CLIENT_ID`, `IDENTITY_GOOGLE_CLIENT_SECRET`, `IDENTITY_GOOGLE_REDIRECT_URL`: required only when Google auth is enabled.
3. `GARAGE_RPC_SECRET`, `GARAGE_ADMIN_TOKEN`: generated local Garage control secrets.
4. `BILLING_S3_ENDPOINT`, `BILLING_S3_REGION`, `BILLING_S3_BUCKET`, `BILLING_S3_ACCESS_KEY_ID`, `BILLING_S3_SECRET_ACCESS_KEY`: billing only object storage configuration.
5. `deploy/platform/versions.yaml`: host and cluster compatibility source. It contains no secret.
6. `deploy/images.lock.yaml`: tag, digest, and architecture source for every external or built image. It contains no registry credential.
7. Existing database, broker, service address, signing key, token public key, and application settings remain environment variables, but their local cluster values use service DNS names instead of host ports.

### Critical test scenarios

1. Happy path: from a stopped Colima profile and an initialized checkout, bootstrap and `task dev` reach `vermouth.localhost`, then `task thread` records the identity event, verifying **AC-1**, **AC-2**, **AC-4**, and **AC-13**.
2. Cold start order: from no cluster, observe the foundation release become ready before Garage initialization, four migration Jobs, and the application release, verifying **AC-2**, **AC-3**, and **AC-11**.
3. Full topology: every workload, Job, Gateway condition, Secret reference, NetworkPolicy, security context, resource value, and PVC matches the committed matrices and readiness is green, verifying **AC-3**, **AC-9**, **AC-10**, **AC-16**, and **AC-17**.
4. Persistence: write distinct markers to all four databases, Redpanda, and Garage, redeploy and stop then start the cluster, and read every marker again, verifying **AC-5**.
5. Test isolation: run the existing real infrastructure integration suite through Compose with the k3d cluster stopped, verifying **AC-6**.
6. Inner loop: change one service, redeploy it, confirm only its digest and rollout change, then confirm its idempotent migration Job ran, verifying **AC-7**.
7. Portability: run `task images:multi`, inspect every required manifest, then pull and start one health capable image under each architecture, verifying **AC-8**.
8. Failure before mutation: break one build and one registry push, then confirm migrations, Secret references, and releases do not change even if unreferenced registry content exists, verifying **AC-11**.
9. Interrupted release: interrupt after new Secret creation, after one migration completes, and after all migrations but before Helm. Each rerun resumes safely, keeps the old application usable, and never runs a down migration, verifying **AC-11** and **AC-12**.
10. Failed rollout: break a readiness probe, confirm `maxUnavailable: 0` kept the old Pod serving until replacement readiness failed, then confirm the application release and Secret references roll back while the foundation stays, verifying **AC-11**.
11. Garage recovery: interrupt after layout, bucket, key, and grant in separate runs, then confirm reconciliation creates no duplicate and refuses conflicting key material, verifying **AC-16**.
12. Capacity and collision: exhaust a PVC, lower available Colima memory, occupy port 80, and occupy port 5111 in separate runs. Each case stops with the named diagnostic and preserves data, verifying **AC-12**, **AC-15**, and **AC-17**.
13. Auth disabled: remove Google OAuth credentials, start successfully, observe the fixed accessible unavailable message and no store runtime config, then complete the development token thread, verifying **AC-14**.
14. Resource envelope: start the full platform in a 4 CPU and 8 GiB Colima profile, capture five minutes of use, and confirm the committed aggregate request and observed ceilings, verifying **AC-15**.
15. Destructive guard: use the wrong context and wrong typed confirmation, confirm no deletion, then confirm clean removes the cluster and confirm recreate removes the old data before producing an empty ready cluster, verifying **AC-5** and **AC-12**.

## Build plan

The Tracer Bullet approach puts one real request through the cluster before the platform is thickened.

1. Build the first complete cluster thread: replace Envoy resources and installation with pinned packaged Traefik configuration and one Ingress, align local k3s to `v1.36.3-k3s1`, commit version and image locks, doctor, bootstrap, registry contract, one chart with foundation and application gates, resources, probes, deadlines, security, traffic, routes, multi architecture Dockerfile inputs, web runtime, identity and notifications Postgres, Redpanda, content named Secrets, two migration Jobs, the native development token image and Job, `task dev`, and `task thread`, satisfies **AC-1**, **AC-2**, **AC-4**, **AC-8**, **AC-9**, **AC-10**, **AC-13**, and **AC-17**.
2. Thicken the same two releases with teaching and billing, their separate Postgres StatefulSets and PVCs, migration images and Jobs, remaining content named Secrets, exact policy rows, probes, resource values, rolling strategy, and full gateway readiness, satisfies **AC-3**, **AC-9**, **AC-10**, and **AC-17**.
3. Add Garage with separate metadata and data PVCs, its idempotent initializer, private invoice bucket, billing only key, stop and guarded clean behavior, then prove all persistent markers survive normal lifecycle operations, satisfies **AC-5** and **AC-16**.
4. Complete the inner loop and failure state machine: service redeploy, build input hashes, digest records, versioned Secret retention, bounded waits, build and push gates, parallel migration failure preservation, application only Helm rollback, full status and log inventory, config hash drift detection, failed Job cleanup, and destructive confirmed recreate, satisfies **AC-7**, **AC-11**, and **AC-12**.
5. Finish the portability and resource proof: push and inspect every required two platform manifest, pull under both architectures, validate chart schema and both release renderings, exercise interruption, disk, port, Garage, and rollout failures, run the Compose isolation scenarios, measure the committed aggregate ceilings, and document the exact daily commands, satisfies **AC-6**, **AC-8**, **AC-14**, **AC-15**, and **AC-17**.

## Consequences

**Positive**:

1. One command proves the real cluster path while the fast, known Compose test path remains available.
2. Immutable image digests and ordered migration gates make the running state explainable after a failed build.
3. The same Dockerfiles create native Apple silicon images and `amd64` images for the later Azure move.
4. Default deny networking makes the database ownership rules physical inside Kubernetes.

**Negative / tradeoffs**:

1. Packaged Traefik ties routing to the Kubernetes Ingress model and k3s chart values rather than Gateway API portability.
2. Four Postgres StatefulSets, Redpanda, Garage, Traefik, and the k3s control plane consume meaningful laptop memory before product work starts.
3. Local path PVCs survive cluster stop but not the confirmed cluster deletion. There is no backup or disaster recovery in this feature.
4. Forward only migration recovery requires every schema change to remain compatible with the previous application revision.
5. Local Kubernetes Secrets are not a production secret system. Feature 16 must replace their source before public deployment.
6. Helm rollback restores Kubernetes resources, not database changes, broker events, object writes, or other business effects a briefly ready Pod already produced. Idempotency and forward compatibility remain the recovery tools.
7. The registry retains unreferenced content until confirmed clean or recreate, so repeated development builds consume disk.

**Neutral**:

1. The local and test platforms hold separate data. Moving a Compose database into Kubernetes is not supported or needed.
2. `task dev` changes meaning from host processes plus Compose to the k3d platform. The `task infra:*` family keeps its current test meaning.
3. Spec 0002 STK-21 still requires migrations before service startup, but this spec replaces its future init container note with explicit Jobs. STK-23 remains one replica here, and the unused advisory lock moves to the digest feature.

## Follow-up

1. `kubernetes-specialist` conventions are not yet captured. `deploy/AGENTS.md` should contain them, with a short root `AGENTS.md` pointer, before implementation begins.
2. `helm-chart-scaffolding` conventions are not yet captured. `deploy/AGENTS.md` should contain them, with a short root `AGENTS.md` pointer, before implementation begins.
3. No high confidence Traefik Agent Skill was found. Kubernetes and Helm skills govern the packaged add on and Ingress implementation.
4. `docker-buildx` conventions are not yet captured. Root `AGENTS.md` should name the two architecture image workflow because every production Dockerfile follows it.
5. `docker-security` conventions are not yet captured. Root `AGENTS.md` should name the image hardening baseline because every production Dockerfile follows it.
6. `s3-storage` conventions are not yet captured. `services/billing/AGENTS.md` should contain the Garage S3 bucket and credential rules before invoice storage is implemented.
7. Feature 9 should attach Traefik metrics and tracing to the central observability stack. Feature 5 keeps structured access logs only.
8. Feature 16 owns Azure DNS, TLS, production Secrets, private Docker Hub, dedicated disk storage, manual export, and production values. It reuses these images and this chart.
9. The first implementation measurement should compare the full ready state with spec 0002's memory estimate. If four Postgres instances do not fit, return to `$architect` before using the named shared instance fallback.

## Migration plan

**Strategy**: Strangler. The Kubernetes development path lands beside the existing Compose and host process path, then becomes the daily default after the real thread passes.

**Phases**:

1. Add bootstrap, cluster, chart, images, and a temporary `task dev:kubernetes` while the existing `task dev` remains unchanged.
2. Prove the end to end thread and persistence scenarios on k3d, then point `task dev` at Kubernetes and retain the old host startup as `task dev:host` for one milestone.
3. Remove `task dev:host` after the Kubernetes verification passes. Keep `task infra:*` and Compose permanently for integration tests.

**Rollback**: Before phase 3, point `task dev` back to the host workflow and stop the k3d cluster. Compose data is untouched because the two platforms never share volumes. After phase 3, Git can restore the task alias while the k3d data remains available for diagnosis.

**Risks**: The two temporary development paths can drift during phase 2. Keep that phase to one milestone and run the same `task thread` against both before cutover.

## Rationale

Reasoning, options, and references: see [rationale.md](rationale.md).
