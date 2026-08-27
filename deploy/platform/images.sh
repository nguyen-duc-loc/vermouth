#!/bin/sh
set -eu

build_plan() {
  requested=${1:-all}
  case "$requested" in
    all)
      printf '%s\n' gateway identity teaching billing notifications web identity-migration teaching-migration billing-migration notifications-migration garage-init devtoken
      ;;
    gateway|web) printf '%s\n' "$requested" ;;
    identity|teaching|billing|notifications)
      printf '%s\n' "$requested" "$requested-migration"
      ;;
    *) fail "redeploy accepts gateway, identity, teaching, billing, notifications, or web" ;;
  esac
}

multi_build_plan() {
  printf '%s\n' gateway identity teaching billing notifications web identity-migration teaching-migration billing-migration notifications-migration garage-init
}

prune_vermouth_host_images() {
  docker_platform image prune --all --force --filter label=vermouth.dev/workload >/dev/null
  remaining=$(docker_platform image ls --quiet --filter label=vermouth.dev/workload | sort -u | wc -l | tr -d ' ')
  [ "$remaining" -eq 0 ] || fail "$remaining unused Vermouth host images remain after cleanup"
}

clean_build_cache() {
  PLATFORM_FAILURE_CODE=3
  need docker
  validate_platform_inputs
  builder=$(yaml_value "$VERSIONS" builder.name)
  printf 'BuildKit cache before cleanup for shared builder %s:\n' "$builder"
  docker_platform buildx du --builder "$builder"
  confirm_exact_vermouth "BuildKit cache cleanup affects every project using shared builder $builder. The next build will be cold."
  docker_platform buildx prune --builder "$builder" --all --force
  printf 'BuildKit cache after cleanup for shared builder %s:\n' "$builder"
  docker_platform buildx du --builder "$builder"
}

verify_external_locks() {
  validate_platform_inputs
  platform_config get "$IMAGE_LOCK" external | jq -c 'to_entries[]' | while IFS= read -r item; do
    name=$(printf '%s' "$item" | jq -r '.key')
    image=$(printf '%s' "$item" | jq -r '.value.image')
    digest=$(printf '%s' "$item" | jq -r '.value.digest')
    details=$(docker_platform buildx imagetools inspect "$image")
    live_digest=$(printf '%s' "$details" | awk '/^Digest:/ {print $2; exit}')
    [ "$live_digest" = "$digest" ] || fail "external image $name tag resolves to $live_digest, expected $digest"
    manifest=$(docker_platform buildx imagetools inspect --raw "$image")
    printf '%s' "$manifest" | jq -e '
      any(.manifests[]?; .platform.os == "linux" and .platform.architecture == "arm64") and
      any(.manifests[]?; .platform.os == "linux" and .platform.architecture == "amd64")
    ' >/dev/null || fail "external image $name does not expose both required platforms"
    printf 'Verified external lock %s at %s.\n' "$name" "$digest"
  done
}

