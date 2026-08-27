# Vermouth Helm chart

One chart renders three ownership modes.

| Value | Owner | Result |
|---|---|---|
| `foundation.enabled` | `vermouth-foundation` | Four Postgres StatefulSets, Redpanda, Garage, static retained volumes, ServiceAccounts, and fixed NetworkPolicies |
| `application.enabled` | `vermouth` | Five Go Deployments, web, Services, ConfigMaps, one Traefik Ingress, and content named Secret references |
| `jobs.migrations.enabled` | Task | One uniquely named Goose Job for each selected service |
| `jobs.garageInit.enabled` | Task | One idempotent Garage reconciliation Job |
| `jobs.devtoken.enabled` | Task | One development token Job |

`values-local.yaml` holds local infrastructure image digests. `.tmp/platform/runtime-values.yaml` holds the current repository image digests and Secret names. Neither file carries a Secret value.

`files/traffic-matrix.yaml` owns every allowed connection. `files/workload-matrix.yaml` owns replicas, resources, probes, identities, and writable mounts. Validation compares every rendered policy and workload with those source rows.

You may lint and render every mode with this command.

```bash
task platform:validate
```

Task owns release ordering. You should not install both release gates under one Helm release because that removes the migration and rollback boundary recorded in spec 0005.
