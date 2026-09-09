# Verify: Cloud deployment for friend testing · spec 0006 · updated 2026-09-09

_Steps derived from spec 0006 acceptance criteria. `$check verify` runs these. `$test` may lock the durable checks later._

## Operator and Azure boundary

- [ ] Set `PROD_AZURE_VM_RESOURCE_ID`, sign in with Azure CLI, then run `task prod:doctor`. Confirm the active subscription, VM, network interface, Public IP, DNS label, and effective inbound rules all come from that resource ID. Change the active subscription and confirm doctor refuses it. → AC-1, AC-11
- [ ] Change the VM resource ID, name, size, location, or public address one at a time. Confirm doctor rejects each mismatch against Azure Instance Metadata Service and the committed target. → AC-1, AC-2
- [ ] Lower available memory below 3 GiB, or provide a fixture that reports that value. Confirm doctor refuses it. Repeat with less than 15 GiB free on the operating system disk. → AC-1, AC-19
- [ ] Run doctor before first bootstrap with and without the dedicated disk. Confirm it reports the dedicated mount when found, otherwise the exact first bootstrap fallback. After bootstrap, confirm it reads only the locked platform identity. → AC-1, AC-3
- [ ] Remove or stop `metrics-server`, then run doctor and bootstrap. Confirm both fail. Restore it and confirm `kubectl top node nguyenducloc-vm2` supplies the accepted sample. → AC-1, AC-2, AC-19
- [ ] Attach the XFS disk with UUID `37f07a74-8426-45fd-8e55-79515376a136`. Run bootstrap and confirm `/var/lib/vermouth` is selected and `/etc/fstab` contains the exact recorded line. → AC-2, AC-3
- [ ] On an empty test target without that disk, run bootstrap. Confirm `/var/lib/rancher/k3s/storage/vermouth` is selected once. Attach the disk later and confirm the selected root does not change. → AC-2, AC-3
- [ ] Change the live k3s version, the committed k3s image tag, the packaged Traefik image, or its chart version. Confirm doctor rejects every mismatch. → AC-1, AC-2
- [ ] Set `PROD_DEPLOY_PUBLIC_KEY` and `PROD_DEPLOY_KEY_ID`, run bootstrap twice, and confirm the same restricted authorized key entry is retained without duplication. → AC-2, AC-5
- [ ] Set the Azure Public IP `domainNameLabel`, then run doctor. Confirm `PROD_DNS_LABEL`, `PROD_HOSTNAME`, Azure DNS, public IP, and live DNS all resolve to `4.194.251.123`. Change any one and confirm refusal. → AC-1, AC-8
- [ ] Run bootstrap with the staging resolver. Confirm `acme-staging.json` contains the production hostname with a nonempty certificate and key before the probe is removed, then confirm the final Ingress uses `letsencrypt-production`, `PROD_ACME_EMAIL`, and the production hostname. → AC-2, AC-8, AC-9
- [ ] Scan the public address. Confirm ports 22, 80, and 443 answer, while 3900, 5000, 5432, 6443, 8080 through 8084, 9092, 9644, and 10250 stay closed. Add one broad Azure allow rule and confirm doctor refuses it. → AC-1, AC-11

## Bootstrap, storage, and configuration