smoke_web_architectures() {
  reference=$1
  for platform in linux/arm64 linux/amd64; do
    architecture=${platform#linux/}
    container=vermouth-web-smoke-$architecture-$(new_run_id)
    docker_platform run --detach --rm \
      --name "$container" \
      --platform "$platform" \
      --read-only \
      --tmpfs /var/cache/nginx:rw,nosuid,nodev,size=32m,uid=101,gid=101 \
      --tmpfs /var/run:rw,nosuid,nodev,size=4m,uid=101,gid=101 \
      --tmpfs /tmp:rw,nosuid,nodev,size=16m,uid=101,gid=101 \
      --publish 127.0.0.1::8080 \
      "$reference" >/dev/null
    address=$(docker_platform port "$container" 8080/tcp)
    deadline=$(( $(date +%s) + 30 ))
    passed=false
    while [ "$(date +%s)" -lt "$deadline" ]; do
      if curl -fsS --max-time 2 "http://$address/" >/dev/null 2>&1; then
        passed=true
        break
      fi
      sleep 1
    done
    docker_platform rm --force "$container" >/dev/null 2>&1 || true
    [ "$passed" = true ] || fail "web smoke failed for $platform"
    printf 'Verified web runtime under %s.\n' "$platform"
  done
}

push_with_retry() {
  reference=$1
  attempt=1
  while ! docker_platform push "$reference"; do
    [ "$attempt" -lt 3 ] || return 1
    if [ "$attempt" -eq 1 ]; then delay=2; else delay=5; fi
    printf 'Registry push failed. Retrying in %s seconds.\n' "$delay"
    sleep "$delay"
    attempt=$((attempt + 1))
  done
}

registry_manifest() {
  repository=$1
  tag=$2
  curl -fsS -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json' "http://$HOST_REGISTRY/v2/$repository/manifests/$tag"
}

registry_digest() {
  repository=$1
  tag=$2
  curl -fsSI -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json' "http://$HOST_REGISTRY/v2/$repository/manifests/$tag" |
    tr -d '\r' |
    awk 'tolower($1) == "docker-content-digest:" {print $2}'
}

registry_labels() {
  repository=$1
  tag=$2
  manifest=$(registry_manifest "$repository" "$tag")
  config_digest=$(printf '%s' "$manifest" | jq -r '.config.digest // empty')
  [ -n "$config_digest" ] || fail "$repository:$tag is not a single platform manifest"
  curl -fsS "http://$HOST_REGISTRY/v2/$repository/blobs/$config_digest" | jq -c '.config.Labels // {}'
}

verify_remote_image() {
  repository=$1
  tag=$2
  input_hash=$3
  workload=$4
  target=$5
  platform=$6
  base_provenance=$7

  labels=$(registry_labels "$repository" "$tag")
  printf '%s' "$labels" | jq -e --arg hash "$input_hash" --arg repository "$repository" --arg workload "$workload" --arg target "$target" --arg platform "$platform" --arg bases "$base_provenance" '
      .["vermouth.dev/input-hash"] == $hash and
      .["vermouth.dev/repository-path"] == $repository and
      .["vermouth.dev/workload"] == $workload and
      .["vermouth.dev/target"] == $target and
      .["vermouth.dev/platform"] == $platform and
      .["vermouth.dev/base-provenance"] == $bases
    ' >/dev/null || fail "$repository:$tag exists with different immutable provenance"
}

verify_local_image() {
  reference=$1
  input_hash=$2
  repository=$3
  workload=$4
  target=$5
  platform=$6
  base_provenance=$7

  labels=$(docker_platform image inspect "$reference" --format '{{json .Config.Labels}}')
  printf '%s' "$labels" | jq -e --arg hash "$input_hash" --arg repository "$repository" --arg workload "$workload" --arg target "$target" --arg platform "$platform" --arg bases "$base_provenance" '
      .["vermouth.dev/input-hash"] == $hash and
      .["vermouth.dev/repository-path"] == $repository and
      .["vermouth.dev/workload"] == $workload and
      .["vermouth.dev/target"] == $target and
      .["vermouth.dev/platform"] == $platform and
      .["vermouth.dev/base-provenance"] == $bases
    ' >/dev/null || fail "$reference has different staged provenance"
}

append_image_record() {
  records=$1
  plan_json=$2
  digest=$3
  dirty=$4
  source_revision=$5
  built_at=$6

  name=$(printf '%s' "$plan_json" | jq -r '.workload')
  repository=$(printf '%s' "$plan_json" | jq -r '.repository_path')
  tag=$(printf '%s' "$plan_json" | jq -r '.tag')
  input_hash=$(printf '%s' "$plan_json" | jq -r '.input_hash')
  host_ref=$HOST_REGISTRY/$repository:$tag
  cluster_ref=$CLUSTER_REGISTRY/$repository@$digest
  next=$records.next
  jq --arg name "$name" --arg workload "$name" --arg repository "$repository" --arg hostRef "$host_ref" --arg clusterRef "$cluster_ref" --arg tag "$tag" --arg inputHash "$input_hash" --arg digest "$digest" --arg revision "$source_revision" --arg builtAt "$built_at" --argjson platforms "$(printf '%s' "$plan_json" | jq -c '.platforms')" --argjson dirty "$dirty" '
      .source_revision = $revision |
      .built_at = $builtAt |
      .images[$name] = {
        workload: $workload,
        repository_path: $repository,
        host_ref: $hostRef,
        cluster_ref: $clusterRef,
        tag: $tag,
        input_hash: $inputHash,
        manifest_digest: $digest,
        platforms: $platforms,
        dirty: $dirty
      }
    ' "$records" >"$next"
  mv "$next" "$records"
}

verify_node_pull() {
  cluster_ref=$1
  docker_platform exec "$(yaml_value "$VERSIONS" cluster.nodeName)" crictl pull "$cluster_ref" >/dev/null ||
    fail "the k3d node could not pull $cluster_ref through $CLUSTER_REGISTRY"
}

validate_complete_image_state() {
  [ -f "$IMAGES_JSON" ] || fail "complete image state is missing. Run task dev."
  platform_config validate-document "$ROOT/deploy/platform/images.schema.json" "$IMAGES_JSON"
  for name in $(build_plan all); do
    plan_json=$(platform_config image-plan "$ROOT" "$name" native)
    repository=$(printf '%s' "$plan_json" | jq -r '.repository_path')
    target=$(printf '%s' "$plan_json" | jq -r '.target')
    input_hash=$(printf '%s' "$plan_json" | jq -r '.input_hash')
    tag=$(printf '%s' "$plan_json" | jq -r '.tag')
    base_provenance=$(printf '%s' "$plan_json" | jq -r '.base_provenance')
    recorded_hash=$(jq -r --arg name "$name" '.images[$name].input_hash // empty' "$IMAGES_JSON")
    [ "$recorded_hash" = "$input_hash" ] || fail "image state for $name is stale. Run task dev."
    verify_remote_image "$repository" "$tag" "$input_hash" "$name" "$target" linux/arm64 "$base_provenance"
    live_digest=$(registry_digest "$repository" "$tag")
    recorded_digest=$(jq -r --arg name "$name" '.images[$name].manifest_digest // empty' "$IMAGES_JSON")
    [ "$live_digest" = "$recorded_digest" ] || fail "live registry descriptor for $name differs from generated state"
  done
}

build_native() {
  PLATFORM_FAILURE_CODE=4
  require_context
  need docker
  need jq
  need curl
  need git
  validate_platform_inputs

  requested=${1:-all}
  if [ "$requested" != all ]; then
    validate_complete_image_state
  fi
  plan=$(build_plan "$requested")
  run_id=$(new_run_id)
  stage_dir=$PLATFORM_TMP/builds/$run_id
  mkdir -p -m 0700 "$stage_dir"
  plans=$stage_dir/native-plans.jsonl
  publish=$stage_dir/native-publish.jsonl
  : >"$plans"
  : >"$publish"
  chmod 0600 "$plans" "$publish"

  source_revision=$(git -C "$ROOT" rev-parse HEAD)
  for name in $plan; do
    plan_json=$(platform_config image-plan "$ROOT" "$name" native)
    printf '%s\n' "$plan_json" >>"$plans"
    repository=$(printf '%s' "$plan_json" | jq -r '.repository_path')
    dockerfile=$(printf '%s' "$plan_json" | jq -r '.dockerfile')
    target=$(printf '%s' "$plan_json" | jq -r '.target')
    input_hash=$(printf '%s' "$plan_json" | jq -r '.input_hash')
    tag=$(printf '%s' "$plan_json" | jq -r '.tag')
    base_provenance=$(printf '%s' "$plan_json" | jq -r '.base_provenance')
    host_ref=$HOST_REGISTRY/$repository:$tag
    local_ref=vermouth-build/$name:$tag

    if registry_manifest "$repository" "$tag" >/dev/null 2>&1; then
      verify_remote_image "$repository" "$tag" "$input_hash" "$name" "$target" linux/arm64 "$base_provenance"
      printf 'Using immutable %s\n' "$host_ref"
      continue
    fi

    printf 'Building %s for linux/arm64\n' "$name"
    docker_platform buildx build --builder "$(yaml_value "$VERSIONS" builder.name)" --platform linux/arm64 --provenance=false --load --file "$ROOT/$dockerfile" --target "$target" --label "vermouth.dev/input-hash=$input_hash" --label "vermouth.dev/repository-path=$repository" --label "vermouth.dev/workload=$name" --label "vermouth.dev/target=$target" --label "vermouth.dev/platform=linux/arm64" --label "vermouth.dev/base-provenance=$base_provenance" --tag "$local_ref" "$ROOT"
    jq -cn --arg localRef "$local_ref" --arg hostRef "$host_ref" --arg repository "$repository" --arg tag "$tag" '{local_ref:$localRef,host_ref:$hostRef,repository:$repository,tag:$tag}' >>"$publish"
  done

  while IFS= read -r item; do
    [ -n "$item" ] || continue
    local_ref=$(printf '%s' "$item" | jq -r '.local_ref')
    host_ref=$(printf '%s' "$item" | jq -r '.host_ref')
    docker_platform tag "$local_ref" "$host_ref"
    push_with_retry "$host_ref" || fail "could not push $host_ref after three attempts"
  done <"$publish"

  if [ "$requested" = all ]; then
    records=$stage_dir/images.json.next
    printf '{"schema_version":1,"source_revision":"","dirty":false,"built_at":"","images":{}}\n' >"$records"
  else
    records=$stage_dir/images.json.next
    cp "$IMAGES_JSON" "$records"
  fi
  chmod 0600 "$records"

  built_at=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
  while IFS= read -r plan_json; do
    [ -n "$plan_json" ] || continue
    name=$(printf '%s' "$plan_json" | jq -r '.workload')
    repository=$(printf '%s' "$plan_json" | jq -r '.repository_path')
    target=$(printf '%s' "$plan_json" | jq -r '.target')
    input_hash=$(printf '%s' "$plan_json" | jq -r '.input_hash')
    tag=$(printf '%s' "$plan_json" | jq -r '.tag')
    base_provenance=$(printf '%s' "$plan_json" | jq -r '.base_provenance')
    verify_remote_image "$repository" "$tag" "$input_hash" "$name" "$target" linux/arm64 "$base_provenance"
    digest=$(registry_digest "$repository" "$tag")
    [ "${#digest}" -eq 71 ] || fail "registry did not return an authoritative manifest digest for $repository:$tag"
    verify_node_pull "$CLUSTER_REGISTRY/$repository@$digest"
    inputs=$(printf '%s' "$plan_json" | jq -r '.inputs[]')
    if git -C "$ROOT" status --porcelain -- $inputs | grep . >/dev/null; then dirty=true; else dirty=false; fi
    append_image_record "$records" "$plan_json" "$digest" "$dirty" "$source_revision" "$built_at"
  done <"$plans"

  outer_dirty=$(jq '[.images[].dirty] | any' "$records")
  next=$records.outer
  jq --argjson dirty "$outer_dirty" '.dirty = $dirty' "$records" >"$next"
  mv "$next" "$records"
  platform_config validate-document "$ROOT/deploy/platform/images.schema.json" "$records"
  expected=$(platform_config get "$IMAGE_LOCK" built | jq 'length')
  actual=$(jq '.images | length' "$records")
  [ "$actual" -eq "$expected" ] || fail "image state has $actual records, expected $expected"
	platform_config atomic-commit "$records" "$IMAGES_JSON"
	find "$stage_dir" -mindepth 1 -delete
	rmdir "$stage_dir"
  prune_vermouth_host_images
	printf 'Recorded native image digests atomically in %s\n' "$IMAGES_JSON"
}

build_multi() {
  PLATFORM_FAILURE_CODE=4
  require_context
  need docker
  need jq
  need curl
  validate_platform_inputs

  run_id=$(new_run_id)
  stage_dir=$PLATFORM_TMP/builds/$run_id
  mkdir -p -m 0700 "$stage_dir"
  plans=$stage_dir/multi-plans.jsonl
  : >"$plans"
  chmod 0600 "$plans"

  for name in $(multi_build_plan); do
    plan_json=$(platform_config image-plan "$ROOT" "$name" multi)
    printf '%s\n' "$plan_json" >>"$plans"
    dockerfile=$(printf '%s' "$plan_json" | jq -r '.dockerfile')
    target=$(printf '%s' "$plan_json" | jq -r '.target')
    input_hash=$(printf '%s' "$plan_json" | jq -r '.input_hash')
    repository=$(printf '%s' "$plan_json" | jq -r '.repository_path')
    tag=$(printf '%s' "$plan_json" | jq -r '.tag')
    base_provenance=$(printf '%s' "$plan_json" | jq -r '.base_provenance')
    for platform in linux/arm64 linux/amd64; do
      architecture=${platform#linux/}
      archive_dir=$stage_dir/$name
      mkdir -p -m 0700 "$archive_dir"
      archive=$archive_dir/$architecture.tar
      stage_ref=vermouth-stage/$name:$tag-$architecture
      docker_platform buildx build --builder "$(yaml_value "$VERSIONS" builder.name)" --platform "$platform" --provenance=false --output "type=docker,dest=$archive" --file "$ROOT/$dockerfile" --target "$target" --label "vermouth.dev/input-hash=$input_hash" --label "vermouth.dev/repository-path=$repository" --label "vermouth.dev/workload=$name" --label "vermouth.dev/target=$target" --label "vermouth.dev/platform=$platform" --label "vermouth.dev/base-provenance=$base_provenance" --tag "$stage_ref" "$ROOT"
      [ -s "$archive" ] || fail "$name $platform archive was not staged"
    done
  done

  while IFS= read -r plan_json; do
    [ -n "$plan_json" ] || continue
    name=$(printf '%s' "$plan_json" | jq -r '.workload')
    repository=$(printf '%s' "$plan_json" | jq -r '.repository_path')
    target=$(printf '%s' "$plan_json" | jq -r '.target')
    input_hash=$(printf '%s' "$plan_json" | jq -r '.input_hash')
    multi_tag=$(printf '%s' "$plan_json" | jq -r '.tag')
    base_provenance=$(printf '%s' "$plan_json" | jq -r '.base_provenance')
    for platform in linux/arm64 linux/amd64; do
      architecture=${platform#linux/}
      archive=$stage_dir/$name/$architecture.tar
      local_ref=vermouth-stage/$name:$multi_tag-$architecture
      docker_platform load --input "$archive" >/dev/null
      docker_platform image inspect "$local_ref" >/dev/null 2>&1 || fail "Docker did not load $local_ref"
      verify_local_image "$local_ref" "$input_hash" "$repository" "$name" "$target" "$platform" "$base_provenance"
    done
  done <"$plans"

  while IFS= read -r plan_json; do
    [ -n "$plan_json" ] || continue
    name=$(printf '%s' "$plan_json" | jq -r '.workload')
    repository=$(printf '%s' "$plan_json" | jq -r '.repository_path')
    target=$(printf '%s' "$plan_json" | jq -r '.target')
    input_hash=$(printf '%s' "$plan_json" | jq -r '.input_hash')
    multi_tag=$(printf '%s' "$plan_json" | jq -r '.tag')
    base_provenance=$(printf '%s' "$plan_json" | jq -r '.base_provenance')
    child_refs=""
    for platform in linux/arm64 linux/amd64; do
      architecture=${platform#linux/}
      local_ref=vermouth-stage/$name:$multi_tag-$architecture
      child_tag=$multi_tag-$architecture
      child_ref=$HOST_REGISTRY/$repository:$child_tag
      docker_platform tag "$local_ref" "$child_ref"
      if registry_manifest "$repository" "$child_tag" >/dev/null 2>&1; then
        verify_remote_image "$repository" "$child_tag" "$input_hash" "$name" "$target" "$platform" "$base_provenance"
      else
        push_with_retry "$child_ref" || fail "could not push $child_ref after three attempts"
        verify_remote_image "$repository" "$child_tag" "$input_hash" "$name" "$target" "$platform" "$base_provenance"
      fi
      child_digest=$(registry_digest "$repository" "$child_tag")
      [ "${#child_digest}" -eq 71 ] || fail "$child_ref has no authoritative remote manifest digest"
      case "$architecture" in
        arm64) arm64_digest=$child_digest ;;
        amd64) amd64_digest=$child_digest ;;
      esac
      child_refs="$child_refs $child_ref"
    done
    final_ref=$HOST_REGISTRY/$repository:$multi_tag
    if ! registry_manifest "$repository" "$multi_tag" >/dev/null 2>&1; then
      docker_platform buildx imagetools create --tag "$final_ref" $child_refs
    fi
    manifest=$(docker_platform buildx imagetools inspect --raw "$final_ref")
    printf '%s' "$manifest" | jq -e --arg arm64 "$arm64_digest" --arg amd64 "$amd64_digest" '
      (.manifests | length == 2) and
      .manifests[0].platform.os == "linux" and
      .manifests[0].platform.architecture == "arm64" and
      .manifests[0].digest == $arm64 and
      .manifests[1].platform.os == "linux" and
      .manifests[1].platform.architecture == "amd64" and
      .manifests[1].digest == $amd64
    ' >/dev/null || fail "$final_ref does not contain exactly linux/arm64 then linux/amd64"
    digest=$(registry_digest "$repository" "$multi_tag")
    [ "${#digest}" -eq 71 ] || fail "$final_ref has no authoritative remote index digest"
    if [ "$name" = web ]; then
      smoke_web_architectures "$final_ref"
    fi
    printf 'Verified %s@%s for linux/arm64 and linux/amd64\n' "$repository" "$digest"
	done <"$plans"
	find "$stage_dir" -mindepth 1 -delete
	rmdir "$stage_dir"
  prune_vermouth_host_images
}
