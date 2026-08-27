# 0005. Local Kubernetes platform and one command startup

**Date**: 2026-08-26
**Status**: Accepted

## Summary

Vermouth will run locally on your Apple silicon Mac in one k3d cluster. One daily command builds the current images, runs migrations, starts every workload, waits for health, and exposes the app at `http://vermouth.localhost:8080`. The application and registry use unprivileged IPv4 loopback ports so Colima can preserve their exact host binds without a profile change. Local and production use the same k3s generation, packaged Traefik edge, chart, and image set. The existing Compose stack stays as the integration test platform, while a separate image command proves every deployable image supports both `linux/arm64` and `linux/amd64`. Successful publication removes temporary Vermouth host images, and an explicit confirmed command owns shared BuildKit cache cleanup so the platform cannot silently consume the developer's SSD.

## Requirements

**User stories**:

1. As the developer, I want one command to start the whole system so I can work on product behavior instead of operating each process.
2. As the future operator, I want the same application images to support Apple silicon and an `amd64` Azure VM so moving hosts does not require new Dockerfiles.
3. As the developer, I want failures to preserve the last usable release and its data so recovery starts from evidence rather than a clean slate.

**Acceptance criteria**:

1. **AC-1**: On macOS with Colima, k3d, kubectl, Helm, Docker Buildx, Task, Go, Node, and pnpm installed, `task platform:bootstrap` starts Colima when needed, validates at least 4 CPUs and 8 GiB, creates missing local configuration and signing keys without overwriting existing values, installs web dependencies, creates the `vermouth` k3d cluster and its local registry, keeps packaged Traefik, and reconciles the pinned Traefik configuration. Running it again changes nothing when the desired state already exists.
2. **AC-2**: `task dev` builds native `linux/arm64` images, pushes immutable content tags through `127.0.0.1:5111`, records their remote manifest digests with cluster pull references under `vermouth-registry:5000`, creates content named application Secrets from `.env`, installs or upgrades the `vermouth-foundation` Helm release, runs Garage initialization and four bounded migration Jobs, upgrades the `vermouth` application release, waits for every required condition, and prints `http://vermouth.localhost:8080` only after the platform is ready. Both releases render from the same chart. The command does not alter the Colima profile or Docker daemon configuration.
3. **AC-3**: The ready platform contains the web app, gateway, identity, teaching, billing, notifications, four separate Postgres 18 instances, one tuned Redpanda node, one Garage node, one packaged Traefik replica, and one ready local registry whose host and cold node transport checks pass. Every application workload has one replica, and `notifications` cannot be configured above one replica in this feature.
4. **AC-4**: `http://vermouth.localhost:8080` serves the web app, routes `/api`, `/health`, and `/ready` through one Kubernetes Ingress to the gateway, routes `/config.json` and `/` to web, preserves every path, and keeps browser API calls on the same origin. Gateway readiness reports every Go service ready.
5. **AC-5**: Identity, teaching, billing, notifications, Redpanda, and Garage data survive pod replacement, service redeploy, `task platform:stop`, and the next `task dev`. Bootstrap precreates the two exact Docker volumes with their committed identity labels and refuses an unlabeled or mismatched existing volume. `task platform:clean` and `task platform:recreate` both show the verified target and require the typed value `vermouth` because deleting the k3d node deletes its local path data. Clean stops there. Recreate then bootstraps an empty cluster.
6. **AC-6**: `test/compose.test.yaml` and the existing `task infra:*` commands remain the real Postgres and Redpanda integration test platform. Kubernetes becomes the normal development platform without making `task test` depend on a cluster.
7. **AC-7**: `task dev:redeploy -- <workload>` accepts `gateway`, `identity`, `teaching`, `billing`, `notifications`, or `web`. It builds and pushes only that native image, runs the named service migration Job when the target owns a database, upgrades only the affected image digest, and waits for that workload.
8. **AC-8**: `task images:multi` builds the gateway, four services, web runtime, Garage initializer, and four service migration images for `linux/arm64` and `linux/amd64` into local staged archives before any push, publishes one manifest list per image through `127.0.0.1:5111`, and fails unless remote inspection finds both platforms. Go builds use `TARGETOS` and `TARGETARCH`, and the web runtime uses a pinned two architecture base. The local only development token image is not a deployment artifact and is excluded. After successful native or multiple architecture publication, Task removes every unused host image carrying the `vermouth.dev/workload` label. `task platform:clean` performs the same project scoped image cleanup. Shared BuildKit cache is never removed automatically. `task platform:cache:clean` reports its size, names builder `colima`, requires typed confirmation `vermouth`, prunes that shared cache, and reports the reclaimed amount.
9. **AC-9**: The `vermouth` namespace starts with deny all NetworkPolicies. The committed traffic matrix names every allowed source selector, destination selector, namespace, port, protocol, stable rule ID, rendered policy name, and rule order for packaged Traefik, web, gateway, all services, all Jobs, four Postgres instances, Redpanda, Garage, DNS, and identity outbound OAuth traffic. Validation derives the policy count from unique policy names and requires a bijection between source rows and rendered rules. A rendered policy outside that matrix fails chart validation.
10. **AC-10**: Every container matches the committed workload matrix for requests, limits, probes, numeric user and group, ServiceAccount, token mounting, writable mounts, and any read only root filesystem exception. All Pods use `RuntimeDefault` seccomp, drop Linux capabilities, and prevent privilege escalation. Vermouth and web images run as nonroot users.
11. **AC-11**: A build failure changes no registry, migration, Secret reference, or Helm state. A partial registry push may leave unreferenced content but changes no migration or release. A migration failure leaves the previous application release and its content named Secrets running, retains failed Job logs, and never runs a down migration. A failed application readiness check rolls only the `vermouth` application release back while preserving the foundation release and any forward migration already applied.
12. **AC-12**: `task platform:doctor`, `platform:status`, `platform:logs`, `platform:stop`, `platform:recreate`, and `platform:clean` pass explicit context `k3d-vermouth` to every Kubernetes request and never read or change the user's current context. A running cluster proves identity through Kubernetes and Docker. An absent or stopped cluster proves identity through k3d metadata, Docker labels, named volume labels, and committed hashes. Mutating commands refuse an unknown or mismatched target before mutation, use bounded deadlines, and point a failure to the next useful status or log command. Only `platform:recreate` may replace a known cluster after typed confirmation.
13. **AC-13**: `task dev:token` runs the development token program as a short lived Kubernetes Job whose image is not a production service image and returns schema version 1 JSON. `task thread` then reaches the gateway through `http://vermouth.localhost:8080` and observes the identity event in notifications.
14. **AC-14**: When Google OAuth credentials are absent, explicit local configuration keeps the platform ready, the web app shows Google sign in as unavailable with a clear message, and the development token thread still works. When Google OAuth is enabled, `IDENTITY_GOOGLE_REDIRECT_URL` must equal `http://vermouth.localhost:8080/api/auth/google/callback` or validation fails before any image, Secret, Job, or Helm mutation. Enabling Google OAuth requires configuration only, not different manifests.
15. **AC-15**: On the 4 CPU and 8 GiB Colima profile, steady declared requests stay at or below 1 CPU and 2.5 GiB, the measured ready platform working set including k3s and the registry stays at or below 7 GiB, and five minute CPU use stays below 3.5 CPUs. Total CPU sums Docker CPU for only the k3d server, load balancer, and registry. Kubernetes Pod CPU is attribution only and is never added. Redpanda remains at one core and 1 GiB of application memory. The result is recorded. The build never collapses the four databases without a later spec change.
16. **AC-16**: An idempotent Garage initialization Job reconciles zone `local`, Garage layout capacity `5G`, Kubernetes data claim `5Gi`, replication factor one, bucket `vermouth-invoices`, fixed key name `billing`, the key values sourced from the billing foundation Secret, and bucket read plus write grants without owner permission. It inspects before every change, resumes after interruption, refuses conflicting layout, key, bucket, or grant state, and creates no duplicate. It proves live key material and grants with signed S3 Put, Get, and Delete requests for one unique probe object, then leaves no probe object behind.
17. **AC-17**: `deploy/platform/versions.yaml` pins compatible host tools, Colima profile `default`, Docker context and Buildx builder `colima`, Buildx driver `docker`, BuildKit `v0.30.0`, k3s, the packaged Traefik image and chart generation, every separate k3d, Docker, containerd, volume, host, and cluster registry identity, and both host port bindings. `deploy/images.lock.yaml` pins every external image and every repository build target with its repository path, inputs, build arguments, stages, ordered platforms, canonical hash schema, separate tag templates, and digest provenance. `deploy/platform/images.schema.json`, `deploy/platform/secrets.schema.json`, and `deploy/helm/vermouth/values.schema.json` own generated image, Secret, and runtime state. The chart, local registry, migration targets, and development token image all resolve through those files. `platform:doctor` reports version, daemon, builder, endpoint, listener, profile and daemon configuration hashes, recorded cold pull proof, identity, registry readiness, and generated state drift without mutating images or silently upgrading.

## Decision

**Chosen option**: Option 2: k3d development beside the Compose test platform

Add a one node k3d platform for daily development, package Vermouth in one Helm chart, use the packaged k3s Traefik release through one Kubernetes Ingress, distribute local images through a k3d managed registry, and preserve Compose for integration tests (basis: the existing Task and Compose workflow in `AGENTS.md`, specs 0002 and 0006, the strangler pattern, and official k3s packaged Traefik guidance).

Host Docker addresses the registry only as `127.0.0.1:5111`. K3d publishes that port on IPv4 loopback only. Kubernetes addresses the same registry by the k3d managed Docker network name `vermouth-registry:5000`. Task requires that exact name in the k3d registry inventory, Docker network DNS names, and generated containerd mirror before it proves a node pull. It resolves the remote manifest digest after the host push, proves the node can pull the same repository path and digest, then writes the cluster reference into runtime values. No command edits the Colima profile, adds a Docker daemon insecure registry, installs a host certificate, or places the host endpoint in a workload.