- [ ] Run `task prod:bootstrap` twice. Confirm `/etc/vermouth/platform.json`, its ConfigMap, the storage marker, eight static PVs, seven application PVCs, `traefik-acme`, and `dockerhub-pull` remain stable. → AC-2, AC-3, AC-12
- [ ] Run ordinary bootstrap on an operator Mac without `age`, `age-keygen`, or `zstd`. Confirm it proceeds. Run disaster bootstrap without them and confirm it stops before SSH with the missing recovery tool. → AC-2, AC-18
- [ ] Compare the selected disk UUID and mount target with `deploy/production/config.json`. Confirm bootstrap refuses a different filesystem, UUID, mount, node, or existing conflicting PV. → AC-2, AC-3
- [ ] Remove the dedicated disk after its identity is locked. Confirm every stateful storage guard blocks startup instead of creating empty data elsewhere. → AC-3
- [ ] Compare live k3s and Traefik versions with the committed production configuration. Confirm the generated `HelmChartConfig` uses those exact pins. → AC-1, AC-2, AC-6
- [ ] Attempt a shell, PTY, forwarding, malformed command, extra argument, wrong Git SHA, wrong checksum, and nonempty `inspect-base` input through the deployment key. Confirm every attempt is refused. → AC-5
- [ ] Hash the exact committed `deploy/images.lock.yaml` bytes. Confirm bootstrap records that hash and doctor rejects a different recorded value. → AC-1, AC-20
- [ ] Inspect `/etc/vermouth/production.env`. Confirm root owns it with mode `0600`, required values are present, and no value appears in Git, image history, Helm values, command output, or logs. → AC-12
- [ ] Change one production secret. Confirm `vermouth-secret-v1` hashes the exact decoded per service map from the spec 0005 inventory, the name is `<nameBase>-<first-12-lowercase-source-sha256>` in namespace `vermouth`, annotation `vermouth.dev/source-sha256` carries the full hash, exact service labels and keys are present, and `immutable=true`. Confirm Helm `release.runtimeSecrets` stores the same name, full hash, and sorted keys beside the chart reference. → AC-12
- [ ] Precreate the derived runtime Secret name with a different full hash, label, key, value, or mutable state. Confirm deployment detects the prefix collision or recreated Secret and fails before Helm mutation. Confirm `inspect-base` also refuses a retained revision whose recorded Secret metadata no longer matches live state. → AC-12, AC-13
- [ ] Add current, previous, valid unreferenced, malformed, foundation, and decoy Secrets. Confirm garbage collection derives its keep set only from current and previous deployable Helm `release.runtimeSecrets`, validates every candidate before deletion, removes only the valid unreferenced runtime Secret after HTTPS readiness and durable platform identity, aborts before deletion on an ambiguous candidate, and stops with a safe partial report on a delete error. → AC-12, AC-20
- [ ] Inspect each migration Job. Confirm it receives only its service database URL and the committed migration directory. Confirm only billing receives Garage credentials. Redeploy the same Git SHA as another workflow attempt and confirm the Job names include both run ID and attempt, while retained failed Job logs remain addressable through `task prod:logs`. → AC-6, AC-12, AC-13, AC-15
- [ ] Compare the Google callback with `https://<live Azure hostname>/api/auth/google/callback`. Change the hostname and confirm the deployment refuses inconsistent values. → AC-8, AC-9
- [ ] Compare `GATEWAY_TRUSTED_PROXY_CIDRS`, the committed chart value, and the explicit live k3s cluster CIDR. Confirm all equal `10.42.0.0/16`, then change each source separately and confirm doctor refuses it. → AC-1, AC-10, AC-11
- [ ] Inspect the committed NetworkPolicies and live policies immediately after bootstrap, before an application Pod exists. Confirm default deny already exists and every allowed source, destination, and port matches the committed traffic matrix. → AC-7, AC-11
- [ ] Inspect every Pod security context. Confirm named ServiceAccounts, disabled token mounts, numeric users, read only root filesystems, dropped capabilities, disabled privilege escalation, and `RuntimeDefault` seccomp. → AC-7, AC-11, AC-12

## Image production and provenance

- [ ] Dispatch production from a named ref and compare the checked out full commit with `images.json.git_sha`. Confirm a ref that resolves differently is refused. → AC-4
- [ ] Run `platformconfig production-image-plan` for all 11 workloads. Change one declared input, Dockerfile, base lock, or image lock and confirm the full build input SHA256 changes. Leave the tree dirty and confirm production publication refuses it. → AC-4
- [ ] For every workload, confirm the tag is `git-`, the first 12 source SHA characters, `-`, and the full 64 character build input SHA256. Alter either source and confirm validation fails. → AC-4, AC-14
- [ ] Inspect the root OCI index and both child manifests. Confirm `org.opencontainers.image.source` equals the exact workflow repository URL on all three documents. → AC-4, AC-14
- [ ] Disable the Docker Hub immutable `git-*` rule for one repository. Confirm the workflow stops before building or contacting the VM. Restore the rule and confirm publication continues. → AC-4
- [ ] Race two matching publications for the same immutable tag. Confirm the winning index is reused only when every provenance field matches. Race a mismatched publication and confirm collision failure before VM contact. → AC-4
- [ ] Build a new image and confirm one whole second UTC creation time appears unchanged on the root index and both child manifests. Reuse the tag and confirm the original time is preserved. → AC-4
- [ ] Inspect every published image. Confirm one root OCI index contains exactly runnable `linux/amd64` and `linux/arm64` child manifests, has no attestation descriptor, and records the root digest. → AC-4, AC-13
- [ ] Compare every repository with `docker.io/<DOCKERHUB_NAMESPACE>/vermouth-<workload>`. Change one repository and confirm production validation fails. → AC-4
- [ ] Run `platformconfig tree-sha256 deploy/helm/vermouth`. Confirm the value equals `images.json.chart_sha256`, then dirty the chart and confirm bundling stops. → AC-4, AC-6
- [ ] Compare `images.json.generated_at` with the workflow time after all registry checks. Confirm it has whole seconds and `Z`, then change the format and confirm validation fails. → AC-4

