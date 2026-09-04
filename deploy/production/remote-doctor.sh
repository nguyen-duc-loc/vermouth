#!/bin/sh
set -eu

doctor_fail() {
  printf 'production doctor: %s\n' "$*" >&2
  exit 2
}

for tool in curl jq kubectl k3s ss findmnt df awk grep stat sha256sum getent nproc systemctl tr; do
  command -v "$tool" >/dev/null 2>&1 || doctor_fail "$tool is required on the VM"
done

metadata=$(curl -fsS --max-time 5 -H Metadata:true \
  'http://169.254.169.254/metadata/instance?api-version=2025-04-07') ||
  doctor_fail "Azure Instance Metadata Service is unavailable"
vm_name=$(printf '%s' "$metadata" | jq -r '.compute.name')
vm_size=$(printf '%s' "$metadata" | jq -r '.compute.vmSize')
location=$(printf '%s' "$metadata" | jq -r '.compute.location')
operator=$(printf '%s' "$metadata" | jq -r '.compute.osProfile.adminUsername')
public_ip=$(printf '%s' "$metadata" | jq -r '[.network.interface[].ipv4.ipAddress[].publicIpAddress | select(length > 0)][0] // empty')
[ "$vm_name" = "$EXPECTED_VM_NAME" ] || doctor_fail "VM name is $vm_name, expected $EXPECTED_VM_NAME"
[ "$vm_size" = "$EXPECTED_VM_SIZE" ] || doctor_fail "VM size is $vm_size, expected $EXPECTED_VM_SIZE"
[ "$location" = "$EXPECTED_LOCATION" ] || doctor_fail "VM location is $location, expected $EXPECTED_LOCATION"
[ "$operator" = "$EXPECTED_OPERATOR" ] || doctor_fail "operator is $operator, expected $EXPECTED_OPERATOR"
[ "$public_ip" = "$EXPECTED_HOST" ] || doctor_fail "public IP is $public_ip, expected $EXPECTED_HOST"
[ "$(nproc)" -eq 2 ] || doctor_fail "the VM must expose 2 CPUs"
printf '%-18s %s\n' VM "$vm_name, $vm_size, $location"

memory_total_kib=$(awk '/^MemTotal:/ {print $2}' /proc/meminfo)
memory_available_kib=$(awk '/^MemAvailable:/ {print $2}' /proc/meminfo)
[ "$memory_total_kib" -ge 7340032 ] && [ "$memory_total_kib" -le 9437184 ] ||
  doctor_fail "total memory is outside the expected 8 GiB VM range"
[ "$memory_available_kib" -ge 3145728 ] || doctor_fail "less than 3 GiB memory is available"
printf '%-18s %s\n' memory "${memory_available_kib} KiB available"

os_free_kib=$(df -Pk / | awk 'NR == 2 {print $4}')
[ "$os_free_kib" -ge 15728640 ] || doctor_fail "less than 15 GiB is free on the operating system disk"
printf '%-18s %s\n' os-disk "${os_free_kib} KiB free"

live_k3s=$(kubectl version -o json | jq -r '.serverVersion.gitVersion')
[ "$live_k3s" = "$EXPECTED_K3S" ] || doctor_fail "k3s is $live_k3s, expected $EXPECTED_K3S"
chart=$(kubectl --namespace kube-system get helmchart traefik -o jsonpath='{.spec.chart}')
case "$chart" in
  *"$EXPECTED_TRAEFIK_CHART".tgz) ;;
  *) doctor_fail "packaged Traefik chart is $chart, expected $EXPECTED_TRAEFIK_CHART" ;;
esac
image=$(kubectl --namespace kube-system get deployment traefik -o jsonpath='{.spec.template.spec.containers[0].image}')
case "$image" in
  "$EXPECTED_TRAEFIK_IMAGE"|*@"$EXPECTED_TRAEFIK_DIGEST") ;;
  *) doctor_fail "packaged Traefik image is $image, expected $EXPECTED_TRAEFIK_IMAGE" ;;
esac
printf '%-18s %s\n' k3s "$live_k3s"
printf '%-18s %s\n' Traefik "$EXPECTED_TRAEFIK_CHART, $image"

kubectl --namespace kube-system rollout status deployment/metrics-server --timeout=30s >/dev/null ||
  doctor_fail "packaged metrics-server is not ready"
kubectl top node "$EXPECTED_NODE" >/dev/null || doctor_fail "metrics-server cannot provide a current node sample"
printf '%-18s %s\n' metrics-server "ready with a current node sample"

