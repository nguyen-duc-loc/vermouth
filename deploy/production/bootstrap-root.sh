#!/bin/sh
set -eu

bootstrap_fail() {
  printf 'production bootstrap: %s\n' "$*" >&2
  exit 3
}

for tool in awk base64 blkid chmod chown cmp cp curl date find findfs flock getent grep helm id install jq kubectl mkdir mktemp mount mountpoint mv rm sed sha256sum sleep sshd stat systemctl tar useradd vermouth-platformconfig visudo; do
  command -v "$tool" >/dev/null 2>&1 || bootstrap_fail "$tool is required on the VM"
done
[ "$(id -u)" -eq 0 ] || bootstrap_fail "bootstrap must run as root"
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
exec 9>/run/lock/vermouth-production.lock
chmod 0600 /run/lock/vermouth-production.lock
flock -n -x 9 || bootstrap_fail "another production mutation is active"

archive=$(mktemp /var/tmp/vermouth-bootstrap.XXXXXX.tar.gz)
work=$(mktemp -d /var/tmp/vermouth-bootstrap.XXXXXX)
cleanup_bootstrap() {
  rm -f "$archive"
  rm -rf "$work"
}
trap cleanup_bootstrap EXIT HUP INT TERM
cat >"$archive"

tar -tzf "$archive" | while IFS= read -r entry; do
  case "$entry" in
    .|./|./bundle-manifest.json|./config.json|./request.json|./runtime-values.yaml|./traefik-config.yaml|./chart/*) ;;
    *) bootstrap_fail "bundle contains an unexpected path $entry" ;;
  esac
  case "$entry" in
    /*|*../*|*/..|..|*\\*) bootstrap_fail "bundle path $entry is unsafe" ;;
  esac
done
tar -tvzf "$archive" | awk '$1 ~ /^[lh]/ {bad=1} END {exit bad}' || bootstrap_fail "bundle contains a link"
tar -xzf "$archive" -C "$work" --no-same-owner --no-same-permissions