## Evidence and deployment transitions

- [ ] Inspect the immutable release identity. Confirm it combines the full Git SHA, GitHub run ID, and run attempt, and that Helm stores the same identity with every evidence checksum plus the exact five entry `release.runtimeSecrets` map. Confirm every parallel chart Secret reference equals its evidence name. → AC-6, AC-12, AC-13, AC-20
- [ ] Change one byte in `release.tar.zst`. Confirm the restricted entrypoint rejects the bundle SHA256 before extraction or final directory creation. → AC-5, AC-13
- [ ] Change, remove, add, or rename one bundle file. Confirm the canonical bundle manifest and evidence checksum checks refuse it. → AC-5, AC-13
- [ ] Send an invalid bundle through both `preflight` and `deploy`. Confirm validation removes the incoming directory and never creates an immutable final release directory. → AC-5, AC-13
- [ ] Hold deploy, rollback, export, and restore in turn. Confirm every other mutation fails immediately, `inspect-base` fails while the exclusive lock is held, and status reports `busy`. Kill the holder and confirm the kernel releases the lock. → AC-13, AC-14, AC-17, AC-18
- [ ] Read the seven production rate values from `values-production.yaml`. Compare each exact environment mapping with the gateway parser. Change one candidate value and confirm evidence verification fails. → AC-10
- [ ] Recompute the expected rate limit threshold SHA256 through `gateway/cmd/ratelimitevidence verify`. Confirm a different normalized threshold fails. → AC-10
- [ ] Run `task test:auth-rate-limit`. Confirm only `.tmp/production/rate-limit-evidence.json` is created, the workflow bundles the same bytes, and the uploaded artifact matches byte for byte. → AC-10
- [ ] Compare rate evidence `git_sha` with `git rev-parse --verify HEAD^{commit}`. Confirm a stale or copied source SHA fails. → AC-10
- [ ] Recompute the seven sorted `NAME=value\n` lines. Confirm the stored threshold SHA256 matches and a changed order, name, or value fails. → AC-10
- [ ] Confirm rate evidence records boolean `true` and a whole second UTC timestamp only after all named checks pass. Force an earlier check to fail and confirm no passing file remains. → AC-10
- [ ] Change whitespace, key order, trailing line feeds, schema, target, Git state, or pass type in rate evidence. Confirm canonical verification refuses each change while accepting an old timestamp as metadata. → AC-10
- [ ] With no application Helm history and empty databases, run restricted `inspect-base`. Confirm `previous_application_revision` and its identity tuple are null and all four Goose versions are zero. → AC-13, AC-20
- [ ] With one deployed application revision, confirm `inspect-base` selects only that revision, then resolves its immutable release identity and source revision. Add a pending or ambiguous state and confirm refusal. → AC-13, AC-20
- [ ] Compare `previous_images_json_sha256` with the exact deployed revision Helm value and immutable file bytes. Change either and confirm inspection fails. → AC-13
- [ ] Compare `observed_at`, candidate Git SHA, run ID, and attempt with the VM UTC clock and restricted request. Change one request field and confirm refusal. → AC-13
- [ ] Compare `previous_goose_versions` with live `goose_db_version` rows. Make one database unavailable or change one version after inspection and confirm deployment stops before Secret creation. → AC-13, AC-20
- [ ] Hash the exact canonical `deployment-base.json` bytes. Confirm migration evidence names that SHA256 and refuses a reformatted or changed base document. → AC-13
- [ ] Run `platformconfig production-migration-set-sha256`. Change, remove, renumber, insert, or link one migration and confirm the candidate migration set hash or clean tree gate refuses it. → AC-13
- [ ] Run `task test:migration-compat -- deployment-base.json` from an existing revision. Confirm every new migration applies in order and the previous revision tests pass after each committed version. → AC-13
- [ ] Compare `candidate_goose_versions` with the highest committed version for all four services and the final temporary database state. Confirm any incorrect map fails. → AC-13
- [ ] Confirm migration evidence uses exact target `task test:migration-compat` and a whole second UTC completion time, and appears only after every database and test passes. → AC-13
- [ ] Change the live Helm revision or one Goose version after inspection. Confirm the exclusive deploy path rejects the stale base before any Secret, migration, Helm, or platform identity mutation. → AC-13
- [ ] Force failure during image publication, base inspection, pull, foundation readiness, migration, application readiness, capacity, and external readiness. Confirm each failure preserves the previous application and follows the specified rollback boundary. For capacity and external readiness failures, confirm the previous application becomes ready on HTTPS with its new live Helm revision recorded, or public Ingress is removed when recovery also fails. → AC-4, AC-6, AC-13, AC-19, AC-20

