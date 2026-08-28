# 0006. Cloud deployment for friend testing

**Date**: 2026-08-25
**Status**: In Progress

## Summary

Vermouth will run on the existing Azure VM through its existing one node k3s installation. The VM has 2 CPUs and 8 GiB of memory, uses packaged Traefik for HTTPS, pulls private Docker Hub images by digest, and keeps state on the dedicated Azure disk when available. GitHub Actions builds and deploys only after manual confirmation, while local Task commands provide inspection, export, restore, and rollback.

## Requirements

**User stories**:

1. As the operator, I want friends to reach Vermouth through a stable secure address so they can test the real sign in and invoice flow.
2. As the operator, I want development and production to use the same k3s, Traefik, Helm, image, migration, and health model so a production release is not a second platform.
3. As the operator, I want deployment failure to keep the previous application and data intact so recovery begins from a working release.
4. As the operator, I want a free manual export to my Mac so the lack of paid backup does not mean there is no recovery path at all.

**Acceptance criteria**:

1. **AC-1**: `task prod:doctor` verifies SSH host identity, the expected Azure VM shape of 2 CPUs and 8 GiB, k3s live version `v1.36.3+k3s1`, k3s image tag `v1.36.3-k3s1`, packaged Traefik image `3.7.8` and chart `40.1.4+up40.1.0`, Docker Hub access, at least 3 GiB available memory, at least 15 GiB free operating system disk, the selected storage root, DNS resolution, ports 80 and 443, production configuration, and every pinned input. It changes nothing and points each failure to the next useful command.
2. **AC-2**: `task prod:bootstrap` targets only `4.194.251.123` through the existing operator SSH configuration, verifies `azureuser` plus sudo without storing that identity in GitHub, writes the XFS mount to `/etc/fstab` by filesystem UUID `37f07a74-8426-45fd-8e55-79515376a136` with options `defaults,nofail,x-systemd.device-timeout=30s`, creates one recorded storage identity before any persistent resource, creates eight deterministic static local PVs through StorageClass `vermouth-local`, configures one packaged Traefik replica with persistent ACME state, installs and proves the restricted `vermouth-deploy` identity, creates namespace `vermouth` and pull Secret `dockerhub-pull`, and stops before application deployment. Rerunning it changes nothing when desired state already exists.
3. **AC-3**: Before any Vermouth PV or PVC exists, bootstrap automatically selects `/var/lib/rancher/k3s/storage/vermouth` when the dedicated disk is unavailable and writes schema version 1 `/etc/vermouth/platform.json` plus the matching platform ConfigMap. After the first PV is created, storage mode, root, node, and disk UUID are immutable. Each stateful Pod verifies the selected root marker before its main container starts. A missing selected root then blocks Vermouth stateful workloads instead of creating empty directories elsewhere.
4. **AC-4**: A manually triggered GitHub Actions workflow requires the typed value `production`, builds the gateway, four services, web, Garage initializer, and four migration images for `linux/arm64` and `linux/amd64`, pushes one private Docker Hub manifest list per image, verifies both platforms, writes schema version 1 `images.json`, and changes no VM state until every required image succeeds. Repositories are `docker.io/<DOCKERHUB_NAMESPACE>/vermouth-<workload>`. Tags are `git-<12 character Git SHA>-<64 character build input SHA256>`, are never overwritten, and an existing tag with mismatched provenance fails.
5. **AC-5**: GitHub Actions uses a temporary dedicated SSH key for user `vermouth-deploy`. Password login, PTY, forwarding, and a general shell are disabled for that key. Its authorized key uses `restrict` plus forced command `/usr/local/sbin/vermouth-ssh-entrypoint`. The entrypoint accepts only the versioned protocol in this spec, validates the original command, bundle checksum, and digest document, then may call root owned `/usr/local/sbin/vermouth-deploy-root` through one exact passwordless sudo rule. The existing `azureuser` key and unrestricted sudo are not stored in GitHub. Port 22 remains public while this transport is active; Azure network rule ownership and key removal remain operator actions.
6. **AC-6**: Production uses the same Helm chart and `vermouth-foundation` plus `vermouth` releases as local development. A deployment prepares content named immutable Secrets, reconciles foundation, initializes Garage, runs four bounded migration Jobs, upgrades the application with public Ingress disabled, waits for internal readiness and capacity, then enables Ingress and waits for HTTPS readiness. Helm keeps at most five revisions. The command prints the public hostname, Git SHA, GitHub run ID, image digests, and final Helm revision only after success.
7. **AC-7**: The ready platform contains web, gateway, identity, teaching, billing, notifications, four separate Postgres 18 instances, one Redpanda node, one Garage node, and one packaged Traefik replica. Application workloads have one replica and `notifications` cannot exceed one replica.
8. **AC-8**: A static Azure Public IP `domainNameLabel`, configured by the operator before bootstrap, produces the exact `PROD_HOSTNAME` under `southeastasia.cloudapp.azure.com` and resolves to `4.194.251.123`. The production Traefik values configure entrypoints `web` and `websecure`, resolvers `letsencrypt-staging` and `letsencrypt-production`, HTTP challenge on `web`, storage files `/data/acme-staging.json` and `/data/acme.json`, PVC `traefik-acme`, and permanent HTTP to HTTPS redirect outside the challenge. Bootstrap proves staging issuance before the final Ingress selects the production resolver. Raw HTTP is never the production fallback.
9. **AC-9**: HTTPS serves web and routes `/api`, `/health`, and `/ready` to the gateway and `/config.json` plus `/` to web without path rewrite. Browser calls stay same origin. Google OAuth uses the exact HTTPS callback derived from `PROD_HOSTNAME`. `/config.json` is schema version 1 with `environment=production`, `googleAuthEnabled=true`, and `apiBasePath=/api`.
10. **AC-10**: Feature 21 auth rate limits are built, verified, and tested before the production Ingress may become externally ready. The current GitHub workflow must run target `task test:auth-rate-limit` and produce schema version 1 `rate-limit-evidence.json` containing the exact Git SHA, test target, threshold configuration SHA256, pass state, and timestamp. Missing, failed, or mismatched evidence blocks public launch.
11. **AC-11**: Azure network rules expose only ports 80 and 443 publicly plus temporary port 22 for the restricted deployment key. k3s API, kubelet, Postgres, Redpanda, Garage, registry, and all service ports remain externally blocked. The `vermouth` namespace begins with deny all NetworkPolicies and admits only the committed Traefik and service traffic matrix.
12. **AC-12**: `/etc/vermouth/production.env` is root owned, mode `0600`, outside the repository, and is the source for content named Kubernetes Secrets. Separate Docker Hub push and read tokens are used. The read token creates `kubernetes.io/dockerconfigjson` Secret `dockerhub-pull`. Runtime Secrets carry service and source hash labels. The sets referenced by the current and immediately previous deployable application revisions remain available. Garbage collection runs only after external readiness and removes no referenced set.
13. **AC-13**: The GitHub workflow runs `task test:migration-compat` for the exact Git SHA and emits schema version 1 `migration-compat-evidence.json` naming the previous application revision, migration set SHA256, test target, pass state, and timestamp. Missing or failed evidence blocks deployment. A build or push failure changes no Secret, migration, or Helm state. A pull or foundation failure leaves the application release untouched. A migration failure leaves the previous application and Secret references running, keeps failed Job logs, and never runs a down migration. A readiness failure rolls back only application resources.
14. **AC-14**: `task prod:rollback` selects the newest earlier Helm application revision whose status is deployed or superseded, whose image manifests still resolve, and whose immutable Secret set still exists. It displays current and target image plus Secret references, requires typed `vermouth`, never rolls back migrations or foundation data, and waits for external HTTPS readiness. An older or incomplete revision is rejected as not deployable.
15. **AC-15**: `task prod:status` reports VM capacity, selected storage identity, Helm revisions, image and Secret references, workloads, Jobs, static PVs and PVCs, Traefik certificate state, internal readiness, and external HTTPS readiness. `task prod:logs -- <target>` accepts `web`, `gateway`, `identity`, `teaching`, `billing`, `notifications`, four `postgres-<service>` targets, `redpanda`, `garage`, `traefik`, `garage-init`, and exact `migrate-<service>-<run-id>` Jobs, plus `all`. It supports current and previous containers, follow and bounded since modes, and never prints Secret values.
16. **AC-16**: Structured application and Traefik logs stay local. System journal use is capped at 512 MiB and container logs rotate. No paid log, metric, alert, or automated backup service is added in this feature.
17. **AC-17**: `task prod:export` disables the public Ingress, scales application Deployments to zero, produces four Postgres custom format dumps while Postgres is ready, then scales all Vermouth StatefulSets and packaged Traefik to zero before archiving the eight deterministic component directories. The schema version 1 `tar.zst.age` archive contains `manifest.json`, `images.json`, `config/production.env`, `postgres/<service>.dump`, `helm/foundation.json`, `helm/application.json`, and `storage/<component>/`. The manifest records timestamp, Git SHA, image digests, database and goose versions, storage identity, archive include list, and SHA256 checksums. The stream is encrypted to `PROD_AGE_RECIPIENT`, written only to the operator Mac, verified there, and the exact prior workload, Traefik, and Ingress state resumes after success or failure.
18. **AC-18**: `task prod:restore -- <archive>` shows VM, storage root, archive manifest, and current Helm state, requires typed `vermouth`, removes public Ingress, stops every Vermouth workload and packaged Traefik, verifies the `age` archive and checksums, and renames current component directories into `<selected-root>/.restore-safety/<run-id>` before extraction. On the existing cluster it keeps the fixed bound PV and PVC objects while replacing their stopped directories. On a new cluster it first bootstraps the recorded storage identity and applies the same static PV and PVC names from Git. It restores all eight component directories, verifies all four logical dumps are readable without applying them over restored Postgres files, recreates recorded Secrets from the encrypted production file, deploys the recorded images, proves every readiness path and event thread, then reopens HTTPS. A failed restore removes partial new directories, renames the safety directories back when possible, leaves traffic closed otherwise, and retains logs plus the safety state. The safety state is removed only by a later confirmed cleanup after a successful export.
19. **AC-19**: Deployment refuses below 3 GiB available memory or 15 GiB free operating system disk. With public Ingress disabled, it samples `/proc/meminfo` for total node memory and `kubectl top` for Vermouth plus k3s Pods every 5 seconds for 5 minutes. The maximum total node memory must stay below 7 GiB. It writes schema version 1 `resource-<run-id>.json` under `/var/lib/vermouth/reports/` and uploads the same file as a GitHub artifact. Failure rolls the application back to the prior deployable revision and restores its Ingress before reporting failure. Other applications on the VM are outside Vermouth ownership and are never inspected, stopped, changed, or deleted by Vermouth commands.
20. **AC-20**: Production state adds no table to any service database. GitHub Actions and Helm histories are the deployment record. Platform identity records hostname, k3s version, storage mode and root, disk UUID, chart hash, image lock hash, current Git SHA, GitHub run ID, and active image digest set.

