#!/bin/sh
set -eu

start_watchdog() {
  seconds=$1
  parent=$$
  (
    timer=
    trap 'if [ -n "$timer" ]; then kill "$timer" 2>/dev/null || true; wait "$timer" 2>/dev/null || true; fi; exit 0' TERM INT
    sleep "$seconds" &
    timer=$!
    wait "$timer" || exit 0
    timer=
    printf 'platform: outer deadline of %s seconds expired\n' "$seconds" >&2
    pkill -TERM -P "$parent" 2>/dev/null || true
    kill -USR1 "$parent" 2>/dev/null || true
  ) &
  WATCHDOG_PID=$!
}

write_platform_identity() {
  config_hash=$(sha256_file "$K3D_CONFIG")
  lock_hash=$(sha256_file "$IMAGE_LOCK")
  traefik_hash=$(sha256_files "$TRAEFIK_COMMON" "$TRAEFIK_LOCAL")
  schemas_hash=$(sha256_files "$ROOT/deploy/platform/images.schema.json" "$ROOT/deploy/platform/secrets.schema.json" "$ROOT/deploy/helm/vermouth/values.schema.json")
  matrices_hash=$(sha256_files "$ROOT/deploy/helm/vermouth/files/traffic-matrix.yaml" "$ROOT/deploy/helm/vermouth/files/workload-matrix.yaml" "$ROOT/deploy/platform/secret-inventory.yaml")
  live_k3s=$(kubectl --context "$CONTEXT" version -o json | jq -r '.serverVersion.gitVersion')
  docker_server=$(docker_platform version --format '{{.Server.Version}}')
  kube create configmap vermouth-platform-identity \
    --from-literal=schemaVersion=1 \
    --from-literal=clusterName="$(yaml_value "$VERSIONS" cluster.name)" \
    --from-literal=colimaProfile="$(yaml_value "$VERSIONS" host.colimaProfile)" \
    --from-literal=dockerContext="$(yaml_value "$VERSIONS" host.dockerContext)" \
    --from-literal=dockerServerVersion="$docker_server" \
    --from-literal=buildxBuilder="$(yaml_value "$VERSIONS" builder.name)" \
    --from-literal=buildxDriver="$(yaml_value "$VERSIONS" builder.driver)" \
    --from-literal=buildkitVersion="$(yaml_value "$VERSIONS" builder.buildkitVersion)" \
    --from-literal=k3sLiveVersion="$live_k3s" \
    --from-literal=k3sImageTag="$(yaml_value "$VERSIONS" cluster.k3sImageTag)" \
    --from-literal=servers="$(yaml_value "$VERSIONS" cluster.servers)" \
    --from-literal=agents="$(yaml_value "$VERSIONS" cluster.agents)" \
    --from-literal=traefikImage="$(yaml_value "$VERSIONS" traefik.imageVersion)" \
    --from-literal=traefikChartVersion="$(yaml_value "$VERSIONS" traefik.chartVersion)" \
    --from-literal=staticStorageRoot="$(yaml_value "$VERSIONS" storage.rootPath)" \
    --from-literal=staticStorageNode="$(yaml_value "$VERSIONS" cluster.nodeName)" \
    --from-literal=dataVolume="$(yaml_value "$VERSIONS" storage.dataVolume)" \
    --from-literal=registryVolume="$(yaml_value "$VERSIONS" registry.dataVolume)" \
    --from-literal=hostPortMap="$(yaml_value "$VERSIONS" bindings.applicationHost):$(yaml_value "$VERSIONS" bindings.applicationPort):80" \
    --from-literal=registryName="$(yaml_value "$VERSIONS" registry.k3dName)" \
    --from-literal=registryContainer="$(yaml_value "$VERSIONS" registry.containerName)" \
    --from-literal=registryNetworkAlias="$(yaml_value "$VERSIONS" registry.networkAlias)" \
    --from-literal=registryHostEndpoint="$HOST_REGISTRY" \
    --from-literal=registryClusterEndpoint="$CLUSTER_REGISTRY" \
    --from-literal=registryMirrorKey="$(yaml_value "$VERSIONS" registry.mirrorKey)" \
    --from-literal=registryMirrorEndpoint="$(yaml_value "$VERSIONS" registry.mirrorEndpoint)" \
    --from-literal=coldProbeDigest="$COLD_PROBE_DIGEST" \
    --from-literal=coldProbePulledAt="$COLD_PROBE_PULLED_AT" \
    --from-literal=coldProbeManifestRequest="$COLD_PROBE_MANIFEST_REQUEST" \
    --from-literal=colimaProfileConfigSHA256="$INITIAL_COLIMA_PROFILE_SHA256" \
    --from-literal=dockerDaemonConfigSHA256="$INITIAL_DOCKER_DAEMON_CONFIG_SHA256" \
    --from-literal=k3dConfigSHA256="$config_hash" \
    --from-literal=traefikConfigSHA256="$traefik_hash" \
    --from-literal=imageLockSHA256="$lock_hash" \
    --from-literal=generatedSchemasSHA256="$schemas_hash" \
    --from-literal=platformMatricesSHA256="$matrices_hash" \
    --dry-run=client -o yaml | kube apply -f - >/dev/null
}