## Runtime, capacity, and friend path

- [ ] Inspect Pods, PVCs, Jobs, Helm state, `/health`, and `/ready` before public Ingress is enabled. Confirm every internal condition is ready. → AC-6, AC-7, AC-9
- [ ] Request `/`, `/config.json`, `/api`, `/health`, and `/ready` through the live Azure hostname. Confirm HTTPS routing has no path rewrite and raw HTTP only redirects outside ACME challenge traffic. → AC-8, AC-9
- [ ] Run the five minute capacity gate. Confirm `/proc/meminfo` supplies 61 samples from second zero through second 300, and `kubectl top` records CPU and memory only for `vermouth` and `kube-system` Pods. Confirm the maximum stays below 7 GiB and the uploaded artifact matches the VM report bytes. → AC-19
- [ ] Run `task prod:status`. Confirm it reports VM capacity, storage identity, lock state, Helm revisions, evidence checksums, Goose versions, migration reports, image and Secret references, workloads, Jobs, PVs, PVCs, certificate state, and both readiness layers. → AC-15, AC-20
- [ ] Run every documented `task prod:logs` target with current, previous, follow, and bounded since modes. Confirm unknown targets fail and production secret values are redacted. → AC-15, AC-16
- [ ] Inspect `/etc/systemd/journald.conf.d/99-vermouth.conf` and the kubelet drop in. Confirm journal use is capped at 512 MiB and container logs rotate at 10 MiB with three files. → AC-16
- [ ] Confirm web, gateway, identity, teaching, billing, notifications, four Postgres instances, Redpanda, Garage, and one packaged Traefik replica are ready. Confirm every application has one replica and notifications cannot scale above one. → AC-7
- [ ] Sign in with Google through the public hostname, create the complete teaching and invoice path, share the invoice, mark it paid, and follow the resulting event thread. Confirm all browser calls stay on the same origin. → AC-7, AC-9, AC-10

## Export, restore, and rollback