## Decision

**Chosen option**: Option 1: Existing Azure VM with k3s, packaged Traefik, private Docker Hub images, and a dedicated data disk

Reuse the inspected `Standard_D2ads_v6` VM and its existing k3s installation. Align local k3d to the production k3s version and packaged Traefik, promote one two architecture image set through private Docker Hub, deploy through manually triggered GitHub Actions and a restricted temporary SSH command, and keep production state under one recorded local storage root.

**Implementation skills**: `kubernetes-specialist` (`jeffallan/claude-skills`, `.agents/skills/kubernetes-specialist/`) · `helm-chart-scaffolding` (`wshobson/agents`, `.agents/skills/helm-chart-scaffolding/`) · `azure-compute` (`microsoft/azure-skills`, `.agents/skills/azure-compute/`) · `azure-diagnostics` (`microsoft/azure-skills`, `.agents/skills/azure-diagnostics/`) · `docker-buildx` (`full-stack-skills/docker-skills`, `.agents/skills/docker-buildx/`) · `docker-security` (`full-stack-skills/docker-skills`, `.agents/skills/docker-security/`) · `go-goose` (`metalagman/agent-skills`, `.agents/skills/go-goose/`)

## Feature design

### Platform topology

| Resource | Choice | Ownership |
|---|---|---|
| VM | Existing Azure `Standard_D2ads_v6`, 2 CPU, 8 GiB, Ubuntu 24.04 | Azure and operator |
| Kubernetes | Existing one node k3s `v1.36.3+k3s1` | systemd `k3s` service |
| Edge | One packaged Traefik v3 replica | k3s packaged Helm release plus committed `HelmChartConfig` |
| Images | Private Docker Hub manifest lists, selected by digest | GitHub Actions builds, k3s pulls |
| Application | Existing `vermouth-foundation` and `vermouth` releases | Repository Helm chart |
| Data | Eight static local PVs for four Postgres instances, Redpanda, Garage metadata and data, and Traefik ACME | StorageClass `vermouth-local` under the selected root |
| Secrets | Root owned production file to immutable Kubernetes Secrets | Task creates, Helm references names only |
| Public name | Azure Public IP DNS label | Azure Public IP configuration |
| Recovery | Encrypted manual export to the operator Mac | Task over restricted SSH |

