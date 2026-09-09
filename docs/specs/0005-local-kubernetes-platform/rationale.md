# Rationale for 0005. Local Kubernetes platform and one command startup

## Context

> ⚠️ Premise note: Kubernetes is more platform than this product needs for local development. The known failure is spending the solo build budget on controllers, charts, and storage before the tutor money path works. The right framing is a thin learning platform with one node, one replica, no TLS, no high availability, no GitOps, and no cloud provisioning. Anything beyond that belongs to feature 16 or feature 9.

> ⚠️ Verification note: the original 49 item runtime checklist mixed a repeatable release gate with destructive fault qualification. It could prove many real behaviors and still report only `BLOCKED`, which hid the working platform and made completion impractical. The repeatable gate now exercises every user visible and operational acceptance criterion through fourteen real workflows. Detailed failure contracts remain executable invariants and regression tests. They return to the release gate only when their owning input or recovery implementation changes.

The current development path starts four Postgres containers and Redpanda through Compose, applies goose migrations from the host, builds five Go binaries, and starts them as host processes. This path already proves the event thread and backs integration tests. It does not exercise Kubernetes, Garage, static web serving, cluster networking, or the image promotion path that later deployment needs.

The target host is now an Apple silicon Mac, not the Azure VM recorded when spec 0002 was written. Colima, k3d, kubectl, Helm, and Buildx are installed. The current machine builds `arm64`, while a typical Azure VM uses `amd64`, so portability is a present build property even though Azure deployment stays deferred. The platform must fit in a Colima profile with at least 4 CPUs and 8 GiB and must stay understandable for one developer.

The existing boundaries remain fixed. Four services keep four databases. Redpanda keeps durable retention and explicit topic creation. Garage is the chosen S3 compatible store. Compose remains mandatory for integration tests. Configuration remains environment only, migrations run before services, and every public request enters through the Go gateway.

The first Docker 29.7.2 startup attempt built every native image, then failed before the first registry upload. Docker treated `k3d-vermouth-registry.localhost:5111` as an HTTPS registry, while the k3d managed registry served plain HTTP. No image content, Secret, Job, or Helm release changed. The transport contract therefore needs one host address Docker accepts for local HTTP and a separate internal address Kubernetes can resolve, without changing the preserved Colima profile.

The next startup proved the host fix, then exposed an incorrect cluster name. K3d `v5.9.0` created registry `vermouth-registry`, attached Docker network DNS name `vermouth-registry`, and generated containerd mirror key `vermouth-registry:5000` with endpoint `http://vermouth-registry:5000`. The specified image name `k3d-vermouth-registry:5000` had no DNS record or mirror, so the first Garage initialization Pod reached `ImagePullBackOff`. The internal endpoint must use the identity k3d actually owns rather than an invented prefix.

The exact Docker application bind `127.0.0.1:80:80` then exposed a host boundary mismatch. Inside Colima, Docker kept the requested IPv4 loopback bind. On macOS, Colima forwarded the privileged port as an IPv6 wildcard listener. The host listener therefore did not prove the local only contract. The application needs a host port whose exact loopback bind survives Colima forwarding, without changing the preserved profile or adding privileged host state.

A completeness pass found related ambiguity in image identity, generated state, stopped cluster targeting, volume ownership, readiness, and measurement. Those gaps share one failure pattern: prose named an outcome but not the exact source or proof. This update therefore makes each identity a separate committed value, gives generated files executable schemas, makes remote descriptors authoritative, and requires live transport and credential checks before readiness.

## Options considered

### Option 1: Improve Compose and host startup in place

Keep the current runtime and make its setup more idempotent, while deferring Kubernetes to the public deployment feature (basis: the existing `Taskfile.yml`, `test/compose.test.yaml`, and the fix in place enhancement pattern).

**Pros**:

1. Least memory and fewest new failure modes.
2. Fastest route back to product work.

**Cons**:

1. Does not meet the scope goal of learning and proving the local Kubernetes path.
2. Leaves Garage, Ingress, cluster Secrets, PVCs, and two architecture promotion untested until deployment.

### Option 2: Add k3d beside Compose, with packaged Traefik

