#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
VERSIONS=$ROOT/deploy/platform/versions.yaml
IMAGE_LOCK=$ROOT/deploy/images.lock.yaml
CHART=$ROOT/deploy/helm/vermouth
LOCAL_VALUES=$CHART/values-local.yaml
PRODUCTION_VALUES=$CHART/values-production.yaml
K3D_CONFIG=$ROOT/deploy/k3d/vermouth.yaml
TRAEFIK_COMMON=$ROOT/deploy/platform/traefik/values-common.yaml
TRAEFIK_LOCAL=$ROOT/deploy/platform/traefik/values-local.yaml
TRAEFIK_PRODUCTION=$ROOT/deploy/platform/traefik/values-production.yaml
PLATFORM_TMP=$ROOT/.tmp/platform
IMAGES_JSON=$PLATFORM_TMP/images.json
SECRETS_JSON=$PLATFORM_TMP/secrets.json
RUNTIME_VALUES=$PLATFORM_TMP/runtime-values.yaml
CONTEXT=k3d-vermouth
CLUSTER=vermouth
NAMESPACE=vermouth
REGISTRY=vermouth-registry
HOST_REGISTRY=127.0.0.1:5111
CLUSTER_REGISTRY=vermouth-registry:5000
RUNTIME_CONFIGURATION_CAPTURED=false
INITIAL_COLIMA_PROFILE_SHA256=
INITIAL_DOCKER_DAEMON_CONFIG_SHA256=
INITIAL_PORT_80_LISTENERS=
VERIFY_PORT_80_LISTENERS=false

mkdir -p -m 0700 "$PLATFORM_TMP"
chmod 0700 "$PLATFORM_TMP"

fail() {
  printf 'platform: %s\n' "$*" >&2
  exit "${PLATFORM_FAILURE_CODE:-1}"
}

fail_code() {
  code=$1
  shift
  printf 'platform: %s\n' "$*" >&2
  exit "$code"
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required. Run task platform:doctor for the full list."
}

platform_config() {
  (
    cd "$ROOT/pkg/vermouth"
    GOWORK=off go run ./cmd/platformconfig "$@"
  )
}

new_run_id() {
  (
    cd "$ROOT/pkg/vermouth"
    GOWORK=off go run ./cmd/runid
  )
}

docker_platform() {
  docker --context "$(yaml_value "$VERSIONS" host.dockerContext)" "$@"
}

kube() {
  kubectl --context "$CONTEXT" --namespace "$NAMESPACE" --request-timeout=15s "$@"
}

kube_cluster() {
  kubectl --context "$CONTEXT" --request-timeout=15s "$@"
}

cluster_exists() {
  k3d cluster list --no-headers 2>/dev/null | awk '{print $1}' | grep -Fx "$CLUSTER" >/dev/null
}

cluster_running() {
  [ "$(docker_platform inspect k3d-vermouth-server-0 --format '{{.State.Running}}' 2>/dev/null || true)" = true ]
}

registry_exists() {
  k3d registry list --no-headers 2>/dev/null | awk '{print $1}' | sed 's/^k3d-//' | grep -Fx "$REGISTRY" >/dev/null
}

require_context() {
  cluster_exists || fail_code 3 "cluster $CLUSTER is absent. Run task platform:bootstrap."
  kubectl config get-contexts "$CONTEXT" -o name 2>/dev/null | grep -Fx "$CONTEXT" >/dev/null ||
    fail_code 3 "explicit context $CONTEXT is absent. Recreate its kubeconfig without changing the current context."
  kubectl --context "$CONTEXT" --request-timeout=15s get --raw=/version >/dev/null 2>&1 ||
    fail_code 3 "explicit context $CONTEXT is unreachable. Run task platform:status for target details."
}

yaml_value() {
  file=$1
  wanted=$2
  platform_config get "$file" "$wanted"
}

validate_platform_inputs() {
  platform_config validate-inputs "$ROOT"
}

verify_volume_identity() {
  volume=$1
  role=$2
  volume_identity_matches "$volume" "$role" || fail "Docker volume $volume has missing or mismatched ownership labels"
}

volume_identity_matches() {
  volume=$1
  role=$2
  docker_platform volume inspect "$volume" >/dev/null 2>&1 || return 1
  platform_key=$(yaml_value "$VERSIONS" storage.labels.platformKey)
  platform_value=$(yaml_value "$VERSIONS" storage.labels.platformValue)
  role_key=$(yaml_value "$VERSIONS" storage.labels.roleKey)
  live_platform=$(docker_platform volume inspect "$volume" --format "{{ index .Labels \"$platform_key\" }}")
  live_role=$(docker_platform volume inspect "$volume" --format "{{ index .Labels \"$role_key\" }}")
  [ "$live_platform" = "$platform_value" ] && [ "$live_role" = "$role" ]
}