There is no application schema change.

The fixed production image inventory is `gateway`, `identity`, `teaching`, `billing`, `notifications`, `web`, `garage-init`, `identity-migration`, `teaching-migration`, `billing-migration`, and `notifications-migration`. The development token image is local only and never enters Docker Hub production promotion.

Every built manifest carries OCI annotations `org.opencontainers.image.source`, `org.opencontainers.image.revision`, `org.opencontainers.image.created`, and `vermouth.dev/build-input-sha256`. If a computed immutable tag already exists, the workflow inspects both platform manifests and reuses it only when revision, build input hash, image inventory name, and required platforms all match. Any mismatch is an immutable tag collision and fails before VM contact.

### Static storage contract

StorageClass `vermouth-local` uses provisioner `kubernetes.io/no-provisioner`, binding mode `WaitForFirstConsumer`, and no default annotation. Each PV uses volume mode `Filesystem`, access mode `ReadWriteOnce`, reclaim policy `Retain`, `local.path` under the selected root, and node affinity for live node `nguyenducloc-vm2`. Bootstrap precreates the directory and numeric ownership before the PV.

| PV and claim | Component directory | Request |
|---|---|---:|
| `postgres-identity` | `postgres-identity` | `2Gi` |
| `postgres-teaching` | `postgres-teaching` | `2Gi` |
| `postgres-billing` | `postgres-billing` | `2Gi` |
| `postgres-notifications` | `postgres-notifications` | `2Gi` |
| `redpanda` | `redpanda` | `4Gi` |
| `garage-metadata` | `garage-metadata` | `1Gi` |
| `garage-data` | `garage-data` | `5Gi` |
| `traefik-acme` | `traefik-acme` | `256Mi` |

