#!/bin/sh
set -eu

doctor() {
  for tool in colima docker k3d kubectl helm task go node corepack jq openssl shasum curl; do
    need "$tool"
  done

  [ "$(uname -s)" = Darwin ] || fail "the local platform supports macOS in this feature"
  [ "$(uname -m)" = arm64 ] || fail "the local platform supports Apple silicon in this feature"

  colima_version=$(colima version | awk 'NR == 1 {print $3}')
  docker_version=$(docker version --format '{{.Client.Version}}')
  buildx_version=$(docker buildx version | sed -n 's/.* v\([0-9][0-9.]*\).*/\1/p')
  k3d_version=$(k3d version | awk 'NR == 1 {sub(/^v/, "", $3); print $3}')
  kubectl_version=$(kubectl version --client -o json | jq -r '.clientVersion.gitVersion' | sed 's/^v//')
  helm_version=$(helm version --template '{{.Version}}' | sed 's/^v//')
  task_version=$(task --version)
  go_version=$(go version | awk '{sub(/^go/, "", $3); print $3}')
  node_version=$(node --version | sed 's/^v//')
  pnpm_version=$(corepack pnpm --version)

  require_series Colima "$colima_version" "$(yaml_value "$VERSIONS" tools.colima.series)" "$(yaml_value "$VERSIONS" tools.colima.minimum)"
  require_series Docker "$docker_version" "$(yaml_value "$VERSIONS" tools.docker.series)" "$(yaml_value "$VERSIONS" tools.docker.minimum)"
  require_series Buildx "$buildx_version" "$(yaml_value "$VERSIONS" tools.buildx.series)" "$(yaml_value "$VERSIONS" tools.buildx.minimum)"
  require_series k3d "$k3d_version" "$(yaml_value "$VERSIONS" tools.k3d.series)" "$(yaml_value "$VERSIONS" tools.k3d.minimum)"
  case "$kubectl_version" in
    1.35.*|1.36.*) printf '%-12s %s\n' kubectl "$kubectl_version" ;;
    *) fail "kubectl $kubectl_version is outside supported series 1.35.x and 1.36.x" ;;
  esac
  require_series Helm "$helm_version" "$(yaml_value "$VERSIONS" tools.helm.series)" "$(yaml_value "$VERSIONS" tools.helm.minimum)"
  require_series Task "$task_version" "$(yaml_value "$VERSIONS" tools.task.series)" "$(yaml_value "$VERSIONS" tools.task.minimum)"
  require_series Go "$go_version" "$(yaml_value "$VERSIONS" tools.go.series)" "$(yaml_value "$VERSIONS" tools.go.minimum)"
  require_series Node "$node_version" "$(yaml_value "$VERSIONS" tools.node.series)" "$(yaml_value "$VERSIONS" tools.node.minimum)"
  [ "$pnpm_version" = "$(yaml_value "$VERSIONS" tools.pnpm.exact)" ] || fail "pnpm $pnpm_version does not match 11.22.0"
  printf '%-12s %s\n' pnpm "$pnpm_version"

  config_hash=$(sha256_file "$ROOT/deploy/k3d/vermouth.yaml")
  lock_hash=$(sha256_file "$IMAGE_LOCK")
  printf '%-12s %s\n' config "$config_hash"
  printf '%-12s %s\n' images "$lock_hash"

  if ! colima status --json >/tmp/vermouth-colima-status.json 2>/dev/null; then
    printf '%s\n' 'Colima is stopped. Bootstrap may start the existing profile.'
    return
  fi
  cpu=$(jq -r '.cpu // .CPUs // 0' /tmp/vermouth-colima-status.json)
  memory=$(jq -r '.memory // .Memory // 0' /tmp/vermouth-colima-status.json)
  [ "$cpu" -ge 4 ] || fail "Colima has $cpu CPUs, at least 4 are required"
  if [ "$memory" -lt 16 ]; then
    memory_gib=$memory
  else
    memory_gib=$((memory / 1073741824))
  fi
  [ "$memory_gib" -ge 8 ] || fail "Colima has ${memory_gib} GiB, at least 8 GiB are required"
  printf '%-12s %s CPU, %s GiB\n' Colima "$cpu" "$memory_gib"

  if cluster_exists; then
    require_context
    live_config=$(kube get configmap vermouth-platform-identity -o jsonpath='{.data.k3dConfigSHA256}' 2>/dev/null || true)
    live_lock=$(kube get configmap vermouth-platform-identity -o jsonpath='{.data.imageLockSHA256}' 2>/dev/null || true)
    [ -z "$live_config" ] || [ "$live_config" = "$config_hash" ] || fail "the cluster k3d configuration drifted. Run task platform:recreate after reviewing the change."
    if [ "${ALLOW_IMAGE_LOCK_DRIFT:-false}" != true ]; then
      [ -z "$live_lock" ] || [ "$live_lock" = "$lock_hash" ] || fail "the image lock drifted. Run task platform:bootstrap to reconcile the pinned inputs."
    fi
    printf '%s\n' "cluster $CLUSTER and context $CONTEXT match"
  fi
}