render_traefik_config() {
  output=$1
  environment_values=${2:-$TRAEFIK_LOCAL}
  {
    printf '%s\n' 'apiVersion: helm.cattle.io/v1'
    printf '%s\n' 'kind: HelmChartConfig'
    printf '%s\n' 'metadata:'
    printf '%s\n' '  name: traefik'
    printf '%s\n' '  namespace: kube-system'
    printf '%s\n' 'spec:'
    printf '%s\n' '  failurePolicy: abort'
    printf '%s\n' '  valuesContent: |-'
    sed 's/^/    /' "$TRAEFIK_COMMON"
    sed 's/^/    /' "$environment_values"
  } >"$output"
}

render_production_traefik_config() {
  output=$1
  email=$2
  case "$email" in
    *@*.*) ;;
    *) fail "PROD_ACME_EMAIL must be a valid email address" ;;
  esac
  case "$email" in
    *[!A-Za-z0-9._%+@-]*) fail "PROD_ACME_EMAIL must be a valid email address" ;;
  esac
  values=$PLATFORM_TMP/traefik-production.$$
  trap 'rm -f "$values"' EXIT HUP INT TERM
  sed "s/__PROD_ACME_EMAIL__/$email/g" "$TRAEFIK_PRODUCTION" >"$values"
  render_traefik_config "$output" "$values"
  rm -f "$values"
  trap - EXIT HUP INT TERM
}

reconcile_traefik() {
  config=$PLATFORM_TMP/traefik-config.yaml
  render_traefik_config "$config"
  kube_cluster apply -f "$config" >/dev/null
  deadline=$(($(date +%s) + 300))
  while ! kubectl --context "$CONTEXT" --namespace kube-system get deployment traefik >/dev/null 2>&1; do
    if [ "$(date +%s)" -ge "$deadline" ]; then
      kubectl --context "$CONTEXT" --namespace kube-system get helmchart,helmchartconfig,job,pod >&2 || true
      fail "packaged Traefik was not created within 5 minutes. Run task platform:status and inspect kube-system events."
    fi
    sleep 2
  done
  kubectl --context "$CONTEXT" --namespace kube-system rollout status deployment/traefik --timeout=5m >/dev/null

  chart=$(kubectl --context "$CONTEXT" --namespace kube-system get helmchart traefik -o jsonpath='{.spec.chart}')
  case "$chart" in
    *traefik-40.1.4+up40.1.0.tgz) ;;
    *) fail "packaged Traefik chart is $chart, expected generation 40.1.4+up40.1.0" ;;
  esac
  image=$(kubectl --context "$CONTEXT" --namespace kube-system get deployment traefik -o jsonpath='{.spec.template.spec.containers[0].image}')
  case "$image" in
    *@sha256:4299bbed850421258fc5448c2e0e6ad350981d4d335a68de11b92448aedbefe5) ;;
    *) fail "packaged Traefik runs $image instead of the pinned 3.7.8 digest" ;;
  esac
}

