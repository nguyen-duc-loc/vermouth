#!/bin/sh
set -eu
umask 077

. /usr/local/lib/vermouth/deploy-root-lib.sh
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml

ops_fail() {
  printf 'production operations: %s\n' "$*" >&2
  exit 5
}

prod_status_root() {
  for tool in awk curl df find flock helm jq kubectl sha256sum stat; do
    command -v "$tool" >/dev/null 2>&1 || ops_fail "$tool is required"
  done
  [ -f /etc/vermouth/platform.json ] || ops_fail "the production platform is not bootstrapped"
  printf '%s\n' 'Platform identity'
  jq '{schema_version,vm_name,node_name,hostname,k3s_version,storage_mode,storage_root,storage_uuid,selected_at,locked,current_git_sha,github_run_id,github_run_attempt,release_identity,images_json_sha256,deployment_base_sha256,migration_compat_evidence_sha256,rate_limit_evidence_sha256,bundle_manifest_sha256,goose_versions,active_image_digest_set,current_application_revision,previous_deployable_revision}' \
    /etc/vermouth/platform.json
  printf '%s\n' 'Production mutation lock'
  if [ ! -e /run/lock/vermouth-production.lock ] ||
    flock -n -s /run/lock/vermouth-production.lock true 2>/dev/null; then
    printf '%s\n' free
  else
    printf '%s\n' busy
  fi
  printf '%s\n' 'Capacity'
  awk '/^MemTotal:|^MemAvailable:/ {print}' /proc/meminfo
  df -h /
  printf '%s\n' 'Helm releases'
  helm --namespace vermouth list
  for release in vermouth-foundation vermouth vermouth-jobs; do
    helm --namespace vermouth history "$release" 2>/dev/null || true
  done
  printf '%s\n' 'Migration reports'
  find /var/lib/vermouth/reports -maxdepth 1 -type f -name 'migration-*.json' -print -exec jq . {} \; 2>/dev/null || true
  printf '%s\n' 'Capacity reports'
  find /var/lib/vermouth/reports -maxdepth 1 -type f -name 'resource-*.json' -print -exec jq . {} \; 2>/dev/null || true
  printf '%s\n' 'Live Goose versions'
  live_goose_versions
  printf '%s\n' 'Image and Secret references'
  release_identity=$(jq -r '.release_identity // empty' /etc/vermouth/platform.json)
  current_revision=$(jq -r '.current_application_revision // empty' /etc/vermouth/platform.json)
  if [ -n "$release_identity" ] && [ -f "/var/lib/vermouth/releases/$release_identity/images.json" ]; then
    jq -r '.images | to_entries[] | "image \(.key) \(.value.repository)@\(.value.digest)"' \
      "/var/lib/vermouth/releases/$release_identity/images.json"
  fi
  if [ -n "$current_revision" ]; then
    helm --namespace vermouth get values vermouth --revision "$current_revision" -o json |
      jq -r '.secrets | to_entries[] | select(.value != "") | "Secret \(.key) \(.value)"'
  fi
  printf '%s\n' 'Restore staging'
  storage_root=$(jq -r '.storage_root' /etc/vermouth/platform.json)
  find "$storage_root/.restore" -maxdepth 3 -type f -print 2>/dev/null || true
  printf '%s\n' 'Workloads and Jobs'
  kubectl --namespace vermouth get deployments,statefulsets,jobs,pods -o wide
  printf '%s\n' 'Static storage'
  kubectl get pv -l app.kubernetes.io/name=vermouth
  kubectl --namespace vermouth get pvc
  kubectl --namespace kube-system get pvc traefik-acme
  printf '%s\n' 'Traefik certificate state'
  storage_root=$(jq -r '.storage_root' /etc/vermouth/platform.json)
  stat -c '%n %s bytes mode %a owner %U:%G' "$storage_root/traefik-acme/acme-staging.json" "$storage_root/traefik-acme/acme.json"
  hostname=$(jq -r '.hostname' /etc/vermouth/platform.json)
  printf '%s\n' 'External readiness'
  curl -fsS --max-time 10 "https://$hostname/health"
  curl -fsS --max-time 10 "https://$hostname/ready"
}

redact_stream() {
  awk '
    function replace_literal(line, value, replacement, before, after, position) {
      if (value == "") return line
      while ((position = index(line, value)) > 0) {
        before = substr(line, 1, position - 1)
        after = substr(line, position + length(value))
        line = before replacement after
      }
      return line
    }
    NR == FNR {
      separator = index($0, "=")
      if (separator > 1) {
        value = substr($0, separator + 1)
        if (value != "") secrets[++count] = value
      }
      next
    }
    {
      line = $0
      for (i = 1; i <= count; i++) line = replace_literal(line, secrets[i], "[REDACTED]")
      print line
    }
  ' /etc/vermouth/production.env -
}

logs_one() {
  target=$1
  namespace=vermouth
  resource=
  case "$target" in
    web|gateway|identity|teaching|billing|notifications) resource=deployment/$target ;;
    postgres-identity|postgres-teaching|postgres-billing|postgres-notifications|redpanda|garage) resource=statefulset/$target ;;
    traefik)
      namespace=kube-system
      resource=deployment/traefik
      ;;
    garage-init)
      job=$(kubectl --namespace vermouth get jobs -l app.kubernetes.io/component=garage-init \
        --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1:].metadata.name}')
      [ -n "$job" ] || ops_fail "no Garage initialization Job exists"
      resource=job/$job
      ;;
    migrate-*) resource=job/$target ;;
    *) ops_fail "unknown production log target $target" ;;
  esac
  set -- kubectl --namespace "$namespace" logs "$resource"
  [ "$logs_previous" = false ] || set -- "$@" --previous
  [ "$logs_follow" = false ] || set -- "$@" --follow
  [ -z "$logs_since" ] || set -- "$@" --since="$logs_since"
  "$@" 2>&1 | redact_stream
}

