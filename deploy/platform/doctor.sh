#!/bin/sh
set -eu

doctor() {
  for tool in colima docker k3d kubectl helm task go node corepack jq openssl shasum curl lsof rg; do
    need "$tool"
  done
  validate_platform_inputs

  [ "$(uname -s)" = Darwin ] || fail "the local platform supports macOS in this feature"
  [ "$(uname -m)" = arm64 ] || fail "the local platform supports Apple silicon in this feature"

  colima_version=$(colima version | awk 'NR == 1 {print $3}')
  docker_client_version=$(docker --version | sed -n 's/^Docker version \([0-9][0-9.]*\).*/\1/p')
  buildx_version=$(docker buildx version | sed -n 's/.* v\([0-9][0-9.]*\).*/\1/p')
  k3d_version=$(k3d version | awk 'NR == 1 {sub(/^v/, "", $3); print $3}')
  kubectl_version=$(kubectl version --client -o json | jq -r '.clientVersion.gitVersion' | sed 's/^v//')
  helm_version=$(helm version --template '{{.Version}}' | sed 's/^v//')
  task_version=$(task --version)
  go_version=$(go version | awk '{sub(/^go/, "", $3); print $3}')
  node_version=$(node --version | sed 's/^v//')
  pnpm_version=$(corepack pnpm --version)

  require_series Colima "$colima_version" "$(yaml_value "$VERSIONS" tools.colima.series)" "$(yaml_value "$VERSIONS" tools.colima.minimum)"
  require_series Docker "$docker_client_version" "$(yaml_value "$VERSIONS" tools.dockerClient.series)" "$(yaml_value "$VERSIONS" tools.dockerClient.minimum)"
  require_series Buildx "$buildx_version" "$(yaml_value "$VERSIONS" tools.buildx.series)" "$(yaml_value "$VERSIONS" tools.buildx.minimum)"
  require_series k3d "$k3d_version" "$(yaml_value "$VERSIONS" tools.k3d.series)" "$(yaml_value "$VERSIONS" tools.k3d.minimum)"
  accepted_kubectl=$(yaml_value "$VERSIONS" tools.kubectl.acceptedSeries)
  case "$kubectl_version" in
    1.35.*|1.36.*) printf '%-12s %s\n' kubectl "$kubectl_version" ;;
    *) fail "kubectl $kubectl_version is outside accepted series $accepted_kubectl" ;;
  esac
  require_series Helm "$helm_version" "$(yaml_value "$VERSIONS" tools.helm.series)" "$(yaml_value "$VERSIONS" tools.helm.minimum)"
  require_series Task "$task_version" "$(yaml_value "$VERSIONS" tools.task.series)" "$(yaml_value "$VERSIONS" tools.task.minimum)"
  require_series Go "$go_version" "$(yaml_value "$VERSIONS" tools.go.series)" "$(yaml_value "$VERSIONS" tools.go.minimum)"
  require_series Node "$node_version" "$(yaml_value "$VERSIONS" tools.node.series)" "$(yaml_value "$VERSIONS" tools.node.minimum)"
  [ "$pnpm_version" = "$(yaml_value "$VERSIONS" tools.pnpm.exact)" ] || fail "pnpm $pnpm_version does not match the pinned version"
  printf '%-12s %s\n' pnpm "$pnpm_version"

  config_hash=$(sha256_file "$K3D_CONFIG")
  lock_hash=$(sha256_file "$IMAGE_LOCK")
  traefik_hash=$(sha256_files "$TRAEFIK_COMMON" "$TRAEFIK_LOCAL")
  schemas_hash=$(sha256_files "$ROOT/deploy/platform/images.schema.json" "$ROOT/deploy/platform/secrets.schema.json" "$ROOT/deploy/helm/vermouth/values.schema.json")
  matrices_hash=$(sha256_files "$ROOT/deploy/helm/vermouth/files/traffic-matrix.yaml" "$ROOT/deploy/helm/vermouth/files/workload-matrix.yaml" "$ROOT/deploy/platform/secret-inventory.yaml")
  printf '%-12s %s\n' k3d-config "$config_hash"
  printf '%-12s %s\n' images "$lock_hash"
  printf '%-12s %s\n' Traefik "$traefik_hash"
  printf '%-12s %s\n' schemas "$schemas_hash"
  printf '%-12s %s\n' matrices "$matrices_hash"

  profile_name=$(yaml_value "$VERSIONS" host.colimaProfile)
  profile=$(colima list --json | jq -c --arg name "$profile_name" 'select(.name == $name)')
  [ -n "$profile" ] || fail "the existing $profile_name Colima profile is required"
  cpu=$(printf '%s' "$profile" | jq -r '.cpus // 0')
  memory=$(printf '%s' "$profile" | jq -r '.memory // 0')
  [ "$cpu" -ge "$(yaml_value "$VERSIONS" host.minimumCPUs)" ] ||
    fail "Colima has $cpu CPUs, at least $(yaml_value "$VERSIONS" host.minimumCPUs) are required"
  if [ "$memory" -lt 16 ]; then
    memory_gib=$memory
  else
    memory_gib=$((memory / 1073741824))
  fi
  [ "$memory_gib" -ge "$(yaml_value "$VERSIONS" host.minimumMemoryGiB)" ] ||
    fail "Colima has ${memory_gib} GiB, at least $(yaml_value "$VERSIONS" host.minimumMemoryGiB) GiB are required"
  printf '%-12s %s CPU, %s GiB\n' Colima "$cpu" "$memory_gib"

  status=$(printf '%s' "$profile" | jq -r '.status')
  if [ "$status" != Running ]; then
    printf '%s\n' 'Colima is stopped. Bootstrap may start the existing profile.'
    return
  fi

  docker_context=$(yaml_value "$VERSIONS" host.dockerContext)
  endpoint=$(docker context inspect "$docker_context" --format '{{ (index .Endpoints "docker").Host }}')
  expected_endpoint="unix://$HOME/.colima/$profile_name/docker.sock"
  [ "$endpoint" = "$expected_endpoint" ] || fail "Docker context $docker_context uses $endpoint, expected $expected_endpoint"
  docker_server_version=$(docker_platform version --format '{{.Server.Version}}')
  require_series DockerServer "$docker_server_version" "$(yaml_value "$VERSIONS" tools.dockerServer.series)" "$(yaml_value "$VERSIONS" tools.dockerServer.minimum)"
  read_runtime_configuration
  printf '%-12s %s\n' profile-hash "$CURRENT_COLIMA_PROFILE_SHA256"
  printf '%-12s %s\n' daemon-hash "$CURRENT_DOCKER_DAEMON_CONFIG_SHA256"

  builder_name=$(yaml_value "$VERSIONS" builder.name)
  builder=$(docker_platform buildx inspect "$builder_name")
  live_builder_name=$(printf '%s' "$builder" | awk -F: '/^Name:/ {sub(/^[[:space:]]+/, "", $2); print $2; exit}')
  live_builder_driver=$(printf '%s' "$builder" | awk -F: '/^Driver:/ {sub(/^[[:space:]]+/, "", $2); print $2; exit}')
  live_builder_status=$(printf '%s' "$builder" | awk -F: '/^Status:/ {sub(/^[[:space:]]+/, "", $2); print $2; exit}')
  live_buildkit=$(printf '%s' "$builder" | awk -F: '/^BuildKit version:/ {sub(/^[[:space:]]+/, "", $2); print $2; exit}')
  builder_platforms=$(printf '%s' "$builder" | awk -F: '/^Platforms:/ {sub(/^[[:space:]]+/, "", $2); print $2; exit}')
  [ "$live_builder_name" = "$builder_name" ] || fail "Buildx builder name drifted"
  [ "$live_builder_driver" = "$(yaml_value "$VERSIONS" builder.driver)" ] ||
    fail "Buildx builder driver drifted"
  [ "$live_builder_status" = "$(yaml_value "$VERSIONS" builder.status)" ] ||
    fail "Buildx builder is not running"
  [ "$live_buildkit" = "$(yaml_value "$VERSIONS" builder.buildkitVersion)" ] ||
    fail "BuildKit version drifted"
  case "$builder_platforms" in
    *linux/arm64*linux/amd64*) ;;
    *) fail "Buildx builder platforms are $builder_platforms" ;;
  esac

  data_volume=$(yaml_value "$VERSIONS" storage.dataVolume)
  registry_volume=$(yaml_value "$VERSIONS" registry.dataVolume)
  if docker_platform volume inspect "$data_volume" >/dev/null 2>&1; then
    verify_volume_identity "$data_volume" "$(yaml_value "$VERSIONS" storage.roles.data)"
  fi
  if docker_platform volume inspect "$registry_volume" >/dev/null 2>&1; then
    verify_volume_identity "$registry_volume" "$(yaml_value "$VERSIONS" storage.roles.registry)"
  fi

  if ! cluster_exists; then
    docker_platform ps -a --filter "name=^/${CLUSTER}$" --format '{{.Names}}' | grep . >/dev/null &&
      fail "a Docker container collides with absent cluster $CLUSTER"
    printf '%s\n' "cluster $CLUSTER is absent and no conflicting target is present"
    return
  fi

  if ! cluster_running; then
    node_image=$(docker_platform inspect k3d-vermouth-server-0 --format '{{.Config.Image}}')
    case "$node_image" in
      *"$(yaml_value "$VERSIONS" cluster.k3sImageTag)"*) ;;
      *) fail "stopped cluster node image is $node_image" ;;
    esac
    verify_volume_identity "$data_volume" "$(yaml_value "$VERSIONS" storage.roles.data)"
    verify_volume_identity "$registry_volume" "$(yaml_value "$VERSIONS" storage.roles.registry)"
    printf '%s\n' "cluster $CLUSTER is stopped. Bootstrap may start it, then verify its live identity."
    return
  fi

  require_context
  if command -v lsof >/dev/null 2>&1; then
    for port in "$(yaml_value "$VERSIONS" bindings.applicationPort)" "$(yaml_value "$VERSIONS" bindings.registryPort)"; do
      listeners=$(lsof -nP -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | awk 'NR > 1 {print $9}')
      [ -n "$listeners" ] || fail "host port $port has no listener"
      printf '%s\n' "$listeners" | while IFS= read -r listener; do
        case "$listener" in
          127.0.0.1:"$port") ;;
          *) fail "host port $port listens on $listener instead of IPv4 loopback only" ;;
        esac
      done
    done
  fi
  load_balancer=k3d-$CLUSTER-serverlb
  application_host_ip=$(docker_platform inspect "$load_balancer" --format '{{ (index (index .HostConfig.PortBindings "80/tcp") 0).HostIp }}')
  application_host_port=$(docker_platform inspect "$load_balancer" --format '{{ (index (index .HostConfig.PortBindings "80/tcp") 0).HostPort }}')
  expected_application="$(yaml_value "$VERSIONS" bindings.applicationHost):$(yaml_value "$VERSIONS" bindings.applicationPort)"
  [ "$application_host_ip:$application_host_port" = "$expected_application" ] ||
    fail "application publishes on $application_host_ip:$application_host_port instead of $expected_application"
  for container in $(docker_platform ps -a --format '{{.Names}}'); do
    case "$container" in
      k3d-vermouth-*|vermouth-registry)
        publishes_port_80=$(docker_platform inspect "$container" | jq -r '
          [.[0].HostConfig.PortBindings // {} | to_entries[] | .value[]? | select(.HostPort == "80")] | length
        ')
        [ "$publishes_port_80" -eq 0 ] || fail "platform container $container publishes forbidden host port 80"
        ;;
    esac
  done
  registry_container=$(yaml_value "$VERSIONS" registry.containerName)
  docker_platform inspect "$registry_container" >/dev/null 2>&1 ||
    fail "exact registry container $registry_container is absent"
  registry_host_ip=$(docker_platform inspect "$registry_container" --format '{{ (index (index .HostConfig.PortBindings "5000/tcp") 0).HostIp }}')
  registry_host_port=$(docker_platform inspect "$registry_container" --format '{{ (index (index .HostConfig.PortBindings "5000/tcp") 0).HostPort }}')
  [ "$registry_host_ip:$registry_host_port" = "$HOST_REGISTRY" ] ||
    fail "registry publishes on $registry_host_ip:$registry_host_port instead of $HOST_REGISTRY"
  network=$(yaml_value "$VERSIONS" cluster.networkName)
  aliases=$(docker_platform inspect "$registry_container" | jq -c --arg network "$network" '.[0].NetworkSettings.Networks[$network].DNSNames')
  printf '%s' "$aliases" | jq -e --arg alias "$(yaml_value "$VERSIONS" registry.networkAlias)" 'index($alias) != null' >/dev/null ||
    fail "registry Docker network alias drifted"
  registry_mount=$(docker_platform inspect "$registry_container" | jq -r '.[] | .Mounts[] | select(.Destination == "/var/lib/registry") | .Name')
  [ "$registry_mount" = "$registry_volume" ] || fail "registry data mount is $registry_mount, expected $registry_volume"
  node=$(yaml_value "$VERSIONS" cluster.nodeName)
  node_network=$(docker_platform inspect "$node" | jq -r --arg network "$network" '.[0].NetworkSettings.Networks[$network].NetworkID // empty')
  registry_network=$(docker_platform inspect "$registry_container" | jq -r --arg network "$network" '.[0].NetworkSettings.Networks[$network].NetworkID // empty')
  [ -n "$node_network" ] && [ "$node_network" = "$registry_network" ] ||
    fail "node and registry do not share the exact Docker network $network"
  mirror_key=$(yaml_value "$VERSIONS" registry.mirrorKey)
  mirror_endpoint=$(yaml_value "$VERSIONS" registry.mirrorEndpoint)
  mirror_config=$(docker_platform exec "$node" cat /etc/rancher/k3s/registries.yaml 2>/dev/null || true)
  printf '%s' "$mirror_config" | rg -F "$mirror_key:" >/dev/null ||
    fail "containerd mirror key $mirror_key is absent"
  printf '%s' "$mirror_config" | rg -F "$mirror_endpoint" >/dev/null ||
    fail "containerd mirror endpoint $mirror_endpoint is absent"
  curl -fsS --max-time 5 "http://$HOST_REGISTRY/v2/" >/dev/null ||
    fail "registry host endpoint is not ready"
  docker_platform exec "$node" wget -qO- "$mirror_endpoint/v2/" >/dev/null ||
    fail "registry mirror endpoint is not ready from the node"

  live_identity=$(kube get configmap vermouth-platform-identity -o name 2>/dev/null || true)
  if [ "$live_identity" != configmap/vermouth-platform-identity ]; then
    if [ "${ALLOW_PARTIAL_IDENTITY:-false}" = true ]; then
      printf '%s\n' "cluster $CLUSTER is a verified partial bootstrap with no platform identity yet"
      return
    fi
    fail "cluster $CLUSTER has no Vermouth platform identity. Review it before confirmed recreate."
  fi

  live_config=$(kube get configmap vermouth-platform-identity -o jsonpath='{.data.k3dConfigSHA256}')
  live_lock=$(kube get configmap vermouth-platform-identity -o jsonpath='{.data.imageLockSHA256}')
  live_traefik=$(kube get configmap vermouth-platform-identity -o jsonpath='{.data.traefikConfigSHA256}')
  live_schemas=$(kube get configmap vermouth-platform-identity -o jsonpath='{.data.generatedSchemasSHA256}')
  live_matrices=$(kube get configmap vermouth-platform-identity -o jsonpath='{.data.platformMatricesSHA256}')
  live_profile_hash=$(kube get configmap vermouth-platform-identity -o jsonpath='{.data.colimaProfileConfigSHA256}')
  live_daemon_hash=$(kube get configmap vermouth-platform-identity -o jsonpath='{.data.dockerDaemonConfigSHA256}')
  cold_digest=$(kube get configmap vermouth-platform-identity -o jsonpath='{.data.coldProbeDigest}')
  cold_pulled_at=$(kube get configmap vermouth-platform-identity -o jsonpath='{.data.coldProbePulledAt}')
  cold_request=$(kube get configmap vermouth-platform-identity -o jsonpath='{.data.coldProbeManifestRequest}')
  [ "$live_config" = "$config_hash" ] || fail "the cluster k3d configuration drifted"
  [ "$live_traefik" = "$traefik_hash" ] || fail "the packaged Traefik configuration drifted"
  [ "$live_schemas" = "$schemas_hash" ] || fail "the generated state schemas drifted"
  [ "$live_matrices" = "$matrices_hash" ] || fail "the platform matrices drifted"
  [ "$live_profile_hash" = "$CURRENT_COLIMA_PROFILE_SHA256" ] || fail "the Colima profile configuration drifted"
  [ "$live_daemon_hash" = "$CURRENT_DOCKER_DAEMON_CONFIG_SHA256" ] || fail "the Docker daemon configuration drifted"
  [ "${#cold_digest}" -eq 71 ] && [ -n "$cold_pulled_at" ] && [ "${#cold_request}" -eq 64 ] ||
    fail "the recorded cold registry transport proof is missing or stale"
  if [ "${ALLOW_IMAGE_LOCK_DRIFT:-false}" != true ]; then
    [ "$live_lock" = "$lock_hash" ] || fail "the image lock drifted"
  fi

  live_k3s=$(kubectl --context "$CONTEXT" version -o json | jq -r '.serverVersion.gitVersion')
  [ "$live_k3s" = "$(yaml_value "$VERSIONS" cluster.k3sLiveVersion)" ] ||
    fail "cluster reports $live_k3s, expected $(yaml_value "$VERSIONS" cluster.k3sLiveVersion)"
  chart=$(kubectl --context "$CONTEXT" --namespace kube-system get helmchart traefik -o jsonpath='{.spec.chart}')
  case "$chart" in
    *"$(yaml_value "$VERSIONS" traefik.chartVersion)".tgz) ;;
    *) fail "live packaged Traefik chart drifted to $chart" ;;
  esac
  image=$(kubectl --context "$CONTEXT" --namespace kube-system get deployment traefik -o jsonpath='{.spec.template.spec.containers[0].image}')
  case "$image" in
    *@"$(yaml_value "$VERSIONS" traefik.imageDigest)") ;;
    *) fail "live packaged Traefik image drifted to $image" ;;
  esac

  if [ -f "$IMAGES_JSON" ]; then
    platform_config validate-document "$ROOT/deploy/platform/images.schema.json" "$IMAGES_JSON"
  fi
  if [ -f "$SECRETS_JSON" ]; then
    platform_config validate-document "$ROOT/deploy/platform/secrets.schema.json" "$SECRETS_JSON"
  fi
  printf '%s\n' "cluster $CLUSTER and explicit context $CONTEXT match"
}
