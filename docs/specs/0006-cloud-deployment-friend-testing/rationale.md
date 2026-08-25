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

## Rationale

Option 1 is the smallest platform that meets the real outcome. The VM has twice the memory first assumed, k3s and Traefik are already healthy, and the dedicated disk already exists. Reusing them is simpler than operating two production models or buying a managed cluster. The 64 GiB E6 Standard SSD LRS base price was USD 4.80 per month in the Azure Retail Prices API for Southeast Asia at decision time.

Packaged Traefik replaces Envoy in spec 0005 because parity matters more than Gateway API here. One Traefik replica is already part of k3s, consumes less memory, owns ports 80 and 443, and can persist file based ACME state. An Azure Public IP DNS label supplies a stable hostname without buying a domain, which gives Let’s Encrypt and Google OAuth one exact address.

Static local PVs are deliberate. Dynamic local path directory IDs are simple for routine scheduling but make disaster restore depend on Kubernetes control plane identity that the export does not capture. Eight fixed component directories, fixed PV and PVC names, Retain policy, and node affinity make the disk archive understandable without backing up k3s etcd. A root and Pod init marker prevents a missing locked mount from looking like empty new storage.

GitHub Actions is useful for repeatable two architecture builds and a durable release record. GitHub OIDC plus Azure VM Run Command was the stronger credential model, but the subscription cannot create its required Entra application. A dedicated user, forced deployment entrypoint, limited sudo rule, pinned host key, and independently rotated key bound the temporary SSH risk. The `azureuser` key never enters GitHub.

The engineer accepted no paid automated backup. The manual `age` export therefore captures both logical Postgres dumps and the full selected storage root. That costs no cloud storage but demands downtime, local space, and restore practice. The engineer also explicitly chose automatic operating system disk fallback before the first PV despite its risk. Platform identity and an immutable pre PV storage choice prevent that compromise from becoming a silent later switch.

The filesystem archive is the authoritative full restore. Postgres dumps are readable verification and a future selective recovery aid, not a second write applied over restored database files. Export makes dumps before stopping Postgres, then stops every stateful writer before copying component directories. Restore keeps current directories by atomic rename until the recovered thread succeeds.

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