prod_logs_root() {
  target=${1:-}
  [ -n "$target" ] || ops_fail "a log target is required"
  shift
  logs_previous=false
  logs_follow=false
  logs_since=
  for option in "$@"; do
    case "$option" in
      --previous) logs_previous=true ;;
      --follow) logs_follow=true ;;
      --since=*) logs_since=${option#--since=} ;;
      *) ops_fail "unknown log option $option" ;;
    esac
  done
  if [ "$target" = all ]; then
    [ "$logs_follow" = false ] || ops_fail "all logs cannot be followed as one stream"
    for item in web gateway identity teaching billing notifications postgres-identity postgres-teaching postgres-billing postgres-notifications redpanda garage traefik garage-init; do
      printf '\n==> %s\n' "$item"
      logs_one "$item" || true
    done
    return
  fi
  logs_one "$target"
}

export_release_record() {
  revision=$1
  identity=$2
  output=$3
  values=$release_work/export-values-$revision.json
  helm --namespace vermouth get values vermouth --revision "$revision" -o json >"$values"
  verify_revision_runtime_secrets "$values"
  release=/var/lib/vermouth/releases/$identity
  [ -d "$release" ] && [ ! -L "$release" ] || ops_fail "release directory $identity is missing"
  for pair in \
    imagesJSONSHA256:images.json \
    deploymentBaseSHA256:deployment-base.json \
    migrationCompatEvidenceSHA256:migration-compat-evidence.json \
    rateLimitEvidenceSHA256:rate-limit-evidence.json \
    bundleManifestSHA256:bundle-manifest.json; do
    value_name=${pair%%:*}
    file_name=${pair#*:}
    verify_release_evidence_checksum "$values" "$release" "$value_name" "$file_name" ||
      ops_fail "release $identity has invalid $file_name evidence"
  done
  jq -S -c -n \
    --arg identity "$identity" --argjson revision "$revision" \
    --arg images "$(jq -r '.release.imagesJSONSHA256' "$values")" \
    --arg base "$(jq -r '.release.deploymentBaseSHA256' "$values")" \
    --arg migration "$(jq -r '.release.migrationCompatEvidenceSHA256' "$values")" \
    --arg rate "$(jq -r '.release.rateLimitEvidenceSHA256' "$values")" \
    --arg manifest "$(jq -r '.release.bundleManifestSHA256' "$values")" \
    --argjson digests "$(jq '.images | with_entries(.value = .value.digest)' "$release/images.json")" '
      {
        identity:$identity,
        helm_revision:$revision,
        images_json_sha256:$images,
        deployment_base_sha256:$base,
        migration_compat_evidence_sha256:$migration,
        rate_limit_evidence_sha256:$rate,
        bundle_manifest_sha256:$manifest,
        image_digests:$digests
      }
    ' >"$output"
}

capture_runtime_secret_snapshots() {
  snapshot_root=$1
  shift
  mkdir -p "$snapshot_root"
  for values in "$@"; do
    [ -f "$values" ] || continue
    runtime_secret_evidence "$values"
    for service in gateway identity teaching billing notifications; do
      name=$(jq -r --arg service "$service" '.release.runtimeSecrets[$service].name' "$values")
      hash=$(jq -r --arg service "$service" '.release.runtimeSecrets[$service].sourceSHA256' "$values")
      keys=$(jq -c --arg service "$service" '.release.runtimeSecrets[$service].keys' "$values")
      verify_runtime_secret "$name" "$service" "$hash" "$keys"
      destination=$snapshot_root/$name.json
      if [ -f "$destination" ]; then
        jq -e --arg name "$name" --arg hash "$hash" --argjson keys "$keys" '
          .name == $name and .source_sha256 == $hash and .keys == $keys
        ' "$destination" >/dev/null || ops_fail "duplicate runtime Secret snapshot $name is inconsistent"
        continue
      fi
      jq -S -c --arg name "$name" --arg service "$service" --arg hash "$hash" --argjson keys "$keys" \
        '{schema_version:1,name:$name,service:$service,source_sha256:$hash,keys:$keys,values:.}' \
        "$release_work/secret-$service-decoded.json" >"$destination"
      chmod 0600 "$destination"
    done
  done
}

capture_scale_target() {
  namespace=$1
  kind=$2
  name=$3
  replicas=$(kubectl --namespace "$namespace" get "$kind/$name" -o jsonpath='{.spec.replicas}')
  printf '%s:%s:%s:%s\n' "$namespace" "$kind" "$name" "$replicas" >>"$export_scale_state"
  kubectl --namespace "$namespace" scale "$kind/$name" --replicas=0 >/dev/null
}

resume_export_state() {
  [ "${export_quiesced:-false}" = true ] || return
  while IFS=: read -r namespace kind name replicas; do
    [ -n "$namespace" ] || continue
    kubectl --namespace "$namespace" scale "$kind/$name" --replicas="$replicas" >/dev/null || true
  done <"$export_scale_state"
  [ ! -s "$export_ingress" ] || kubectl apply -f "$export_ingress" >/dev/null || true
}

prod_export_root() {
  for tool in cp find helm install jq kubectl sed sha256sum sleep tar zstd; do
    command -v "$tool" >/dev/null 2>&1 || ops_fail "$tool is required"
  done
  production_lock_exclusive
  work=$(mktemp -d /var/tmp/vermouth-export.XXXXXX)
  release_work=$work/release-work
  mkdir "$release_work"
  export release_work
  export_scale_state=$work/scale-state
  export_ingress=$work/ingress.json
  : >"$export_scale_state"
  : >"$export_ingress"
  export_quiesced=false
  cleanup_export_root() {
    resume_export_state
    rm -rf "$work"
  }
  trap cleanup_export_root EXIT HUP INT TERM

  helm --namespace vermouth history vermouth -o json >"$work/history.json" ||
    ops_fail "the application Helm release is unavailable"
  current_revision=$(jq '[.[] | select(.status == "deployed") | .revision] | if length == 1 then .[0] else null end' "$work/history.json")
  [ "$current_revision" != null ] || ops_fail "one deployed application revision is required for export"
  helm --namespace vermouth get values vermouth --revision "$current_revision" -o json >"$work/current-values.json"
  current_identity=$(jq -r '.release.identity // empty' "$work/current-values.json")
  printf '%s' "$current_identity" | grep -Eq '^[0-9a-f]{40}/[1-9][0-9]*-[1-9][0-9]*$' ||
    ops_fail "the current release identity is invalid"
  export_release_record "$current_revision" "$current_identity" "$work/current-release.json"
  previous_target=$(rollback_target "$current_revision" "$current_identity")
  previous_release_json=null
  if [ -n "$previous_target" ]; then
    previous_revision=${previous_target%%:*}
    previous_identity=${previous_target#*:}
    export_release_record "$previous_revision" "$previous_identity" "$work/previous-release.json"
    previous_release_json=$(cat "$work/previous-release.json")
  fi

  export_quiesced=true
  if kubectl --namespace vermouth get ingress vermouth -o json >"$work/ingress-live.json" 2>/dev/null; then
    jq 'del(.metadata.creationTimestamp,.metadata.generation,.metadata.managedFields,.metadata.resourceVersion,.metadata.uid,.status)' \
      "$work/ingress-live.json" >"$export_ingress"
    kubectl --namespace vermouth delete ingress vermouth --wait=true >/dev/null
  fi
  for deployment in web gateway identity teaching billing notifications; do
    capture_scale_target vermouth deployment "$deployment"
  done

  stage=$work/stage
  mkdir -p "$stage/config" "$stage/postgres" "$stage/helm" "$stage/releases" "$stage/storage" "$stage/secrets/runtime"
  capture_runtime_secret_snapshots "$stage/secrets/runtime" \
    "$work/current-values.json" "$release_work/export-values-${previous_revision:-missing}.json"
  for service in identity teaching billing notifications; do
    kubectl --namespace vermouth exec "statefulset/postgres-$service" -- \
      pg_dump -U "vermouth_$service" -d "vermouth_$service" --format=custom >"$stage/postgres/$service.dump"
  done
  helm --namespace vermouth get values vermouth-foundation -o json |
    jq -S -c --argjson revision "$(helm --namespace vermouth history vermouth-foundation -o json | jq '[.[] | select(.status == "deployed") | .revision] | max')" \
      '{revision:$revision,values:.}' >"$stage/helm/foundation.json"
  jq -S -c -n --argjson revision "$current_revision" --slurpfile values "$work/current-values.json" \
    '{revision:$revision,values:$values[0]}' >"$stage/helm/application.json"
  if [ "$previous_release_json" != null ]; then
    jq -S -c -n --argjson revision "$previous_revision" \
      --slurpfile values "$release_work/export-values-$previous_revision.json" \
      '{revision:$revision,values:$values[0]}' >"$stage/helm/previous.json"
  fi

  for stateful in postgres-identity postgres-teaching postgres-billing postgres-notifications redpanda garage; do
    capture_scale_target vermouth statefulset "$stateful"
  done
  capture_scale_target kube-system deployment traefik
  while kubectl --namespace vermouth get pods -o json |
    jq -e '[.items[] | select(.metadata.ownerReferences[0].kind == "ReplicaSet" or .metadata.ownerReferences[0].kind == "StatefulSet")] | length > 0' >/dev/null; do
    sleep 2
  done
  while kubectl --namespace kube-system get pods -l app.kubernetes.io/name=traefik -o json |
    jq -e '.items | length > 0' >/dev/null; do
    sleep 2
  done

  mkdir "$stage/releases/current"
  cp -a "/var/lib/vermouth/releases/$current_identity/." "$stage/releases/current/"
  if [ "$previous_release_json" != null ]; then
    mkdir "$stage/releases/previous"
    cp -a "/var/lib/vermouth/releases/$previous_identity/." "$stage/releases/previous/"
  fi
  install -o root -g root -m 0600 /etc/vermouth/production.env "$stage/config/production.env"
  storage_root=$(jq -r '.storage_root' /etc/vermouth/platform.json)
  for component in postgres-identity postgres-teaching postgres-billing postgres-notifications redpanda garage-metadata garage-data traefik-acme; do
    mkdir "$stage/storage/$component"
    cp -a "$storage_root/$component/." "$stage/storage/$component/"
  done

  roots='["config/production.env","helm/application.json","helm/foundation.json","postgres/billing.dump","postgres/identity.dump","postgres/notifications.dump","postgres/teaching.dump","releases/current","secrets/runtime","storage/garage-data","storage/garage-metadata","storage/postgres-billing","storage/postgres-identity","storage/postgres-notifications","storage/postgres-teaching","storage/redpanda","storage/traefik-acme"]'
  if [ "$previous_release_json" != null ]; then
    roots=$(printf '%s' "$roots" | jq -c '. + ["helm/previous.json","releases/previous"] | sort')
  fi
  platform=/etc/vermouth/platform.json
  jq -S -c -n \
    --arg created_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg hostname "$(jq -r '.hostname' "$platform")" \
    --arg vm "$(jq -r '.vm_name' "$platform")" --arg node "$(jq -r '.node_name' "$platform")" \
    --arg mode "$(jq -r '.storage_mode' "$platform")" --arg root "$storage_root" \
    --argjson uuid "$(jq '.storage_uuid' "$platform")" --arg selected_at "$(jq -r '.selected_at' "$platform")" \
    --arg marker "$(cat "$storage_root/.vermouth-storage")" \
    --argjson current "$(cat "$work/current-release.json")" --argjson previous "$previous_release_json" \
    --argjson goose "$(live_goose_versions)" --argjson roots "$roots" '
      {
        schema_version:1,
        created_at:$created_at,
        hostname:$hostname,
        storage_identity:{vm_name:$vm,node_name:$node,storage_mode:$mode,storage_root:$root,storage_uuid:$uuid,selected_at:$selected_at,marker_sha256:$marker},
        current_release:$current,
        previous_release:$previous,
        goose_versions:$goose,
        roots:$roots
      }
    ' >"$work/metadata.json"
  vermouth-platformconfig production-export-manifest "$work/metadata.json" "$stage" >"$stage/manifest.json"
  vermouth-platformconfig validate-document /usr/local/share/vermouth/export-manifest.schema.json "$stage/manifest.json"
  (
    cd "$stage"
    find . -mindepth 1 -print | sed 's|^./||' | LC_ALL=C sort >"$work/archive-files"
    tar --format=ustar --no-recursion --numeric-owner -cf - -T "$work/archive-files" | zstd -q -T0 -19 -c
  )
}

extract_validated_export() {
  archive=$1
  destination=$2
  mkdir "$destination"
  zstd -q -dc "$archive" | tar --numeric-owner -xpf - -C "$destination"
}

verify_export_release_manifest() {
  manifest=$1
  field=$2
  release=$3
  [ -d "$release" ] || ops_fail "$field release directory is missing from the export"
  for pair in \
    images_json_sha256:images.json \
    deployment_base_sha256:deployment-base.json \
    migration_compat_evidence_sha256:migration-compat-evidence.json \
    rate_limit_evidence_sha256:rate-limit-evidence.json \
    bundle_manifest_sha256:bundle-manifest.json; do
    key=${pair%%:*}
    file=${pair#*:}
    expected=$(jq -r --arg field "$field" --arg key "$key" '.[$field][$key] // empty' "$manifest")
    [ "$(sha256sum "$release/$file" | awk '{print $1}')" = "$expected" ] ||
      ops_fail "$field release $file checksum is invalid"
  done
}

verify_restore_platform_resources() {
  config=/etc/vermouth/platform-config.json
  [ -f "$config" ] || ops_fail "the committed production platform configuration is missing"
  expected_k3s=$(jq -r '.k3s.live_version' "$config")
  live_k3s=$(kubectl version -o json | jq -r '.serverVersion.gitVersion')
  [ "$live_k3s" = "$expected_k3s" ] || ops_fail "the live k3s version does not match the restore target"

  expected_pvs='["garage-data","garage-metadata","postgres-billing","postgres-identity","postgres-notifications","postgres-teaching","redpanda","traefik-acme"]'
  live_pvs=$(kubectl get pv -o json | jq -c '
    [.items[] | select(.spec.storageClassName == "vermouth-local") | .metadata.name] | sort
  ')
  [ "$live_pvs" = "$expected_pvs" ] || ops_fail "the restore target does not have the exact eight static PV names"

  expected_pvcs='["garage-data","garage-metadata","postgres-billing","postgres-identity","postgres-notifications","postgres-teaching","redpanda"]'
  live_pvcs=$(kubectl --namespace vermouth get pvc -o json | jq -c '
    [.items[] | select(.spec.storageClassName == "vermouth-local") | .metadata.name] | sort
  ')
  [ "$live_pvcs" = "$expected_pvcs" ] || ops_fail "the restore target does not have the exact seven application PVC names"
  traefik_claim=$(kubectl --namespace kube-system get pvc traefik-acme -o json)
  printf '%s' "$traefik_claim" | jq -e '
    .metadata.name == "traefik-acme" and .spec.storageClassName == "vermouth-local" and .spec.volumeName == "traefik-acme"
  ' >/dev/null || ops_fail "the restore target does not have the exact Traefik PVC"
}

restore_preflight_tree() (
  tree=$1
  manifest=$tree/manifest.json
  vermouth-platformconfig validate-document /usr/local/share/vermouth/export-manifest.schema.json "$manifest"
  platform=/etc/vermouth/platform.json
  expected_marker=$(jq -r '.storage_identity.marker_sha256' "$manifest")
  storage_root=$(jq -r '.storage_root' "$platform")
  [ "$(cat "$storage_root/.vermouth-storage")" = "$expected_marker" ] ||
    ops_fail "the export storage marker does not match this production target"
  jq -e --slurpfile platform "$platform" '
    .hostname == $platform[0].hostname and
    .storage_identity.vm_name == $platform[0].vm_name and
    .storage_identity.node_name == $platform[0].node_name and
    .storage_identity.storage_mode == $platform[0].storage_mode and
    .storage_identity.storage_root == $platform[0].storage_root and
    .storage_identity.storage_uuid == $platform[0].storage_uuid
  ' "$manifest" >/dev/null || ops_fail "the export belongs to another production platform"
  verify_restore_platform_resources

  current_identity=$(jq -r '.current_release.identity' "$manifest")
  current_sha=${current_identity%%/*}
  current_run=${current_identity#*/}
  current_run_id=${current_run%%-*}
  current_attempt=${current_run#*-}
  verify_export_release_manifest "$manifest" current_release "$tree/releases/current"
  release_work=$(mktemp -d /var/tmp/vermouth-restore-release.XXXXXX)
  export release_work
  validate_release_documents "$tree/releases/current" "$current_sha" "$current_run_id" "$current_attempt"
  previous_identity=$(jq -r '.previous_release.identity // empty' "$manifest")
  if [ -n "$previous_identity" ]; then
    previous_sha=${previous_identity%%/*}
    previous_run=${previous_identity#*/}
    verify_export_release_manifest "$manifest" previous_release "$tree/releases/previous"
    validate_release_documents \
      "$tree/releases/previous" "$previous_sha" "${previous_run%%-*}" "${previous_run#*-}"
  fi
  set -a
  . "$tree/config/production.env"
  set +a
  for name in DOCKERHUB_READ_USERNAME DOCKERHUB_READ_TOKEN; do
    eval "value=\${$name:-}"
    [ -n "$value" ] || ops_fail "the archived production configuration is missing $name"
  done
  verify_registry_images "$tree/releases/current/images.json"
  [ -z "$previous_identity" ] || verify_registry_images "$tree/releases/previous/images.json"
  rm -rf "$release_work"
)

prod_restore_receive_root() {
  for tool in awk basename cat df find helm install jq kubectl mktemp mv sha256sum sync tar zstd; do
    command -v "$tool" >/dev/null 2>&1 || ops_fail "$tool is required"
  done
  expected_sha=${1:-}
  printf '%s' "$expected_sha" | grep -Eq '^[0-9a-f]{64}$' || ops_fail "the expected restore SHA256 is invalid"
  production_lock_exclusive
  [ -f /etc/vermouth/platform.json ] || ops_fail "the production platform is not bootstrapped"
  storage_root=$(jq -r '.storage_root' /etc/vermouth/platform.json)
  incoming_root=$storage_root/.restore/.incoming
  staged_root=$storage_root/.restore/.staged
  install -d -o root -g root -m 0700 "$incoming_root" "$staged_root"
  find "$incoming_root" -type f -mmin +60 -delete
  incoming=$(mktemp "$incoming_root/restore.XXXXXX")
  chmod 0600 "$incoming"
  cleanup_receive() {
    [ -z "${incoming:-}" ] || rm -f "$incoming"
    [ -z "${receive_work:-}" ] || rm -rf "$receive_work"
  }
  trap cleanup_receive EXIT HUP INT TERM
  cat >"$incoming"
  [ "$(sha256sum "$incoming" | awk '{print $1}')" = "$expected_sha" ] || ops_fail "the received restore stream SHA256 is wrong"
  [ "$(vermouth-platformconfig production-archive-validate <"$incoming")" = "$expected_sha" ] ||
    ops_fail "the received restore archive validation did not preserve its SHA256"
  sync "$incoming" "$incoming_root"
  run_id=$(basename "$incoming")
  staged=$staged_root/$run_id.tar.zst
  [ ! -e "$staged" ] || ops_fail "restore staging $run_id already exists"
  mv "$incoming" "$staged"
  sync "$staged" "$staged_root"
  incoming=$staged
  receive_work=$(mktemp -d /var/tmp/vermouth-restore-preflight.XXXXXX)
  extract_validated_export "$staged" "$receive_work/tree"
  restore_preflight_tree "$receive_work/tree"
  required=$(jq -r '.regular_file_bytes' "$receive_work/tree/manifest.json")
  available=$(df -Pk "$storage_root" | awk 'NR == 2 {print $4 * 1024}')
  [ "$available" -gt "$((required + 1073741824))" ] || ops_fail "the selected storage root lacks the restore data plus 1 GiB"
  printf '%s\n' "Restore preflight: $(jq -r '.hostname' "$receive_work/tree/manifest.json")"
  printf '%s\n' "VM: $(jq -r '.vm_name' /etc/vermouth/platform.json)"
  printf '%s\n' "Storage root: $storage_root"
  printf '%s\n' "Archive bytes: $required, available after staging: $available"
  printf '%s\n' 'Archive manifest'
  jq '{schema_version,created_at,hostname,storage_identity,current_release,previous_release,goose_versions,roots,regular_file_bytes}' \
    "$receive_work/tree/manifest.json"
  printf '%s\n' 'Current Helm state'
  helm --namespace vermouth history vermouth 2>/dev/null || printf '%s\n' 'No application Helm history'
  printf '%s\n' "RESTORE_RUN_ID=$run_id"
  incoming=
}

prod_restore_discard_root() {
  run_id=${1:-}
  printf '%s' "$run_id" | grep -Eq '^restore\.[A-Za-z0-9]+$' || ops_fail "the restore run ID is invalid"
  production_lock_exclusive
  storage_root=$(jq -r '.storage_root' /etc/vermouth/platform.json)
  staged=$storage_root/.restore/.staged/$run_id.tar.zst
  [ ! -e "$staged" ] || rm -f "$staged"
}

prod_restore_safety_status_root() {
  for tool in find jq; do
    command -v "$tool" >/dev/null 2>&1 || ops_fail "$tool is required"
  done
  [ -f /etc/vermouth/platform.json ] || ops_fail "the production platform is not bootstrapped"
  storage_root=$(jq -r '.storage_root' /etc/vermouth/platform.json)
  safety_root=$storage_root/.restore-safety
  [ ! -d "$safety_root" ] || find "$safety_root" -mindepth 1 -maxdepth 1 -type d -name 'restore.*' -print
}

prod_restore_safety_clean_root() {
  [ "${1:-}" = vermouth ] || ops_fail "typed restore safety cleanup confirmation is required"
  for tool in find jq rm; do
    command -v "$tool" >/dev/null 2>&1 || ops_fail "$tool is required"
  done
  production_lock_exclusive
  [ -f /etc/vermouth/platform.json ] || ops_fail "the production platform is not bootstrapped"
  storage_root=$(jq -r '.storage_root' /etc/vermouth/platform.json)
  safety_root=$storage_root/.restore-safety
  [ ! -d "$safety_root" ] || find "$safety_root" -mindepth 1 -maxdepth 1 -type d -name 'restore.*' -exec rm -rf -- {} +
  printf '%s\n' 'Prior restore safety state removed after the verified export.'
}

restore_scale_all_zero() {
  kubectl --namespace vermouth delete ingress vermouth --ignore-not-found --wait=true >/dev/null
  for deployment in web gateway identity teaching billing notifications; do
    kubectl --namespace vermouth scale "deployment/$deployment" --replicas=0 >/dev/null 2>&1 || true
  done
  for stateful in postgres-identity postgres-teaching postgres-billing postgres-notifications redpanda garage; do
    kubectl --namespace vermouth scale "statefulset/$stateful" --replicas=0 >/dev/null 2>&1 || true
  done
  kubectl --namespace kube-system scale deployment/traefik --replicas=0 >/dev/null
}

restore_prepare_release() {
  release=$1
  identity=$(jq -r '.git_sha + "/" + .github_run_id + "-" + .github_run_attempt' "$release/images.json")
  git_sha=${identity%%/*}
  run=${identity#*/}
  validate_release_documents "$release" "$git_sha" "${run%%-*}" "${run#*-}"
  verify_registry_images "$release/images.json"
  restore_release_secrets "$release"
  write_platform_values
  restore_values="--values $release/chart/values-production.yaml --values $release/values-production.yaml --values $release_evidence_values --values $secrets_values --values $platform_values"
}

restore_release_secrets() {
  release=$1
  values=$release_work/restore-release-values.json
  identity=$(jq -r '.git_sha + "/" + .github_run_id + "-" + .github_run_attempt' "$release/images.json")
  revision=$(jq -r --arg identity "$identity" '
    if .current_release.identity == $identity then .current_release.helm_revision
    elif .previous_release.identity == $identity then .previous_release.helm_revision
    else empty end
  ' "$restore_manifest")
  [ -n "$revision" ] || ops_fail "the restore release is absent from the archive manifest"
  if [ "$(jq -r '.current_release.helm_revision' "$restore_manifest")" = "$revision" ]; then
    cp "$restore_tree/helm/application.json" "$release_work/application-record.json"
    jq '.values' "$release_work/application-record.json" >"$values"
  else
    jq '.values' "$restore_tree/helm/previous.json" >"$values"
  fi
  runtime_secret_evidence "$values"
  secrets_values=$release_work/secrets-values.yaml
  {
    printf '%s\n' 'release:'
    printf '%s\n' '  runtimeSecrets:'
    for service in gateway identity teaching billing notifications; do
      name=$(jq -r --arg service "$service" '.release.runtimeSecrets[$service].name' "$values")
      hash=$(jq -r --arg service "$service" '.release.runtimeSecrets[$service].sourceSHA256' "$values")
      keys=$(jq -c --arg service "$service" '.release.runtimeSecrets[$service].keys' "$values")
      snapshot=$restore_tree/secrets/runtime/$name.json
      jq -e --arg name "$name" --arg service "$service" --arg hash "$hash" --argjson keys "$keys" '
        .schema_version == 1 and .name == $name and .service == $service and
        .source_sha256 == $hash and .keys == $keys and (.values | keys | sort) == $keys
      ' "$snapshot" >/dev/null || ops_fail "runtime Secret snapshot $name does not match its release evidence"
      jq -S -c '.values' "$snapshot" >"$release_work/$service.json"
      [ "$(vermouth-platformconfig secret-hash <"$release_work/$service.json")" = "$hash" ] ||
        ops_fail "runtime Secret snapshot $name values do not match its source hash"
      apply_secret "$name" "$service" "$release_work/$service.json" runtime-secret
      printf '    %s:\n      name: %s\n      sourceSHA256: %s\n      keys: %s\n' "$service" "$name" "$hash" "$keys"
    done
    printf '%s\n' 'secrets:'
    for service in gateway identity teaching billing notifications; do
      printf '  %s: %s\n' "$service" "$(jq -r --arg service "$service" '.release.runtimeSecrets[$service].name' "$values")"
    done
  } >"$secrets_values"
}

prod_restore_apply_root() {
  for tool in cp diff dirname helm install jq kubectl mktemp mv rm sha256sum tar zstd; do
    command -v "$tool" >/dev/null 2>&1 || ops_fail "$tool is required"
  done
  run_id=${1:-}
  [ "${2:-}" = vermouth ] || ops_fail "typed restore confirmation is required"
  printf '%s' "$run_id" | grep -Eq '^restore\.[A-Za-z0-9]+$' || ops_fail "the restore run ID is invalid"
  production_lock_exclusive
  storage_root=$(jq -r '.storage_root' /etc/vermouth/platform.json)
  staged=$storage_root/.restore/.staged/$run_id.tar.zst
  [ -f "$staged" ] && [ ! -L "$staged" ] || ops_fail "the staged restore archive is missing"
  work=$(mktemp -d /var/tmp/vermouth-restore-apply.XXXXXX)
  previous_env=$work/production.env.previous
  install -o root -g root -m 0600 /etc/vermouth/production.env "$previous_env"
  release_work=$work/release-work
  mkdir "$release_work"
  export release_work
  restore_success=false
  safety=$storage_root/.restore-safety/$run_id
  cleanup_restore_apply() {
    rm -f "$staged"
    if [ "$restore_success" != true ] && [ -d "$safety" ]; then
      restore_scale_all_zero || true
      for component in postgres-identity postgres-teaching postgres-billing postgres-notifications redpanda garage-metadata garage-data traefik-acme; do
        rm -rf "$storage_root/$component"
        [ ! -d "$safety/$component" ] || mv "$safety/$component" "$storage_root/$component"
      done
      install -o root -g root -m 0600 "$previous_env" /etc/vermouth/production.env
    fi
    rm -rf "$work"
  }
  trap cleanup_restore_apply EXIT HUP INT TERM
  vermouth-platformconfig production-archive-validate <"$staged" >/dev/null
  extract_validated_export "$staged" "$work/tree"
  restore_tree=$work/tree
  restore_manifest=$restore_tree/manifest.json
  export restore_tree restore_manifest
  restore_preflight_tree "$work/tree"
  printf '%s\n' "Restoring $(jq -r '.current_release.identity' "$work/tree/manifest.json") to $(jq -r '.hostname' "$work/tree/manifest.json")"
  restore_scale_all_zero
  install -d -o root -g root -m 0700 "$safety"
  for component in postgres-identity postgres-teaching postgres-billing postgres-notifications redpanda garage-metadata garage-data traefik-acme; do
    mv "$storage_root/$component" "$safety/$component"
    mkdir "$storage_root/$component"
    cp -a "$work/tree/storage/$component/." "$storage_root/$component/"
  done
  install -o root -g root -m 0600 "$work/tree/config/production.env" /etc/vermouth/production.env
  set -a
  . /etc/vermouth/production.env
  set +a

  for field in current previous; do
    identity=$(jq -r ".${field}_release.identity // empty" "$work/tree/manifest.json")
    [ -n "$identity" ] || continue
    source=$work/tree/releases/$field
    target=/var/lib/vermouth/releases/$identity
    install -d -o root -g root -m 0755 "$(dirname "$target")"
    if [ -d "$target" ]; then
      diff -qr "$source" "$target" >/dev/null || ops_fail "existing release $identity differs from the export"
    else
      cp -a "$source" "$target"
      chmod -R a-w "$target"
    fi
  done

  current_release=$work/tree/releases/current
  restore_prepare_release "$current_release"
  helm upgrade --install vermouth-foundation "$current_release/chart" --namespace vermouth --create-namespace \
    $restore_values --set foundation.enabled=true --set foundation.bootstrapStorage=false \
    --atomic --wait --timeout 10m --history-max 5 >/dev/null
  kubectl --namespace kube-system scale deployment/traefik --replicas=1 >/dev/null
  kubectl --namespace kube-system rollout status deployment/traefik --timeout=5m >/dev/null
  for service in identity teaching billing notifications; do
    kubectl --namespace vermouth exec -i "statefulset/postgres-$service" -- pg_restore --list \
      <"$work/tree/postgres/$service.dump" >/dev/null
  done

  previous_release=$work/tree/releases/previous
  previous_revision=null
  if [ -d "$previous_release" ]; then
    restore_prepare_release "$previous_release"
    helm upgrade --install vermouth "$previous_release/chart" --namespace vermouth \
      $restore_values --set application.enabled=true --set ingress.enabled=false \
      --atomic --wait --timeout 10m --history-max 5 >/dev/null
    previous_revision=$(helm --namespace vermouth history vermouth -o json | jq '[.[] | select(.status == "deployed") | .revision] | max')
  fi
  restore_prepare_release "$current_release"
  helm upgrade --install vermouth "$current_release/chart" --namespace vermouth \
    $restore_values --set application.enabled=true --set ingress.enabled=true \
    --atomic --wait --timeout 10m --history-max 5 >/dev/null
  wait_for_internal_release
  hostname=$(jq -r '.hostname' /etc/vermouth/platform.json)
  wait_for_external_release "$hostname"
  current_revision=$(helm --namespace vermouth history vermouth -o json | jq '[.[] | select(.status == "deployed") | .revision] | max')
  record_platform_release "$current_release" "$previous_revision" "$current_revision"
  garbage_collect_runtime_secrets "$current_revision" "$previous_revision"
  restore_success=true
  printf '%s\n' "Production restore completed at https://$hostname"
}

rollback_target() {
  current_revision=$1
  current_identity=$2
  helm --namespace vermouth history vermouth -o json |
    jq -r --argjson current "$current_revision" '
      [.[] | select(.revision < $current and (.status == "deployed" or .status == "superseded")) | .revision] |
      sort | reverse | .[]
    ' | while IFS= read -r revision; do
      values=$release_work/revision-$revision.json
      helm --namespace vermouth get values vermouth --revision "$revision" -o json >"$values"
      identity=$(jq -r '.release.identity // empty' "$values")
      [ -n "$identity" ] && [ "$identity" != "$current_identity" ] || continue
      printf '%s' "$identity" | grep -Eq '^[0-9a-f]{40}/[1-9][0-9]*-[1-9][0-9]*$' || continue
      release=/var/lib/vermouth/releases/$identity
      [ -d "$release" ] && [ -f "$release/images.json" ] || continue
      expected=$(jq -r '.release.imagesJSONSHA256 // empty' "$values")
      [ "$(sha256sum "$release/images.json" | awk '{print $1}')" = "$expected" ] || continue
      verify_release_evidence_checksum "$values" "$release" deploymentBaseSHA256 deployment-base.json || continue
      verify_release_evidence_checksum "$values" "$release" migrationCompatEvidenceSHA256 migration-compat-evidence.json || continue
      verify_release_evidence_checksum "$values" "$release" rateLimitEvidenceSHA256 rate-limit-evidence.json || continue
      verify_release_evidence_checksum "$values" "$release" bundleManifestSHA256 bundle-manifest.json || continue
      missing=false
      jq -r '.secrets | .. | strings | select(length > 0)' "$values" | while IFS= read -r secret; do
        kubectl --namespace vermouth get secret "$secret" >/dev/null 2>&1 || exit 1
      done || missing=true
      [ "$missing" = false ] || continue
      printf '%s\n' "$revision:$identity"
      return
    done
}

prod_rollback_root() {
  production_lock_exclusive
  release_work=$(mktemp -d /var/tmp/vermouth-rollback.XXXXXX)
  export release_work
  trap 'rm -rf "$release_work"' EXIT HUP INT TERM
  set -a
  . /etc/vermouth/production.env
  set +a
  current_revision=$(helm --namespace vermouth history vermouth -o json | jq '[.[] | select(.status == "deployed") | .revision] | max')
  current_values=$release_work/current.json
  helm --namespace vermouth get values vermouth --revision "$current_revision" -o json >"$current_values"
  current_identity=$(jq -r '.release.identity' "$current_values")
  current_release=/var/lib/vermouth/releases/$current_identity
  current_sha=${current_identity%%/*}
  current_run=${current_identity#*/}
  [ -d "$current_release" ] || ops_fail "the current immutable release directory is missing"
  validate_release_documents "$current_release" "$current_sha" "${current_run%%-*}" "${current_run#*-}"
  target=$(rollback_target "$current_revision" "$current_identity")
  [ -n "$target" ] || ops_fail "no earlier deployable application revision is retained"
  target_revision=${target%%:*}
  target_identity=${target#*:}
  target_release=/var/lib/vermouth/releases/$target_identity
  target_sha=${target_identity%%/*}
  target_run=${target_identity#*/}
  validate_release_documents "$target_release" "$target_sha" "${target_run%%-*}" "${target_run#*-}"
  live_versions=$(live_goose_versions)
  if jq -e --argjson live "$live_versions" '.candidate_goose_versions == $live' \
    "$target_release/migration-compat-evidence.json" >/dev/null; then
    :
  elif jq -e --arg target "$target_identity" --argjson live "$live_versions" '
    .previous_release_identity == $target and .candidate_goose_versions == $live
  ' "$current_release/migration-compat-evidence.json" >/dev/null; then
    :
  else
    ops_fail "no retained migration evidence proves the rollback target against the live Goose versions"
  fi
  verify_registry_images "$target_release/images.json"

  printf '%s\n' "Current application: revision $current_revision, release $current_identity"
  printf '%s\n' "Rollback target: revision $target_revision, release $target_identity"
  jq -r '.images | to_entries[] | "\(.key) \(.value.repository)@\(.value.digest)"' "$target_release/images.json"
  helm --namespace vermouth get values vermouth --revision "$target_revision" -o json |
    jq -r '.secrets | to_entries[] | select(.value != "") | "Secret \(.key) \(.value)"'
  printf '%s' 'Type vermouth to roll the application back: ' >&2
  IFS= read -r confirmation || ops_fail "rollback cancelled"
  [ "$confirmation" = vermouth ] || ops_fail "rollback cancelled"
  helm --namespace vermouth rollback vermouth "$target_revision" --wait --timeout 10m >/dev/null
  hostname=$(jq -r '.hostname' /etc/vermouth/platform.json)
  if ! wait_for_external_release "$hostname"; then
    if helm --namespace vermouth rollback vermouth "$current_revision" --wait --timeout 10m >/dev/null &&
      wait_for_external_release "$hostname"; then
      restored_revision=$(helm --namespace vermouth history vermouth -o json |
        jq '[.[] | select(.status == "deployed") | .revision] | max')
      record_recovered_application_revision "$restored_revision"
      ops_fail "the rollback target failed external HTTPS readiness, so the current application was restored"
    fi
    kubectl --namespace vermouth delete ingress vermouth --ignore-not-found --wait=true >/dev/null 2>&1 || true
    ops_fail "the rollback target and recovery both failed external HTTPS readiness, so public traffic was closed"
  fi
  new_revision=$(helm --namespace vermouth history vermouth -o json | jq '[.[] | select(.status == "deployed") | .revision] | max')
  record_platform_release "$target_release" "$current_revision" "$new_revision"
  garbage_collect_runtime_secrets "$new_revision" "$current_revision"
  printf '%s\n' "Application rolled back to $target_identity at https://$hostname"
}

command=${1:-}
[ -n "$command" ] && shift
case "$command" in
  status) prod_status_root ;;
  logs) prod_logs_root "$@" ;;
  rollback) prod_rollback_root "$@" ;;
  export) prod_export_root ;;
  restore-receive) prod_restore_receive_root "$@" ;;
  restore-discard) prod_restore_discard_root "$@" ;;
  restore-apply) prod_restore_apply_root "$@" ;;
  restore-safety-status) prod_restore_safety_status_root ;;
  restore-safety-clean) prod_restore_safety_clean_root "$@" ;;
  *) ops_fail "unknown production operation $command" ;;
esac
