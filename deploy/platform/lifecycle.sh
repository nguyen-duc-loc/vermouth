#!/bin/sh
set -eu

start_watchdog() {
  seconds=$1
  parent=$$
  (
    sleep "$seconds"
    printf 'platform: outer deadline of %s seconds expired\n' "$seconds" >&2
    kill -TERM "$parent" 2>/dev/null || true
  ) &
  WATCHDOG_PID=$!
  trap 'kill "$WATCHDOG_PID" 2>/dev/null || true' EXIT INT TERM
}

write_platform_identity() {
  config_hash=$(sha256_file "$ROOT/deploy/k3d/vermouth.yaml")
  lock_hash=$(sha256_file "$IMAGE_LOCK")
  kube create configmap vermouth-platform-identity \
    --from-literal=schemaVersion=1 \
    --from-literal=clusterName=vermouth \
    --from-literal=k3sVersion=v1.35.5-k3s1 \
    --from-literal=servers=1 \
    --from-literal=agents=0 \
    --from-literal=traefikEnabled=false \
    --from-literal=hostPortMap=80:80 \
    --from-literal=registryHostEndpoint="$HOST_REGISTRY" \
    --from-literal=registryClusterEndpoint="$CLUSTER_REGISTRY" \
    --from-literal=k3dConfigSHA256="$config_hash" \
    --from-literal=envoyChartVersion=v1.9.0 \
    --from-literal=imageLockSHA256="$lock_hash" \
    --dry-run=client -o yaml | kube apply -f - >/dev/null
}

check_port_free() {
  port=$1
  if command -v lsof >/dev/null 2>&1 && lsof -nP -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | grep . >/dev/null; then
    fail "local TCP port $port is already in use"
  fi
}

bootstrap() {
  if ! colima status >/dev/null 2>&1; then
    printf '%s\n' 'Starting the existing Colima profile.'
    colima start
  fi

  ALLOW_IMAGE_LOCK_DRIFT=true doctor
  ensure_local_configuration
  task -d "$ROOT" web:install

  if ! cluster_exists; then
    check_port_free 80
    check_port_free 5111
    k3d cluster create --config "$ROOT/deploy/k3d/vermouth.yaml" --timeout 8m
    printf '%s\n' "Selecting the new $CONTEXT context explicitly."
    kubectl config use-context "$CONTEXT" >/dev/null
  else
    k3d cluster start "$CLUSTER"
    if registry_exists; then k3d registry start "$REGISTRY"; fi
  fi
  require_context

  node_image=$(docker inspect k3d-vermouth-server-0 --format '{{.Config.Image}}')
  case "$node_image" in
    *v1.35.5-k3s1*) ;;
    *) fail "cluster node image is $node_image, expected v1.35.5-k3s1" ;;
  esac

  kube_cluster create namespace "$NAMESPACE" --dry-run=client -o yaml | kube_cluster apply -f - >/dev/null

  chart_digest=$(docker buildx imagetools inspect --format '{{json .Manifest}}' \
    docker.io/envoyproxy/gateway-helm:v1.9.0 | jq -r '.digest')
  expected_chart=$(yaml_value "$VERSIONS" envoyGateway.digest)
  [ "$chart_digest" = "$expected_chart" ] || fail "Envoy Gateway chart digest changed from $expected_chart to $chart_digest"

  helm --kube-context "$CONTEXT" upgrade --install envoy-gateway \
    oci://docker.io/envoyproxy/gateway-helm --version v1.9.0 \
    --namespace envoy-gateway-system --create-namespace \
    --values "$ROOT/deploy/platform/envoy-values.yaml" \
    --atomic --wait --timeout 5m
  kubectl --context "$CONTEXT" --namespace envoy-gateway-system wait \
    deployment/envoy-gateway --for=condition=Available --timeout=5m

  write_platform_identity
  printf '%s\n' 'Bootstrap matches the committed cluster, registry, and Envoy Gateway inputs.'
}

foundation_release() {
  helm --kube-context "$CONTEXT" upgrade --install vermouth-foundation "$CHART" \
    --namespace "$NAMESPACE" --values "$LOCAL_VALUES" --values "$RUNTIME_VALUES" \
    --set foundation.enabled=true --set application.enabled=false \
    --atomic --wait --timeout 8m
}

render_jobs() {
  helm template vermouth-jobs "$CHART" --namespace "$NAMESPACE" \
    --values "$LOCAL_VALUES" --values "$RUNTIME_VALUES" "$@"
}

