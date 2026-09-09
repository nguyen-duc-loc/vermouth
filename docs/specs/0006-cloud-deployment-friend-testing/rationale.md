# Rationale for 0006. Cloud deployment for friend testing

## Context

> ⚠️ Premise note: This public deployment has no automated backup and runs every stateful component on one VM. The manual encrypted export is a recovery tool, not a guarantee. The engineer also chose automatic operating system disk fallback before the first PV. That improves first bootstrap availability but can place new production data on a shared nearly full disk. The safe boundary is an immutable storage choice before the first PV, with startup refusal whenever that locked root is missing.

Friends need a real HTTPS address for Google sign in and the invoice flow. The existing Azure VM is already paid for and already runs a healthy one node k3s control plane with packaged Traefik. The operator is one person, expects low friend testing traffic, accepts brief planned downtime, and cannot currently create the Microsoft Entra application needed for GitHub OIDC deployment.

The inspected VM is `nguyenducloc-vm2`, Azure size `Standard_D2ads_v6`, in Southeast Asia. It has 2 CPUs, 8 GiB memory, Ubuntu 24.04, and k3s `v1.36.3+k3s1`. At inspection it had about 4.9 GiB available memory. The root disk was 87 percent full with 8 GiB free. Ports 22 and 80 were externally reachable, while 443, k3s, kubelet, and Vermouth service ports were blocked.

A 64 GiB XFS Azure disk is mounted read write at `/var/lib/vermouth` with UUID `37f07a74-8426-45fd-8e55-79515376a136` and about 63 GiB free. The mount is not yet persistent in `/etc/fstab`, and k3s still provisions local PVCs under `/var/lib/rancher/k3s/storage`. These are production blockers, not reasons to invent another platform.

The product boundaries remain fixed. Four services keep four Postgres instances. Redpanda and Garage remain local stateful components. Images remain architecture neutral. Migrations remain forward only and precede application rollout. Feature 21 rate limiting is a public launch prerequisite.

## Options considered

### Option 1: Existing VM, k3s, Traefik, dedicated disk, and Docker Hub

Reuse the current VM and k3s. Align local development to the same k3s and Traefik generation, build private two architecture images in GitHub Actions, and deploy through a restricted temporary SSH command (basis: live VM evidence, specs 0002 and 0005, installed Azure, Kubernetes, Helm, Buildx, and security skills).

**Pros**:

1. Lowest new monthly cost and fewest new services.
2. Reuses a healthy control plane, existing chart, and known images.
3. Gives development and production one edge and release model.

**Cons**:

1. One VM and one disk remain single points of failure.
2. The operator owns capacity, TLS, secrets, upgrades, and recovery.
3. Temporary SSH credentials must live in GitHub until Entra OIDC becomes available.

### Option 2: Docker Compose or system services on the VM

Run production without Kubernetes, using the existing host or Compose path (basis: the existing host and Compose workflows).

**Pros**:

1. Lower control plane overhead.
2. Familiar process and container tools.

**Cons**:

1. Creates a second deployment model beside the Helm chart.
2. Reimplements migration gates, immutable Secret handoff, rollback, NetworkPolicy, and health ordering.
3. Makes development less like production, contrary to the chosen goal.

### Option 3: Managed Azure Kubernetes or managed application services

Move the platform to AKS or separate managed Azure services (basis: Azure managed platform practice and the installed Azure skills).

**Pros**:

1. Better infrastructure lifecycle, identity integration, and future scaling.
2. Removes some host level storage and control plane work.

**Cons**:

1. Adds cost and operational surface far beyond friend testing.
2. Does not remove the need to operate four databases, Redpanda, Garage, and application releases unless more managed services are purchased.
3. Conflicts with the current subscription and budget constraints.

### Option 4: Wait for a custom domain and paid backup

Keep the app private until a domain and automated backup are funded (basis: conservative production readiness practice).

**Pros**:

1. Avoids launching with known recovery limits.
2. Allows a cleaner long term identity and DNS setup.

**Cons**:

1. Blocks friend testing even though Azure can provide a stable free DNS label.
2. Delays feedback on the real sign in and invoice path.

### Backup identity sources compared

An explicit `PROD_AGE_IDENTITY` path to exactly one native `age` identity on the operator Mac is chosen. It keeps the private key out of Git, GitHub, the VM, command arguments, and archive contents while letting both export verification and restore use the same named source. Requiring one `age-keygen -y` output line removes ambiguity from files containing multiple or plugin identities. A fixed default path was the runner up, but it hides a required secret dependency and makes migration between Macs harder. Reading the identity from standard input was rejected because export and restore already use pipelines for archive data, which makes failure handling and interactive use harder. A macOS Keychain or third party `age` plugin was also rejected because it adds another provider and recovery procedure to a one operator feature (basis: official `age` and `age-keygen` manuals, least privilege secret handling).