prepare_storage_paths() {
  node=$(yaml_value "$VERSIONS" cluster.nodeName)
  root_path=$(yaml_value "$VERSIONS" storage.rootPath)
  for item in \
    postgres-identity:70:70 postgres-teaching:70:70 postgres-billing:70:70 postgres-notifications:70:70 \
    redpanda:101:101 garage-metadata:1000:1000 garage-data:1000:1000 traefik-acme:65532:65532; do
    name=${item%%:*}
    owner=${item#*:}
    user=${owner%%:*}
    group=${owner##*:}
    docker_platform exec "$node" mkdir -p "$root_path/$name"
    docker_platform exec "$node" chown "$user:$group" "$root_path/$name"
    docker_platform exec "$node" chmod 0700 "$root_path/$name"
  done
}

check_port_free() {
  port=$1
  if command -v lsof >/dev/null 2>&1 && lsof -nP -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | grep . >/dev/null; then
    fail "local TCP port $port is already in use"
  fi
}

prove_registry_transport() {
  lock_hash=$(sha256_file "$IMAGE_LOCK")
  child_tag=$lock_hash-arm64
  final_tag=$lock_hash
  repository=$(yaml_value "$IMAGE_LOCK" probe.repositoryPath)
  platform=$(yaml_value "$IMAGE_LOCK" probe.platform)
  dockerfile=$(yaml_value "$IMAGE_LOCK" probe.dockerfile)
  local_ref=vermouth-build/platform-probe:$child_tag
  child_ref=$HOST_REGISTRY/$repository:$child_tag
  final_ref=$HOST_REGISTRY/$repository:$final_tag
  builder=$(yaml_value "$VERSIONS" builder.name)

	docker_platform buildx build --builder "$builder" --platform "$platform" --provenance=false --load --file "$ROOT/$dockerfile" --label vermouth.dev/workload=platform-probe --tag "$local_ref" "$ROOT"
  docker_platform tag "$local_ref" "$child_ref"
  push_with_retry "$child_ref" || fail "could not push the registry transport probe"
  docker_platform buildx imagetools create --tag "$final_ref" "$child_ref"
  digest=$(registry_digest "$repository" "$final_tag")
  [ "${#digest}" -eq 71 ] || fail "the registry transport probe has no remote descriptor"

  node=$(yaml_value "$VERSIONS" cluster.nodeName)
  cluster_ref=$CLUSTER_REGISTRY/$repository@$digest
  docker_platform exec "$node" crictl rmi "$cluster_ref" >/dev/null 2>&1 || true
  if docker_platform exec "$node" crictl images -o json | jq -e --arg ref "$cluster_ref" '.images[]? | select(.repoDigests[]? == $ref)' >/dev/null; then
    fail "the registry transport probe descriptor remained cached before the cold pull"
  fi
  started=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
  docker_platform exec "$node" crictl pull "$cluster_ref" >/dev/null ||
    fail "the node could not pull the cold registry transport probe"
  request=$(docker_platform logs --since "$started" "$(yaml_value "$VERSIONS" registry.containerName)" 2>&1 | rg "/v2/$repository/manifests/" | tail -1)
  [ -n "$request" ] || fail "the registry did not record a manifest request after the cold pull started"
  pulled=$(docker_platform exec "$node" crictl images -o json | jq -r --arg ref "$cluster_ref" '.images[]? | select(.repoDigests[]? == $ref) | .repoDigests[] | select(. == $ref)' | tail -1)
  [ "$pulled" = "$cluster_ref" ] || fail "the node pulled $pulled instead of $cluster_ref"

  COLD_PROBE_DIGEST=$digest
  COLD_PROBE_PULLED_AT=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
  COLD_PROBE_MANIFEST_REQUEST=$(printf '%s' "$request" | sha256_text)
}

reconcile_registry_identity() {
  wanted=$(yaml_value "$VERSIONS" registry.containerName)
  generated=k3d-$(yaml_value "$VERSIONS" registry.k3dName)
  if ! docker_platform inspect "$wanted" >/dev/null 2>&1; then
    docker_platform inspect "$generated" >/dev/null 2>&1 ||
      fail "the managed registry container is absent"
    docker_platform rename "$generated" "$wanted"
  fi
  host_ip=$(docker_platform inspect "$wanted" --format '{{ (index (index .HostConfig.PortBindings "5000/tcp") 0).HostIp }}')
  host_port=$(docker_platform inspect "$wanted" --format '{{ (index (index .HostConfig.PortBindings "5000/tcp") 0).HostPort }}')
  [ "$host_ip:$host_port" = "$HOST_REGISTRY" ] ||
    fail "registry publishes on $host_ip:$host_port instead of $HOST_REGISTRY"
  network=$(yaml_value "$VERSIONS" cluster.networkName)
  aliases=$(docker_platform inspect "$wanted" | jq -c --arg network "$network" '.[0].NetworkSettings.Networks[$network].DNSNames')
  printf '%s' "$aliases" | jq -e --arg alias "$(yaml_value "$VERSIONS" registry.networkAlias)" 'index($alias) != null' >/dev/null ||
    fail "registry has no exact Docker network alias $(yaml_value "$VERSIONS" registry.networkAlias)"
}

bootstrap() {
  ALLOW_IMAGE_LOCK_DRIFT=true ALLOW_PARTIAL_IDENTITY=true doctor
  if ! colima status >/dev/null 2>&1; then
    printf '%s\n' 'Starting the existing Colima profile.'
    colima start
  fi
  ALLOW_IMAGE_LOCK_DRIFT=true ALLOW_PARTIAL_IDENTITY=true doctor
  had_cluster=false
  if cluster_exists; then had_cluster=true; fi
  ensure_local_configuration
  task -d "$ROOT" web:install
  ensure_volume "$(yaml_value "$VERSIONS" storage.dataVolume)" "$(yaml_value "$VERSIONS" storage.roles.data)"
  ensure_volume "$(yaml_value "$VERSIONS" registry.dataVolume)" "$(yaml_value "$VERSIONS" storage.roles.registry)"

  if ! cluster_exists; then
    check_port_free "$(yaml_value "$VERSIONS" bindings.applicationPort)"
    check_port_free "$(yaml_value "$VERSIONS" bindings.registryPort)"
    k3d cluster create --config "$K3D_CONFIG" --timeout 8m
  else
    if registry_exists; then docker_platform start "$(yaml_value "$VERSIONS" registry.containerName)" >/dev/null; fi
    k3d cluster start "$CLUSTER"
  fi
  reconcile_registry_identity
  require_context
  ensure_platform_lease
  if [ "$had_cluster" = true ]; then ALLOW_IMAGE_LOCK_DRIFT=true ALLOW_PARTIAL_IDENTITY=true doctor; fi

  node_image=$(docker_platform inspect "$(yaml_value "$VERSIONS" cluster.nodeName)" --format '{{.Config.Image}}')
  case "$node_image" in
    *"$(yaml_value "$VERSIONS" cluster.k3sImageTag)"*) ;;
    *) fail "cluster node image is $node_image, expected $(yaml_value "$VERSIONS" cluster.k3sImageTag)" ;;
  esac

  kube_cluster create namespace "$NAMESPACE" --dry-run=client -o yaml | kube_cluster apply -f - >/dev/null
  prepare_storage_paths
  reconcile_traefik
	prove_registry_transport
	write_platform_identity
	prune_vermouth_host_images
	printf '%s\n' 'Bootstrap matches the committed cluster, registry, and packaged Traefik inputs.'
}

foundation_release() {
  PLATFORM_FAILURE_CODE=6
  helm --kube-context "$CONTEXT" upgrade --install vermouth-foundation "$CHART" \
    --namespace "$NAMESPACE" --values "$LOCAL_VALUES" --values "$RUNTIME_VALUES" \
    --set foundation.enabled=true --set application.enabled=false \
    --atomic --wait --timeout 8m --history-max 3
}

render_jobs() {
  helm template vermouth-jobs "$CHART" --namespace "$NAMESPACE" \
    --values "$LOCAL_VALUES" --values "$RUNTIME_VALUES" "$@"
}