`/etc/vermouth/platform.json` is written atomically before this StorageClass or any PV. It contains `schema_version`, `vm_name`, `node_name`, `storage_mode`, `storage_root`, `storage_uuid`, `selected_at`, and `locked`. A marker `.vermouth-storage` under the selected root carries the same identity hash. Doctor, the root deployment entrypoint, and a small init guard on each stateful Pod all require the marker. Missing locked storage never falls through to another path.

### Production Traefik contract

K3s owns the only Traefik release. Task renders `deploy/platform/traefik/values-common.yaml` plus `values-production.yaml` into HelmChartConfig `traefik` in `kube-system`. Common values match spec 0005. Production values add the following exact behavior:

1. EntryPoints `web` on port 80 and `websecure` on port 443 are exposed by the existing Traefik LoadBalancer Service.
2. EntryPoint `web` redirects to `websecure` with HTTPS and permanent status, while Traefik handles `/.well-known/acme-challenge/` before redirect.
3. Resolver `letsencrypt-staging` uses HTTP challenge on `web`, the Let’s Encrypt staging directory, and `/data/acme-staging.json`.
4. Resolver `letsencrypt-production` uses HTTP challenge on `web`, the Let’s Encrypt production directory, and `/data/acme.json`.
5. Both resolvers use `PROD_ACME_EMAIL`. Files live on claim `traefik-acme`, mounted at `/data`, and remain mode `0600`.
6. A temporary staging probe Ingress for `PROD_HOSTNAME` must obtain and serve the staging certificate once. Task then removes it and the final application Ingress selects `letsencrypt-production`.
7. The final Ingress uses `spec.ingressClassName=traefik` plus annotations `traefik.ingress.kubernetes.io/router.entrypoints=websecure`, `traefik.ingress.kubernetes.io/router.tls=true`, and `traefik.ingress.kubernetes.io/router.tls.certresolver=letsencrypt-production`.

The application chart keeps `values.yaml` as common defaults, `values-local.yaml` for `vermouth.localhost`, and `values-production.yaml` for public deployment. Production values require `PROD_HOSTNAME`, enable the final Ingress and TLS annotations above, set runtime config to `{"schemaVersion":1,"environment":"production","googleAuthEnabled":true,"apiBasePath":"/api"}`, select StorageClass `vermouth-local`, require `dockerhub-pull`, and contain image digests plus Secret names only. Secret values never enter any Helm values file.

### Operational data model

| State | Required fields | Source | Relationship |
|---|---|---|---|
| Deployment revision | GitHub run ID, Git SHA, Helm revision | GitHub Actions and Helm | Selects one image and Secret set |
| Image set | workload, repository, tag, digest, platforms | Completed Docker Hub push and manifest inspection | Selected by deployment revision |
| Platform identity | schema version, hostname, k3s version, storage mode, storage root, disk UUID, chart hash, image lock hash | Committed desired state plus live discovery | Immutable storage choice before first PV |
| Runtime Secret set | service, Secret name, canonical source hash | `/etc/vermouth/production.env` | Current and previous Helm revisions reference it |
| Persistent state | fixed PV and PVC name, deterministic local path, selected root, node affinity | committed eight component inventory | Survives application revisions and supports deterministic restore |
| Export manifest | time, Git SHA, digests, database versions, storage identity, checksums | `prod:export` | Describes one encrypted archive |