if [ "${BOOTSTRAP_MODE:-false}" != true ]; then
  grep -Fx 'SystemMaxUse=512M' /etc/systemd/journald.conf.d/99-vermouth.conf >/dev/null ||
    doctor_fail "system journal retention is not capped at 512 MiB"
  grep -Fx 'RuntimeMaxUse=512M' /etc/systemd/journald.conf.d/99-vermouth.conf >/dev/null ||
    doctor_fail "runtime journal retention is not capped at 512 MiB"
  grep -Fx 'containerLogMaxSize: 10Mi' /var/lib/rancher/k3s/agent/etc/kubelet.conf.d/90-vermouth-logging.conf >/dev/null ||
    doctor_fail "container log size rotation is not configured"
  grep -Fx 'containerLogMaxFiles: 3' /var/lib/rancher/k3s/agent/etc/kubelet.conf.d/90-vermouth-logging.conf >/dev/null ||
    doctor_fail "container log file retention is not configured"
  printf '%-18s %s\n' log-retention "journal 512 MiB, container logs 10 MiB times 3"

  live_pod_cidr=$(systemctl show k3s --property=ExecStart --value | tr ' ' '\n' |
    awk -F= '$1 == "--cluster-cidr" {print $2; exit}')
  if [ -z "$live_pod_cidr" ]; then
    for config_file in /etc/rancher/k3s/config.yaml /etc/rancher/k3s/config.yaml.d/*.yaml; do
      [ -f "$config_file" ] || continue
      configured=$(awk '$1 == "cluster-cidr:" {print $2; exit}' "$config_file")
      [ -z "$configured" ] || live_pod_cidr=$configured
    done
  fi
  [ "$live_pod_cidr" = "$EXPECTED_POD_CIDR" ] ||
    doctor_fail "live k3s cluster CIDR is ${live_pod_cidr:-not explicit}, expected $EXPECTED_POD_CIDR"
  printf '%-18s %s\n' pod-network "$live_pod_cidr"
fi

resolved=$(getent ahostsv4 "$EXPECTED_HOSTNAME" | awk '{print $1}' | LC_ALL=C sort -u)
[ "$resolved" = "$EXPECTED_HOST" ] || doctor_fail "$EXPECTED_HOSTNAME does not resolve to $EXPECTED_HOST from the VM"
for port in 80 443; do
  ss -ltnH | awk -v port=":$port" '$4 ~ port "$" {found=1} END {exit !found}' ||
    doctor_fail "nothing listens on TCP port $port"
done

env_file=/etc/vermouth/production.env
[ -f "$env_file" ] || doctor_fail "$env_file is missing"
[ "$(stat -c '%U:%G:%a' "$env_file")" = root:root:600 ] ||
  doctor_fail "$env_file must be root owned with mode 0600"
for name in POSTGRES_PASSWORD IDENTITY_TOKEN_KID IDENTITY_TOKEN_PRIVATE_KEY TOKEN_PUBLIC_KEYS \
  IDENTITY_GOOGLE_CLIENT_ID IDENTITY_GOOGLE_CLIENT_SECRET GARAGE_RPC_SECRET GARAGE_ADMIN_TOKEN \
  BILLING_S3_ACCESS_KEY_ID BILLING_S3_SECRET_ACCESS_KEY DOCKERHUB_READ_USERNAME DOCKERHUB_READ_TOKEN \
  GATEWAY_TRUSTED_PROXY_CIDRS; do
  grep -Eq "^${name}=.+" "$env_file" || doctor_fail "$env_file is missing $name"
done
printf '%-18s %s\n' configuration "root owned production values are present"

set -a
. "$env_file"
set +a
[ "$GATEWAY_TRUSTED_PROXY_CIDRS" = "$EXPECTED_POD_CIDR" ] ||
  doctor_fail "GATEWAY_TRUSTED_PROXY_CIDRS must equal the live k3s cluster CIDR $EXPECTED_POD_CIDR"
curl_config=$(mktemp)
trap 'rm -f "$curl_config"' EXIT HUP INT TERM
chmod 0600 "$curl_config"
{
  printf 'silent\n'
  printf 'show-error\n'
  printf 'fail\n'
  printf 'max-time = 10\n'
  printf 'user = "%s:%s"\n' "$DOCKERHUB_READ_USERNAME" "$DOCKERHUB_READ_TOKEN"
} >"$curl_config"
curl --config "$curl_config" "https://auth.docker.io/token?service=registry.docker.io&scope=repository:$DOCKERHUB_NAMESPACE/vermouth-gateway:pull" |
  jq -e '.token | strings | length > 0' >/dev/null || doctor_fail "Docker Hub read access failed"
rm -f "$curl_config"
trap - EXIT HUP INT TERM
printf '%-18s %s\n' Docker-Hub "private pull token accepted"

if [ -f /etc/vermouth/platform.json ]; then
  platform=$(cat /etc/vermouth/platform.json)
  printf '%s' "$platform" | jq -e \
    --arg vm "$EXPECTED_VM_NAME" --arg node "$EXPECTED_NODE" --arg host "$EXPECTED_HOSTNAME" \
    --arg k3s "$EXPECTED_K3S" --arg dedicated "$EXPECTED_STORAGE_ROOT" --arg fallback "$EXPECTED_FALLBACK_ROOT" \
    '.schema_version == 1 and .vm_name == $vm and .node_name == $node and .hostname == $host and .k3s_version == $k3s and .locked == true and (.storage_root == $dedicated or .storage_root == $fallback)' \
    >/dev/null || doctor_fail "platform identity does not match the production target"
  storage_root=$(printf '%s' "$platform" | jq -r '.storage_root')
  marker=$storage_root/.vermouth-storage
  [ -f "$marker" ] || doctor_fail "locked storage marker $marker is missing"
  identity_hash=$(printf '%s' "$platform" | jq -S -c '{schema_version,vm_name,node_name,storage_mode,storage_root,storage_uuid,selected_at,locked}' | sha256sum | awk '{print $1}')
  [ "$(cat "$marker")" = "$identity_hash" ] || doctor_fail "locked storage marker does not match platform identity"
  printf '%-18s %s\n' storage "$(printf '%s' "$platform" | jq -r '.storage_mode + " at " + .storage_root')"
else
  if findmnt -rn -S "UUID=$EXPECTED_STORAGE_UUID" >/dev/null 2>&1; then
    printf '%-18s %s\n' storage "$EXPECTED_STORAGE_UUID is ready for first bootstrap"
  else
    printf '%-18s %s\n' storage "$EXPECTED_FALLBACK_ROOT will be selected on first bootstrap"
  fi
fi

printf '%s\n' "Remote production doctor passed for $EXPECTED_VM_NAME"