manifest=$work/bundle-manifest.json
request=$work/request.json
config=$work/config.json
[ -f "$manifest" ] && [ -f "$request" ] && [ -f "$config" ] || bootstrap_fail "bundle metadata is incomplete"
jq -e '.schema_version == 1 and (.files | type == "object")' "$manifest" >/dev/null || bootstrap_fail "bundle manifest is invalid"
jq -r '.files | keys[]' "$manifest" | while IFS= read -r relative; do
  case "$relative" in
    config.json|request.json|runtime-values.yaml|traefik-config.yaml|chart/*) ;;
    *) bootstrap_fail "manifest contains an unexpected file $relative" ;;
  esac
  file=$work/$relative
  [ -f "$file" ] || bootstrap_fail "bundle file $relative is missing"
  expected=$(jq -r --arg name "$relative" '.files[$name]' "$manifest")
  actual=$(sha256sum "$file" | awk '{print $1}')
  [ "$actual" = "$expected" ] || bootstrap_fail "bundle checksum failed for $relative"
done

jq -e '
  .schema_version == 1 and
  (.hostname | type == "string" and length > 0) and
  (.dns_label | type == "string" and length > 0) and
  (.acme_email | type == "string" and length > 0) and
  (.deploy_public_key | startswith("ssh-ed25519 ")) and
  (.deploy_key_id | test("^[A-Za-z0-9._@-]{1,80}$")) and
  (.dockerhub_namespace | test("^[a-z0-9][a-z0-9_-]*$")) and
    (.age_recipient | startswith("age1")) and
    (.chart_sha256 | test("^[0-9a-f]{64}$")) and
    (.image_lock_sha256 | test("^[0-9a-f]{64}$")) and
    (.restore_bootstrap | type == "boolean")
' "$request" >/dev/null || bootstrap_fail "bootstrap request is invalid"

expected_host=$(jq -r '.target.host' "$config")
expected_vm=$(jq -r '.target.vm_name' "$config")
expected_size=$(jq -r '.target.vm_size' "$config")
expected_location=$(jq -r '.target.location' "$config")
expected_node=$(jq -r '.target.node_name' "$config")
expected_k3s=$(jq -r '.k3s.live_version' "$config")
storage_uuid=$(jq -r '.storage.uuid' "$config")
dedicated_root=$(jq -r '.storage.dedicated_root' "$config")
fallback_root=$(jq -r '.storage.fallback_root' "$config")
hostname=$(jq -r '.hostname' "$request")

metadata=$(curl -fsS --max-time 5 -H Metadata:true 'http://169.254.169.254/metadata/instance?api-version=2025-04-07') ||
  bootstrap_fail "Azure Instance Metadata Service is unavailable"
[ "$(printf '%s' "$metadata" | jq -r '.compute.name')" = "$expected_vm" ] || bootstrap_fail "the target VM name does not match"
[ "$(printf '%s' "$metadata" | jq -r '.compute.vmSize')" = "$expected_size" ] || bootstrap_fail "the target VM size does not match"
[ "$(printf '%s' "$metadata" | jq -r '.compute.location')" = "$expected_location" ] || bootstrap_fail "the target VM location does not match"
public_ip=$(azure_public_ip "$metadata") || bootstrap_fail "Azure public IP metadata is unavailable"
[ "$public_ip" = "$expected_host" ] ||
  bootstrap_fail "the target public IP does not match"
[ "$(kubectl version -o json | jq -r '.serverVersion.gitVersion')" = "$expected_k3s" ] || bootstrap_fail "the live k3s version does not match"
[ "$(kubectl get node -o json | jq -r '.items | length')" -eq 1 ] || bootstrap_fail "production must contain one k3s node"
[ "$(kubectl get node -o json | jq -r '.items[0].metadata.name')" = "$expected_node" ] || bootstrap_fail "the k3s node name does not match"

k3s_changed=false
install -d -o root -g root -m 0755 /etc/systemd/journald.conf.d
cat >"$work/99-vermouth.conf" <<'EOF'
[Journal]
SystemMaxUse=512M
RuntimeMaxUse=512M
EOF
if [ ! -f /etc/systemd/journald.conf.d/99-vermouth.conf ] ||
  ! cmp -s "$work/99-vermouth.conf" /etc/systemd/journald.conf.d/99-vermouth.conf; then
  install -o root -g root -m 0644 "$work/99-vermouth.conf" /etc/systemd/journald.conf.d/99-vermouth.conf
  systemctl kill --kill-who=main --signal=SIGHUP systemd-journald
fi

install -d -o root -g root -m 0755 /var/lib/rancher/k3s/agent/etc/kubelet.conf.d
cat >"$work/90-vermouth-logging.conf" <<'EOF'
apiVersion: kubelet.config.k8s.io/v1beta1
kind: KubeletConfiguration
containerLogMaxSize: 10Mi
containerLogMaxFiles: 3
EOF
if [ ! -f /var/lib/rancher/k3s/agent/etc/kubelet.conf.d/90-vermouth-logging.conf ] ||
  ! cmp -s "$work/90-vermouth-logging.conf" /var/lib/rancher/k3s/agent/etc/kubelet.conf.d/90-vermouth-logging.conf; then
  install -o root -g root -m 0644 "$work/90-vermouth-logging.conf" \
    /var/lib/rancher/k3s/agent/etc/kubelet.conf.d/90-vermouth-logging.conf
  k3s_changed=true
fi
install -d -o root -g root -m 0755 /etc/rancher/k3s/config.yaml.d
cat >"$work/90-vermouth-network.yaml" <<'EOF'
cluster-cidr: 10.42.0.0/16
EOF
if [ ! -f /etc/rancher/k3s/config.yaml.d/90-vermouth-network.yaml ] ||
  ! cmp -s "$work/90-vermouth-network.yaml" /etc/rancher/k3s/config.yaml.d/90-vermouth-network.yaml; then
  install -o root -g root -m 0644 "$work/90-vermouth-network.yaml" \
    /etc/rancher/k3s/config.yaml.d/90-vermouth-network.yaml
  k3s_changed=true
fi
if [ "$k3s_changed" = true ]; then
  systemctl restart k3s
  systemctl is-active --quiet k3s || bootstrap_fail "k3s did not restart with the log rotation configuration"
  kubectl wait --for=condition=Ready "node/$expected_node" --timeout=5m >/dev/null ||
    bootstrap_fail "the k3s node did not become ready after log rotation configuration"
fi
kubectl --namespace kube-system rollout status deployment/metrics-server --timeout=5m >/dev/null ||
  bootstrap_fail "packaged metrics-server is not ready"
kubectl top node "$expected_node" >/dev/null || bootstrap_fail "metrics-server cannot provide a current node sample"

mkdir -p /etc/vermouth
chmod 0700 /etc/vermouth
env_file=/etc/vermouth/production.env
[ -f "$env_file" ] || bootstrap_fail "$env_file is missing"
[ "$(stat -c '%U:%G:%a' "$env_file")" = root:root:600 ] || bootstrap_fail "$env_file must be root owned with mode 0600"

platform_file=/etc/vermouth/platform.json
restore_bootstrap=$(jq -r '.restore_bootstrap' "$request")
restore_manifest=/etc/vermouth/restore-bootstrap-manifest.json
if [ "$restore_bootstrap" = true ]; then
  [ ! -e "$platform_file" ] || bootstrap_fail "disaster bootstrap requires an empty platform identity"
  [ -f "$restore_manifest" ] && [ ! -L "$restore_manifest" ] ||
    bootstrap_fail "the verified restore bootstrap manifest is missing"
  vermouth-platformconfig validate-document /usr/local/share/vermouth/export-manifest.schema.json "$restore_manifest"
  if [ -d /var/lib/vermouth/releases ] &&
    find /var/lib/vermouth/releases -mindepth 1 -maxdepth 1 -print -quit | grep -q .; then
    bootstrap_fail "disaster bootstrap requires no existing immutable release directories"
  fi
  jq -e --arg hostname "$hostname" --arg vm "$expected_vm" --arg node "$expected_node" '
    .schema_version == 1 and .hostname == $hostname and
    .storage_identity.vm_name == $vm and .storage_identity.node_name == $node
  ' "$restore_manifest" >/dev/null || bootstrap_fail "the restore bootstrap manifest belongs to another target"
  existing_vermouth_resources=$(kubectl --namespace vermouth get deployment,statefulset,pvc \
    -l app.kubernetes.io/name=vermouth -o json 2>/dev/null | jq '.items | length' || printf '%s' 0)
  [ "$existing_vermouth_resources" -eq 0 ] || bootstrap_fail "disaster bootstrap requires an empty Vermouth namespace"
fi
if [ -f "$platform_file" ]; then
  platform_existing=true
  jq -e --arg vm "$expected_vm" --arg node "$expected_node" --arg hostname "$hostname" --arg dedicated "$dedicated_root" --arg fallback "$fallback_root" \
    '.schema_version == 1 and .vm_name == $vm and .node_name == $node and .hostname == $hostname and .locked == true and (.storage_root == $dedicated or .storage_root == $fallback)' \
    "$platform_file" >/dev/null || bootstrap_fail "locked platform identity does not match this bootstrap"
  storage_mode=$(jq -r '.storage_mode' "$platform_file")
  storage_root=$(jq -r '.storage_root' "$platform_file")
  selected_uuid=$(jq -r '.storage_uuid // empty' "$platform_file")
  selected_at=$(jq -r '.selected_at' "$platform_file")
  [ -d "$storage_root" ] && [ ! -L "$storage_root" ] || bootstrap_fail "locked storage root $storage_root is missing"
  marker=$storage_root/.vermouth-storage
  [ -f "$marker" ] && [ ! -L "$marker" ] || bootstrap_fail "locked storage marker $marker is missing"
  locked_identity_hash=$(jq -S -c '{schema_version,vm_name,node_name,storage_mode,storage_root,storage_uuid,selected_at,locked}' "$platform_file" | sha256sum | awk '{print $1}')
  [ "$(cat "$marker")" = "$locked_identity_hash" ] || bootstrap_fail "locked storage marker does not match platform identity"
else
  platform_existing=false
  existing_pvs=$(kubectl get pv -o json | jq '[.items[] | select(.spec.storageClassName == "vermouth-local")] | length')
  [ "$existing_pvs" -eq 0 ] || bootstrap_fail "Vermouth PVs exist before the platform storage identity"
  device=$(findfs "UUID=$storage_uuid" 2>/dev/null || true)
  if [ -n "$device" ]; then
    [ "$(blkid -o value -s TYPE "$device")" = xfs ] || bootstrap_fail "the dedicated storage filesystem must be XFS"
    storage_mode=dedicated
    storage_root=$dedicated_root
    selected_uuid=$storage_uuid
    mounted=$(findmnt -rn -S "UUID=$storage_uuid" -o TARGET 2>/dev/null || true)
    [ -z "$mounted" ] || [ "$mounted" = "$dedicated_root" ] || bootstrap_fail "the dedicated disk is mounted at $mounted"
    mkdir -p "$dedicated_root"
    fstab_line="UUID=$storage_uuid $dedicated_root xfs defaults,nofail,x-systemd.device-timeout=30s 0 2"
    if ! grep -Fx "$fstab_line" /etc/fstab >/dev/null; then
      grep -Eq "(^UUID=$storage_uuid[[:space:]]|[[:space:]]$dedicated_root[[:space:]])" /etc/fstab &&
        bootstrap_fail "an existing fstab entry conflicts with Vermouth storage"
      cp -p /etc/fstab /etc/fstab.vermouth-backup
      printf '%s\n' "$fstab_line" >>/etc/fstab
    fi
    mountpoint -q "$dedicated_root" || mount "$dedicated_root"
  else
    storage_mode=os-fallback
    storage_root=$fallback_root
    selected_uuid=
    mkdir -p "$fallback_root"
  fi
  selected_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  if [ "$restore_bootstrap" = true ]; then
    recorded_mode=$(jq -r '.storage_identity.storage_mode' "$restore_manifest")
    recorded_root=$(jq -r '.storage_identity.storage_root' "$restore_manifest")
    recorded_uuid=$(jq -r '.storage_identity.storage_uuid // empty' "$restore_manifest")
    [ "$storage_mode" = "$recorded_mode" ] && [ "$storage_root" = "$recorded_root" ] &&
      [ "$selected_uuid" = "$recorded_uuid" ] ||
      bootstrap_fail "the available storage does not match the exported platform identity"
    selected_at=$(jq -r '.storage_identity.selected_at' "$restore_manifest")
    for component in postgres-identity postgres-teaching postgres-billing postgres-notifications redpanda garage-metadata garage-data traefik-acme; do
      [ ! -e "$storage_root/$component" ] || bootstrap_fail "disaster bootstrap target $storage_root/$component is not empty"
    done
  fi
fi

chart_hash=$(jq -r '.chart_sha256' "$request")
image_lock_hash=$(jq -r '.image_lock_sha256' "$request")
next=$platform_file.next
if [ "$platform_existing" = true ]; then
  jq --arg chart "$chart_hash" --arg image_lock "$image_lock_hash" \
    '.chart_sha256 = $chart | .image_lock_sha256 = $image_lock' "$platform_file" >"$next"
else
  mkdir -p "$storage_root"
  jq -n \
    --arg vm "$expected_vm" --arg node "$expected_node" --arg hostname "$hostname" --arg k3s "$expected_k3s" \
    --arg mode "$storage_mode" --arg root "$storage_root" --arg uuid "$selected_uuid" --arg selected_at "$selected_at" \
    --arg chart "$chart_hash" --arg image_lock "$image_lock_hash" \
    '{schema_version:1,vm_name:$vm,node_name:$node,hostname:$hostname,k3s_version:$k3s,storage_mode:$mode,storage_root:$root,storage_uuid:(if $uuid == "" then null else $uuid end),selected_at:$selected_at,locked:true,chart_sha256:$chart,image_lock_sha256:$image_lock,current_git_sha:"",github_run_id:"",github_run_attempt:"",release_identity:"",images_json_sha256:"",deployment_base_sha256:"",migration_compat_evidence_sha256:"",rate_limit_evidence_sha256:"",bundle_manifest_sha256:"",goose_versions:{identity:0,teaching:0,billing:0,notifications:0},active_image_digest_set:[],current_application_revision:null,previous_deployable_revision:null}' \
    >"$next"
fi
chmod 0600 "$next"
vermouth-platformconfig validate-document /usr/local/share/vermouth/platform.schema.json "$next"
mv "$next" "$platform_file"
identity_hash=$(jq -S -c '{schema_version,vm_name,node_name,storage_mode,storage_root,storage_uuid,selected_at,locked}' "$platform_file" | sha256sum | awk '{print $1}')
marker=$storage_root/.vermouth-storage
if [ "$platform_existing" = false ]; then
  if [ "$restore_bootstrap" = true ]; then
    [ "$identity_hash" = "$(jq -r '.storage_identity.marker_sha256' "$restore_manifest")" ] ||
      bootstrap_fail "the recreated platform identity does not match the export marker"
  fi
  printf '%s\n' "$identity_hash" >"$marker"
fi
chown root:root "$marker"
chmod 0644 "$marker"

for item in postgres-identity:70:70 postgres-teaching:70:70 postgres-billing:70:70 postgres-notifications:70:70 redpanda:101:101 garage-metadata:1000:1000 garage-data:1000:1000 traefik-acme:65532:65532; do
  name=${item%%:*}
  owner=${item#*:}
  mkdir -p "$storage_root/$name"
  chown "$owner" "$storage_root/$name"
  chmod 0700 "$storage_root/$name"
done
for file in acme-staging.json acme.json; do
  [ -e "$storage_root/traefik-acme/$file" ] || : >"$storage_root/traefik-acme/$file"
  chown 65532:65532 "$storage_root/traefik-acme/$file"
  chmod 0600 "$storage_root/traefik-acme/$file"
done

set -a
. "$env_file"
set +a
for name in DOCKERHUB_READ_USERNAME DOCKERHUB_READ_TOKEN; do
  eval "value=\${$name:-}"
  [ -n "$value" ] || bootstrap_fail "$env_file is missing $name"
done
secret_dir=$(mktemp -d /var/tmp/vermouth-pull-secret.XXXXXX)
chmod 0700 "$secret_dir"
auth=$(printf '%s:%s' "$DOCKERHUB_READ_USERNAME" "$DOCKERHUB_READ_TOKEN" | base64 -w 0)
jq -n --arg username "$DOCKERHUB_READ_USERNAME" --arg password "$DOCKERHUB_READ_TOKEN" --arg auth "$auth" \
  '{auths:{"https://index.docker.io/v1/":{username:$username,password:$password,auth:$auth}}}' >"$secret_dir/.dockerconfigjson"
chmod 0600 "$secret_dir/.dockerconfigjson"
kubectl create namespace vermouth --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl --namespace vermouth create secret generic dockerhub-pull --type=kubernetes.io/dockerconfigjson \
  --from-file=.dockerconfigjson="$secret_dir/.dockerconfigjson" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
rm -rf "$secret_dir"
unset DOCKERHUB_READ_TOKEN

runtime_values=$work/runtime-values-selected.yaml
sed "s|rootPath: .*|rootPath: $storage_root|; s|markerSHA256: .*|markerSHA256: $identity_hash|" "$work/runtime-values.yaml" >"$runtime_values"
helm upgrade --install vermouth-foundation "$work/chart" --namespace vermouth --create-namespace \
  --values "$work/chart/values-production.yaml" --values "$runtime_values" \
  --set foundation.enabled=false --set foundation.bootstrapStorage=true \
  --atomic --wait --timeout 5m --history-max 5 >/dev/null
[ "$(kubectl get pv -o json | jq '[.items[] | select(.spec.storageClassName == "vermouth-local")] | length')" -eq 8 ] ||
  bootstrap_fail "bootstrap did not create eight static PVs"
[ "$(kubectl --namespace vermouth get pvc -o json | jq '[.items[] | select(.spec.storageClassName == "vermouth-local")] | length')" -eq 7 ] ||
  bootstrap_fail "bootstrap did not create seven application data claims"
[ "$(kubectl --namespace kube-system get pvc traefik-acme -o json | jq -r '.spec.storageClassName')" = vermouth-local ] ||
  bootstrap_fail "bootstrap did not create the Traefik ACME claim in kube-system"

kubectl --namespace vermouth delete ingress acme-staging-probe --ignore-not-found >/dev/null
kubectl --namespace vermouth delete service acme-staging-probe --ignore-not-found >/dev/null
install -o root -g root -m 0644 "$work/traefik-config.yaml" /var/lib/rancher/k3s/server/manifests/traefik-config.yaml
kubectl --namespace kube-system rollout status deployment/traefik --timeout=5m >/dev/null
traefik_deadline=$(( $(date +%s) + 300 ))
while ! kubectl --namespace kube-system get pod \
  --selector=app.kubernetes.io/instance=traefik-kube-system,app.kubernetes.io/name=traefik -o json |
  jq -e '
    (.items | length) == 1 and
    .items[0].metadata.deletionTimestamp == null and
    any(.items[0].status.conditions[]; .type == "Ready" and .status == "True")
  ' >/dev/null; do
  [ "$(date +%s)" -lt "$traefik_deadline" ] || bootstrap_fail "the prior Traefik Pod did not leave the rollout"
  sleep 2
done

deploy_user=$(jq -r '.target.deploy_user' "$config")
if ! id "$deploy_user" >/dev/null 2>&1; then
  useradd --create-home --home-dir /var/lib/vermouth-deploy --shell /bin/sh "$deploy_user"
fi
install -d -o "$deploy_user" -g "$deploy_user" -m 0700 /var/lib/vermouth-deploy/.ssh
public_key=$(jq -r '.deploy_public_key' "$request")
key_type=$(printf '%s' "$public_key" | awk '{print $1}')
key_body=$(printf '%s' "$public_key" | awk '{print $2}')
key_id=$(jq -r '.deploy_key_id' "$request")
authorized=/var/lib/vermouth-deploy/.ssh/authorized_keys
next_authorized=$authorized.next
if [ -f "$authorized" ]; then
  grep -Fv " $key_id" "$authorized" >"$next_authorized" || true
else
  : >"$next_authorized"
fi
printf 'restrict,command="/usr/local/sbin/vermouth-ssh-entrypoint" %s %s %s\n' "$key_type" "$key_body" "$key_id" >>"$next_authorized"
chown "$deploy_user:$deploy_user" "$next_authorized"
chmod 0600 "$next_authorized"
mv "$next_authorized" "$authorized"

install -d -o root -g root -m 0755 /var/lib/vermouth/releases
install -d -o "$deploy_user" -g "$deploy_user" -m 0700 /var/lib/vermouth/releases/.incoming
printf '%s\n' "$deploy_user ALL=(root) NOPASSWD: /usr/local/sbin/vermouth-deploy-root" >/etc/sudoers.d/vermouth-deploy
chmod 0440 /etc/sudoers.d/vermouth-deploy
visudo -cf /etc/sudoers.d/vermouth-deploy >/dev/null
cat >/etc/ssh/sshd_config.d/99-vermouth.conf <<EOF
PermitRootLogin no
Match User $deploy_user
    AuthenticationMethods publickey
    PasswordAuthentication no
    PermitTTY no
    X11Forwarding no
    AllowTcpForwarding no
    PermitTunnel no
    GatewayPorts no
EOF
chmod 0644 /etc/ssh/sshd_config.d/99-vermouth.conf
sshd -t
systemctl reload ssh

cp "$config" /etc/vermouth/platform-config.json
chmod 0600 /etc/vermouth/platform-config.json
kubectl --namespace vermouth create configmap vermouth-platform-identity \
  --from-file=platform.json="$platform_file" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
rm -f "$restore_manifest"

cat <<EOF | kubectl --namespace vermouth apply -f - >/dev/null
apiVersion: v1
kind: Service
metadata:
  name: acme-staging-probe
spec:
  ports:
    - name: http
      port: 8080
      targetPort: 8080
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: acme-staging-probe
  annotations:
    traefik.ingress.kubernetes.io/router.entrypoints: websecure
    traefik.ingress.kubernetes.io/router.tls: "true"
    traefik.ingress.kubernetes.io/router.tls.certresolver: letsencrypt-staging
spec:
  ingressClassName: traefik
  rules:
    - host: $hostname
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: acme-staging-probe
                port:
                  number: 8080
EOF
deadline=$(( $(date +%s) + 300 ))
while ! jq -e --arg hostname "$hostname" '
  .. | objects |
  select(
    (.domain?.main? // "") == $hostname and
    (.certificate? | type == "string" and length > 0) and
    (.key? | type == "string" and length > 0)
  )
' "$storage_root/traefik-acme/acme-staging.json" >/dev/null 2>&1; do
  [ "$(date +%s)" -lt "$deadline" ] || bootstrap_fail "staging certificate was not issued within 5 minutes"
  sleep 5
done
kubectl --namespace vermouth delete ingress acme-staging-probe --ignore-not-found >/dev/null
kubectl --namespace vermouth delete service acme-staging-probe --ignore-not-found >/dev/null

printf '%s\n' "Production storage and Traefik are ready at $hostname"