ensure_volume() {
  volume=$1
  role=$2
  if docker_platform volume inspect "$volume" >/dev/null 2>&1; then
    verify_volume_identity "$volume" "$role"
    return
  fi
  platform_key=$(yaml_value "$VERSIONS" storage.labels.platformKey)
  platform_value=$(yaml_value "$VERSIONS" storage.labels.platformValue)
  role_key=$(yaml_value "$VERSIONS" storage.labels.roleKey)
  docker_platform volume create \
    --label "$platform_key=$platform_value" \
    --label "$role_key=$role" \
    "$volume" >/dev/null
  verify_volume_identity "$volume" "$role"
}

version_at_least() {
  actual=$1
  minimum=$2
  awk -v actual="$actual" -v minimum="$minimum" 'BEGIN {
    split(actual, a, "."); split(minimum, b, ".")
    for (i = 1; i <= 4; i++) {
      av = a[i] + 0; bv = b[i] + 0
      if (av > bv) exit 0
      if (av < bv) exit 1
    }
    exit 0
  }'
}

require_series() {
  label=$1
  actual=$2
  series=$3
  minimum=$4
  case "$actual" in
    "$series"|"$series".*) ;;
    *) fail "$label $actual is outside supported series $series.x" ;;
  esac
  version_at_least "$actual" "$minimum" || fail "$label $actual is older than $minimum"
  printf '%-12s %s\n' "$label" "$actual"
}

sha256_file() {
  shasum -a 256 "$1" | awk '{print $1}'
}

sha256_text() {
  shasum -a 256 | awk '{print $1}'
}

sha256_files() {
  for file in "$@"; do
    printf '%s  %s\n' "$(sha256_file "$file")" "${file#"$ROOT/"}"
  done | sha256_text
}

read_runtime_configuration() {
  profile_name=$(yaml_value "$VERSIONS" host.colimaProfile)
  colima_root=${COLIMA_HOME:-$HOME/.colima}
  profile_config=$colima_root/$profile_name/colima.yaml
  [ -f "$profile_config" ] || fail "Colima profile configuration $profile_config is missing"
  CURRENT_COLIMA_PROFILE_SHA256=$(sha256_file "$profile_config")
  if colima ssh -- sudo test -f /etc/docker/daemon.json >/dev/null 2>&1; then
    CURRENT_DOCKER_DAEMON_CONFIG_SHA256=$(colima ssh -- sudo sha256sum /etc/docker/daemon.json | awk '{print $1}')
  else
    CURRENT_DOCKER_DAEMON_CONFIG_SHA256=absent
  fi
  CURRENT_PORT_80_LISTENERS=$(lsof -nP -iTCP:80 -sTCP:LISTEN 2>/dev/null | awk 'NR > 1 {print $1 "|" $2 "|" $9}' | sort || true)
}

capture_runtime_configuration() {
  read_runtime_configuration
  INITIAL_COLIMA_PROFILE_SHA256=$CURRENT_COLIMA_PROFILE_SHA256
  INITIAL_DOCKER_DAEMON_CONFIG_SHA256=$CURRENT_DOCKER_DAEMON_CONFIG_SHA256
  if [ "$VERIFY_PORT_80_LISTENERS" = true ]; then
    INITIAL_PORT_80_LISTENERS=$CURRENT_PORT_80_LISTENERS
  fi
  RUNTIME_CONFIGURATION_CAPTURED=true
}

verify_runtime_configuration_unchanged() {
  [ "$RUNTIME_CONFIGURATION_CAPTURED" = true ] || return 0
  read_runtime_configuration
  [ "$CURRENT_COLIMA_PROFILE_SHA256" = "$INITIAL_COLIMA_PROFILE_SHA256" ] ||
    fail "the Colima profile configuration changed during $PLATFORM_COMMAND"
  [ "$CURRENT_DOCKER_DAEMON_CONFIG_SHA256" = "$INITIAL_DOCKER_DAEMON_CONFIG_SHA256" ] ||
    fail "the Docker daemon configuration changed during $PLATFORM_COMMAND"
  if [ "$VERIFY_PORT_80_LISTENERS" = true ]; then
    [ "$CURRENT_PORT_80_LISTENERS" = "$INITIAL_PORT_80_LISTENERS" ] ||
      fail "the host port 80 listener set changed during $PLATFORM_COMMAND"
  fi
}

confirm_exact_vermouth() {
  description=$1
  printf '%s\n' "$description"
  printf 'Type vermouth to continue: '
  IFS= read -r answer
  [ "$answer" = vermouth ] || fail_code 8 "confirmation did not match vermouth, nothing changed"
}

confirm_vermouth() {
  action=$1
  confirm_exact_vermouth "$action targets cluster $CLUSTER, registry $REGISTRY, and their local data."
}

job_succeeded() {
  kube get job "$1" -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}' 2>/dev/null | grep -Fx True >/dev/null
}