### Recovery evidence and bootstrap options compared

Copying the complete current and previous immutable release directories is chosen because status, rollback, and the next deployment already trust those exact bytes. Exporting only image documents and Helm summaries was smaller, but restore would have to invent a second evidence model or reconstruct files that were originally immutable. Minting a new restore release identity was rejected because it would claim a build and migration proof that never ran (basis: spec 0006 immutable release model, content addressed recovery).

Ordinary restore now requires a bootstrapped platform. An empty replacement cluster uses the explicit `task prod:bootstrap -- --from-export <archive.tar.zst.age>` step first, which restores only the recorded platform identity and production configuration before any application data. Embedding a second bootstrap branch inside restore was rejected because it mixes platform creation with destructive data replacement and makes failure recovery harder to reason about (basis: Tracer Bullet delivery, separation of platform and data mutations).

Restore uses two local decrypt passes. The first authenticates and validates the archive while computing the plaintext stream hash. The second sends the same bytes to an incoming VM file that becomes staged only after the hash, sync, and full validation pass. A single pass was smaller, but a failed local decrypt can still close its pipe after partial output, which the receiver must never mistake for a complete backup (basis: fail closed streaming and atomic publish practice).

### Secret identity and recovery options compared

The full runtime Secret source SHA256 is stored as annotation `vermouth.dev/source-sha256`, while labels keep only queryable application, component, and service identity. This reuses spec 0005's working contract and preserves the complete integrity value. A full hash label is impossible because Kubernetes label values allow only 63 characters. Truncating the label to 63 characters was rejected because the evidence model already has a full hash, and splitting it across labels adds assembly rules without operational value.

Each Helm application revision records its referenced Secret name, full hash, and sorted keys. Name only references were rejected because a deleted and recreated Secret could keep the same name with different metadata or values. Putting decoded Secret values in Helm or immutable release bundles was also rejected because those stores are not secret containers.

Encrypted exports snapshot the decoded current and previous referenced runtime Secret values after validating them against live Helm evidence. Keeping only the current `production.env` was rejected because it cannot reconstruct the previous revision after credential rotation. Capturing live referenced Secrets adds archive size and validation work, but it keeps rollback and disaster restore honest without creating another persistent secret store on the VM.

### Migration baseline sources compared

Live Helm history plus the immutable release directory is the chosen source. It is closest to the application state that migrations must preserve and it can prove the prior source revision through the stored `images.json`. Its cost is a read only SSH round trip and a required stale base comparison before deployment.

`/etc/vermouth/platform.json` was considered as the source. It is cheap to read and already records release pointers, but it updates only after external readiness and can be stale after an interrupted release or a manual Helm action.

A GitHub environment variable was considered as the source. It would avoid VM inspection, but it creates a second mutable pointer that cannot update atomically with Helm. Generating evidence on the VM was also rejected because it would split evidence ownership and complicate the immutable bundle checksum.

## Rationale

Option 1 is the smallest platform that meets the real outcome. The VM has twice the memory first assumed, k3s and Traefik are already healthy, and the dedicated disk already exists. Reusing them is simpler than operating two production models or buying a managed cluster. The 64 GiB E6 Standard SSD LRS base price was USD 4.80 per month in the Azure Retail Prices API for Southeast Asia at decision time.

Packaged Traefik replaces Envoy in spec 0005 because parity matters more than Gateway API here. One Traefik replica is already part of k3s, consumes less memory, owns ports 80 and 443, and can persist file based ACME state. An Azure Public IP DNS label supplies a stable hostname without buying a domain, which gives Let’s Encrypt and Google OAuth one exact address.

Static local PVs are deliberate. Dynamic local path directory IDs are simple for routine scheduling but make disaster restore depend on Kubernetes control plane identity that the export does not capture. Eight fixed component directories, fixed PV and PVC names, Retain policy, and node affinity make the disk archive understandable without backing up k3s etcd. A root and Pod init marker prevents a missing locked mount from looking like empty new storage.

GitHub Actions is useful for repeatable two architecture builds and a durable release record. GitHub OIDC plus Azure VM Run Command was the stronger credential model, but the subscription cannot create its required Entra application. A dedicated user, forced deployment entrypoint, limited sudo rule, pinned host key, and independently rotated key bound the temporary SSH risk. The `azureuser` key never enters GitHub.