- [ ] Rotate one runtime Secret between current and previous deployable revisions, create distinct markers in four databases, Redpanda, Garage metadata, Garage data, and Traefik ACME state, then run `task prod:export -- <new-path>`. Inspect the authenticated manifest and confirm it contains live storage, four custom dumps, Helm state, production configuration, complete current plus optional previous release directories, and one canonical `secrets/runtime/<secret-name>.json` snapshot for every unique referenced Secret in both Helm revisions. Recompute every snapshot hash from its decoded values. → AC-17
- [ ] Try an omitted, existing, linked, invalid, or unwritable export destination. Confirm export refuses it without changing production. Confirm a successful export appears only through atomic rename at the exact requested path. → AC-17
- [ ] Try a missing, relative, linked, wrongly owned, or broadly readable `PROD_AGE_IDENTITY`. Confirm refusal before SSH contact. → AC-17, AC-18
- [ ] Put zero, multiple, or plugin identities in the identity file, or change `PROD_AGE_RECIPIENT`. Confirm `age-keygen -y` must produce exactly one matching native recipient. → AC-17, AC-18
- [ ] During export, inspect local storage and process files. Confirm plaintext never appears on the Mac and the encrypted temporary file is renamed only after authenticated streaming validation checks zstd, tar paths, manifest fields, sizes, and checksums. → AC-17
- [ ] On an empty exact replacement VM, confirm ordinary restore refuses the target. Run `task prod:bootstrap -- --from-export <archive>`, then confirm only authenticated manifest and production configuration cross SSH before the recorded platform is recreated. → AC-18
- [ ] Run restore and compare the first authenticated local decrypt SHA256 with the staged VM archive SHA256. Change the ciphertext or second stream and confirm staging is removed. → AC-18
- [ ] Confirm the incoming archive uses a root owned mode `0600` unpredictable path, and its basename becomes the restore run ID. Interrupt the receiver and confirm the partial file is removed. → AC-18
- [ ] Confirm the VM syncs the complete received file and directory, validates the full archive, then atomically moves it from `.incoming` to `.staged`. Confirm no invalid archive reaches staged state. → AC-18
- [ ] During restore preflight, confirm registry reads use only `DOCKERHUB_READ_USERNAME` and `DOCKERHUB_READ_TOKEN` from the authenticated archived configuration. Confirm neither value reaches arguments, output, logs, or a lasting environment. → AC-18
- [ ] After staging, lower selected root free space to the manifest byte total plus 1 GiB or less. Confirm restore refuses before confirmation or traffic changes. → AC-18
- [ ] Before confirmation, change a release checksum, image tag, root digest, child manifest, provenance annotation, platform set, pull permission, target identity, k3s version, PV, or PVC. Confirm preflight refuses each mismatch without changing traffic or data. → AC-18
- [ ] Before restore confirmation, remove, add, duplicate, rename, relabel, make mutable, change a key, change a decoded value, or change the full hash in one runtime Secret snapshot. Confirm preflight rejects every mismatch without changing traffic or data. → AC-18
- [ ] Complete restore after typed `vermouth`. Confirm all eight stateful markers, four logical dumps, current and previous immutable releases, every current and previous runtime Secret name, full hash annotation, exact labels, keys, decoded values, and immutable state, image digests, HTTPS paths, status resolution, rollback candidate, Google sign in, invoice flow, and event thread match the export. Confirm no older Secret was derived from the current `production.env`. → AC-18
- [ ] Force a restore apply failure after safety directories are created. Confirm partial new directories are removed, original directories and the prior production configuration return when possible, traffic stays closed otherwise, and the staged plaintext archive is removed. Complete a later verified export, decline safety cleanup once, then type `vermouth` and confirm only the retained `restore.*` safety directories are removed while the production lock is held. → AC-17, AC-18
- [ ] Deploy two compatible application revisions with different runtime Secret hashes, including two attempts for one Git SHA. Run `task prod:rollback`, confirm the newest earlier deployable Helm revision is selected from live history, and verify every Secret name, full hash annotation, sorted keys, exact labels, immutable state, evidence checksum, and live Goose version compatibility proof before mutation. Confirm the target image and Secret references are displayed before the remote command accepts typed `vermouth` while holding the production lock. → AC-14
- [ ] For the rollback candidate, change a source revision, build input hash, canonical tag, root digest, child manifest, provenance value, Secret reference, or evidence checksum. Confirm rollback refuses it before Helm mutation. → AC-14
- [ ] Complete rollback after typed `vermouth`. Confirm migrations, database data, broker events, Garage objects, and foundation resources stay forward, while platform identity records the new live Helm revision, restored release identity, evidence hashes, image digests, and prior current revision as the next candidate. → AC-14, AC-20

## Acceptance criteria coverage

- AC-1 is covered by Operator and Azure boundary, Bootstrap, storage, and configuration.
- AC-2 is covered by Operator and Azure boundary, Bootstrap, storage, and configuration.
- AC-3 is covered by Operator and Azure boundary, Bootstrap, storage, and configuration.
- AC-4 is covered by Image production and provenance, Evidence and deployment transitions.
- AC-5 is covered by Bootstrap, storage, and configuration, Evidence and deployment transitions.
- AC-6 is covered by Bootstrap, storage, and configuration, Evidence and deployment transitions, Runtime, capacity, and friend path.
- AC-7 is covered by Bootstrap, storage, and configuration, Runtime, capacity, and friend path.
- AC-8 is covered by Operator and Azure boundary, Bootstrap, storage, and configuration, Runtime, capacity, and friend path.
- AC-9 is covered by Operator and Azure boundary, Bootstrap, storage, and configuration, Runtime, capacity, and friend path.
- AC-10 is covered by Bootstrap, storage, and configuration, Evidence and deployment transitions, Runtime, capacity, and friend path.
- AC-11 is covered by Operator and Azure boundary, Bootstrap, storage, and configuration.
- AC-12 is covered by Bootstrap, storage, and configuration.
- AC-13 is covered by Evidence and deployment transitions.
- AC-14 is covered by Export, restore, and rollback.
- AC-15 is covered by Runtime, capacity, and friend path.
- AC-16 is covered by Runtime, capacity, and friend path.
- AC-17 is covered by Export, restore, and rollback.
- AC-18 is covered by Export, restore, and rollback.
- AC-19 is covered by Operator and Azure boundary, Evidence and deployment transitions, Runtime, capacity, and friend path.
- AC-20 is covered by Bootstrap, storage, and configuration, Evidence and deployment transitions, Runtime, capacity, and friend path, Export, restore, and rollback.