### State transitions

1. Storage: `unselected` to `dedicated` or `os-fallback`, then `locked` before the first PV. A locked missing root becomes `blocked`, never another storage mode.
2. Certificate: `absent` to `staging proven` to `production ready`. Any failure keeps public HTTPS closed.
3. Release: `source` to `images verified` to `foundation ready` to `migrations complete` to `application ready` to `external ready`.
4. Export: `running` to `quiesced` to `streaming` to `verified` to `running`. Failure returns to the prior running state and reports an incomplete export.
5. Restore: `running` to typed confirmation to `traffic closed` to `data restored` to `release restored` to `verified` to `traffic open`.

### Command and workflow surface

| Action | Key inputs | Success output | Authorization | Key failures |
|---|---|---|---|---|
| `task prod:doctor` | committed config, SSH target | read only readiness report | operator SSH key | identity, capacity, mount, DNS, TLS, rate limit, or registry mismatch |
| `task prod:bootstrap` | VM, disk UUID, hostname, root Secret file metadata | ready base platform and restricted deploy identity, no app | existing operator SSH identity plus sudo | wrong VM, unsafe mount, existing PV conflict, Traefik or deploy identity failure |
| GitHub `workflow_dispatch` | Git ref, typed `production` | verified digest set and deployed revision | GitHub production Secrets plus restricted SSH key | build, push, SSH, migration, Helm, capacity, or readiness failure |
| `task prod:deploy` | Git ref, typed `production` | GitHub workflow URL and final revision | GitHub CLI identity | invalid ref, confirmation, or workflow failure |
| `task prod:status` | SSH target | capacity and platform summary | operator SSH key | unreachable VM or incomplete state |
| `task prod:logs` | target, previous, follow, since | redacted structured logs | operator SSH key | unknown target or missing retained Job |
| `task prod:export` | age recipient, output path | verified encrypted archive | operator SSH key | quiesce, dump, stream, encryption, checksum, or resume failure |
| `task prod:restore` | archive path, typed `vermouth` | restored data and ready HTTPS | operator SSH key plus restricted sudo | wrong identity, corrupt archive, extraction, migration, or readiness failure |
| `task prod:rollback` | immediately previous retained Helm revision, typed `vermouth` | previous app revision ready on HTTPS | operator SSH key plus restricted sudo | unretained target, migration compatibility, or readiness failure |

### Restricted SSH deployment protocol

The GitHub deployment key and the operator key are separate. GitHub uses only `vermouth-deploy` and forced command. Local `prod:doctor`, `status`, `logs`, `export`, `restore`, and `rollback` use the existing operator SSH configuration and never reuse the GitHub private key.

The authorized key line uses `restrict,command="/usr/local/sbin/vermouth-ssh-entrypoint"`. `vermouth-ssh-entrypoint` accepts only `preflight <git-sha> <manifest-sha256>` and `deploy <git-sha> <manifest-sha256>`. It rejects extra arguments, unknown commands, nonhex values, a Git SHA other than 40 lowercase hexadecimal characters, or a checksum other than 64 lowercase hexadecimal characters.

GitHub streams one `tar.zst` release bundle over stdin. The entrypoint hashes the raw stream before extraction, writes it under `/var/lib/vermouth/releases/.incoming/<github-run-id>`, rejects links and paths outside the staging directory, and permits only `images.json`, `rate-limit-evidence.json`, `migration-compat-evidence.json`, `bundle-manifest.json`, `values-production.yaml`, and `chart/`. It verifies every file checksum from `bundle-manifest.json`, then atomically renames the directory to `/var/lib/vermouth/releases/<git-sha>`.

Schema version 1 `images.json` contains `schema_version`, `git_sha`, `github_run_id`, `generated_at`, `chart_sha256`, and an `images` object keyed by the fixed 11 workload names. Each image requires `repository`, `tag`, `digest`, `platforms`, `build_input_sha256`, and `source_revision`. The root deployment program rechecks each Docker Hub manifest digest and both required platforms before Helm.

`/etc/sudoers.d/vermouth-deploy` permits exactly `/usr/local/sbin/vermouth-deploy-root` with no command line arguments. The forced entrypoint sends one validated schema version 1 request JSON on stdin. The root program validates it again and reads release content only from the fixed release directory. Bootstrap installs both root owned programs mode `0755`; deployment bundles cannot replace them.