Use k3d for daily development and preserve Compose for tests. Build and push immutable local images, render foundation and application releases from one Helm chart, run explicit migration Jobs between those releases, and enter through the Traefik release packaged with the same k3s version production runs (basis: the strangler pattern, official k3d and k3s guidance, spec 0006, and the installed platform skills).

**Pros**:

1. Proves the future deployment shape without destabilizing the real integration test harness.
2. Gives one application chart and architecture neutral images to feature 16.
3. Development and production share the same edge generation and Ingress behavior.

**Cons**:

1. Kubernetes Ingress ties route behavior to Traefik rather than the more portable Gateway API model.
2. Two infrastructure paths remain on purpose and must not drift in their shared service configuration.

### Option 3: Replace Compose with k3d for development and tests

Run every local and integration workflow against Kubernetes and remove Compose (basis: one platform as a simplification goal, weighed against STK-15).

**Pros**:

1. One runtime model and one set of service addresses.
2. Every integration test exercises cluster networking and storage.

**Cons**:

1. Makes routine Go tests pay cluster startup and scheduling costs.
2. Replaces a known real broker and database harness while solving no test correctness gap.
3. Increases flake and recovery time on the smallest feedback loop.

### Option 4: Use a remote development cluster now

Build on the Mac and send images to the Azure VM for every development loop (basis: the future Azure goal weighed against local iteration and registry operations).

**Pros**:

1. Exercises the eventual host and network earlier.
2. Keeps Kubernetes load off the Mac.

**Cons**:

1. Adds a remote registry, credentials, network latency, and VM availability to every edit.
2. Blurs feature 5 with feature 16 and makes offline development impossible.

### Host registry transport options

#### Option A: Literal IPv4 loopback plus the k3d managed cluster endpoint

Host Docker pushes and inspects through `127.0.0.1:5111`. K3d publishes the managed registry only on that IPv4 loopback address. Kubernetes uses k3d registry name and Docker network DNS name `vermouth-registry:5000`, then deploys by digest. The node requires the generated containerd mirror for that exact name before Task records runtime state. The measured `colima` Buildx builder uses driver `docker`, BuildKit `v0.30.0`, host networking, and both required platforms. All builds stage before publishing, and remote registry descriptors decide deployment digests (basis: the two observed Docker and k3d failures, the k3d managed registry contract, and the installed Buildx and Docker security skills).

**Pros**:

1. Uses Docker's local loopback path without a daemon trust exception.
2. Keeps the unauthenticated HTTP registry off the LAN.
3. Uses one internal identity across k3d inventory, Docker DNS, containerd mirrors, runtime references, status, stop, clean, and recreate.
4. Preserves the existing build before push failure boundary for native and two architecture workflows.

**Cons**:

1. Host and cluster callers use different repository names for the same content.
2. Staged archives and child tags consume additional local disk.

#### Option B: Add the custom hostname to Docker's insecure registry list

Keep `k3d-vermouth-registry.localhost:5111` and modify the Docker daemon inside Colima to allow plain HTTP for it (basis: Docker insecure registry configuration).

**Pros**:

1. Keeps the previous endpoint spelling.

**Cons**:

1. Mutates the preserved runtime profile and adds machine state outside the repository.
2. Requires restart and drift handling for the Docker daemon.

#### Option C: Add TLS to the local registry

Keep a custom hostname, issue a local certificate, configure registry TLS, and install trust for Docker and cluster containerd (basis: encrypted registry transport).

**Pros**:

1. Uses authenticated transport semantics and avoids an insecure registry path.

**Cons**:

1. Adds certificate generation, trust installation, renewal, and recovery to a disposable local platform.
2. Changes host and cluster trust stores, which is more persistent state than the product needs.

#### Option D: Preserve the prefixed cluster name with a custom alias

Keep `k3d-vermouth-registry:5000` by adding a custom Docker network alias and a second containerd mirror that points at the k3d managed registry (basis: Docker network aliases and containerd registry mirrors).

**Pros**:

1. Preserves the earlier cluster endpoint spelling.

**Cons**:

1. Creates a second registry identity that k3d does not own or report.
2. Adds immutable cluster configuration and a recreate solely to preserve an incorrect prefix.

### Host application transport options

