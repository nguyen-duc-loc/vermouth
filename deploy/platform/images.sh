#!/bin/sh
set -eu

image_info() {
  case "$1" in
    gateway) printf '%s|%s|%s\n' gateway/Dockerfile runtime "gateway pkg/vermouth" ;;
    identity) printf '%s|%s|%s\n' services/identity/Dockerfile runtime "services/identity pkg/vermouth" ;;
    teaching) printf '%s|%s|%s\n' services/teaching/Dockerfile runtime "services/teaching pkg/vermouth" ;;
    billing) printf '%s|%s|%s\n' services/billing/Dockerfile runtime "services/billing pkg/vermouth" ;;
    notifications) printf '%s|%s|%s\n' services/notifications/Dockerfile runtime "services/notifications pkg/vermouth" ;;
    web) printf '%s|%s|%s\n' deploy/images/web/Dockerfile runtime "deploy/images/web web package.json pnpm-lock.yaml pnpm-workspace.yaml" ;;
    identity-migration) printf '%s|%s|%s\n' services/identity/Dockerfile migration "services/identity pkg/vermouth" ;;
    teaching-migration) printf '%s|%s|%s\n' services/teaching/Dockerfile migration "services/teaching pkg/vermouth" ;;
    billing-migration) printf '%s|%s|%s\n' services/billing/Dockerfile migration "services/billing pkg/vermouth" ;;
    notifications-migration) printf '%s|%s|%s\n' services/notifications/Dockerfile migration "services/notifications pkg/vermouth" ;;
    garage-init) printf '%s|%s|%s\n' deploy/images/garage-init/Dockerfile runtime "deploy/images/garage-init" ;;
    devtoken) printf '%s|%s|%s\n' services/identity/Dockerfile devtoken "services/identity pkg/vermouth" ;;
    *) fail "unknown image workload $1" ;;
  esac
}

input_hash() {
  paths=$1
  {
    for path in $paths; do
      if [ -d "$ROOT/$path" ]; then
        find "$ROOT/$path" -type f \
          ! -path '*/node_modules/*' ! -path '*/dist/*' ! -path '*/.vite/*' \
          ! -path '*/.tmp/*' ! -name '*.tsbuildinfo'
      elif [ -f "$ROOT/$path" ]; then
        printf '%s\n' "$ROOT/$path"
      fi
    done
  } | LC_ALL=C sort | while IFS= read -r file; do
    relative=${file#"$ROOT/"}
    printf '%s  %s\n' "$(sha256_file "$file")" "$relative"
  done | sha256_text
}

build_plan() {
  requested=${1:-all}
  case "$requested" in
    all)
      printf '%s\n' gateway identity teaching billing notifications web \
        identity-migration teaching-migration billing-migration notifications-migration garage-init devtoken
      ;;
    gateway|web) printf '%s\n' "$requested" ;;
    identity|teaching|billing|notifications)
      printf '%s\n' "$requested" "$requested-migration"
      ;;
    *) fail "redeploy accepts gateway, identity, teaching, billing, notifications, or web" ;;
  esac
}

push_with_retry() {
  reference=$1
  attempt=1
  while ! docker push "$reference"; do
    [ "$attempt" -lt 3 ] || return 1
    if [ "$attempt" -eq 1 ]; then delay=2; else delay=5; fi
    printf 'Registry push failed. Retrying in %s seconds.\n' "$delay"
    sleep "$delay"
    attempt=$((attempt + 1))
  done
}