run_garage_init() {
  PLATFORM_FAILURE_CODE=5
  run_id=$1
  job=garage-init-$run_id
  render_jobs --set jobs.runID="$run_id" --set jobs.garageInit.enabled=true | kube apply -f - >/dev/null
  if kube wait --for=condition=complete "job/$job" --timeout=305s >/dev/null 2>&1; then
    kube logs "job/$job"
    kube delete "job/$job" --wait=false >/dev/null
    return
  fi
  kube logs "job/$job" --all-containers=true >&2 || true
  fail "Garage initialization failed. Inspect it with task platform:logs -- garage-init."
}

run_migrations() {
  PLATFORM_FAILURE_CODE=5
  run_id=$1
  only=${2:-all}
  if [ "$only" != all ]; then
    render_jobs --set jobs.runID="$run_id" --set jobs.migrations.enabled=true \
      --set "jobs.migrations.services[0]=$only" | kube apply -f - >/dev/null
  else
    render_jobs --set jobs.runID="$run_id" --set jobs.migrations.enabled=true | kube apply -f - >/dev/null
  fi

  if [ "$only" = all ]; then services="identity teaching billing notifications"; else services=$only; fi
  wait_failed=false
  pids=""
  for service in $services; do
    job=migrate-$service-$run_id
    kube wait --for=condition=complete "job/$job" --timeout=305s >/dev/null 2>&1 &
    pids="$pids $!"
  done
  for pid in $pids; do
    wait "$pid" || wait_failed=true
  done
  for service in $services; do
    job=migrate-$service-$run_id
    if job_succeeded "$job"; then
      kube logs "job/$job"
      kube delete "job/$job" --wait=false >/dev/null
    else
      kube logs "job/$job" --all-containers=true >&2 || true
      wait_failed=true
    fi
  done
  [ "$wait_failed" = false ] || fail "a migration failed. The application release was not changed. Use task platform:status and task platform:logs."
}

application_release() {
  PLATFORM_FAILURE_CODE=6
  previous_values=$PLATFORM_TMP/application-previous-values.json
  previous_present=false
  if helm --kube-context "$CONTEXT" --namespace "$NAMESPACE" status vermouth >/dev/null 2>&1; then
    previous_present=true
    helm --kube-context "$CONTEXT" --namespace "$NAMESPACE" get values vermouth -o json >"$previous_values"
    chmod 0600 "$previous_values"
  fi
  if helm --kube-context "$CONTEXT" upgrade --install vermouth "$CHART" \
    --namespace "$NAMESPACE" --values "$LOCAL_VALUES" --values "$RUNTIME_VALUES" \
    --set foundation.enabled=false --set application.enabled=true \
    --atomic --cleanup-on-fail --wait --timeout 5m --history-max 3; then
    status=$(helm --kube-context "$CONTEXT" --namespace "$NAMESPACE" status vermouth -o json | jq -r '.info.status')
    [ "$status" = deployed ] || fail "application release finished with status $status"
    return
  fi
  if [ "$previous_present" = false ]; then
    helm --kube-context "$CONTEXT" --namespace "$NAMESPACE" uninstall vermouth --ignore-not-found >/dev/null 2>&1 || true
    fail "the first application install failed and its release metadata was removed"
  fi
  live_values=$PLATFORM_TMP/application-live-values.json
  helm --kube-context "$CONTEXT" --namespace "$NAMESPACE" get values vermouth -o json >"$live_values" ||
    fail "the application upgrade failed and Helm did not restore a deployed release"
  if ! jq -e -s '.[0].images == .[1].images and .[0].secrets == .[1].secrets' "$previous_values" "$live_values" >/dev/null; then
    fail "the application upgrade failed and its image or Secret references were not restored"
  fi
  fail "the application upgrade failed and Helm restored the prior image and Secret references"
}

recover_pending_application_release() {
  status=$(helm --kube-context "$CONTEXT" --namespace "$NAMESPACE" status vermouth -o json 2>/dev/null | jq -r '.info.status // empty' || true)
  case "$status" in
    pending-install|pending-upgrade|pending-rollback)
      revision=$(helm --kube-context "$CONTEXT" --namespace "$NAMESPACE" history vermouth -o json |
        jq -r '[.[] | select(.status == "deployed" or .status == "superseded")] | sort_by(.revision) | last | .revision // empty')
      if [ -z "$revision" ]; then
        helm --kube-context "$CONTEXT" --namespace "$NAMESPACE" uninstall vermouth >/dev/null
        printf '%s\n' 'Removed a failed first application release before retry.'
      else
        helm --kube-context "$CONTEXT" --namespace "$NAMESPACE" rollback vermouth "$revision" --wait --timeout 5m >/dev/null
        printf 'Restored application release revision %s before retry.\n' "$revision"
      fi
      ;;
  esac
}

