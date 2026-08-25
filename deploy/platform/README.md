# Local Kubernetes platform

This platform is for daily development on Apple silicon. Compose remains the integration test platform. The cluster and Compose never share data.

## Daily use

You may start everything with one command.

```bash
task dev
```

The command validates your tools and Colima profile. It builds all native images before it pushes any image. It then prepares Secrets, reconciles the foundation release, initializes Garage, runs four migrations, upgrades the application release, and waits for the public path. It prints `http://vermouth.localhost` only when readiness passes.

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
```

A failed migration Job remains for diagnosis. You may remove one named finished Job after reading its logs.

```bash
task platform:jobs:clean -- migrate-identity-0123456789ab
```

The application upgrade uses Helm atomic rollback. A failed rollout restores only the application release. The foundation and any forward migration remain.

## Data lifecycle

`task platform:stop` stops the cluster and registry. Their volumes remain.

`task platform:clean` prints the exact target and asks you to type `vermouth`. It deletes the cluster, registry, databases, broker log, Garage objects, and local registry data.

`task platform:recreate` uses the same confirmation, deletes the old platform, and bootstraps an empty one.

## Portable images

You may build and inspect every deployment artifact for both required architectures.

```bash
task images:multi
```

The command pushes manifest lists for `linux/arm64` and `linux/amd64`. The local development token image stays native and is not a deployment artifact.

## Resource envelope

The steady declared requests are 660 millicpu and 1664 MiB. This includes five Go applications, web, four Postgres instances, Redpanda, Garage, the Envoy controller, and the Envoy proxy. Jobs are short lived and are outside the steady total. The platform gate still requires a 4 CPU and 8 GiB Colima profile because k3s, the registry, image builds, and runtime overhead sit outside Pod requests.