K3d publishes the application through `127.0.0.1:8080`, an unprivileged host port, to Traefik port 80 inside the load balancer. Port 80 is not published on the Mac. This avoids Colima's privileged port forwarding path, which can turn an exact Docker loopback bind into an IPv6 wildcard listener. Doctor requires the exact Docker bind and macOS IPv4 loopback listener. A wildcard IPv4 listener or any IPv6 listener is drift. The canonical local origin includes `:8080` everywhere, including Google OAuth callback configuration.

**Implementation skills**: `kubernetes-specialist` (`jeffallan/claude-skills`, `.agents/skills/kubernetes-specialist/`) · `helm-chart-scaffolding` (`wshobson/agents`, `.agents/skills/helm-chart-scaffolding/`) · `docker-buildx` (`full-stack-skills/docker-skills`, `.agents/skills/docker-buildx/`) · `docker-security` (`full-stack-skills/docker-skills`, `.agents/skills/docker-security/`) · `s3-storage` (`terminalskills/skills`, `.agents/skills/s3-storage/`) · `go-goose` (`metalagman/agent-skills`, `.agents/skills/go-goose/`)

### Platform choices

| Layer | Choice | Build contract |
|---|---|---|
| Local container runtime | Existing Colima profile | Bootstrap may start and validate it, but may not install it or overwrite its profile |
| Local Kubernetes | k3d `v5.9.0` with k3s `v1.36.3-k3s1`, one server, no agents | The committed k3d config names the cluster `vermouth`, keeps packaged Traefik, maps host `127.0.0.1:8080` to load balancer port 80, and attaches the managed registry |
| Application packaging | One Helm chart under `deploy/helm/vermouth`, rendered as `vermouth-foundation` and `vermouth` releases | The chart has `Chart.yaml`, documented defaults, `values.schema.json`, `values-local.yaml`, component gates, helpers, NOTES, and lint plus render checks. Separate releases make cold start ordering and application rollback real without creating a second chart (basis: the installed Helm chart skill and Helm chart practices) |
| Edge routing | Packaged Traefik image `rancher/mirrored-library-traefik:3.7.8`, chart `40.1.4+up40.1.0`, one replica | A committed `HelmChartConfig` fixes resources and JSON access logs. Vermouth owns one standard Kubernetes Ingress. Local uses HTTP only; spec 0006 adds production ACME and HTTPS values (basis: official k3s Helm and networking guidance) |
| Local image distribution | k3d managed registry `vermouth-registry`, host endpoint `127.0.0.1:5111`, cluster endpoint `vermouth-registry:5000` | K3d publishes port 5111 only on IPv4 loopback. Host Docker builds, pushes, and inspects through `127.0.0.1`. Kubernetes pulls through the k3d generated Docker network name and deploys by digest. No custom alias, Colima profile, Docker daemon trust, or host certificate change is allowed. The registry is not a production artifact store (basis: the observed k3d `v5.9.0` registry state and the installed Buildx and Docker security skills) |
| Multiplatform builds | Docker Buildx for `linux/arm64` and `linux/amd64` | Builder stages run on `$BUILDPLATFORM`; Go receives `TARGETOS` and `TARGETARCH`; six runtime application images, four migration images, and the Garage initializer use registry manifest lists (basis: https://docs.docker.com/build/building/multi-platform/ and the installed Buildx skill) |
| Web runtime | Repository image derived from `nginx:stable-alpine3.24`, pinned by digest | It serves `web/dist`, uses SPA fallback, listens on 8080, writes temporary files under writable `emptyDir` mounts, and runs as a nonroot user (basis: https://hub.docker.com/_/nginx and the installed Docker security skill) |
| Stateful storage | Vermouth specific static local PVs with `ReadWriteOnce`, `Retain`, node affinity, and deterministic paths under `/var/lib/vermouth` | Docker volume `vermouth-data`, labelled `vermouth.dev/platform=vermouth` and `vermouth.dev/role=data`, mounts `/var/lib/vermouth` into the k3d server. Eight fixed directories hold four Postgres instances, Redpanda, Garage metadata and data, and Traefik ACME state. Registry volume `vermouth-registry-data` carries the same platform label and role `registry`. Dynamic local path directory IDs are not used (basis: spec 0006 and Kubernetes local volume practice) |
| Migrations | One uniquely named Kubernetes Job per service, orchestrated by Task before the application upgrade | Each migration image contains goose and only that service migration directory. Jobs use `restartPolicy: Never`, `backoffLimit: 1`, and a deadline. Task removes successful Jobs and retains failed Jobs for diagnosis (basis: STK-5, STK-21, https://kubernetes.io/docs/concepts/workloads/controllers/job/, and the installed goose skill) |
| Object storage | Garage `v2.2.0` in one node mode | One init Job owns the local layout, private bucket, fixed billing key, and bucket grant (basis: spec 0002 and https://www.mintlify.com/deuxfleurs-org/garage/installation/docker) |
| Broker | Redpanda `v25.3.5`, one node, no host listener | It keeps the exact Compose memory, partition, retention, and automatic topic creation behavior. Services still own topic creation through `EnsureTopics` |
| Secrets | Stable foundation Secrets plus content named application Secrets, created directly from ignored local values | Foundation credential drift requires confirmed recreate. Application release references change atomically with the release. Secret values never appear in Helm arguments, committed values files, image layers, or logs |
| Observability | Existing JSON application logs plus JSON Traefik access logs to stdout | `platform:logs` reads current and previous containers. Prometheus, tracing, log aggregation, and alerts stay with feature 9 (basis: spec 0002 and packaged Traefik configuration) |

### Packaged Traefik contract

K3s owns one Traefik release. Vermouth never installs a second controller. The committed files are `deploy/platform/traefik/values-common.yaml`, `values-local.yaml`, and `values-production.yaml`. Task renders the common file plus exactly one environment file into the k3s `HelmChartConfig` named `traefik` in `kube-system`.

Common values fix one replica, image `rancher/mirrored-library-traefik:3.7.8`, chart `40.1.4+up40.1.0`, CPU request and limit `25m` and `250m`, memory request and limit `64Mi` and `256Mi`, official probes, JSON access logs to stdout, entrypoints `web` and `websecure`, and container log rotation. Local values expose only entrypoint `web`. K3d maps its load balancer port 80 to host `127.0.0.1:8080`. Local config has no certificate resolver or redirect. Production values come from spec 0006 and add persistent ACME plus HTTPS.

The application chart renders one Ingress. Local annotations select IngressClass `traefik` and entrypoint `web`. Production annotations select `websecure`, TLS, and the final production certificate resolver. The route inventory and destination Services stay identical across environments.

Bootstrap desired state comes only from `deploy/platform/versions.yaml`, `deploy/images.lock.yaml`, the committed k3d configuration, the three committed Traefik values files, the Helm matrix and local values files, `deploy/platform/secret-inventory.yaml`, and `.env.example`. The existing Colima profile is the runtime source and must already satisfy the CPU and memory values in `versions.yaml`; bootstrap never rewrites it. Missing `.env` is copied from `.env.example` with mode `0600`, then only generated absent values from the Secret inventory are filled. Web dependencies install with `corepack pnpm install --frozen-lockfile` in `web/`. Generated platform files live only under ignored `.tmp/platform`, with directories mode `0700` and files containing Secret names or local state mode `0600`. Any other local file or live default is drift, not configuration.

### Startup order and release ownership

| Step | Owner | Completion gate | Failure result |
|---|---|---|---|
| 1. Validate host and desired state | `platform:doctor` | Version, capacity, context, ports, config hash, and image locks pass | No cluster or release mutation |
| 2. Build and push | Buildx and local registry | Every required native digest is pushed and remotely inspected through `127.0.0.1:5111`, the locked cold probe proves node resolution through `vermouth-registry:5000`, then image and proof state are recorded atomically | No Secret, Job, or release mutation. Partial unreferenced registry content may remain |
| 3. Prepare Secrets | Task | Stable foundation Secret hashes match, new content named application Secrets exist | Old release keeps old Secret references |
| 4. Reconcile foundation | `vermouth-foundation` release | Postgres, Redpanda, Garage, Services, and PVCs are ready | Application release is untouched |
| 5. Initialize Garage | Task owned Job | Desired layout, bucket, key, and grants match | Application release is untouched, failed Job remains |
| 6. Run migrations | Four Task owned Jobs in parallel | All four Jobs complete | Application release is untouched, failed Jobs remain, no down migration runs |
| 7. Upgrade applications | `vermouth` release | Deployments, Ingress, routes, policies, and probes reach their current revision | Only this release rolls back to its prior revision and Secret references |
| 8. Verify final readiness | Task | Registry container and volume identity match, host `/v2/` responds, the current cold transport proof is recorded, Traefik is Available, the Ingress exists, and `/ready` is ready through `vermouth.localhost:8080` | Command fails with status and log targets |

The foundation release owns the namespace identity, StatefulSets, their Services, PVC templates, NetworkPolicies, and references to stable bootstrap credentials. Task creates the actual Secrets. The application release owns the web app, gateway, four service Deployments and Services, the Ingress, application ConfigMaps, and content named runtime Secret references. K3s owns packaged Traefik. Migration, Garage initialization, and development token Jobs are rendered from the chart but created and waited on by Task, so they do not race a Helm release.

### Compatibility and image inventories

`deploy/platform/versions.yaml` is the only source for host and controller compatibility.

| Tool or component | Accepted value |
|---|---|
| Host | macOS on `arm64` |
| Colima | `0.10.x`, minimum `0.10.3` |
| Docker client | `29.x`, minimum `29.7.2` |
| Docker server | `29.x`, minimum `29.5.2` |
| Docker Buildx | `0.36.x`, minimum `0.36.1` |
| Buildx builder | Exact name `colima`, driver `docker` |
| BuildKit | Exact `v0.30.0` |
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

`deploy/images.lock.yaml` records source registry, repository, selected tag, resolved remote descriptor digest, resolution timestamp, and supported platforms for every external image. Updating a lock requires an explicit remote resolution command and records its output. Runtime pulls always use the digest, so an external tag move cannot silently upgrade a build. `task platform:locks:verify` may query every source tag and fails when its current digest differs, while normal offline doctor validates the committed provenance and never substitutes a tag. For images built from this repository, the lock records the executable build inventory described below. Their per worktree runtime digests live in ignored `.tmp/platform/images.json`, because committing a digest for every local edit would make the lock meaningless. A missing provenance field, external digest, build input, target, or required architecture fails doctor and chart rendering. The lock contains at least these entries:

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

### Authoritative platform inputs

`deploy/platform/versions.yaml` is the only authority for host identity, tool compatibility, builder identity, cluster identity, host bindings, registry endpoints, immutable identity fields, and command deadlines. The committed k3d configuration is generated from or validated against these values before cluster mutation. A mismatch fails. Neither file wins silently.

The local host contract is exact:

| Value | Required source value |
|---|---|
| Colima profile | `default` |
| Docker context and endpoint | context `colima`, endpoint derived as `$HOME/.colima/default/docker.sock` |
| Docker client | `29.x`, minimum `29.7.2` |
| Docker server | `29.x`, minimum `29.5.2` |
| Buildx builder | name `colima`, driver `docker`, status `running` |
| BuildKit | exact `v0.30.0` |
| Builder platforms | includes `linux/arm64` and `linux/amd64` |
| Application host bind | `127.0.0.1:8080` |
| Registry k3d configuration, inventory, and Docker container name | `vermouth-registry` |
| Registry Docker network and alias | `k3d-vermouth`, alias `vermouth-registry` |
| Registry host bind and endpoint | `127.0.0.1:5111` |
| Registry cluster endpoint and containerd mirror key | `vermouth-registry:5000` |
| Registry containerd mirror endpoint | `http://vermouth-registry:5000` |
| Registry data volume | `vermouth-registry-data`, with the fixed platform and role labels |

Every committed machine readable platform input has `schemaVersion: 1`, rejects unknown keys, and is parsed by one Go helper at `pkg/vermouth/cmd/platformconfig`. Generated documents carry the version key required by their committed schema. The helper emits canonical JSON for shell and Helm validation. These files are the executable inputs:

| File | Owns |
|---|---|
| `deploy/platform/versions.yaml` | Host, builder, cluster, registry, volume, network, readiness, deletion, identity, and deadline values |
| `deploy/images.lock.yaml` | External image provenance plus repository build targets, canonical hash inputs, platform domains, and tag templates |
| `deploy/platform/images.schema.json` | Generated image state outer object and record fields |
| `deploy/platform/secrets.schema.json` | Generated Secret state outer object and metadata fields |
| `deploy/helm/vermouth/values.schema.json` | Chart values and generated runtime values modes |
| `deploy/helm/vermouth/files/traffic-matrix.yaml` | Every allowed source, destination, selector, namespace, port, protocol, stable ID, policy name, and rule order |
| `deploy/helm/vermouth/files/workload-matrix.yaml` | Every workload replica, resource, probe, identity, ServiceAccount, token mount, writable mount, and root filesystem rule |
| `deploy/platform/secret-inventory.yaml` | Environment source, Secret name base, Kubernetes key, database fields, URL template, consumer, generation, and required mode |

The Helm chart reads its two matrix files with `.Files.Get`. `platformconfig` validates the same files and compares rendered resources with their rows. Prose explains the contract, but a builder never duplicates a prose table into code by hand.

### Image inventory and publication

Each built entry in `deploy/images.lock.yaml` contains `workload`, `repositoryPath`, `dockerfile`, `target`, `nativePlatforms`, `multiPlatforms`, `inputs`, `buildArgs`, `baseLocks`, `hashSchema`, `nativeTagTemplate`, `multiTagTemplate`, and `childTagTemplate`. `hashSchema` is exactly `vermouth-image-v1`. Native tags are `dev-<first 12 characters of native_input_hash>`. Multiple architecture tags are `multi-<first 12 characters of multi_input_hash>`. Child tags append `-arm64` or `-amd64` to that multiple architecture tag. Repository paths are fixed as `vermouth/<workload>`. Migration paths are `vermouth/<service>-migration`. The Garage initializer is `vermouth/garage-init`. The development token is `vermouth/devtoken` and has no multiple architecture output.

For one inventory entry, Task derives references exactly:

```text
host_ref = 127.0.0.1:5111/<repositoryPath>:<tag>
cluster_ref = vermouth-registry:5000/<repositoryPath>@<manifest_digest>
```

The input hash is SHA256 over a versioned canonical byte stream. The stream starts with UTF8 bytes `vermouth-image-v1`. Every following scalar is encoded as an eight byte unsigned big endian byte length followed by its raw bytes. Every collection starts with a four byte unsigned big endian item count. Fields occur in this order: Dockerfile repository path and bytes, `.dockerignore` presence byte and bytes when present, target, ordered platform list, sorted build argument key and value pairs, sorted base lock name and digest pairs, then sorted declared inputs. The presence marker is exactly one byte, `0x00` for absent and `0x01` for present. Unix permission bits masked to `0777` are encoded as one four byte unsigned big endian integer. File content uses the standard scalar encoding, so its eight byte length prefix is the only file byte length field. A native hash carries exactly `linux/arm64`. A multiple architecture hash carries exactly `linux/arm64`, then `linux/amd64`. Each input records its normalized slash separated repository path, encoded permission bits, and scalar encoded bytes. Duplicate paths, overlapping input declarations, symlinks, nonregular files, and paths outside the repository fail. A missing `.dockerignore` carries the absent marker and no content scalar. Generated and untracked files count only when an explicit `inputs` entry includes them. No secret is a build argument or hash input.

Native publication has two phases. The build phase builds every required `linux/arm64` target with the pinned `colima` Docker driver and loads it into the local daemon. Every image config carries labels for the full input hash, repository path, target, and platform. No registry write occurs until every build succeeds. The publish phase pushes an absent immutable tag, then treats the remote registry descriptor returned for that tag as the authoritative manifest digest. If a tag already exists, Task reads its manifest and config, requires the full input hash, repository path, target, platform, and pinned base provenance to match, then reuses its remote descriptor. A generated record for the same full input hash must name the same descriptor. Task never compares a registry manifest digest with a Docker image ID or config digest. Any mismatch fails without overwrite.

Two architecture publication also has two phases. The build phase exports one Docker archive per workload and architecture to `.tmp/platform/builds/<run-id>/<workload>/<architecture>.tar`. Every archive must exist and pass the same full hash, repository, target, platform, and base provenance inspection before publishing starts. The publish phase loads and pushes immutable child tags `<multi-tag>-arm64` and `<multi-tag>-amd64`, then creates final tag `<multi-tag>` with `docker buildx imagetools create`. The remote index descriptor is authoritative only after inspection proves exactly the two ordered platform manifests and their recorded provenance. Any failure after publishing starts is a push failure and may leave content or child tags that no release references. A build failure changes no registry.

After successful native or multiple architecture publication, Task runs a project scoped host cleanup. Docker may remove only unused images whose configuration carries `vermouth.dev/workload`. The registry manifest and k3s containerd image remain authoritative and are not host images. A failed publication keeps its local evidence for diagnosis until the next successful build, confirmed platform clean, or explicit cache cleanup.

BuildKit cache belongs to the shared Colima builder, not to one repository. Normal startup, image publication, stop, clean, and recreate never prune it. `task platform:cache:clean` prints `docker buildx du --builder colima`, explains that the next build will be cold, requires typed confirmation `vermouth`, runs `docker buildx prune --builder colima --all --force`, and prints the before and after totals. It never removes Docker images, containers, volumes, registry content, or source files.

Doctor requires the pinned builder to report both architectures before a build. Task never installs QEMU or runs a privileged binfmt container. The existing Colima profile owns emulation support. Portability smoke runs the pinned web image once per architecture with read only root, its declared tmpfs mounts, a loopback random port, and a successful `GET /`.

After a push, Task resolves the remote descriptor with an HTTP `HEAD` request to `/v2/<repositoryPath>/manifests/<tag>` using the OCI index, OCI manifest, Docker manifest list, and Docker v2 manifest accept types. The required `Docker-Content-Digest` header is `manifest_digest`. Native publication records its remote single platform descriptor. Two architecture publication records the final remote index digest. Local image IDs, config digests, child platform digests, and command output are never deployment digests.

Before writing runtime values, Task resolves the same repository path and digest through the k3d node. It requires k3d registry name `vermouth-registry`, Docker network DNS name `vermouth-registry`, and generated containerd mirror key `vermouth-registry:5000` with endpoint `http://vermouth-registry:5000`. It verifies the node and registry share the expected Docker network, then pulls the digest with the node runtime. Host and cluster checks must agree on `manifest_digest`.

Committed schema `deploy/platform/images.schema.json` owns `.tmp/platform/images.json`. Its outer object contains `schema_version: 1`, `source_revision`, `dirty`, `built_at`, and an `images` object keyed by every built workload. `source_revision` is Git `HEAD` captured once at build start. Each image record has its own `dirty` value, true only when Git reports a change in that image's declared inputs. Outer `dirty` is true when any image record is dirty. `built_at` is one UTC RFC3339 run clock value captured after publication and before the atomic commit. Each image record contains `workload`, `repository_path`, `host_ref`, `cluster_ref`, `tag`, `input_hash`, `manifest_digest`, ordered `platforms`, and `dirty`. Task writes a complete next file, validates it against the committed schema and every required live registry descriptor, calls `fsync`, then renames it atomically to `.tmp/platform/images.json`. A missing, partial, stale, or live mismatched record is never reused.

Committed schema `deploy/platform/secrets.schema.json` owns `.tmp/platform/secrets.json`. Its outer object contains `schema_version: 1`, a `foundation` object keyed by stable Secret base, and a `runtime` object keyed by service. Every entry contains only immutable Secret name, full canonical source hash, and sorted key names. It contains no Secret value. Task validates this document against the Secret inventory and live Secret metadata before runtime values use any name.

`deploy/helm/vermouth/values.schema.json` owns `.tmp/platform/runtime-values.yaml` in runtime mode. The file contains only `schemaVersion: 1`, complete digest based image references, immutable Secret names, public runtime configuration, and the current Job run ID. Public runtime configuration is exactly environment `local`, explicit Google availability, and API base path `/api`. The file contains no Secret value. It follows the same validate, `fsync`, and rename sequence as image state.

Bootstrap creates a dedicated probe at `vermouth/platform-probe:<images-lock-hash>-arm64`, pushes it through `127.0.0.1:5111`, then uses `docker buildx imagetools create` to publish final probe tag `<images-lock-hash>`. It remotely inspects the final digest. While holding the platform mutation lock, it removes only that probe reference and its exact manifest or index descriptor from the k3s containerd namespace after proving no other image references the descriptor. It proves both are absent, starts registry log capture, pulls `vermouth-registry:5000/vermouth/platform-probe@<digest>` through CRI, requires a manifest request after pull start, and requires the pulled digest to match. Cached layer blobs may remain because the missing descriptor and observed request prove registry resolution. Bootstrap records probe digest, pull time, and observed manifest request in `vermouth-platform-identity` before any Vermouth release mutation.

Doctor remains read only. It validates the recorded cold proof against the current registry identity and image lock, makes a direct node HTTP request to the exact mirror endpoint and manifest path, and reports stale proof. A later locked bootstrap or deployment refreshes a stale cold proof. These checks prove Docker push, Buildx registry resolution and manifest creation, remote descriptor extraction, Docker DNS, containerd mirror behavior, registry reachability, and internal CRI pull without changing the Colima profile.

### Registry and generated state

| Value | Fixed source |
|---|---|
| Registry k3d configuration name | `vermouth-registry` |
| Registry k3d inventory name | `vermouth-registry`, never derived with a prefix |
| Docker container name | `vermouth-registry` |
| Docker network and alias | `k3d-vermouth`, alias `vermouth-registry` |
| Host endpoint | `127.0.0.1:5111` |
| Cluster endpoint | `vermouth-registry:5000` |
| Containerd mirror | key `vermouth-registry:5000`, endpoint `http://vermouth-registry:5000` |
| Host publication | `127.0.0.1:5111`, never `0.0.0.0:5111` |
| Storage | Docker named volume `vermouth-registry-data`, stopped but not removed by `platform:stop` |
| Transport | Plain HTTP. Host Docker uses IPv4 loopback. K3d marks only the internal endpoint as insecure in generated containerd configuration. No Colima profile, Docker daemon, or certificate trust change is made |
| Native tag | `dev-<first 12 characters of native_input_hash>` |
| Multiple architecture tag | `multi-<first 12 characters of multi_input_hash>` |
| Architecture child tags | Multiple architecture tag plus `-arm64` or `-amd64` |
| Digest record | Ignored `.tmp/platform/images.json`, schema version 1 |
| Runtime values | Ignored `.tmp/platform/runtime-values.yaml`, containing image digests, Secret names, public configuration, and run ID but no Secret values |
| Cold transport proof | Probe digest, pull time, and observed registry manifest request in `vermouth-platform-identity` |
| Cleanup | Normal startup never deletes registry content. Confirmed clean and recreate delete the registry volume. No automatic garbage collection is promised |

The host and cluster endpoints name the same registry content but serve different callers. Host references exist only during build, push, and manifest inspection. Runtime values use only the cluster repository plus the remote manifest digest. The executable image inventory defines the hash inputs and forbids tag overwrite. The generated record uses the complete image state schema in the publication contract above.

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

`deploy/helm/vermouth/files/workload-matrix.yaml` is authoritative. The table above is its human view. The file records quantities as Kubernetes quantity strings and every writable volume with medium, size, mount, owner, and purpose. Postgres gets memory backed `/var/run/postgresql` at `16Mi` and `/tmp` at `64Mi`. Redpanda gets memory backed `/etc/redpanda` at `16Mi` and `/tmp` at `64Mi`. Garage gets memory backed `/tmp` at `64Mi`. Go applications get memory backed `/tmp` at `16Mi`. Migration Jobs get memory backed `/tmp` at `8Mi`. Garage initialization gets `/tmp` at `8Mi`. Web gets disk backed cache `32Mi`, run `4Mi`, and tmp `16Mi`. Development token writes nowhere. Packaged Traefik must match its pinned chart security and writable volume rendering exactly.

Measurement waits 60 seconds after public readiness, then records 60 samples at 5 second intervals. VM used memory is `MemTotal - MemAvailable` from Colima `/proc/meminfo` and is the only total memory pass value. Docker and Kubernetes memory are diagnostic views and are never added to it. Total CPU is the sum of Docker `CPUPerc` for only the k3d server, k3d load balancer, and managed registry, where 100 percent is one CPU. Vermouth Pods run inside the k3d server container and are already included in that value. Kubernetes per Pod CPU is recorded for attribution only and is never added to Docker CPU. The report records maximum and arithmetic mean for VM used memory, total CPU, each counted Docker container, and each Pod. Missing, stale, unparsable, or unavailable metrics fail the run. Pass means every steady request total is within the declared ceiling, maximum VM used memory is at most 7 GiB, and every sampled total CPU value is below 350 percent.

### Probe and deadline matrix

| Target | Readiness | Liveness | Deadline or retry |
|---|---|---|---|
| Go application | `GET /ready`, every 5 seconds, 2 second timeout, 12 failures | `GET /health`, every 10 seconds, 2 second timeout, 3 failures | Deployment 5 minutes |
| Web | `GET /`, every 5 seconds, 2 second timeout, 6 failures | Same path every 10 seconds, 3 failures | Deployment 3 minutes |
| Postgres | `pg_isready`, every 5 seconds, 3 second timeout, 12 failures | Same command every 10 seconds, 3 failures | Foundation 8 minutes |
| Redpanda | `rpk cluster health`, every 10 seconds, 5 second timeout, 12 failures | Same command every 20 seconds, 3 failures | Foundation 8 minutes |
| Garage | `/garage status`, every 10 seconds, 5 second timeout, 12 failures | `/garage status`, every 20 seconds, 5 second timeout, 3 failures | Foundation 8 minutes |
| Packaged Traefik | Official chart readiness and liveness probes | Official chart probes | Reconcile 5 minutes |
| Ingress | Route exists and HTTP `/ready` succeeds through `vermouth.localhost:8080` | Not applicable | 2 minutes |
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
| Packaged Traefik | K3s packaged Deployment and Service | 1 locally | None | Host loopback port 8080 to load balancer port 80 |

There is no application schema change. Existing Postgres schemas remain authoritative. Kubernetes resources map the fixed local claims to deterministic component directories through StorageClass `vermouth-local`. The local node name is `k3d-vermouth-server-0`. Local workloads bind seven claims. The eighth retained PV, `traefik-acme`, is reserved without a local claim because local Traefik has no ACME storage; spec 0006 claims it for production values.

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

Every PV uses `local.path`, reclaim policy `Retain`, volume mode `Filesystem`, access mode `ReadWriteOnce`, StorageClass `vermouth-local`, and node affinity for `k3d-vermouth-server-0`. Before k3d creation, bootstrap creates exact Docker volumes `vermouth-data` and `vermouth-registry-data` with the platform label and their fixed `data` or `registry` role label. An existing volume with a missing or mismatched label fails before attachment. K3d only attaches those verified volumes. Bootstrap creates the component directories with the numeric owner required by each workload before applying PVs. Volume names and labels live in the `versions.yaml` deletion inventory. Clean and recreate may delete only cluster `vermouth`, registry `vermouth-registry`, those two verified volumes, and generated `.tmp/platform` state after every Docker label and platform identity matches and typed confirmation succeeds. Any extra or mismatched target fails closed.

### State transitions

1. Platform lifecycle: `absent` to `bootstrapped` to `running` to `stopped` to `running`.
2. Destructive lifecycle: `stopped` or `running` to typed confirmation to `absent`.
3. Release lifecycle: `source` to `images pushed` to `foundation ready` to `Garage ready` to `migrations complete` to `application ready`.
4. Failure before migrations leaves both releases unchanged except for a safe foundation reconciliation. Migration failure leaves the old application and Secret references running. Readiness failure rolls application resources back and keeps the foundation plus forward schema.

Service redeploy requires a complete live validated `.tmp/platform/images.json` from a successful full `task dev`. It rebuilds the named workload and its migration target when applicable, merges only their new records into a complete next file, then validates every unchanged record against the live registry before publication or Helm. Missing or stale unchanged state refuses redeploy and points to `task dev`. A target build or push failure leaves the current generated file and application revision unchanged. Application rollback follows the Helm transaction contract below.

### Command surface

| Command | Inputs | Success output | Important failures |
|---|---|---|---|
| `task platform:bootstrap` | Committed platform config, installed tools, `.env` | Cluster, registry, packaged Traefik, Ingress prerequisites | Missing tool, low Colima resources, context collision, version drift |
| `task dev` | Worktree, `.env`, local values | Ready workloads and `http://vermouth.localhost:8080` | Build, registry, Secret, migration, Helm, probe, or timeout failure |
| `task dev:redeploy -- <workload>` | Allowed workload name | New digest and ready workload | Invalid name, build, migration, or rollout failure |
| `task images:multi` | All Dockerfiles | Verified two platform manifest for every image | Builder lacks platform, build failure, missing manifest entry |
| `task platform:doctor` | Host and cluster state | Version, capacity, explicit target, registry, controller report | Any unsupported or drifted prerequisite |
| `task platform:validate` | Committed chart and machine readable inventories | Linted chart plus exact release, Job, policy, storage, and workload rendering | Schema, source row, render, or inventory mismatch |
| `task platform:locks:verify` | Committed external image lock | Every source tag still resolves to its committed digest and platforms | Network unavailable, tag drift, missing provenance |
| `task platform:status` | Exact context | Both Helm releases, Ingress, every supported log target, Job, PVC, registry, and readiness summary | Wrong context or unreachable cluster |
| `task platform:logs -- <target> [--previous|--follow|--since <duration>]` | Named target and optional mode | Current or requested logs | Invalid target or missing workload |
| `task platform:stop` | Exact context | Stopped cluster and registry with their data retained | Wrong context or stop timeout |
| `task platform:recreate` | Exact context and typed `vermouth` | Empty cluster recreated from committed configuration | Wrong confirmation or target |
| `task platform:clean` | Exact context and typed `vermouth` | Removed cluster and persistent data | Wrong confirmation or target |
| `task platform:cache:clean` | Shared builder `colima` and typed `vermouth` | Reclaimable BuildKit cache removed with before and after totals | Wrong confirmation, active builder operation, or prune failure |
| `task platform:jobs:clean -- <job>` | Exact failed Job name | Named Job and Pod removed | Unknown or running Job |
| `task platform:lock:clear` | Stale host lock, stale Lease, typed `vermouth` | Stale records owned by no live process removed | Live owner, fresh renewal, wrong confirmation |
| `task dev:token` | Optional `--email`, `--name`, `--timezone`, `--language` flags | JSON with `schema_version`, `tutor_id`, `email`, `access_token`, `access_expires_at` | Invalid flags, Job, database, or signer failure |
| `task thread` | Ready platform | Recorded identity event through notifications | Gateway, relay, broker, or consumer timeout |

The log and status target inventory is fixed: `web`, `gateway`, `identity`, `teaching`, `billing`, `notifications`, `postgres-identity`, `postgres-teaching`, `postgres-billing`, `postgres-notifications`, `redpanda`, `garage`, `traefik`, `registry`, `garage-init`, `devtoken`, and `migrate-<service>-<run-id>`. `all` visits every applicable target. Failed Jobs remain addressable until the named cleanup or cluster clean.

`dev:token` defaults `name` to `Tracer Tutor`, `timezone` to `Asia/Ho_Chi_Minh`, `language` to `vi`, and generates a unique `tutor-<nanoseconds>@example.com` email when none is supplied. Its JSON contains exactly `schema_version: 1`, `tutor_id`, `email`, `access_token`, and `access_expires_at`. It prints no other stdout content, so `task thread` can consume it directly.

Gateway readiness is exact. HTTP 200 returns `{"status":"ready","service":"gateway","checks":{"identity":"ok","teaching":"ok","billing":"ok","notifications":"ok"}}`. Any missing, unreachable, or nonready service returns HTTP 503, status `not ready`, all four check keys, and the failing value `unreachable: <reason>` or `not ready`. The fixed Service names and ports are gateway 8080, identity 8081, teaching 8082, billing 8083, notifications 8084, four `postgres-<service>` Services on 5432, Redpanda 9092 and 9644, and Garage 3900, 3901, and 3903.

### HTTP surface

| Host and path | Destination | Access | Source of behavior |
|---|---|---|---|
| `PathPrefix /api` | Gateway service | Local host | Prefix is preserved, with no rewrite |
| `Exact /health` | Gateway service | Local host | Path is preserved |
| `Exact /ready` | Gateway service | Local host | Path is preserved |
| `Exact /config.json` | Web service | Local host | Mounted ConfigMap response with `Cache-Control: no-store` |
| `PathPrefix /` | Web service | Local host | Final catch all rule, with Nginx SPA fallback |

All rules use hostname `vermouth.localhost` on Traefik entrypoint `web` at container port 80. K3d publishes that entrypoint only at `127.0.0.1:8080`. Exact matches outrank prefixes, and longer prefixes outrank `/`. No route rewrites a path. `/config.json` is exactly `{"schemaVersion":1,"environment":"local","googleAuthEnabled":<boolean>,"apiBasePath":"/api"}`.

### Value sourcing

| Action | Value produced | Source |
|---|---|---|
| Bootstrap | Cluster name, k3s version, port bindings, registry name | `deploy/platform/versions.yaml`, validated against the committed k3d configuration |
| Bootstrap | Traefik image, chart, replicas, resources, and log format | Exact k3s version plus committed `HelmChartConfig` |
| Bootstrap | static PV names, paths, node affinity, and reclaim policy | Fixed eight component inventory, root `/var/lib/vermouth`, node `k3d-vermouth-server-0`, and StorageClass `vermouth-local` |
| Doctor | Profile, daemon, builder, minimum CPU and memory, compatible tool versions | `deploy/platform/versions.yaml` |
| Runtime integrity | Colima profile hash | SHA256 of `${COLIMA_HOME:-$HOME/.colima}/default/colima.yaml`, captured before platform mutation and recorded in `vermouth-platform-identity` |
| Runtime integrity | Docker daemon configuration hash | SHA256 of `/etc/docker/daemon.json` inside profile `default`, or a fixed absent marker, read through `colima ssh`, captured before platform mutation and recorded in `vermouth-platform-identity` |
| Registry routing | Every k3d, Docker, containerd, volume, host, and cluster registry identity | Separate named fields in `deploy/platform/versions.yaml`, never a derived `k3d-` prefix |
| Native build | Repository path, tag, inputs, platforms, remote manifest digest | Built entry in `deploy/images.lock.yaml`, canonical hash algorithm, and registry descriptor |
| Multiplatform build | Required platform entries, staged archives, final index digest | Built entry in `deploy/images.lock.yaml` and remote registry descriptors |
| Multiplatform build | Required image set | Built entries whose `multiPlatforms` contains both required platforms |
| Generated image state | Schema version, source revision, dirty values, build time, records | `images.schema.json`, Git `HEAD` at run start, Git state over each declared input set, UTC run clock, and validated remote descriptors |
| Generated Secret state | Schema version, immutable Secret names, source hashes, sorted key names | `secrets.schema.json`, `secret-inventory.yaml`, canonical Secret hashes, and live Secret metadata |
| Runtime values | Image references, Secret names, public configuration, run ID | Validated `images.json`, validated `secrets.json`, explicit local configuration, generated UUIDv7 run ID, and `values.schema.json` runtime mode |
| Secret creation | Application credentials, signing keys, formats, consumers | Existing `.env` plus `deploy/platform/secret-inventory.yaml` |
| Secret naming | Immutable application Secret suffix | Versioned length prefixed encoding in the Secret inventory contract |
| Database URL | Service DNS, port, database, role, password, URL shape | One database owning row in `deploy/platform/secret-inventory.yaml` and its named password Secret |
| Migration | Database target and SQL set | The named service Secret and its `db/migrations/` directory |
| NetworkPolicy rendering | Policy count, resource names, rule order, selectors and ports | Unique `policyName` values and stable rule rows in `traffic-matrix.yaml` |
| Helm upgrade | Image references | Atomically committed, live validated cluster references in `.tmp/platform/images.json` |
| Web startup | Google sign in availability | Public `googleAuthEnabled` value in the local ConfigMap, derived from explicit local auth configuration |
| Web startup | schema version, environment, API path | Fixed runtime config schema version 1, local values, and same origin `/api` contract |
| Platform readiness | Component state | Kubernetes conditions, Traefik availability, Ingress existence, Job completion, PVC binding, registry container state, registry volume identity, host `/v2/` response, cold node transport probe, and existing `/health` plus `/ready` responses |
| Local address | `http://vermouth.localhost:8080` | Committed Ingress and k3d port mapping, an ordinary hostname request, and a forced `127.0.0.1` request with Host header `vermouth.localhost` |
| Google callback | `http://vermouth.localhost:8080/api/auth/google/callback` | Fixed local origin plus path `/api/auth/google/callback`; when auth is enabled, `IDENTITY_GOOGLE_REDIRECT_URL` must match it exactly |
| Garage initialization | Node layout, bucket, S3 key, grants | Fixed Garage desired state in this spec and `billing-s3-bootstrap` |
| Resource report | VM memory, total Docker CPU, Pod attribution, pass decision | Colima `/proc/meminfo`, Docker stats for the three named containers, Kubernetes metrics, and the AC-15 formula |
| Clean | Exact deletion target | `versions.yaml` deletion inventory, Docker identity labels, cluster identity, and typed `vermouth` confirmation |
| Host image cleanup | Project owned image set | Unused Docker images carrying label `vermouth.dev/workload` |
| BuildKit cache cleanup | Builder name and cache inventory | `versions.yaml` builder name plus live `docker buildx du`, with typed `vermouth` confirmation |
| Drift decision | Reconcile or require recreate | `versions.yaml` immutable field inventory and live `vermouth-platform-identity` ConfigMap |

### Secret lifecycle

Stable foundation Secrets are `postgres-<service>-bootstrap`, `garage-bootstrap`, and `billing-s3-bootstrap`. Redpanda has no local authentication Secret. Bootstrap annotates each Secret with the SHA256 of its canonical source values. If a live foundation Secret hash differs from `.env`, doctor refuses normal startup because changing a bootstrap password or imported storage key does not rotate stored data. Confirmed recreate is the local recovery path.

Application Secrets are immutable and content named, such as `identity-runtime-<hash>`. Task creates the new names before migrations, passes each service migration Job its service runtime Secret, and changes Deployment references only inside the application Helm upgrade. Old Pods keep their process environment and old Secret references. After a successful release, Task keeps the Secrets referenced by the current and previous Helm revisions and removes older unreferenced application Secrets. It never changes stable foundation Secrets.

`.tmp/platform/runtime-values.yaml` contains image references, Secret names, public runtime configuration, and the Job run ID, but no Secret value. Database URLs use the exact service DNS name, port, database, role, password source, and URL template recorded for that service in `deploy/platform/secret-inventory.yaml`. They are not copied from the host port URLs in `.env`.

### Secret inventory and hashing

`deploy/platform/secret-inventory.yaml` is the complete Secret contract. Its rows are:

| Secret name base | Keys | Source and generation | Consumers |
|---|---|---|---|
| `postgres-<service>-bootstrap` | `POSTGRES_PASSWORD` | Required existing `.env` value, never generated | Matching Postgres StatefulSet |
| `garage-bootstrap` | `GARAGE_RPC_SECRET`, `GARAGE_ADMIN_TOKEN` | Generate each missing value as 32 random bytes encoded as 64 lowercase hexadecimal characters | Garage and Garage initializer |
| `billing-s3-bootstrap` | `BILLING_S3_ACCESS_KEY_ID`, `BILLING_S3_SECRET_ACCESS_KEY` | Generate a missing access key as `GK` plus 24 lowercase hexadecimal characters, and a missing secret as 32 random bytes encoded as 64 lowercase hexadecimal characters | Garage initializer and billing runtime Secret construction |
| `gateway-runtime` | `TOKEN_PUBLIC_KEYS` | Existing generated Ed25519 public key set from `.env` | Gateway |
| `identity-runtime` | `IDENTITY_DATABASE_URL`, `TOKEN_PUBLIC_KEYS`, `IDENTITY_TOKEN_KID`, `IDENTITY_TOKEN_PRIVATE_KEY`, optional Google client ID and secret | Database fields and URL template from this inventory, signing values generated together by `cmd/devkeys`, Google values required only when the explicit flag is true | Identity, identity migration Job, and development token Job |
| `teaching-runtime` | `TEACHING_DATABASE_URL`, `TOKEN_PUBLIC_KEYS` | Database fields and URL template from this inventory, public key from `.env` | Teaching and teaching migration Job |
| `billing-runtime` | `BILLING_DATABASE_URL`, `TOKEN_PUBLIC_KEYS`, `BILLING_S3_ACCESS_KEY_ID`, `BILLING_S3_SECRET_ACCESS_KEY` | Database fields and URL template from this inventory, public key, and billing foundation values | Billing and billing migration Job |
| `notifications-runtime` | `NOTIFICATIONS_DATABASE_URL`, `TOKEN_PUBLIC_KEYS` | Database fields and URL template from this inventory, public key from `.env` | Notifications and notifications migration Job |

Every database owning row also records `serviceDNS`, `port: 5432`, `database`, `role`, `passwordSecret`, and URL template `postgres://{role}:{password}@{serviceDNS}:{port}/{database}?sslmode=disable`. The inventory schema requires these fields and rejects an unknown placeholder or consumer.

An empty or ASCII whitespace only required value is absent. Bootstrap fills only generated absent values. It never trims, normalizes, or rewrites a populated value. `.env` has mode `0600`. The temporary Secret directory has mode `0700`, every source file has mode `0600`, and successful creation removes those files. Standard output and logs contain names and hashes only.

Content naming hashes decoded environment values, not their textual `.env` representation. Keys sort by raw UTF8 bytes. The canonical stream starts with `vermouth-secret-v1`, then for every key appends a four byte big endian key length, key bytes, a four byte big endian value length, and value bytes. The name suffix is the first 12 lowercase hexadecimal characters of SHA256 over that stream. The Secret is immutable and carries the full hash annotation.

The signing set is all or nothing. Bootstrap creates one Ed25519 key pair, key ID `dev-1`, private key value, and public key set when all three signing values are absent. A partial set fails. Google auth never uses credential presence as a switch. `VERMOUTH_GOOGLE_AUTH_ENABLED=false` ignores absent Google credentials and keeps auth unavailable. `true` requires all three Google values to be nonempty and uses the fixed callback.

### Cluster identity and drift

Bootstrap writes ConfigMap `vermouth-platform-identity` from the immutable field inventory in `deploy/platform/versions.yaml`. It includes schema version, cluster name, Colima profile, Docker context, Docker server version, Buildx builder, driver, BuildKit version, k3s live version, k3s image tag, server count, agent count, packaged Traefik image and chart, static storage volumes, labels and root, node name, host port bindings, every registry identity field, cold transport probe digest, pull time and observed manifest request, Colima profile config SHA256, Docker daemon config SHA256 or the fixed absent marker, k3d config SHA256, Traefik config SHA256, image lock SHA256, generated state schema hashes, and matrix hashes. Doctor requires exact agreement between the file, k3d inventory, Docker state, containerd mirror, cluster state, generated state, and the ConfigMap. Every mutating command captures both runtime configuration hashes before its first platform mutation, compares them again before success, and fails if either changed.

Changes to k3s version, node count, port map, Traefik state, registry endpoints, or local path layout require confirmed recreate. Changes to application images, resource values, probes, routes, policies, or nonsecret ConfigMaps reconcile through Helm. A busy local port 8080 or 5111, an unknown same name cluster, or a foundation Secret hash mismatch stops before mutation.

`versions.yaml` lists every immutable identity field. Doctor compares that list rather than carrying a second hard coded list. It distinguishes these states:

| State | Result |
|---|---|
| Cluster absent | Doctor proves absence through k3d and Docker metadata. Bootstrap may create it after host, port, and named volume checks without reading or switching the current Kubernetes context |
| Known cluster stopped | Doctor verifies k3d metadata, Docker labels, node image, registry identity, named volume labels, and committed hashes. Bootstrap may start it, then must verify live identity before further mutation |
| Known cluster running | Doctor requires all live identity, endpoint, listener, builder, registry, and matrix checks |
| Same name without matching labels or identity | Unknown cluster, fail and require explicit target review |
| User current context names another cluster | Ignore it, pass explicit `--context k3d-vermouth`, and prove that target without changing the user's selection |
| Explicit `k3d-vermouth` context missing while the cluster is running | Fail before mutation because the live target cannot be proven |
| Registry content exists but generated state is absent or stale | Keep registry content, rebuild and republish immutable targets, never guess a runtime reference |

### Platform serialization and recovery

Every mutating platform command uses one run ID and holds two locks for its full outer deadline. After Colima starts, Task atomically creates `/var/run/vermouth-platform-lock` inside the Colima VM and writes owner PID, process start identity, worktree path, command, run ID, acquisition time, renewal time, and deadline. After the namespace exists it also acquires Kubernetes Lease `vermouth-platform-lock` with the same owner and renews both every 10 seconds. Read only doctor, status, and logs never delete or pull images and do not acquire the lock.

A live owner blocks another mutation and reports its command and age. A lock is stale only when its renewal is older than its recorded deadline plus 60 seconds and the recorded host PID with the recorded process start identity no longer exists. `task platform:lock:clear` prints both records, requires typed `vermouth`, refuses a live owner, and clears both stale records. A normal exit releases both through a trap. No automatic timeout steals a lock.

The command contract is:

| Command | Outer deadline | Exit code on its named failure | Remediation |
|---|---:|---:|---|
| `platform:doctor` | 2 minutes | 2 prerequisite or drift, 3 context or target | Fix the reported input, or review `platform:status` before confirmed recreate |
| `platform:bootstrap` | 10 minutes | 2 prerequisite or drift, 3 target, 7 deadline | Run doctor, then status and kube system events |
| Cold `dev` | 20 minutes | 4 build or push, 5 Garage or migration, 6 rollout or readiness, 7 deadline | Run status, then the named log target |
| Warm `dev` and `dev:redeploy` | 8 minutes | Same stage codes as cold `dev` | Run status, then the named workload or Job logs |
| `platform:status` | 30 seconds | 3 context or unreachable cluster | Restore or recreate explicit context `k3d-vermouth` without changing the current selection, then retry |
| `platform:logs` | 30 seconds unless `--follow` | 3 target, 7 deadline | Use status to find the current workload or retained Job name |
| `platform:stop` | 2 minutes | 3 target, 7 deadline | Inspect Docker container state |
| `platform:clean` | 5 minutes | 3 target, 8 confirmation, 7 deadline | Review the printed deletion inventory |
| `platform:recreate` | 15 minutes | 3 target, 8 confirmation, 7 deadline | Review status and Docker volumes before retry |
| `platform:jobs:clean` | 2 minutes | 3 target | Read the retained Job logs and name one failed Job |

Exit code 0 means success and 1 means an unexpected script defect. The outer watchdog terminates children, preserves stage evidence, and releases only locks owned by its run ID.

### Helm transaction contract

Task captures the last deployed revision and its runtime Secret references before an application upgrade. Foundation uses `helm upgrade --install vermouth-foundation ... --atomic --wait --timeout 8m --history-max 3`. Application uses `helm upgrade --install vermouth ... --atomic --cleanup-on-fail --wait --timeout 5m --history-max 3`. Values files carry image digests and Secret names, never Secret values.

On first install failure, Helm atomic cleanup leaves no application release. On upgrade failure, Task requires Helm to restore the captured deployed revision, then verifies revision, deployed status, image digests, and Secret references. The foundation stays. A pending Helm state with a live platform lock is left to its owner. A pending state with a stale cleared lock rolls back to the most recent deployed revision before any new migration or upgrade. If no deployed revision exists, Task uninstalls only the failed application release metadata and starts the first install again.

Runtime Secret cleanup runs only after a deployed application revision passes public readiness. It keeps every Secret referenced by the current deployed revision and the previous deployed revision, and removes older unreferenced runtime Secrets. Failed, pending, superseded, and foundation revisions do not widen that keep set.

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

The committed desired state is zone `local`, Garage layout capacity string `5G` meaning 5,000,000,000 bytes, Kubernetes data claim `5Gi`, replication factor one, metadata directory `/var/lib/garage/meta`, data directory `/var/lib/garage/data`, S3 port 3900, RPC port 3901, admin port 3903, bucket `vermouth-invoices`, key name `billing`, and private bucket access. Bootstrap generates the fixed access key and secret formats from the Secret inventory into ignored `.env`; the billing foundation Secret is their source.

The initializer reads the live node ID, layout version, bucket, key, and grants before changing anything. It assigns the node only when its zone or capacity is absent, applies exactly the next layout version only when the layout differs, creates the bucket only when absent, imports the fixed key only when absent, and grants read plus write without owner permission only when missing. Matching compares node ID, zone `local`, normalized decimal capacity, key name, access key ID, bucket identity, and read plus write grant. Garage does not reveal an imported secret. After identity and grant checks, the initializer uses the desired credentials to make signed S3 Put, Get, and Delete requests for zero byte object `platform-probe/<run-id>`. Authentication failure proves conflicting live key material. Read, write, owner, or cleanup mismatch fails without accepting the state. A different key ID for name `billing`, the same ID under another name, owner permission, an unexpected bucket identity, or a conflicting layout also fails without overwriting. The next `task dev` resumes from the first missing step after interruption.

### Key invariants

1. Platform commands pass exact context `k3d-vermouth` to every Kubernetes request, never depend on the user's current context, and never switch it.
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
13. Host Docker builds, pushes, and inspects only through `127.0.0.1:5111`. Kubernetes image references use only the k3d managed name `vermouth-registry:5000` plus a remote manifest digest. A custom registry alias is drift.
14. The local registry publishes only on host loopback. No startup path changes the Colima profile, Docker daemon insecure registry list, or host certificate trust.
15. Successful image publication and confirmed platform clean remove unused Vermouth labelled host images. No automatic path prunes the shared BuildKit cache. Only confirmed `task platform:cache:clean` may do that.

### Security model

The platform is local only and serves plain HTTP on `http://vermouth.localhost:8080`. Public access, TLS, production secret storage, image signing, backups, and Azure network policy belong to feature 16.

Every application Pod uses a named ServiceAccount with no RBAC permissions and `automountServiceAccountToken: false`. Jobs follow the same rule. Packaged Traefik keeps the RBAC owned by k3s in `kube-system`. Default deny NetworkPolicies enforce service ownership, including one allowed database client set per Postgres instance. The application edge is reachable from the host only through `127.0.0.1:8080`. The unauthenticated local registry is reachable from the host only through `127.0.0.1:5111` and from the cluster only through its private Docker network. Neither desired port may listen on a nonloopback IPv4 address or any IPv6 address.

Doctor ties listener ownership to exact Docker `HostConfig.PortBindings`. The k3d load balancer must publish container `80/tcp` only as host `127.0.0.1:8080`. The registry must publish container `5000/tcp` only as host `127.0.0.1:5111`. No Vermouth platform container may publish host port 80. Bootstrap captures the macOS port 80 listener set immediately before cluster creation and requires that cluster creation adds no entry. Doctor checks macOS listeners only for the desired host ports and does not classify an unrelated port 80 process as Vermouth drift. It proves the public path with both ordinary `http://vermouth.localhost:8080` resolution and a request forced to `127.0.0.1:8080` with Host header `vermouth.localhost`. The registry has no Ingress, LAN bind, credential, profile exception, or production role.

Standard Kubernetes NetworkPolicy cannot filter an external destination by DNS name. The identity exception therefore allows outbound TCP port 443 to external addresses when Google auth is enabled. Application level OAuth configuration still fixes the actual Google endpoints. A later CNI with DNS policy support would be a feature 16 decision, not a hidden local dependency.

The chart renders this exact traffic matrix from `deploy/helm/vermouth/files/traffic-matrix.yaml`. Each row has a stable ID, exact `policyName`, direction, environment condition, source namespace and selector, destination namespace and selector or allowed external CIDR, protocol, port list, and rule order. Rows with one `policyName` group into one NetworkPolicy. Chart validation requires a bijection between source rows and rendered rules by stable ID, and derives the expected resource count from unique `policyName` values. No literal policy count is a contract. Selectors use `app.kubernetes.io/name` and `app.kubernetes.io/component`. Cross namespace rules also require the standard namespace name label. Chart validation fails on an extra rendered rule, a missing row, duplicate ID, unknown selector key, unnamed port, or a destination broader than its source row.

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
| `garage-init` | Garage | TCP 3900 | Signed S3 credential and grant proof |
| `identity`, only when Google auth is enabled | Public IPv4 except RFC 1918 and link local ranges | TCP 443 | OAuth and Google token verification |

Every other ingress and egress path is denied. Postgres, Redpanda, Garage, and internal service ports have no Ingress route. The identity external rule excludes `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, and `169.254.0.0/16`.

The workload security matrix is also fixed. A pinned image must pass its stated numeric identity before its digest can enter `deploy/images.lock.yaml`.

| Workload | ServiceAccount | User and group | Root filesystem and writable paths | Token mount |
|---|---|---|---|---|
| Go applications | Matching workload name, no RBAC | `65534:65534` | Read only, memory `/tmp` `16Mi` | Off |
| Migration Jobs | Matching Job family, no RBAC | `65534:65534` | Read only, memory `/tmp` `8Mi` | Off |
| Garage initialization Job | `garage-init`, no RBAC | `65534:65534` | Read only, `/tmp` `8Mi` | Off |
| Development token Job | `devtoken`, no RBAC | `65534:65534` | Read only, no writable mount | Off |
| Web | `web`, no RBAC | `101:101` | Read only, `emptyDir` at `/var/cache/nginx`, `/var/run`, and `/tmp` | Off |
| Postgres | Matching database name, no RBAC | `70:70`, `fsGroup: 70` | Read only, PVC at `/var/lib/postgresql`, memory volumes at `/var/run/postgresql` `16Mi` and `/tmp` `64Mi` | Off |
| Redpanda | `redpanda`, no RBAC | `101:101`, `fsGroup: 101` | Read only, PVC at `/var/lib/redpanda/data`, memory volumes at `/etc/redpanda` `16Mi` and `/tmp` `64Mi` | Off |
| Garage | `garage`, no RBAC | `1000:1000`, `fsGroup: 1000` | Read only, PVCs at the configured metadata and data paths, memory `/tmp` `64Mi` | Off |
| Packaged Traefik | K3s packaged account and RBAC | Pinned image identity | Official writable paths, explicit requests and limits | Only packaged controller permissions |

All entries set `allowPrivilegeEscalation: false`, `capabilities.drop: [ALL]`, and `seccompProfile.type: RuntimeDefault`. If an upstream image cannot run with its listed identity and mounts, implementation stops and returns to this matrix instead of silently granting root or a writable root filesystem.

Secrets are base64 encoded Kubernetes Secrets, not encrypted application storage. That is acceptable only because the cluster is local and disposable. Secret values stay out of Helm release values and command output. Production must replace this source before any public deployment.

When `VERMOUTH_GOOGLE_AUTH_ENABLED=false`, identity ignores Google credential presence and does not require the three Google variables. Missing credentials never disable auth implicitly when the flag is true. Auth start and callback return the standard `APIError` with HTTP 503 and code `auth_unavailable`. `/config.json` returns `{"googleAuthEnabled":false}` with `Cache-Control: no-store`. The disabled button uses `aria-describedby` and this fixed message in the active language: English, `Google sign in is not configured for this local environment. Use task dev:token for the development thread.` Vietnamese, `Đăng nhập Google chưa được cấu hình cho môi trường cục bộ này. Hãy dùng task dev:token để chạy luồng phát triển.`

When the flag is true, all three Google variables are required and `IDENTITY_GOOGLE_REDIRECT_URL` must equal `http://vermouth.localhost:8080/api/auth/google/callback` character for character. Validation happens before any image, Secret, Job, or Helm mutation. A missing or mismatched value fails with the required callback in the diagnostic. The OpenAPI document records the 503 response, and the browser runtime config changes without rebuilding the web image.

### Configuration required

1. `VERMOUTH_GOOGLE_AUTH_ENABLED`: public local runtime flag. `false` lets the platform start without OAuth secrets.
2. `IDENTITY_GOOGLE_CLIENT_ID`, `IDENTITY_GOOGLE_CLIENT_SECRET`, `IDENTITY_GOOGLE_REDIRECT_URL`: required only when Google auth is enabled.
3. `GARAGE_RPC_SECRET`, `GARAGE_ADMIN_TOKEN`: generated local Garage control secrets.
4. `BILLING_S3_ENDPOINT`, `BILLING_S3_REGION`, `BILLING_S3_BUCKET`, `BILLING_S3_ACCESS_KEY_ID`, `BILLING_S3_SECRET_ACCESS_KEY`: billing only object storage configuration.
5. `deploy/platform/versions.yaml`: host, cluster, registry, volume, readiness, deletion, and compatibility source. It contains no secret.
6. `deploy/images.lock.yaml`: external provenance, repository build inventory, tag templates, and canonical input hash schema. It contains no registry credential.
7. `deploy/platform/images.schema.json`: exact generated image state shape and field requirements.
8. `deploy/platform/secrets.schema.json`: exact generated Secret state shape and metadata requirements.
9. `deploy/platform/secret-inventory.yaml`: exact Secret keys, database fields, URL templates, sources, generation rules, consumers, and hashes. It contains no value.
10. `deploy/helm/vermouth/files/traffic-matrix.yaml`: exact allowed traffic rows and rendered policy mapping.
11. `deploy/helm/vermouth/files/workload-matrix.yaml`: exact replicas, resources, probes, identities, and writable mounts.
12. `deploy/helm/vermouth/values.schema.json`: committed values and generated runtime values shape.
13. Existing database, broker, service address, signing key, token public key, and application settings remain environment variables, but their local cluster values use service names instead of host ports.

### Critical test scenarios

1. Happy path: from a stopped Colima profile and an initialized checkout, bootstrap and `task dev` reach `vermouth.localhost:8080`, then `task thread` records the identity event, verifying **AC-1**, **AC-2**, **AC-4**, and **AC-13**.
2. Cold start order: from no cluster, observe the foundation release become ready before Garage initialization, four migration Jobs, and the application release, verifying **AC-2**, **AC-3**, and **AC-11**.
3. Full topology: every workload, Job, Ingress, Secret reference, NetworkPolicy, security context, resource value, and PVC matches the committed matrices and readiness is green, verifying **AC-3**, **AC-9**, **AC-10**, **AC-16**, and **AC-17**.
4. Persistence: write distinct markers to all four databases, Redpanda, and Garage, redeploy and stop then start the cluster, and read every marker again, verifying **AC-5**.
5. Test isolation: run the existing real infrastructure integration suite through Compose with the k3d cluster stopped, verifying **AC-6**.
6. Inner loop: change one service, redeploy it, confirm only its digest and rollout change, then confirm its idempotent migration Job ran, verifying **AC-7**.
7. Portability: run `task images:multi`, inspect every required manifest, then pull and start one health capable image under each architecture, verifying **AC-8**.
8. Failure before mutation: break one build and one registry push, then confirm migrations, Secret references, and releases do not change even if unreferenced registry content exists, verifying **AC-11**.
9. Interrupted release: interrupt after new Secret creation, after one migration completes, and after all migrations but before Helm. Each rerun resumes safely, keeps the old application usable, and never runs a down migration, verifying **AC-11** and **AC-12**.
10. Failed rollout: break a readiness probe, confirm `maxUnavailable: 0` kept the old Pod serving until replacement readiness failed, then confirm the application release and Secret references roll back while the foundation stays, verifying **AC-11**.
11. Garage recovery: interrupt after layout, bucket, key, and grant in separate runs, then confirm reconciliation creates no duplicate and refuses conflicting key material, verifying **AC-16**.
12. Capacity and collision: exhaust a PVC, lower available Colima memory, occupy port 8080, and occupy port 5111 in separate runs. Each case stops with the named diagnostic and preserves data, verifying **AC-12**, **AC-15**, and **AC-17**.
13. Auth disabled: remove Google OAuth credentials, start successfully, observe the fixed accessible unavailable message and no store runtime config, then complete the development token thread, verifying **AC-14**.
14. Resource envelope: start the full platform in a 4 CPU and 8 GiB Colima profile, capture five minutes of use, and confirm the committed aggregate request and observed ceilings, verifying **AC-15**.
15. Destructive guard: select another current context and confirm every platform command still targets explicit `k3d-vermouth` without changing the selection. Remove or stop the target cluster and confirm Docker and k3d metadata decide identity. Use a wrong typed confirmation and confirm no deletion, then confirm clean removes the known cluster and recreate removes the old data before producing an empty ready cluster, verifying **AC-5** and **AC-12**.
16. Host transport: capture the port 80 listener set before cluster creation, then confirm cluster creation adds none. Confirm the load balancer publishes only container port 80 to host `127.0.0.1:8080`, the registry publishes only container port 5000 to host `127.0.0.1:5111`, no Vermouth container publishes host port 80, and macOS listens on the two desired ports only through IPv4 loopback. Require both an ordinary hostname request and a forced IPv4 request with Host header `vermouth.localhost`. Confirm k3d and Docker name the registry `vermouth-registry`, containerd mirrors `vermouth-registry:5000` to `http://vermouth-registry:5000`, the exact registry and storage volumes carry their committed labels, runtime image references use that cluster name plus the remote digest, a removed CRI probe reference pulls again, and the Colima profile and Docker daemon configuration hashes are unchanged, verifying **AC-2**, **AC-4**, **AC-8**, **AC-11**, and **AC-17**.
17. Publication staging: fail the last native build and the last two architecture build, confirm no registry change, then fail child and final manifest publication separately and confirm only unreferenced content remains while generated state and releases stay unchanged, verifying **AC-8** and **AC-11**.
18. State and concurrency: interrupt each atomic generated file write, start two mutating commands from different worktrees, expire a dead owner lock, and create a pending Helm revision. Confirm valid prior files survive, one owner proceeds, live locks cannot be stolen, stale recovery requires confirmation, and the last deployed revision is restored before a new release, verifying **AC-7**, **AC-11**, and **AC-12**.
19. Executable inputs: remove and add unknown rows or keys in each platform input, generated state schema, runtime values schema, image hash field, Secret database field, and rendered policy or workload mapping. Confirm validation fails before mutation and names the source file and row, verifying **AC-1**, **AC-9**, **AC-10**, **AC-11**, and **AC-17**.
20. Garage credential proof: after reconciliation, use the desired billing credentials for signed Put, Get, and Delete of the unique zero byte probe object. Change only the live Garage secret and expect authentication refusal without accepting the foundation Secret hash as proof, verifying **AC-16**.
21. Host storage: run native and multiple architecture publication, then require zero unused host images carrying `vermouth.dev/workload`. Record BuildKit cache size, reject a wrong cache cleanup confirmation, accept `vermouth`, require zero reclaimable cache, and confirm the live platform remains ready, verifying **AC-8** and **AC-17**.

## Build plan

The Tracer Bullet approach puts one real request through the cluster before the platform is thickened.

1. Land the executable platform inputs first: `platformconfig`, authoritative versions, complete image and Secret inventories, generated state and runtime values schemas, canonical image hash encoding, separate tag templates, remote descriptor authority, exact registry and volume identities, traffic and workload matrices, Colima and builder identity, loopback binds, staged publication, cold node transport probe, atomic generated state, explicit context targeting, mutation locks, and doctor validation, satisfies **AC-1**, **AC-2**, **AC-8**, **AC-9**, **AC-10**, **AC-11**, **AC-12**, and **AC-17**.
2. Build the first complete cluster thread: replace Envoy resources and installation with pinned packaged Traefik configuration and one Ingress, align local k3s to `v1.36.3-k3s1`, bootstrap, one chart with foundation and application gates, web runtime, identity and notifications Postgres, Redpanda, content named Secrets, two migration Jobs, the versioned development token image and Job, `task dev`, and `task thread`, satisfies **AC-1**, **AC-2**, **AC-4**, **AC-9**, **AC-10**, **AC-13**, and **AC-17**.
3. Thicken the same two releases with teaching and billing, their separate Postgres StatefulSets and PVCs, migration images and Jobs, remaining content named Secrets, exact matrix rows, rolling strategy, and full gateway readiness schema, satisfies **AC-3**, **AC-9**, **AC-10**, and **AC-17**.
4. Add Garage with separate metadata and data PVCs, its exact key and layout initializer, signed S3 credential proof, private invoice bucket, billing only key, stop and guarded clean behavior, then prove all persistent markers survive normal lifecycle operations, satisfies **AC-5** and **AC-16**.
5. Complete the inner loop and failure state machine: service redeploy, immutable digest records, versioned Secret retention, bounded waits, parallel migration failure preservation, exact Helm rollback, pending release recovery, full status and log inventory, failed Job cleanup, lock recovery, and destructive confirmed recreate, satisfies **AC-7**, **AC-11**, and **AC-12**.
6. Finish the portability, storage, and resource proof: stage then publish every required two platform manifest, inspect remote indexes, run the exact web smoke under both architectures, remove project owned host images, add confirmed shared cache cleanup, validate both release renderings against executable matrices, exercise interruption, disk, port, Garage, rollout, generated state, and concurrency failures, run Compose isolation, measure the exact committed formula, and document the daily commands, satisfies **AC-6**, **AC-8**, **AC-14**, **AC-15**, and **AC-17**.

## Consequences

**Positive**:

1. One command proves the real cluster path while the fast, known Compose test path remains available.
2. Immutable image digests and ordered migration gates make the running state explainable after a failed build.
3. The same Dockerfiles create native Apple silicon images and `amd64` images for the later Azure move.
4. Default deny networking makes the database ownership rules physical inside Kubernetes.
5. The registry transport works without a persistent exception in the Colima profile or Docker daemon.

**Negative / tradeoffs**:

1. Packaged Traefik ties routing to the Kubernetes Ingress model and k3s chart values rather than Gateway API portability.
2. Four Postgres StatefulSets, Redpanda, Garage, Traefik, and the k3s control plane consume meaningful laptop memory before product work starts.
3. Local path PVCs survive cluster stop but not the confirmed cluster deletion. There is no backup or disaster recovery in this feature.
4. Forward only migration recovery requires every schema change to remain compatible with the previous application revision.
5. Local Kubernetes Secrets are not a production secret system. Feature 16 must replace their source before public deployment.
6. Helm rollback restores Kubernetes resources, not database changes, broker events, object writes, or other business effects a briefly ready Pod already produced. Idempotency and forward compatibility remain the recovery tools.
7. The registry retains unreferenced content until confirmed clean or recreate, so repeated development builds consume disk.
8. Host and cluster callers use different registry names, so generated digest records must keep their repository field explicit.
9. Two architecture builds use local archives and temporary child tags. Successful publication removes their host image records, but BuildKit cache remains until the engineer runs the explicit shared cache cleanup command. That cleanup makes the next build slower.
10. Platform configuration now has one Go parser and four machine readable inventories. This is more structure, but it removes duplicated shell and Helm decisions.

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

1. Land authoritative inventories, loopback host bindings, the k3d managed cluster registry name, staged image publication, transport probes, locks, and exact recovery before any application data is accepted. The current in progress cluster recorded the incorrect cluster endpoint and has no application release or persistent business data, so one confirmed recreate applies the corrected immutable identity.
2. Add bootstrap, cluster, chart, and a temporary `task dev:kubernetes` while the existing `task dev` remains unchanged.
3. Prove the end to end thread and persistence scenarios on k3d, then point `task dev` at Kubernetes and retain the old host startup as `task dev:host` for one milestone.
4. Remove `task dev:host` after the Kubernetes verification passes. Keep `task infra:*` and Compose permanently for integration tests.

**Rollback**: Before phase 4, point `task dev` back to the host workflow and stop the k3d cluster. During phase 1, revert the platform commit and use confirmed recreate only while the cluster remains empty. Compose data is untouched because the two platforms never share volumes. After phase 4, Git can restore the task alias while the k3d data remains available for diagnosis.

**Risks**: The two temporary development paths can drift during phase 3. Keep that phase to one milestone and run the same `task thread` against both before cutover. Staged archives and child tags use extra local disk, so status reports their size and confirmed clean removes them.

## Rationale

Reasoning, options, and references: see [rationale.md](rationale.md).