verify_entry_path() {
  PLATFORM_FAILURE_CODE=6
  kubectl --context "$CONTEXT" --namespace kube-system rollout status deployment/traefik --timeout=120s >/dev/null
  kube get ingress vermouth >/dev/null
  deadline=$(($(date +%s) + 120))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    ordinary=$(curl --noproxy '*' -fsS --max-time 10 http://vermouth.localhost:8080/ready 2>/dev/null || true)
    forced=$(curl --noproxy '*' --resolve vermouth.localhost:8080:127.0.0.1 -fsS --max-time 10 \
      http://vermouth.localhost:8080/ready 2>/dev/null || true)
    if printf '%s\n%s\n' "$ordinary" "$forced" | jq -se '
      length == 2 and all(.[];
        .status == "ready" and
        .service == "gateway" and
        .checks == {identity:"ok",teaching:"ok",billing:"ok",notifications:"ok"}
      )
    ' >/dev/null; then
      return
    fi
    sleep 2
  done
  fail "the Ingress did not return the exact gateway readiness schema through both local routes within 2 minutes. Run task platform:status."
}

platform_dev() {
  run_id=$PLATFORM_RUN_ID
  bootstrap
  build_native all
  recover_pending_application_release
  prepare_secrets
  write_runtime_values "$run_id"
  foundation_release
  run_garage_init "$run_id"
  run_migrations "$run_id" all
  application_release
  verify_entry_path
  prune_runtime_secrets
  printf '%s\n' 'http://vermouth.localhost:8080'
}

redeploy() {
  redeploy_target=${1:-}
  [ -n "$redeploy_target" ] || fail "name gateway, identity, teaching, billing, notifications, or web"
  run_id=$PLATFORM_RUN_ID
  require_context
  build_native "$redeploy_target"
  recover_pending_application_release
  prepare_secrets
  write_runtime_values "$run_id"
  case "$redeploy_target" in
    identity|teaching|billing|notifications)
      run_migrations "$run_id" "$redeploy_target"
      ;;
  esac
  application_release
  kube rollout status "deployment/$redeploy_target" --timeout=5m
  prune_runtime_secrets
}

development_token() {
  PLATFORM_FAILURE_CODE=5
  require_context
  [ -f "$RUNTIME_VALUES" ] || fail "runtime values are missing. Run task dev first."
  run_id=$PLATFORM_RUN_ID
  job=devtoken-$run_id
  if [ "$#" -eq 0 ]; then
    args_json='[]'
  else
    args_json=$(printf '%s\n' "$@" | jq -R . | jq -s .)
  fi
  render_jobs --set jobs.runID="$run_id" --set jobs.devtoken.enabled=true \
    --set-json jobs.devtoken.args="$args_json" | kube apply -f - >/dev/null
  if kube wait --for=condition=complete "job/$job" --timeout=125s >/dev/null 2>&1; then
    kube logs "job/$job"
    kube delete "job/$job" --wait=false >/dev/null
    return
  fi
  kube logs "job/$job" --all-containers=true >&2 || true
  fail "development token Job failed. Inspect it with task platform:logs -- devtoken."
}

platform_status() {
  require_context
  printf '%s\n' 'Platform identity'
  kube get configmap vermouth-platform-identity -o jsonpath='{.data}'
  printf '\n'
  printf '%s\n' 'Helm releases'
  helm --kube-context "$CONTEXT" --namespace "$NAMESPACE" list
  kubectl --context "$CONTEXT" --namespace kube-system get helmchart,helmchartconfig traefik
  printf '%s\n' 'Workloads'
  kube get deployment,statefulset,pod
  kubectl --context "$CONTEXT" --namespace kube-system get deployment traefik
  printf '%s\n' 'Ingress'
  kube get ingress vermouth
  printf '%s\n' 'Jobs and storage'
  kube get job,pvc
  kube_cluster get storageclass vermouth-local
  kube_cluster get persistentvolume -l app.kubernetes.io/name=vermouth
  printf '%s\n' 'Registry'
  docker_platform ps --filter "name=^/$(yaml_value "$VERSIONS" registry.containerName)$" --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'
  printf '%s\n' 'Readiness'
  curl -sS --max-time 10 http://vermouth.localhost:8080/ready || true
  printf '\n'
  printf '%s\n' 'Log targets'
  printf '%s\n' 'web gateway identity teaching billing notifications postgres-identity postgres-teaching postgres-billing postgres-notifications redpanda garage traefik registry garage-init devtoken migrate-<service>-<run-id> all'
}

platform_logs() {
  require_context
  target=${1:-}
  shift || true
  [ -n "$target" ] || fail "name a log target or all"
  if [ "$target" = all ]; then
    for item in web gateway identity teaching billing notifications postgres-identity postgres-teaching postgres-billing postgres-notifications redpanda garage traefik registry; do
      printf '==> %s\n' "$item"
      platform_logs "$item" --since 10m || true
    done
    return
  fi
  case "$target" in
    web|gateway|identity|teaching|billing|notifications)
      kube logs "deployment/$target" "$@"
      ;;
    postgres-*|redpanda|garage)
      kube logs "statefulset/$target" "$@"
      ;;
    traefik)
      kubectl --context "$CONTEXT" --namespace kube-system logs deployment/traefik "$@"
      ;;
    registry)
      docker_platform logs "$@" "$(yaml_value "$VERSIONS" registry.containerName)"
      ;;
    garage-init)
      job=$(kube get jobs -l app.kubernetes.io/component=garage-init --sort-by=.metadata.creationTimestamp -o name | tail -1)
      job=${job#job.batch/}
      [ -n "$job" ] || fail "no Garage initialization Job remains"
      kube logs "job/$job" "$@"
      ;;
    devtoken)
      job=$(kube get jobs -l app.kubernetes.io/component=devtoken --sort-by=.metadata.creationTimestamp -o name | tail -1)
      job=${job#job.batch/}
      [ -n "$job" ] || fail "no development token Job remains"
      kube logs "job/$job" "$@"
      ;;
    migrate-*) kube logs "job/$target" "$@" ;;
    *) fail "unknown log target $target" ;;
  esac
}