#### Option A: Use unprivileged host port 8080 on exact IPv4 loopback

K3d maps `127.0.0.1:8080` on the Colima Docker host to port 80 on its load balancer. Colima forwards the high port to the same exact macOS loopback address. The local origin becomes `http://vermouth.localhost:8080` (basis: the observed port 80 listener, Colima automatic port forwarding behavior, and least privilege host networking).

**Pros**:

1. Preserves the local only boundary without root access or persistent host configuration.
2. Keeps the Colima profile and Docker daemon untouched.
3. Lets doctor prove the Docker bind and macOS listener with the same address and port.

**Cons**:

1. Every local URL and OAuth callback must include `:8080`.
2. An existing port 8080 user causes a startup collision that the developer must clear.

#### Option B: Keep host port 80 and change Colima forwarding

Change the Colima profile or its generated Lima rules so the privileged forward binds only IPv4 loopback (basis: Colima and Lima port forwarding configuration).

**Pros**:

1. Preserves the short local URL with no explicit port.

**Cons**:

1. Violates the requirement that platform commands preserve the existing Colima profile.
2. Creates machine state outside the repository and makes profile upgrades part of Vermouth recovery.

#### Option C: Keep host port 80 behind a privileged host relay or firewall rule

Run a root owned proxy or install a macOS packet filter rule that confines port 80, then relay to an unprivileged Colima port (basis: host firewall confinement and local proxy patterns).

**Pros**:

1. Preserves the short local URL and can confine external traffic.

**Cons**:

1. Adds root access, host firewall ownership, cleanup, and recovery to one developer startup.
2. A stale rule or relay can affect unrelated local software after Vermouth stops.

## Rationale

Option 2 is the narrowest choice that meets the actual learning goal. Keeping Compose is not duplication by accident. It preserves the fast, real infrastructure test harness required by STK-15, while k3d proves the deployment concerns that Compose cannot. This is the strangler pattern applied to a development platform, with a short cutover for `task dev` and no data migration between runtimes.

Packaged Traefik is the simpler edge and now the deliberate choice. The inspected production VM already runs Traefik `3.7.8` from packaged chart `40.1.4+up40.1.0`, and the engineer values development and production parity more than Gateway API portability. Keeping the k3s add on avoids another controller, CRD set, proxy, and lifecycle. One standard Ingress preserves the same host and path contract.

The image path separates fast daily work from portability proof. Native `arm64` builds keep the edit loop reasonable. The explicit multiplatform command uses one Dockerfile per workload to produce `arm64` and `amd64` manifest entries, which proves the Azure architecture without adding a remote registry now. Immutable digests keep Helm rollbacks meaningful even with uncommitted source changes.

Explicit migration Jobs are safer here than init containers or Helm hooks. One chart renders a foundation release and an application release, so Task can wait for stateful infrastructure, run each service with only its own migrations and credentials, and stop before changing the application release. Content named Secrets keep the old application on its old configuration until the upgrade succeeds. This preserves STK-21 while avoiding first install hook ordering, replica races, and a rollback that accidentally touches databases or the broker.

For registry transport, Option A follows both proven boundaries. Literal `127.0.0.1:5111` avoids host name and IPv6 ambiguity. Inside the k3d network, `vermouth-registry:5000` is the name created by k3d, resolved by Docker, and configured as a plain HTTP containerd mirror. The measured Docker driver shares the Colima daemon network and supports both required platforms, so it avoids a separate BuildKit transport exception. The split repository path and remote descriptor digest are explicit in generated state, so a host reference can never leak into a workload.

For application transport, Option A removes the privileged port from Colima's automatic forwarding path. Port 8080 keeps the exact IPv4 loopback bind already required by the platform and needs no new operator or root owned state. The explicit port in local URLs is a small visible cost. It is safer than making a disposable learning platform own the developer's Colima profile or macOS firewall. Doctor must check both the Docker mapping and the host listener because either layer can widen exposure.

The platform never depends on the user's selected Kubernetes context. Explicit context arguments protect unrelated clusters, while k3d metadata, Docker labels, volume labels, and committed hashes still prove an absent or stopped target. This keeps bootstrap and guarded deletion possible without weakening target identity.