Production image tags keep the full 64 character build input SHA256 and shorten only the Git SHA to 12 characters. This keeps the tag readable while preserving the complete content identity that detects a changed build input. The tag remains within Docker Hub limits. A 12 character build input prefix, as used by the local workflow in spec 0005, would create an avoidable second collision domain in the production release record. The existing canonical hash algorithm remains the one source of build identity, with a production image plan command adding the full revision, Docker Hub repository, and tag.

Production Secret identity reuses the complete `vermouth-secret-v1` algorithm and inventory from spec 0005. A 12 character name suffix is only an address, never the authority. The full annotation, sorted keys, decoded values, immutable state, and per revision Helm evidence decide whether an existing Secret is the intended object. This makes prefix collision, recreation, rollback, export, and restore checks explicit while leaving Secret values out of Helm and release bundles.

Runtime still uses the root OCI index digest, while the tag remains an immutable discovery key. Docker Hub immutable tag protection closes the race between inspection and publication. Workflow concurrency keeps normal production runs orderly, and a losing publisher inspects the winner rather than overwriting it. Disabling attestations keeps the root index to the exact two runnable platforms this small release model understands.

Release directories include the Git SHA, GitHub run ID, and run attempt because the same commit can be dispatched or rerun more than once. Helm stores that identity with the exact checksum for every release evidence file. This lets status, export, restore, and rollback recover the right immutable evidence without guessing from a Git SHA or a mutable current pointer.

Migration compatibility needs the application that is actually running before a candidate release. Live Helm history is the authority because it owns the active application revision, while that revision's stored release identity and immutable `images.json` prove which source produced it. `/etc/vermouth/platform.json` remains useful for status and recovery, but it is written only after external readiness and can be stale after an interrupted or manual Helm action. A GitHub variable would add another mutable pointer with no atomic relationship to the VM.

The workflow therefore uses a two phase restricted handshake. After every candidate image is verified, a read only `inspect-base` action returns the proven active revision and immutable release tuple. GitHub tests the candidate migrations against that base and records both documents in the final bundle. The root deploy path resolves the same Helm state again before its first mutation. This extra comparison is necessary because workflow concurrency cannot prevent the operator from rolling back between inspection and deployment.

The stale base comparison is not enough on its own. A rollback, export, or restore could begin immediately after that comparison unless the VM serializes every mutation. One root owned `flock` keeps the check and the change in the same critical section, while a shared inspection lock prevents the workflow from observing a half changed release. The kernel releases the lock when a process dies, so this does not introduce a stale lock recovery procedure.

Helm values store checksums for every file that authorizes or describes a release, not only `images.json`. That lets inspection and rollback prove the base document, migration evidence, rate limit evidence, and bundle manifest from the same revision without trusting a mutable current pointer. Per service Goose versions add the database half of that identity. Testing the previous application after each committed migration step is slower, but it is the only credible way to promise that a partially applied forward migration leaves the active application usable.

The rate limit feature owns one canonical launch artifact at `.tmp/production/rate-limit-evidence.json`. Its Task target removes stale evidence before testing and writes the file only through a small gateway command that reuses the production policy parser. The same command verifies the expected hash from committed production Helm values, current Git identity, clean inputs, and canonical bytes before the deployment workflow consumes and uploads the original file. A feature local path followed by a copy was the runner up, but it creates two names for one authorization record and permits the copied bytes to drift from what the producer proved. A boolean `pass` matches the migration evidence contract and makes the schema reject every state except successful completion.

The timestamp is operational metadata, not an authorization clock. A freshness window would add runner start time and clock skew decisions while proving less than the current Git SHA, clean relevant inputs, exact candidate threshold hash, fixed target, schema, and canonical byte checks. Trusted proxy CIDRs remain outside the threshold hash because they change caller identity, not request budgets. Production validates that separate security input against the committed chart and live k3s pod CIDR before opening traffic.

The engineer accepted no paid automated backup. The manual `age` export therefore captures both logical Postgres dumps and the full selected storage root. The private identity is supplied by one explicit Mac path because `age` has no automatic native identity lookup, and `age-keygen -y` gives the command a direct way to prove that the identity matches the configured recipient before any remote mutation. Export verifies authenticated decryption and every archive checksum through pipes, so the Mac never needs a plaintext backup file. Restore also decrypts on the Mac, then uses the existing pinned SSH trust boundary for a short lived root owned staging archive on the VM. This costs no cloud storage but demands downtime, local space, identity custody, and restore practice. The engineer also explicitly chose automatic operating system disk fallback before the first PV despite its risk. Platform identity and an immutable pre PV storage choice prevent that compromise from becoming a silent later switch.