Port 22 remains an operator owned Azure network rule while temporary SSH deployment exists. `PasswordAuthentication` and `PermitRootLogin` are off. The restricted key allows no PTY or forwarding. Rotation adds the new public key and GitHub Secret, proves one deployment, then removes the old authorized key line. The old private key is deleted from GitHub after that proof. Migration to Azure OIDC removes the restricted key and user only after an OIDC deployment succeeds.

### Value sourcing

| Action | Value produced or displayed | Source |
|---|---|---|
| Doctor | VM name, size, CPU, memory, public IP | Azure Instance Metadata Service and live host state |
| Doctor | available memory and disk | `free`, `findmnt`, and `df` on the target VM |
| Doctor | selected storage mode and root | live platform identity, or first bootstrap selection when no PVC exists |
| Bootstrap | dedicated disk identity | filesystem UUID `37f07a74-8426-45fd-8e55-79515376a136` and mount target `/var/lib/vermouth` |
| Bootstrap | fallback root | k3s default `/var/lib/rancher/k3s/storage/vermouth`, only before the first PV |
| Bootstrap | k3s and Traefik versions | exact committed production version matching live `v1.36.3+k3s1` |
| Bootstrap | restricted deployment authorized key | `PROD_DEPLOY_PUBLIC_KEY`, `PROD_DEPLOY_KEY_ID`, and fixed forced command options from this spec |
| DNS | label and public hostname | operator supplied `PROD_DNS_LABEL`, Azure Public IP `domainNameLabel`, region `southeastasia`, and live DNS resolution to `PROD_HOST` |
| TLS | ACME account and certificate names | `PROD_ACME_EMAIL`, public hostname, staging proof, then production issuer |
| Build | image tag | first 12 characters of the target build input SHA256 |
| Build | image digest and platforms | completed Docker Hub push plus Buildx manifest inspection |
| Build | image repository | `DOCKERHUB_NAMESPACE` plus the fixed workload name from the committed image inventory |
| Deploy | release identity | Git SHA, GitHub run ID, image digest record, and Helm revision |
| Secret creation | values | root owned `/etc/vermouth/production.env` |
| Secret naming | immutable suffix | first 12 characters of SHA256 over canonical sorted key and value bytes |
| Migration | database and SQL set | named service Secret and its committed `db/migrations/` directory |
| OAuth | redirect URI | `https://<live Azure hostname>/api/auth/google/callback` |
| Public launch | rate limit evidence | the named feature 21 auth integration tests passing in the current GitHub workflow |
| Migration gate | previous revision compatibility | matching `migration-compat-evidence.json` from `task test:migration-compat` in the current GitHub workflow |
| Readiness | internal state | Pod, PVC, Job, Helm, `/health`, and `/ready` conditions |
| Readiness | public state | HTTPS requests through the live Azure hostname |
| Capacity | samples, maximum, average, decision | `/proc/meminfo` and `kubectl top` sampled every 5 seconds for 5 minutes into `/var/lib/vermouth/reports/resource-<run-id>.json` |
| Export | archive contents and manifest | active selected storage root, four `pg_dump` outputs, deployment state, and production configuration |
| Export | encryption recipient | `PROD_AGE_RECIPIENT`, whose private key remains on the operator Mac |
| Restore | target state | verified full export manifest, archive checksums, and recorded deployment state |
| Rollback | application target | immediately previous Helm application revision with retained Secret references |

### Key invariants

1. Production commands target only `4.194.251.123` and verify its SSH host key, Azure identity, and k3s identity before mutation.
2. Image digests, not mutable tags, select running code.
3. Local and production use the same k3s version, packaged Traefik generation, chart, image set, Jobs, probes, and release order.
4. The selected storage root becomes immutable before the first PV exists.
5. A missing locked storage root starts no stateful workload.
6. Build and push finish before any VM state changes.
7. Database down migrations never run automatically.
8. Each service and migration Job receives only its own database URL. Only billing receives Garage credentials.
9. Public traffic reaches only Traefik on ports 80 and 443, then web or gateway.
10. Other applications on the VM are outside Vermouth ownership.
11. An application rollback never changes foundation data, database versions, broker events, or Garage objects.
12. Secrets never appear in Git, image layers, Helm values, command arguments, or logs.

### Security model

1. Friends reach only HTTPS. HTTP exists for ACME and redirect.
2. Google OAuth and session behavior remain spec 0004. Feature 21 rate limits are a hard launch prerequisite.
3. GitHub stores a temporary restricted deployment private key, Docker Hub push token, Docker Hub username, target host key, and `age` public recipient. It never stores `azureuser` credentials or production application values.
4. The VM deployment user cannot run a general shell through its deployment key. Root owns the validated deployment entrypoint and exact sudo rule.
5. `/etc/vermouth/production.env` is mode `0600`. Task reads it into Kubernetes Secrets without logging values.
6. Docker Hub uses a write token in GitHub and a separate read token in the VM pull Secret.
7. Vermouth Pods keep named ServiceAccounts, token mounts off, default deny NetworkPolicies, numeric users, read only root filesystems, dropped capabilities, no privilege escalation, and `RuntimeDefault` seccomp.
8. Compliance scope is ordinary tutor account and education records. No PCI, health, or regulated payment processing is introduced because payment state remains a manual tutor action.

