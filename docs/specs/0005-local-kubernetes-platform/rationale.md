# Rationale for 0005. Local Kubernetes platform and one command startup

## Context

> ⚠️ Premise note: Kubernetes is more platform than this product needs for local development. The known failure is spending the solo build budget on controllers, charts, and storage before the tutor money path works. The right framing is a thin learning platform with one node, one replica, no TLS, no high availability, no GitOps, and no cloud provisioning. Anything beyond that belongs to feature 16 or feature 9.

The current development path starts four Postgres containers and Redpanda through Compose, applies goose migrations from the host, builds five Go binaries, and starts them as host processes. This path already proves the event thread and backs integration tests. It does not exercise Kubernetes, Garage, static web serving, cluster networking, or the image promotion path that later deployment needs.

The target host is now an Apple silicon Mac, not the Azure VM recorded when spec 0002 was written. Colima, k3d, kubectl, Helm, and Buildx are installed. The current machine builds `arm64`, while a typical Azure VM uses `amd64`, so portability is a present build property even though Azure deployment stays deferred. The platform must fit in a Colima profile with at least 4 CPUs and 8 GiB and must stay understandable for one developer.

The existing boundaries remain fixed. Four services keep four databases. Redpanda keeps durable retention and explicit topic creation. Garage is the chosen S3 compatible store. Compose remains mandatory for integration tests. Configuration remains environment only, migrations run before services, and every public request enters through the Go gateway.

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

## Rationale

Option 2 is the narrowest choice that meets the actual learning goal. Keeping Compose is not duplication by accident. It preserves the fast, real infrastructure test harness required by STK-15, while k3d proves the deployment concerns that Compose cannot. This is the strangler pattern applied to a development platform, with a short cutover for `task dev` and no data migration between runtimes.

Packaged Traefik is the simpler edge and now the deliberate choice. The inspected production VM already runs Traefik `3.7.8` from packaged chart `40.1.4+up40.1.0`, and the engineer values development and production parity more than Gateway API portability. Keeping the k3s add on avoids another controller, CRD set, proxy, and lifecycle. One standard Ingress preserves the same host and path contract.

The image path separates fast daily work from portability proof. Native `arm64` builds keep the edit loop reasonable. The explicit multiplatform command uses one Dockerfile per workload to produce `arm64` and `amd64` manifest entries, which proves the Azure architecture without adding a remote registry now. Immutable digests keep Helm rollbacks meaningful even with uncommitted source changes.

Explicit migration Jobs are safer here than init containers or Helm hooks. One chart renders a foundation release and an application release, so Task can wait for stateful infrastructure, run each service with only its own migrations and credentials, and stop before changing the application release. Content named Secrets keep the old application on its old configuration until the upgrade succeeds. This preserves STK-21 while avoiding first install hook ordering, replica races, and a rollback that accidentally touches databases or the broker.

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
6. Dynamic PVC provisioning with `ReadWriteOnce` storage for one node stateful workloads.

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