run_garage_init() {
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
  helm --kube-context "$CONTEXT" upgrade --install vermouth "$CHART" \
    --namespace "$NAMESPACE" --values "$LOCAL_VALUES" --values "$RUNTIME_VALUES" \
    --set foundation.enabled=false --set application.enabled=true \
    --atomic --cleanup-on-fail --wait --timeout 5m
}

verify_entry_path() {
  kube_cluster wait gatewayclass/vermouth --for=condition=Accepted --timeout=120s >/dev/null
  kube wait gateway/vermouth --for=condition=Programmed --timeout=120s >/dev/null
  curl -fsS --max-time 10 http://vermouth.localhost/ready >/dev/null || fail "the Gateway is programmed but /ready failed. Run task platform:status."
}

platform_dev() {
  if helm --kube-context "$CONTEXT" --namespace "$NAMESPACE" status vermouth >/dev/null 2>&1; then
    start_watchdog 480
  else
    start_watchdog 1200
  fi
  bootstrap
  build_native all
  prepare_secrets
  write_runtime_values
  foundation_release
  run_id=$(go run "$ROOT/pkg/vermouth/cmd/runid")
  run_garage_init "$run_id"
  run_migrations "$run_id" all
  application_release
  verify_entry_path
  prune_runtime_secrets
  printf '%s\n' 'http://vermouth.localhost'
}

redeploy() {
  workload=${1:-}
  [ -n "$workload" ] || fail "name gateway, identity, teaching, billing, notifications, or web"
  start_watchdog 480
  require_context
  build_native "$workload"
  prepare_secrets
  write_runtime_values
  case "$workload" in
    identity|teaching|billing|notifications)
      run_id=$(go run "$ROOT/pkg/vermouth/cmd/runid")
      run_migrations "$run_id" "$workload"
      ;;
  esac
  application_release
  kube rollout status "deployment/$workload" --timeout=5m
  prune_runtime_secrets
}

development_token() {
  require_context
  [ -f "$RUNTIME_VALUES" ] || fail "runtime values are missing. Run task dev first."
  run_id=$(go run "$ROOT/pkg/vermouth/cmd/runid")
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
  printf '%s\n' 'Helm releases'
  helm --kube-context "$CONTEXT" --namespace "$NAMESPACE" list
  printf '%s\n' 'Workloads'
  kube get deployment,statefulset,pod
  printf '%s\n' 'Gateway'
  kube_cluster get gatewayclass vermouth
  kube get gateway,httproute
  printf '%s\n' 'Jobs and storage'
  kube get job,pvc
  printf '%s\n' 'Registry'
  docker ps --filter "name=k3d-$REGISTRY" --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'
  printf '%s\n' 'Readiness'
  curl -sS --max-time 10 http://vermouth.localhost/ready || true
  printf '\n'
}

platform_logs() {
  require_context
  target=${1:-}
  shift || true
  [ -n "$target" ] || fail "name a log target or all"
  if [ "$target" = all ]; then
    for item in web gateway identity teaching billing notifications postgres-identity postgres-teaching postgres-billing postgres-notifications redpanda garage envoy-controller envoy-proxy registry; do
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
    envoy-controller)
      kubectl --context "$CONTEXT" --namespace envoy-gateway-system logs deployment/envoy-gateway "$@"
      ;;
    envoy-proxy)
      kubectl --context "$CONTEXT" --namespace envoy-gateway-system logs \
        -l gateway.envoyproxy.io/owning-gateway-name=vermouth "$@"
      ;;
    registry)
      docker logs "$@" "k3d-$REGISTRY"
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
  k3d cluster stop "$CLUSTER"
  if registry_exists; then k3d registry stop "$REGISTRY"; fi
  printf '%s\n' 'The cluster and registry are stopped. Their data remains.'
}

delete_platform() {
  if cluster_exists; then k3d cluster delete "$CLUSTER"; fi
  if registry_exists; then k3d registry delete "$REGISTRY"; fi
  docker volume rm vermouth-registry-data >/dev/null 2>&1 || true
}

platform_clean() {
  require_context
  confirm_vermouth Clean
  delete_platform
  printf '%s\n' 'The cluster, registry, and local platform data were removed.'
}

platform_recreate() {
  require_context
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
  kube delete job "$job"
}

validate_platform() {
  need rg
  helm lint "$CHART" --values "$LOCAL_VALUES" --set foundation.enabled=true
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
  helm template vermouth "$CHART" --namespace "$NAMESPACE" \
    --values "$LOCAL_VALUES" --values "$validation" --set application.enabled=true >"$PLATFORM_TMP/application.yaml"
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
  printf '%s\n' 'Chart lint, both releases, Jobs, immutable images, and workload inventories are valid.'
}