### Helm and Secret retention

Every application upgrade passes `--history-max 5`. A revision is deployable only when Helm reports status `deployed` or `superseded`, every referenced runtime Secret exists, every image digest still resolves in Docker Hub, and matching migration compatibility evidence covers the current database versions. Platform identity records `current_application_revision` and `previous_deployable_revision` after external readiness.

Runtime Secrets carry labels `app.kubernetes.io/name=vermouth`, `app.kubernetes.io/component=runtime-secret`, `vermouth.dev/runtime-service=<service>`, and `vermouth.dev/source-sha256=<full hash>`. Garbage collection computes references from the current and previous deployable revisions, runs only after external HTTPS readiness, and deletes only an unreferenced content named runtime Secret. A failed upgrade or automatic Helm rollback performs no Secret garbage collection. Foundation Secrets are never garbage collected automatically.

`prod:rollback` recomputes the previous deployable revision from live Helm history and required artifacts rather than trusting a cached number. The encrypted export includes `/etc/vermouth/production.env`, current and previous revision metadata, and their Secret source hashes, so restore can recreate both retained sets even if Kubernetes Secret objects were lost.

### Configuration required

**Repository or GitHub environment variables**:

1. `PROD_HOST`: exact target, `4.194.251.123`.
2. `PROD_DNS_LABEL`: operator selected Azure Public IP label, configured before bootstrap.
3. `PROD_HOSTNAME`: live `<PROD_DNS_LABEL>.southeastasia.cloudapp.azure.com`, accepted only after Azure and DNS agree.
4. `PROD_SSH_USER`: `vermouth-deploy`.
5. `PROD_K3S_LIVE_VERSION`: `v1.36.3+k3s1`.
6. `PROD_K3S_IMAGE_TAG`: `v1.36.3-k3s1`.
7. `PROD_TRAEFIK_IMAGE`: `rancher/mirrored-library-traefik:3.7.8`.
8. `PROD_TRAEFIK_CHART`: `40.1.4+up40.1.0`.
9. `PROD_STORAGE_UUID`: `37f07a74-8426-45fd-8e55-79515376a136`.
10. `PROD_STORAGE_ROOT`: `/var/lib/vermouth`.
11. `PROD_AGE_RECIPIENT`: public `age` recipient held by the operator.
12. `DOCKERHUB_NAMESPACE`: private image namespace.
13. `PROD_ACME_EMAIL`: Let’s Encrypt account email.
14. `PROD_DEPLOY_PUBLIC_KEY`: public half of the temporary restricted GitHub deployment key.
15. `PROD_DEPLOY_KEY_ID`: stable identifier used for rotation and authorized key removal.

**GitHub production Secrets**:

1. `DOCKERHUB_USERNAME`: push identity.
2. `DOCKERHUB_PUSH_TOKEN`: token limited to image publication.
3. `PROD_SSH_PRIVATE_KEY`: temporary restricted deployment key.
4. `PROD_SSH_HOST_KEY`: pinned host public key or known hosts line.

**VM only configuration**:

1. `/etc/vermouth/production.env`: application, database, signing, Google OAuth, Garage, `DOCKERHUB_READ_USERNAME`, and `DOCKERHUB_READ_TOKEN` values.
2. `/etc/sudoers.d/vermouth-deploy`: exact restricted deployment command.
3. `/var/lib/rancher/k3s/server/manifests/traefik-config.yaml`: committed packaged Traefik `HelmChartConfig` content.
4. `/etc/vermouth/platform.json`: immutable schema version 1 platform and storage identity.

### Critical test scenarios

