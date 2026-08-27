# Local Kubernetes platform

This platform is for daily development on Apple silicon. Compose remains the integration test platform. The cluster and Compose never share data.

## Daily use

You may start everything with one command.

```bash
task dev
```

The command validates your tools and Colima profile. It builds all native images before it pushes any image. It then prepares Secrets, reconciles the foundation release, initializes Garage, runs four migrations, upgrades the application release, and waits for the public path. It prints `http://vermouth.localhost:8080` only when readiness passes.

The command always targets Kubernetes context `k3d-vermouth`. It does not read or change your selected Kubernetes context. Docker pushes through `127.0.0.1:5111`. Cluster pulls use `vermouth-registry:5000`.

You may prove the event thread with this command.

```bash
task thread
```

You may rebuild one workload with this command.

```bash
task dev:redeploy -- identity
```

Database owners run one new migration Job before their rollout. The accepted names are `gateway`, `identity`, `teaching`, `billing`, `notifications`, and `web`.

## Inspect and recover

You may check pinned inputs and live drift before any mutation.

```bash
task platform:doctor
task platform:status
task platform:logs -- identity --previous
task platform:logs -- all
task platform:measure
task platform:locks:verify
```

A failed migration Job remains for diagnosis. You may remove one named failed Job after reading its logs.

```bash
task platform:jobs:clean -- migrate-identity-0123456789ab
```

One mutating command owns the platform at a time. A live lock reports its command and age. If its process is gone and its deadline plus grace has passed, you may inspect and clear it with typed confirmation.

```bash
task platform:lock:clear
```

The application upgrade uses Helm atomic rollback. A failed rollout restores only the application release. The foundation and any forward migration remain.

## Data lifecycle

`task platform:stop` stops the cluster and registry. Their volumes remain.

`task platform:clean` prints the exact target and asks you to type `vermouth`. It deletes the cluster, registry, databases, broker log, Garage objects, local registry data, generated state, and unused host images carrying the Vermouth workload label.

`task platform:recreate` uses the same confirmation, deletes the old platform, and bootstraps an empty one.

## Portable images

You may build and inspect every deployment artifact for both required architectures.

```bash
task images:multi
```

The command stages every `linux/arm64` and `linux/amd64` archive before it writes to the registry. It then publishes immutable child manifests, creates the final index, checks the remote descriptor, and runs the web image under both architectures. The local development token image stays native and is not a deployment artifact.

Successful native and multiple architecture publication removes unused Vermouth host images. The running cluster keeps its own content addressed images in k3s containerd, so this does not restart a workload.

BuildKit cache belongs to the shared Colima builder and is not removed automatically. You may inspect and clear every reclaimable cache record with explicit confirmation. This affects other projects using builder `colima`, and the next build will be cold.

```bash
task platform:cache:clean
```

## Resource envelope

The steady declared requests are 635 millicpu and 1600 MiB. This includes five Go applications, web, four Postgres instances, Redpanda, Garage, and packaged Traefik. Jobs are short lived and are outside the steady total. `task platform:measure` samples Colima, Docker, and Kubernetes every five seconds for five minutes and writes the result under `.tmp/platform/reports/`. The platform gate still requires a 4 CPU and 8 GiB Colima profile because k3s, the registry, image builds, and runtime overhead sit outside Pod requests.