platform_stop() {
  require_context
  release_platform_lease
  k3d cluster stop "$CLUSTER"
  if registry_exists; then docker_platform stop "$(yaml_value "$VERSIONS" registry.containerName)" >/dev/null; fi
  printf '%s\n' 'The cluster and registry are stopped. Their data remains.'
}

delete_platform() {
  verify_destructive_target
  if cluster_exists; then k3d cluster delete "$CLUSTER"; fi
  if registry_exists; then k3d registry delete "$REGISTRY"; fi
  data_volume=$(yaml_value "$VERSIONS" storage.dataVolume)
  registry_volume=$(yaml_value "$VERSIONS" registry.dataVolume)
  if docker_platform volume inspect "$data_volume" >/dev/null 2>&1; then
    docker_platform volume rm "$data_volume" >/dev/null
  fi
	if docker_platform volume inspect "$registry_volume" >/dev/null 2>&1; then
		docker_platform volume rm "$registry_volume" >/dev/null
	fi
	prune_vermouth_host_images
	find "$PLATFORM_TMP" -mindepth 1 -delete
}

verify_destructive_target() {
  validate_platform_inputs
  data_volume=$(yaml_value "$VERSIONS" storage.dataVolume)
  registry_volume=$(yaml_value "$VERSIONS" registry.dataVolume)
  if ! cluster_exists; then
    if docker_platform volume inspect "$data_volume" >/dev/null 2>&1; then
      verify_volume_identity "$data_volume" "$(yaml_value "$VERSIONS" storage.roles.data)"
    fi
    if docker_platform volume inspect "$registry_volume" >/dev/null 2>&1; then
      verify_volume_identity "$registry_volume" "$(yaml_value "$VERSIONS" storage.roles.registry)"
    fi
    return
  fi
  cluster_metadata=$(k3d cluster list -o json)
  registry_metadata=$(k3d registry list -o json)
  data_attached=$(printf '%s' "$cluster_metadata" | jq -r --arg cluster "$CLUSTER" --arg node "$(yaml_value "$VERSIONS" cluster.nodeName)" --arg volume "$data_volume:$(yaml_value "$VERSIONS" storage.rootPath)" '
    [.[] | select(.name == $cluster) | .nodes[] | select(.name == $node) | .volumes[]] |
    index($volume) != null
  ')
  registry_attached=$(printf '%s' "$registry_metadata" | jq -r --arg registry "$REGISTRY" --arg volume "$registry_volume:/var/lib/registry" '
    [.[] | select(.name == $registry) | .volumes[]] | index($volume) != null
  ')
  [ "$data_attached" = true ] && [ "$registry_attached" = true ] ||
    fail "k3d metadata does not attach the exact Vermouth data and registry volumes"
  verify_volume_identity "$data_volume" "$(yaml_value "$VERSIONS" storage.roles.data)"
  verify_volume_identity "$registry_volume" "$(yaml_value "$VERSIONS" storage.roles.registry)"
  node=$(yaml_value "$VERSIONS" cluster.nodeName)
  live_cluster=$(docker_platform inspect "$node" --format '{{ index .Config.Labels "k3d.cluster" }}' 2>/dev/null || true)
  [ "$live_cluster" = "$CLUSTER" ] || fail "Docker node $node does not carry the expected k3d cluster label"
  if cluster_running; then
    require_context
    identity=$(kube get configmap vermouth-platform-identity -o jsonpath='{.data.clusterName}' 2>/dev/null || true)
    [ "$identity" = "$CLUSTER" ] || fail "running cluster $CLUSTER has no matching platform identity"
  fi
}

platform_clean() {
  verify_destructive_target
  confirm_vermouth Clean
  delete_platform
  printf '%s\n' 'The cluster, registry, and local platform data were removed.'
}

platform_recreate() {
  verify_destructive_target
  confirm_vermouth Recreate
  delete_platform
  bootstrap
  printf '%s\n' 'The empty platform was bootstrapped. Run task dev to deploy the application.'
}

clean_job() {
  require_context
  job=${1:-}
  [ -n "$job" ] || fail "name one failed Job"
  kube get job "$job" >/dev/null 2>&1 || fail "Job $job does not exist"
  active=$(kube get job "$job" -o jsonpath='{.status.active}')
  [ -z "$active" ] || [ "$active" = 0 ] || fail "Job $job is still running"
  failed=$(kube get job "$job" -o jsonpath='{.status.failed}')
  [ -n "$failed" ] && [ "$failed" -gt 0 ] || fail "Job $job did not fail, so it is not a recovery target"
  kube delete job "$job"
}

measure_platform() {
  require_context
  need jq
  need colima
  need docker
  report_dir=$PLATFORM_TMP/reports
  mkdir -p "$report_dir"
  run_id=$(go run "$ROOT/pkg/vermouth/cmd/runid")
  samples_file=$report_dir/resource-$run_id.samples.jsonl
  report=$report_dir/resource-$run_id.json
  started=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
  sample=1
  sleep 60

  while [ "$sample" -le 60 ]; do
    timestamp=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
    colima_used_kib=$(colima ssh -- sh -c "awk '/MemTotal:/ { total = \$2 } /MemAvailable:/ { available = \$2 } END { print total - available }' /proc/meminfo")
    docker_json=$(docker_platform stats --no-stream --format '{{json .}}' \
      "$(yaml_value "$VERSIONS" cluster.nodeName)" \
      k3d-vermouth-serverlb \
      "$(yaml_value "$VERSIONS" registry.containerName)" | jq -s .)
    [ "$(printf '%s' "$docker_json" | jq 'length')" -eq 3 ] || fail "resource measurement requires exactly the server, load balancer, and registry"
    kubernetes_json=$(kubectl --context "$CONTEXT" get --raw "/apis/metrics.k8s.io/v1beta1/namespaces/$NAMESPACE/pods")
    jq -cn --arg timestamp "$timestamp" --argjson colimaUsedKiB "$colima_used_kib" \
      --argjson docker "$docker_json" --argjson kubernetes "$kubernetes_json" \
      '{timestamp:$timestamp,colima_used_kib:$colimaUsedKiB,docker:$docker,kubernetes:$kubernetes}' >>"$samples_file"
    if [ "$sample" -lt 60 ]; then sleep 5; fi
    sample=$((sample + 1))
  done

  finished=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
  jq -s --arg started "$started" --arg finished "$finished" \
    --arg colima "$(colima version | awk 'NR == 1 {print $3}')" \
    --arg docker "$(docker_platform version --format '{{.Client.Version}}')" \
    --arg kubernetes "$(kubectl --context "$CONTEXT" version -o json | jq -r '.serverVersion.gitVersion')" '
      def cpu_total: [.docker[]?.CPUPerc | rtrimstr("%") | tonumber] | add // 0;
      (map(.colima_used_kib) | max) as $maxMemory |
      (map(cpu_total) | max) as $maxCPU |
      {
        schema_version: 1,
        started_at: $started,
        finished_at: $finished,
        interval_seconds: 5,
        duration_seconds: 300,
        tool_versions: {colima:$colima,docker:$docker,kubernetes:$kubernetes},
        limits: {colima_used_kib:7340032,docker_cpu_percent:350},
        maxima: {colima_used_kib:$maxMemory,docker_cpu_percent:$maxCPU},
        averages: {
          colima_used_kib:(map(.colima_used_kib) | add / length),
          docker_cpu_percent:(map(cpu_total) | add / length)
        },
        pass: ($maxMemory <= 7340032 and $maxCPU < 350),
        samples: .
      }
    ' "$samples_file" >"$report"
  rm -f "$samples_file"

  jq '{report:input_filename,pass,limits,maxima,averages}' "$report"
  jq -e '.pass' "$report" >/dev/null || fail "resource measurement exceeded the committed ceiling. Review $report"
  printf 'Resource measurement passed. Report: %s\n' "$report"
}

validate_platform() {
  need rg
  validate_platform_inputs
  helm lint "$CHART" --values "$LOCAL_VALUES" --set foundation.enabled=true
  helm lint "$CHART" --values "$PRODUCTION_VALUES"
  render_traefik_config "$PLATFORM_TMP/traefik-config.yaml"
  render_production_traefik_config "$PLATFORM_TMP/traefik-production-config.yaml" operator@example.com
  duplicate_traefik_keys=$(awk '/^[a-zA-Z][a-zA-Z0-9]*:/ {print $1}' "$TRAEFIK_COMMON" "$TRAEFIK_LOCAL" | sort | uniq -d)
  [ -z "$duplicate_traefik_keys" ] || fail "Traefik common and local values both own: $duplicate_traefik_keys"
  rg -q '^kind: HelmChartConfig$' "$PLATFORM_TMP/traefik-config.yaml" || fail "Traefik HelmChartConfig did not render"
  rg -q 'entryPoints.web.allowACMEByPass=true' "$PLATFORM_TMP/traefik-production-config.yaml" || fail "production Traefik does not preserve ACME HTTP challenges"
  rg -q 'storage: /data/acme.json' "$PLATFORM_TMP/traefik-production-config.yaml" || fail "production Traefik does not persist production ACME state"
  validation=$PLATFORM_TMP/validation-values.yaml
  digest=sha256:0000000000000000000000000000000000000000000000000000000000000000
  {
    printf 'images:\n'
    for image in gateway identity teaching billing notifications web garageInit devtoken; do
      printf '  %s: example.invalid/vermouth/%s@%s\n' "$image" "$image" "$digest"
    done
    printf '  migrations:\n'
    for service in identity teaching billing notifications; do
      printf '    %s: example.invalid/vermouth/%s-migration@%s\n' "$service" "$service" "$digest"
    done
    printf 'secrets:\n'
    for service in gateway identity teaching billing notifications; do
      printf '  %s: %s-runtime-validation\n' "$service" "$service"
    done
  } >"$validation"
  helm template vermouth-foundation "$CHART" --namespace "$NAMESPACE" \
    --values "$LOCAL_VALUES" --values "$validation" --set foundation.enabled=true >"$PLATFORM_TMP/foundation.yaml"
  helm template vermouth-foundation "$CHART" --namespace "$NAMESPACE" \
    --values "$LOCAL_VALUES" --values "$validation" --set foundation.enabled=true \
    --set config.googleAuthEnabled=true >"$PLATFORM_TMP/foundation-google.yaml"
  helm template vermouth-foundation "$CHART" --namespace "$NAMESPACE" \
    --values "$PRODUCTION_VALUES" --values "$validation" --set foundation.enabled=true \
    --set-string storage.markerSHA256=0000000000000000000000000000000000000000000000000000000000000000 \
    >"$PLATFORM_TMP/foundation-production.yaml"
  helm template vermouth-foundation "$CHART" --namespace "$NAMESPACE" \
    --values "$PRODUCTION_VALUES" --values "$validation" --set foundation.bootstrapStorage=true \
    --set-string storage.markerSHA256=0000000000000000000000000000000000000000000000000000000000000000 \
    >"$PLATFORM_TMP/foundation-production-bootstrap.yaml"
  helm template vermouth "$CHART" --namespace "$NAMESPACE" \
    --values "$LOCAL_VALUES" --values "$validation" --set application.enabled=true >"$PLATFORM_TMP/application.yaml"
  helm template vermouth "$CHART" --namespace "$NAMESPACE" \
    --values "$PRODUCTION_VALUES" --values "$validation" --set application.enabled=true \
    --set-string ingress.hostname=vermouth-test.southeastasia.cloudapp.azure.com \
    --set-string config.identityAppURL=https://vermouth-test.southeastasia.cloudapp.azure.com \
    --set-string config.identityGoogleRedirectURL=https://vermouth-test.southeastasia.cloudapp.azure.com/api/auth/google/callback \
    >"$PLATFORM_TMP/application-production.yaml"
  helm template vermouth-jobs "$CHART" --namespace "$NAMESPACE" \
    --values "$LOCAL_VALUES" --values "$validation" --set jobs.runID=000000000000 \
    --set jobs.migrations.enabled=true --set jobs.garageInit.enabled=true \
    --set jobs.devtoken.enabled=true >"$PLATFORM_TMP/jobs.yaml"
  if rg -n 'image: .*:(latest|dev)([[:space:]]|$)' "$PLATFORM_TMP"/*.yaml >/dev/null; then
    fail "a rendered workload uses a mutable image tag"
  fi
  [ "$(rg -c '^kind: StatefulSet$' "$PLATFORM_TMP/foundation.yaml")" -eq 6 ] || fail "foundation must render six StatefulSets"
  [ "$(rg -c '^kind: Deployment$' "$PLATFORM_TMP/application.yaml")" -eq 6 ] || fail "application must render six Deployments"
  [ "$(rg -c '^kind: Job$' "$PLATFORM_TMP/jobs.yaml")" -eq 6 ] || fail "Jobs rendering must contain four migrations, Garage initialization, and devtoken"
  [ "$(rg -c '^kind: PersistentVolume$' "$PLATFORM_TMP/foundation.yaml")" -eq 8 ] || fail "foundation must render eight static PersistentVolumes"
  [ "$(rg -c '^kind: PersistentVolumeClaim$' "$PLATFORM_TMP/foundation.yaml")" -eq 8 ] || fail "foundation must render eight local data claims"
  [ "$(rg -c '^kind: NetworkPolicy$' "$PLATFORM_TMP/foundation-production-bootstrap.yaml")" -gt 0 ] ||
    fail "production bootstrap must establish the deny first network boundary"
  [ "$(rg -c 'name: verify-storage-root' "$PLATFORM_TMP/foundation-production.yaml")" -eq 6 ] || fail "every production stateful workload must verify the locked storage marker"
  rg -U -q 'kind: PersistentVolumeClaim\nmetadata:\n  name: traefik-acme\n  namespace: kube-system' \
    "$PLATFORM_TMP/foundation-production.yaml" || fail "the Traefik ACME claim must live in kube-system"
  [ "$(rg -c '^kind: Ingress$' "$PLATFORM_TMP/application.yaml")" -eq 1 ] || fail "application must render one Ingress"
  [ "$(rg -c '^kind: Ingress$' "$PLATFORM_TMP/application-production.yaml")" -eq 1 ] || fail "production application must render one Ingress"
  rg -q 'host: .*vermouth-test.southeastasia.cloudapp.azure.com' "$PLATFORM_TMP/application-production.yaml" || fail "production Ingress hostname did not render"
  rg -q 'traefik.ingress.kubernetes.io/router.tls.certresolver: .*letsencrypt-production' "$PLATFORM_TMP/application-production.yaml" || fail "production Ingress does not select the production certificate resolver"
  rg -q '"environment":"production"' "$PLATFORM_TMP/application-production.yaml" || fail "production web runtime configuration did not render"
  if rg -n 'GatewayClass|HTTPRoute|envoyproxy|envoy-gateway' "$PLATFORM_TMP"/*.yaml >/dev/null; then
    fail "rendered platform still contains the superseded Envoy Gateway path"
  fi
  platform_config validate-rendered "$ROOT/deploy/helm/vermouth/files/traffic-matrix.yaml" "$ROOT/deploy/helm/vermouth/files/workload-matrix.yaml" "$PLATFORM_TMP/foundation.yaml" "$PLATFORM_TMP/application.yaml" "$PLATFORM_TMP/jobs.yaml"
  platform_config validate-rendered "$ROOT/deploy/helm/vermouth/files/traffic-matrix.yaml" "$ROOT/deploy/helm/vermouth/files/workload-matrix.yaml" "$PLATFORM_TMP/foundation-google.yaml" "$PLATFORM_TMP/application.yaml" "$PLATFORM_TMP/jobs.yaml" google-enabled
  printf '%s\n' 'Chart lint, local and production releases, Traefik values, Jobs, immutable images, and workload inventories are valid.'
}