1. Happy path: manual GitHub workflow builds both architectures, pushes private digests, deploys through restricted SSH, runs migrations, reaches HTTPS, completes Google sign in and invoice flow, and records the event thread, verifies **AC-4**, **AC-5**, **AC-6**, **AC-8**, **AC-9**, and **AC-10**.
2. Storage selection: first bootstrap chooses dedicated storage when mounted and default k3s storage when absent, then a missing locked root blocks stateful startup, verifies **AC-2** and **AC-3**.
3. Failure before mutation: break one build, push, pull, migration, and readiness condition in separate runs and confirm previous release state, verifies **AC-13**.
4. Security: test the deploy key against an unapproved command, scan public ports, inspect Pod permissions, and confirm Secrets do not reach logs or Helm values, verifies **AC-5**, **AC-11**, and **AC-12**.
5. Capacity: fail below each preflight threshold, then measure five warm minutes below 7 GiB total memory, verifies **AC-19**.
6. Export and restore: create distinct markers in four databases, Redpanda, Garage, and ACME state, export to the Mac, destroy the test state, perform a full restore, and prove every marker plus HTTPS and the thread, verifies **AC-17** and **AC-18**.
7. Rollback: deploy two compatible revisions, roll back application resources only, and confirm data and migrations remain forward, verifies **AC-14**.
8. Operations: inspect every status and log target after success and retained failure, verifies **AC-15** and **AC-16**.
9. Doctor and topology: vary VM identity, disk, DNS, registry access, configuration, and rate limit test evidence, then inspect every workload, policy, image, Secret, and platform identity field, verifies **AC-1**, **AC-7**, **AC-10**, and **AC-20**.

## Build plan

The Tracer Bullet approach first proves one secure request through the final production path, then adds state, recovery, and operational depth.

1. Replace the local Envoy edge with packaged Traefik, align k3s versions, and prove the local thread through one Ingress, satisfying the prerequisite from spec 0005 and **AC-7**, **AC-9**.
2. Build `prod:doctor` and `prod:bootstrap` around exact VM identity, dedicated or first bootstrap fallback storage, packaged Traefik, Azure DNS, staging plus production ACME, private Docker Hub pull credentials, restricted SSH, and feature 21 evidence. Deploy web and gateway through HTTPS before adding state, satisfies **AC-1**, **AC-2**, **AC-3**, **AC-5**, **AC-8**, **AC-10**, **AC-11**, and **AC-12**.
3. Add the manual GitHub Actions workflow, rate limit and migration compatibility evidence, and two architecture Docker Hub promotion, then deploy identity and notifications plus their databases and Redpanda through the full foundation, migration, application, capacity, and external readiness sequence. Complete the development token thread only as a private diagnostic, satisfies **AC-4**, **AC-6**, **AC-9**, **AC-10**, and **AC-13**.
4. Add teaching, billing, their databases, Garage, remaining Secrets, resource gates, full invoice and Google OAuth flows, status, logs, rollback, and local log retention, satisfies **AC-7**, **AC-9**, **AC-14**, **AC-15**, **AC-16**, **AC-19**, and **AC-20**.
5. Add encrypted export and guarded restore, then prove markers across every stateful system and the complete external thread, satisfies **AC-17** and **AC-18**.

## Consequences

**Positive**:

1. Development and production share one chart, k3s version, packaged edge, release order, and image set.
2. The existing VM and a low cost disk avoid a new managed platform bill.
3. Private immutable images and restricted SSH make the temporary deployment path auditable and replaceable.
4. A manual encrypted export gives a real free recovery path.

**Negative and tradeoffs**:

1. The one node VM is a single point of failure and has no automated backup.
2. The engineer chose automatic operating system disk fallback before first PV creation. A mistaken first bootstrap can therefore put production data on the nearly full shared disk. Platform identity prevents silent switching later, but it cannot make the initial fallback safe.
3. GitHub Actions holds a temporary SSH private key because the subscription cannot create the Entra application needed for OIDC.
4. File based Traefik ACME state requires one Traefik replica.
5. Manual export needs downtime, operator discipline, local Mac storage, and periodic restore practice.
6. Private Docker Hub availability and limits become part of every deployment.

**Neutral**:

1. Other applications on the VM are neither a Vermouth dependency nor a Vermouth management target.
2. There is no application database migration for deployment metadata.
3. The local registry remains a development detail. Production pulls Docker Hub digests.

## Follow-up

1. Add the official Azure MCP Server when the subscription permits Microsoft Entra authentication and RBAC. Replace temporary SSH deployment with GitHub OIDC plus Azure VM Run Command when Entra application creation becomes available.
2. Reconsider scheduled external HTTPS checks. The engineer deferred the recommended 15 minute GitHub Actions health workflow.
3. Add paid automated backup before the system carries data whose loss is unacceptable.
4. Record `azure-compute`, `azure-diagnostics`, Kubernetes, Helm, Buildx, and container security conventions in the correct `AGENTS.md` context before implementation.
5. No credible `age` specific or high confidence Traefik Agent Skill was found. Existing security, Kubernetes, and Helm skills govern those tools.

## Rationale

Reasoning and options: see [rationale.md](rationale.md).