Image tags use separate native and multiple architecture hash domains because one immutable tag cannot safely change from a single manifest into an index. The remote registry descriptor is the deployment digest. Full input hash and provenance labels prove whether an existing local tag belongs to the same canonical input. Generated state schemas, cold CRI pulls, direct registry checks, signed Garage requests, and derived policy counts turn the remaining readiness claims into executable evidence.

Staging every archive before publication is the necessary cost of AC-11. Direct sequential `buildx --push` would let an early image enter the registry before a later image build failed. Once publication begins, a failure is correctly classified as a push failure and may leave content no release uses. Atomic generated files and one mutation owner preserve the last complete deployable state through interruption.

Host storage needs a separate ownership boundary. Vermouth can identify its own loaded images through `vermouth.dev/workload`, so successful publication and confirmed clean may remove those unused records safely. BuildKit cache has no repository boundary in the Docker driver and is shared by every project using builder `colima`. Automatic pruning would silently slow unrelated projects. A separate confirmed cache cleanup command makes that shared effect visible and deliberate while giving the developer a complete SSD recovery path.

Option B violates the promise that bootstrap preserves the Colima profile. Option C adds certificate lifecycle to a disposable one developer registry. Option D adds a custom alias and mirror with no product benefit, while making k3d inventory and runtime identity disagree. None offers enough value to justify its operational cost.

The application transport alternatives follow the same boundary. Changing Colima would break the preservation promise. A privileged host relay or firewall rule would replace one forwarding defect with a larger host lifecycle. Port 8080 is the only option that stays fully owned by the repository and remains reversible by stopping the cluster.

## References

**Project sources**:

1. `AGENTS.md`, the Tracer Bullet build approach, environment only configuration, one database per service, image, test, and Task conventions.
2. `docs/scope/scope.md`, feature 5 intent and its one command, gateway reachability, and persistence outcomes.
3. `docs/specs/0002-stack-and-scaffold/index.md`, especially STK-5, STK-15, STK-16, STK-17, STK-21, STK-23, STK-24, the memory budget, Garage, and the empty `deploy/` ownership.
4. `test/compose.test.yaml`, the current four Postgres and Redpanda settings that Kubernetes must preserve.
5. `Taskfile.yml`, the current setup, migration, thread, image, and Compose command surfaces.
6. The implementation skills named in [index.md](index.md), which define the Kubernetes, Helm, Buildx, container security, S3, and goose conventions applied here.

**Practices and standards**:

1. Strangler migration for replacing a working operational path.
2. Immutable image references and content addressed deployment.
3. Forward compatible database migration with no automatic down migration.
4. Least privilege Secrets, RBAC, security contexts, and default deny NetworkPolicies.
5. Standard Kubernetes Ingress ownership and longest path matching.
6. Static local PersistentVolumes with `ReadWriteOnce`, retained data, and fixed node affinity.
7. Loopback confinement for an unauthenticated local service.
8. Least privilege host networking, with no root owned relay or firewall state for local development.

**Links**:

1. k3d managed registries: https://k3d.io/v5.0.0/usage/registries/
2. K3s packaged Traefik: https://docs.k3s.io/networking/networking-services
3. K3s HelmChartConfig: https://docs.k3s.io/add-ons/helm
4. Helm introduction and release model: https://helm.sh/docs/intro/
5. Helm chart best practices: https://docs.helm.sh/docs/chart_best_practices/
6. Docker two architecture builds: https://docs.docker.com/build/building/multi-platform/
7. Kubernetes Jobs: https://kubernetes.io/docs/concepts/workloads/controllers/job/
8. Kubernetes persistent volumes: https://kubernetes.io/docs/concepts/storage/persistent-volumes/
9. K3s bundled local storage overview: https://docs.k3s.io/add-ons/storage
10. Garage Docker installation and pinned image: https://www.mintlify.com/deuxfleurs-org/garage/installation/docker
11. Docker Official Nginx image and architecture list: https://hub.docker.com/_/nginx
12. Colima automatic port forwarding and privileged port behavior: https://github.com/abiosoft/colima/discussions/1139
13. Colima port forwarding configuration: https://colima.run/docs/configuration/