The filesystem archive is the authoritative full restore. Postgres dumps are readable verification and a future selective recovery aid, not a second write applied over restored database files. Export makes dumps before stopping Postgres, then stops every stateful writer before copying component directories. The strict archive contract rejects links, special files, duplicate paths, and incomplete checksums before extraction. Numeric storage ownership and modes are preserved only below fixed component roots. Restore keeps current directories by atomic rename until the recovered thread succeeds.

The complete immutable release directories travel with the storage because the operational model depends on their original evidence after disaster recovery. Archived Docker Hub read credentials let preflight prove those images on an empty replacement cluster without trusting a surviving live Secret. The credential remains inside the root restore process and is replaced into `/etc/vermouth/production.env` only after the archive and target are accepted.

Packaged `metrics-server` is the least additional machinery for the existing `kubectl top` capacity contract because it is already part of the pinned k3s platform. Doctor and bootstrap make its readiness explicit rather than letting the deployment discover that dependency during the five minute gate. Azure network exposure is proven twice: the operator's read only Azure CLI identity checks effective rules from the exact VM resource ID, and external probes check the observed boundary. One source alone can be stale or incomplete (basis: K3s packaged components documentation, defense in depth network verification).

## Supporting evidence

1. VM uptime was 41 days with no failed system services or recent memory kill evidence.
2. k3s node state was Ready. Only packaged system workloads were present.
3. The dedicated disk was XFS, 64 GiB, 2 percent used, root owned, and mounted at `/var/lib/vermouth`.
4. No persistent mount unit or `/etc/fstab` entry named the dedicated disk.
5. k3s local path configuration still named `/var/lib/rancher/k3s/storage`.
6. The root disk had only 8 GiB free, below the chosen production gate.
7. HTTPS port 443 was not externally reachable at inspection time.
8. Vermouth host processes were alive but not ready because their Compose Postgres and Redpanda endpoints were absent. Production must not adopt that unmanaged host path.

## References

**Project sources**:

1. `AGENTS.md`, Tracer Bullet delivery, service ownership, image, Secret, migration, and test conventions.
2. `docs/scope/scope.md`, feature 16 public deployment outcome and feature 21 rate limit prerequisite.
3. `docs/specs/0002-stack-and-scaffold/index.md`, four database ownership, Redpanda, Garage, image, migration, and resource constraints.
4. `docs/specs/0004-tutor-sign-in-google-oauth/index.md`, exact Google OAuth callback and session security.
5. `docs/specs/0005-local-kubernetes-platform/index.md`, shared chart, release order, images, Jobs, probes, policies, and local parity.
6. Installed skills named in the build spec, especially Azure compute and diagnostics, Kubernetes, Helm, Buildx, and container security.

**Practices and standards**:

1. Immutable image promotion and content addressed deployment.
2. Forward compatible migration with no automatic down migration.
3. Least privilege SSH, sudo, ServiceAccounts, NetworkPolicies, and container security contexts.
4. ACME HTTP challenge with persistent account state.
5. Quiesced filesystem export plus logical database dumps.
6. Strangler replacement of an existing development edge before production reuse.

**Links**:

1. Azure Public IP DNS labels: https://learn.microsoft.com/en-us/azure/virtual-network/ip-services/public-ip-addresses
2. Azure Public IP CLI: https://learn.microsoft.com/en-us/cli/azure/network/public-ip?view=azure-cli-latest
3. Azure managed disk attachment: https://learn.microsoft.com/en-us/azure/virtual-machines/linux/attach-disk-portal
4. Azure Retail Prices API: https://learn.microsoft.com/en-us/rest/api/cost-management/retail-prices/azure-retail-prices
5. K3s HelmChartConfig: https://docs.k3s.io/add-ons/helm
6. K3s packaged Traefik: https://docs.k3s.io/networking/networking-services
7. K3s local storage: https://docs.k3s.io/add-ons/storage
8. Traefik Kubernetes setup: https://doc.traefik.io/traefik/master/setup/kubernetes/
9. Traefik ACME: https://doc.traefik.io/traefik/v3.6/https/acme/
10. Google OAuth web server flow: https://developers.google.com/identity/protocols/oauth2/web-server
11. Google OAuth policy: https://developers.google.com/identity/protocols/oauth2/policies
12. Kubernetes private registry pulls: https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/
13. Azure MCP Server overview: https://learn.microsoft.com/en-us/azure/developer/azure-mcp-server/overview
14. Azure MCP Server repository: https://github.com/Azure/azure-mcp
15. `age` command manual: https://github.com/FiloSottile/age/blob/main/doc/age.1.html
16. `age-keygen` command manual: https://github.com/FiloSottile/age/blob/main/doc/age-keygen.1.html
17. K3s packaged components: https://docs.k3s.io/installation/packaged-components