build_native() {
  require_context
  need docker
  need jq

  requested=${1:-all}
  plan=$(build_plan "$requested")
  stage_dir=$PLATFORM_TMP/build-stage
  mkdir -p "$stage_dir"
  rm -f "$stage_dir/plan" "$stage_dir/to-push" "$stage_dir/images.json" "$stage_dir/images.json.next" "$stage_dir/images.json.merged"
  records=$stage_dir/images.json
  printf '{"schema_version":1,"images":{}}\n' >"$records"

  for name in $plan; do
    info=$(image_info "$name")
    dockerfile=$(printf '%s' "$info" | cut -d '|' -f 1)
    target=$(printf '%s' "$info" | cut -d '|' -f 2)
    paths=$(printf '%s' "$info" | cut -d '|' -f 3)
    hash=$(input_hash "$paths")
    tag=dev-$(printf '%s' "$hash" | cut -c1-12)
    host_ref=$HOST_REGISTRY/vermouth/$name:$tag
    local_ref=vermouth-build/$name:$tag

    if docker buildx imagetools inspect "$host_ref" >/dev/null 2>&1; then
      printf 'Using immutable %s\n' "$host_ref"
    else
      printf 'Building %s for linux/arm64\n' "$name"
      docker buildx build --platform linux/arm64 --load \
        --file "$ROOT/$dockerfile" --target "$target" --tag "$local_ref" "$ROOT"
      printf '%s|%s|%s|%s|%s\n' "$name" "$local_ref" "$host_ref" "$hash" "$paths" >>"$stage_dir/to-push"
    fi
    printf '%s|%s|%s|%s\n' "$name" "$host_ref" "$hash" "$paths" >>"$stage_dir/plan"
  done

  if [ -f "$stage_dir/to-push" ]; then
    while IFS='|' read -r name local_ref host_ref hash paths; do
      docker tag "$local_ref" "$host_ref"
      push_with_retry "$host_ref" || fail "could not push $name after three attempts"
    done <"$stage_dir/to-push"
  fi

  source_revision=$(git -C "$ROOT" rev-parse HEAD)
  built_at=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
  while IFS='|' read -r name host_ref hash paths; do
    digest=$(docker buildx imagetools inspect --format '{{json .Manifest}}' "$host_ref" | jq -r '.digest')
    [ -n "$digest" ] && [ "$digest" != null ] || fail "registry did not report a digest for $name"
    cluster_repository=$CLUSTER_REGISTRY/vermouth/$name
    cluster_ref=$cluster_repository@$digest
    if git -C "$ROOT" status --porcelain -- $paths | grep . >/dev/null; then dirty=true; else dirty=false; fi
    next=$records.next
    jq --arg name "$name" --arg repository "$cluster_repository" \
      --arg tag "${host_ref##*:}" --arg digest "$digest" --arg platform linux/arm64 \
      --arg revision "$source_revision" --argjson dirty "$dirty" --arg builtAt "$built_at" \
      --arg clusterRef "$cluster_ref" \
      '.images[$name] = {
        repository: $repository,
        tag: $tag,
        digest: $digest,
        platform: $platform,
        source_revision: $revision,
        dirty: $dirty,
        built_at: $builtAt,
        cluster_ref: $clusterRef
      }' "$records" >"$next"
    mv "$next" "$records"
  done <"$stage_dir/plan"

  if [ "$requested" != all ] && [ -f "$IMAGES_JSON" ]; then
    merged=$records.merged
    jq -s '.[0] * {images: (.[0].images * .[1].images)}' "$IMAGES_JSON" "$records" >"$merged"
    mv "$merged" "$records"
  fi
  mv "$records" "$IMAGES_JSON"
  rm -f "$stage_dir/plan" "$stage_dir/to-push"
  printf 'Recorded native image digests in %s\n' "$IMAGES_JSON"
}

build_multi() {
  require_context
  plan="gateway identity teaching billing notifications web identity-migration teaching-migration billing-migration notifications-migration garage-init"
  for name in $plan; do
    info=$(image_info "$name")
    dockerfile=$(printf '%s' "$info" | cut -d '|' -f 1)
    target=$(printf '%s' "$info" | cut -d '|' -f 2)
    paths=$(printf '%s' "$info" | cut -d '|' -f 3)
    tag=multi-$(input_hash "$paths" | cut -c1-12)
    reference=$HOST_REGISTRY/vermouth/$name:$tag
    docker buildx build --platform linux/arm64,linux/amd64 --push \
      --file "$ROOT/$dockerfile" --target "$target" --tag "$reference" "$ROOT"
    manifest=$(docker buildx imagetools inspect --format '{{json .Manifest}}' "$reference")
    printf '%s' "$manifest" | jq -e '[.manifests[].platform | select(.os == "linux") | .architecture] | index("arm64") != null and index("amd64") != null' >/dev/null \
      || fail "$reference does not contain both linux/arm64 and linux/amd64"
    printf 'Verified %s for linux/arm64 and linux/amd64\n' "$reference"
  done
}
