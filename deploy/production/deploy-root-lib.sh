#!/bin/sh

release_fail() {
  printf 'vermouth release: %s\n' "$*" >&2
  exit 4
}

release_need() {
  command -v "$1" >/dev/null 2>&1 || release_fail "$1 is required"
}

production_lock_shared() {
  install -d -o root -g root -m 0755 /run/lock
  exec 9>/run/lock/vermouth-production.lock
  chmod 0600 /run/lock/vermouth-production.lock
  flock -n -s 9 || release_fail "another production mutation is active"
}

production_lock_exclusive() {
  install -d -o root -g root -m 0755 /run/lock
  exec 9>/run/lock/vermouth-production.lock
  chmod 0600 /run/lock/vermouth-production.lock
  flock -n -x 9 || release_fail "another production mutation is active"
}

production_job_run_id() {
  run_id=$1
  run_attempt=$2
  printf '%s' "$run_id" | grep -Eq '^[1-9][0-9]*$' || release_fail "the production Job run ID is invalid"
  printf '%s' "$run_attempt" | grep -Eq '^[1-9][0-9]*$' || release_fail "the production Job run attempt is invalid"
  printf '%s-%s\n' "$run_id" "$run_attempt"
}

read_goose_version() {
  service=$1
  if ! kubectl --namespace vermouth get "statefulset/postgres-$service" >/dev/null 2>&1; then
    printf '%s\n' 0
    return
  fi
  exists=$(kubectl --namespace vermouth exec "statefulset/postgres-$service" -- \
    psql -U "vermouth_$service" -d "vermouth_$service" -Atqc \
    "SELECT to_regclass('public.goose_db_version') IS NOT NULL" 2>/dev/null) ||
    release_fail "cannot inspect the $service Goose version"
  if [ "$exists" != t ]; then
    printf '%s\n' 0
    return
  fi
  version=$(kubectl --namespace vermouth exec "statefulset/postgres-$service" -- \
    psql -U "vermouth_$service" -d "vermouth_$service" -Atqc \
    'SELECT COALESCE(MAX(version_id) FILTER (WHERE is_applied), 0) FROM goose_db_version' 2>/dev/null) ||
    release_fail "cannot read the $service Goose version"
  printf '%s' "$version" | grep -Eq '^[0-9]+$' || release_fail "$service returned an invalid Goose version"
  printf '%s\n' "$version"
}

live_goose_versions() {
  jq -n \
    --argjson identity "$(read_goose_version identity)" \
    --argjson teaching "$(read_goose_version teaching)" \
    --argjson billing "$(read_goose_version billing)" \
    --argjson notifications "$(read_goose_version notifications)" \
    '{identity:$identity,teaching:$teaching,billing:$billing,notifications:$notifications}'
}

verify_release_evidence_checksum() {
  values=$1
  release=$2
  value_name=$3
  file_name=$4
  expected=$(jq -r --arg name "$value_name" '.release[$name] // empty' "$values")
  printf '%s' "$expected" | grep -Eq '^[0-9a-f]{64}$' || return 1
  [ -f "$release/$file_name" ] && [ ! -L "$release/$file_name" ] || return 1
  [ "$(sha256sum "$release/$file_name" | awk '{print $1}')" = "$expected" ]
}

write_deployment_base_unlocked() (
  candidate_git_sha=$1
  github_run_id=$2
  github_run_attempt=$3
  output=$4
  observed_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  versions=$(live_goose_versions)
  inspection=$(mktemp -d /var/tmp/vermouth-inspect.XXXXXX)
  trap 'rm -rf "$inspection"' EXIT HUP INT TERM

  helm --namespace vermouth list --filter '^vermouth$' -o json >"$inspection/releases.json" ||
    release_fail "cannot list the application Helm release"
  release_count=$(jq 'length' "$inspection/releases.json")
  if [ "$release_count" -eq 0 ]; then
    application_resources=$(kubectl --namespace vermouth get deployment,statefulset \
      -l app.kubernetes.io/instance=vermouth -o json | jq '.items | length') ||
      release_fail "cannot inspect application resources"
    [ "$application_resources" -eq 0 ] || release_fail "application resources exist without Helm history"
    printf '%s' "$versions" | jq -e 'all(.[]; . == 0)' >/dev/null ||
      release_fail "applied Goose versions exist without an application Helm history"
    jq -S -c -n \
      --arg observed_at "$observed_at" --arg candidate "$candidate_git_sha" \
      --arg run_id "$github_run_id" --arg run_attempt "$github_run_attempt" \
      --argjson versions "$versions" '
        {
          schema_version:1,
          observed_at:$observed_at,
          candidate_git_sha:$candidate,
          github_run_id:$run_id,
          github_run_attempt:$run_attempt,
          previous_application_revision:null,
          previous_release_identity:null,
          previous_source_revision:null,
          previous_images_json_sha256:null,
          previous_goose_versions:$versions
        }
      ' >"$output"
    rm -rf "$inspection"
    trap - EXIT HUP INT TERM
    return
  fi
  [ "$release_count" -eq 1 ] || release_fail "more than one application Helm release matched vermouth"

  helm --namespace vermouth history vermouth -o json >"$inspection/history.json" ||
    release_fail "cannot inspect the application Helm history"
  jq -e 'all(.[]; ((.status | startswith("pending-")) or .status == "uninstalling") | not)' \
    "$inspection/history.json" >/dev/null || release_fail "the application Helm history contains a pending operation"
  deployed_count=$(jq '[.[] | select(.status == "deployed")] | length' "$inspection/history.json")
  [ "$deployed_count" -eq 1 ] || release_fail "the application Helm history must contain exactly one deployed revision"
  previous_revision=$(jq '[.[] | select(.status == "deployed")][0].revision' "$inspection/history.json")
  values=$inspection/values.json
  helm --namespace vermouth get values vermouth --revision "$previous_revision" -o json >"$values" ||
    release_fail "cannot read the deployed application values"
  previous_identity=$(jq -r '.release.identity // empty' "$values")
  printf '%s' "$previous_identity" | grep -Eq '^[0-9a-f]{40}/[1-9][0-9]*-[1-9][0-9]*$' ||
    release_fail "the deployed revision has no valid immutable release identity"
  previous_release=/var/lib/vermouth/releases/$previous_identity
  [ -d "$previous_release" ] && [ ! -L "$previous_release" ] ||
    release_fail "the deployed revision release directory is missing"
  verify_release_evidence_checksum "$values" "$previous_release" imagesJSONSHA256 images.json ||
    release_fail "the deployed revision images.json checksum is invalid"
  verify_release_evidence_checksum "$values" "$previous_release" deploymentBaseSHA256 deployment-base.json ||
    release_fail "the deployed revision deployment base checksum is invalid"
  verify_release_evidence_checksum "$values" "$previous_release" migrationCompatEvidenceSHA256 migration-compat-evidence.json ||
    release_fail "the deployed revision migration evidence checksum is invalid"
  verify_release_evidence_checksum "$values" "$previous_release" rateLimitEvidenceSHA256 rate-limit-evidence.json ||
    release_fail "the deployed revision rate limit evidence checksum is invalid"
  verify_release_evidence_checksum "$values" "$previous_release" bundleManifestSHA256 bundle-manifest.json ||
    release_fail "the deployed revision bundle manifest checksum is invalid"
  jq -r '.secrets | .. | strings | select(length > 0)' "$values" | while IFS= read -r secret; do
    kubectl --namespace vermouth get secret "$secret" >/dev/null 2>&1 ||
      release_fail "the deployed revision runtime Secret $secret is missing"
  done
  previous_source=$(jq -r '.git_sha' "$previous_release/images.json")
  [ "${previous_identity%%/*}" = "$previous_source" ] ||
    release_fail "the deployed revision source identity does not match images.json"
  previous_images_sha=$(sha256sum "$previous_release/images.json" | awk '{print $1}')

  if [ -f /etc/vermouth/platform.json ]; then
    cached_revision=$(jq -r '.current_application_revision // empty' /etc/vermouth/platform.json)
    cached_identity=$(jq -r '.release_identity // empty' /etc/vermouth/platform.json)
    [ -z "$cached_revision" ] || [ "$cached_revision" = "$previous_revision" ] ||
      release_fail "platform identity disagrees with the deployed Helm revision"
    [ -z "$cached_identity" ] || [ "$cached_identity" = "$previous_identity" ] ||
      release_fail "platform identity disagrees with the deployed release identity"
  fi

  jq -S -c -n \
    --arg observed_at "$observed_at" --arg candidate "$candidate_git_sha" \
    --arg run_id "$github_run_id" --arg run_attempt "$github_run_attempt" \
    --argjson previous_revision "$previous_revision" --arg previous_identity "$previous_identity" \
    --arg previous_source "$previous_source" --arg previous_images_sha "$previous_images_sha" \
    --argjson versions "$versions" '
      {
        schema_version:1,
        observed_at:$observed_at,
        candidate_git_sha:$candidate,
        github_run_id:$run_id,
        github_run_attempt:$run_attempt,
        previous_application_revision:$previous_revision,
        previous_release_identity:$previous_identity,
        previous_source_revision:$previous_source,
        previous_images_json_sha256:$previous_images_sha,
        previous_goose_versions:$versions
      }
    ' >"$output"
  rm -rf "$inspection"
  trap - EXIT HUP INT TERM
)

inspect_deployment_base() {
  candidate_git_sha=$1
  github_run_id=$2
  github_run_attempt=$3
  production_lock_shared
  output=$(mktemp /var/tmp/vermouth-deployment-base.XXXXXX)
  trap 'rm -f "$output"' EXIT HUP INT TERM
  write_deployment_base_unlocked "$candidate_git_sha" "$github_run_id" "$github_run_attempt" "$output"
  vermouth-platformconfig validate-document /usr/local/share/vermouth/deployment-base.schema.json "$output"
  cat "$output"
}

verify_live_deployment_base() {
  base=$1
  live=$release_work/live-deployment-base.json
  write_deployment_base_unlocked \
    "$(jq -r '.candidate_git_sha' "$base")" \
    "$(jq -r '.github_run_id' "$base")" \
    "$(jq -r '.github_run_attempt' "$base")" \
    "$live"
  jq -S -c 'del(.observed_at)' "$base" >"$release_work/base-expected.json"
  jq -S -c 'del(.observed_at)' "$live" >"$release_work/base-live.json"
  cmp -s "$release_work/base-expected.json" "$release_work/base-live.json" ||
    release_fail "the live deployment base changed after migration compatibility testing"
}

install_release_directory() {
  incoming=$1
  git_sha=$2
  run_id=$3
  run_attempt=$4
  bundle_sha=$5
  parent=/var/lib/vermouth/releases/$git_sha
  final=$parent/$run_id-$run_attempt
  install -d -o root -g root -m 0755 "$parent"
  if [ -d "$final" ]; then
    [ -f "$final/.bundle-sha256" ] && [ "$(cat "$final/.bundle-sha256")" = "$bundle_sha" ] ||
      release_fail "the immutable release directory already exists with different content"
    diff -qr "$incoming" "$final" --exclude=.bundle-sha256 >/dev/null ||
      release_fail "the immutable release directory does not match the incoming bundle"
    rm -rf "$incoming"
    printf '%s\n' "$final"
    return
  fi
  mv "$incoming" "$final"
  printf '%s\n' "$bundle_sha" >"$final/.bundle-sha256"
  chmod -R a-w "$final"
  printf '%s\n' "$final"
}

validate_release_documents() {
  release=$1
  git_sha=$2
  run_id=$3
  run_attempt=$4
  share=/usr/local/share/vermouth
  vermouth-platformconfig validate-document "$share/images.schema.json" "$release/images.json"
  vermouth-platformconfig validate-document "$share/deployment-base.schema.json" "$release/deployment-base.json"
  vermouth-platformconfig validate-document "$share/rate-limit-evidence.schema.json" "$release/rate-limit-evidence.json"
  vermouth-platformconfig validate-document "$share/migration-compat-evidence.schema.json" "$release/migration-compat-evidence.json"
  for canonical in images.json deployment-base.json rate-limit-evidence.json migration-compat-evidence.json bundle-manifest.json; do
    jq -S -c . "$release/$canonical" >"$release_work/$canonical"
    cmp -s "$release/$canonical" "$release_work/$canonical" ||
      release_fail "$canonical is not canonical JSON"
  done
  jq -e --arg sha "$git_sha" --arg run "$run_id" --arg attempt "$run_attempt" '
    .git_sha == $sha and .github_run_id == $run and .github_run_attempt == $attempt
  ' "$release/images.json" >/dev/null || release_fail "images.json does not match the release identity"
  jq -e --arg sha "$git_sha" --arg run "$run_id" --arg attempt "$run_attempt" '
    .candidate_git_sha == $sha and .github_run_id == $run and .github_run_attempt == $attempt
  ' "$release/deployment-base.json" >/dev/null || release_fail "deployment base does not match the release identity"
  jq -e --arg sha "$git_sha" '.git_sha == $sha and .pass == true' "$release/rate-limit-evidence.json" >/dev/null ||
    release_fail "rate limit evidence does not authorize this source revision"
  base_sha=$(sha256sum "$release/deployment-base.json" | awk '{print $1}')
  jq -e --arg sha "$git_sha" --arg base_sha "$base_sha" \
    --slurpfile base "$release/deployment-base.json" '
      .git_sha == $sha and .pass == true and .deployment_base_sha256 == $base_sha and
      .previous_application_revision == $base[0].previous_application_revision and
      .previous_release_identity == $base[0].previous_release_identity and
      .previous_source_revision == $base[0].previous_source_revision and
      .previous_images_json_sha256 == $base[0].previous_images_json_sha256 and
      .previous_goose_versions == $base[0].previous_goose_versions
    ' "$release/migration-compat-evidence.json" >/dev/null ||
    release_fail "migration evidence does not authorize the inspected deployment base"
  images_sha=$(sha256sum "$release/images.json" | awk '{print $1}')
  grep -Fx "  identity: $git_sha/$run_id-$run_attempt" "$release/values-production.yaml" >/dev/null ||
    release_fail "release values contain the wrong identity"
  grep -Fx "  imagesJSONSHA256: $images_sha" "$release/values-production.yaml" >/dev/null ||
    release_fail "release values contain the wrong images.json checksum"
  grep -Fx "  deploymentBaseSHA256: $base_sha" "$release/values-production.yaml" >/dev/null ||
    release_fail "release values contain the wrong deployment base checksum"

  migration_sha=$(sha256sum "$release/migration-compat-evidence.json" | awk '{print $1}')
  rate_sha=$(sha256sum "$release/rate-limit-evidence.json" | awk '{print $1}')
  manifest_sha=$(sha256sum "$release/bundle-manifest.json" | awk '{print $1}')
  grep -Fx "  migrationCompatEvidenceSHA256: $migration_sha" "$release/values-production.yaml" >/dev/null ||
    release_fail "release values contain the wrong migration evidence checksum"
  grep -Fx "  rateLimitEvidenceSHA256: $rate_sha" "$release/values-production.yaml" >/dev/null ||
    release_fail "release values contain the wrong rate limit evidence checksum"
  release_evidence_values=$release_work/release-evidence-values.yaml
  {
    printf '%s\n' 'release:'
    printf '  imagesJSONSHA256: %s\n' "$images_sha"
    printf '  deploymentBaseSHA256: %s\n' "$base_sha"
    printf '  migrationCompatEvidenceSHA256: %s\n' "$migration_sha"
    printf '  rateLimitEvidenceSHA256: %s\n' "$rate_sha"
    printf '  bundleManifestSHA256: %s\n' "$manifest_sha"
  } >"$release_evidence_values"
}

dockerhub_token() {
  repository_path=$1
  output=$2
  login_config=$release_work/dockerhub-login.config
  {
    printf '%s\n' 'silent'
    printf '%s\n' 'show-error'
    printf '%s\n' 'fail'
    printf '%s\n' 'max-time = 30'
    printf 'user = "%s:%s"\n' "$DOCKERHUB_READ_USERNAME" "$DOCKERHUB_READ_TOKEN"
    printf 'url = "https://auth.docker.io/token?service=registry.docker.io&scope=repository:%s:pull"\n' "$repository_path"
  } >"$login_config"
  curl --config "$login_config" | jq -er '.token | strings | select(length > 0)' >"$output" ||
    release_fail "Docker Hub denied pull access to $repository_path"
}

registry_manifest() {
  repository_path=$1
  reference=$2
  token_file=$3
  body=$4
  headers=$5
  config=$release_work/registry.config
  token=$(cat "$token_file")
  {
    printf '%s\n' 'silent'
    printf '%s\n' 'show-error'
    printf '%s\n' 'fail'
    printf '%s\n' 'max-time = 30'
    printf 'header = "Authorization: Bearer %s"\n' "$token"
    printf '%s\n' 'header = "Accept: application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json"'
  } >"$config"
  curl --config "$config" --dump-header "$headers" --output "$body" \
    "https://registry-1.docker.io/v2/$repository_path/manifests/$reference"
}

verify_registry_images() {
  images=$1
  repository_source=$(jq -r '.images | to_entries[0].value.repository' "$images")
  namespace=${repository_source#docker.io/}
  namespace=${namespace%%/*}
  printf '%s' "$namespace" | grep -Eq '^[a-z0-9][a-z0-9_-]*$' || release_fail "the Docker Hub namespace is invalid"
  source_url=$(jq -r '.images | to_entries[0].value.source_revision' "$images")
  [ "$source_url" = "$(jq -r '.git_sha' "$images")" ] || release_fail "the image source revisions disagree"

  jq -r '.images | keys[]' "$images" | while IFS= read -r workload; do
    repository=$(jq -r --arg workload "$workload" '.images[$workload].repository' "$images")
    tag=$(jq -r --arg workload "$workload" '.images[$workload].tag' "$images")
    digest=$(jq -r --arg workload "$workload" '.images[$workload].digest' "$images")
    input_hash=$(jq -r --arg workload "$workload" '.images[$workload].build_input_sha256' "$images")
    revision=$(jq -r --arg workload "$workload" '.images[$workload].source_revision' "$images")
    created=$(jq -r --arg workload "$workload" '.images[$workload].created_at' "$images")
    expected_repository=docker.io/$namespace/vermouth-$workload
    [ "$repository" = "$expected_repository" ] || release_fail "$workload repository is not canonical"
    [ "$tag" = "git-${revision%${revision#????????????}}-$input_hash" ] || release_fail "$workload tag is not canonical"
    repository_path=${repository#docker.io/}
    token_file=$release_work/$workload.token
    dockerhub_token "$repository_path" "$token_file"
    root=$release_work/$workload-root.json
    headers=$release_work/$workload-root.headers
    registry_manifest "$repository_path" "$tag" "$token_file" "$root" "$headers"
    resolved=$(awk 'BEGIN {IGNORECASE=1} /^docker-content-digest:/ {gsub("\r", "", $2); print $2; exit}' "$headers")
    [ "$resolved" = "$digest" ] || release_fail "$workload tag resolves to $resolved instead of $digest"
    jq -e \
      --arg revision "$revision" --arg workload "$workload" --arg input "$input_hash" --arg created "$created" '
        .mediaType == "application/vnd.oci.image.index.v1+json" and
        (.manifests | length) == 2 and
        ([.manifests[].mediaType] | all(. == "application/vnd.oci.image.manifest.v1+json")) and
        ([.manifests[].platform | (.os + "/" + .architecture)] | sort == ["linux/amd64","linux/arm64"]) and
        (.annotations["org.opencontainers.image.source"] | test("^https://github.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$")) and
        .annotations["org.opencontainers.image.revision"] == $revision and
        .annotations["org.opencontainers.image.created"] == $created and
        .annotations["vermouth.dev/workload"] == $workload and
        .annotations["vermouth.dev/build-input-sha256"] == $input
      ' "$root" >/dev/null || release_fail "$workload root index provenance is invalid"
    source_annotation=$(jq -r '.annotations["org.opencontainers.image.source"]' "$root")
    jq -r '.manifests[].digest' "$root" | while IFS= read -r child_digest; do
      child=$release_work/$workload-${child_digest#sha256:}.json
      child_headers=$child.headers
      registry_manifest "$repository_path" "$child_digest" "$token_file" "$child" "$child_headers"
      child_resolved=$(awk 'BEGIN {IGNORECASE=1} /^docker-content-digest:/ {gsub("\r", "", $2); print $2; exit}' "$child_headers")
      [ "$child_resolved" = "$child_digest" ] ||
        release_fail "$workload child resolves to $child_resolved instead of $child_digest"
      jq -e \
        --arg source "$source_annotation" --arg revision "$revision" --arg workload "$workload" \
        --arg input "$input_hash" --arg created "$created" '
          .mediaType == "application/vnd.oci.image.manifest.v1+json" and
          .annotations["org.opencontainers.image.source"] == $source and
          .annotations["org.opencontainers.image.revision"] == $revision and
          .annotations["org.opencontainers.image.created"] == $created and
          .annotations["vermouth.dev/workload"] == $workload and
          .annotations["vermouth.dev/build-input-sha256"] == $input
        ' "$child" >/dev/null || release_fail "$workload child manifest provenance is invalid"
    done
  done
}

apply_secret() {
  name=$1
  component=$2
  values=$3
  label_component=${4:-$component}
  source_hash=$(vermouth-platformconfig secret-hash <"$values")
  if kubectl --namespace vermouth get secret "$name" >/dev/null 2>&1; then
    live_hash=$(kubectl --namespace vermouth get secret "$name" -o jsonpath='{.metadata.labels.vermouth\.dev/source-sha256}')
    [ "$live_hash" = "$source_hash" ] || release_fail "existing Secret $name has different source values"
    kubectl --namespace vermouth label secret "$name" --overwrite \
      "app.kubernetes.io/name=vermouth" \
      "app.kubernetes.io/component=$label_component" \
      "vermouth.dev/runtime-service=$component" \
      "vermouth.dev/source-sha256=$source_hash" >/dev/null
    return
  fi
  jq \
    --arg name "$name" --arg component "$component" --arg label_component "$label_component" --arg hash "$source_hash" '
      {
        apiVersion:"v1",
        kind:"Secret",
        metadata:{
          name:$name,
          namespace:"vermouth",
          labels:{
            "app.kubernetes.io/name":"vermouth",
            "app.kubernetes.io/component":$label_component,
            "vermouth.dev/runtime-service":$component,
            "vermouth.dev/source-sha256":$hash
          }
        },
        immutable:true,
        type:"Opaque",
        data:(with_entries(.value |= @base64))
      }
    ' "$values" | kubectl apply -f - >/dev/null
}

content_named_secret() {
  base=$1
  component=$2
  values=$3
  source_hash=$(vermouth-platformconfig secret-hash <"$values")
  name=$base-${source_hash%${source_hash#????????????}}
  apply_secret "$name" "$component" "$values" runtime-secret
  printf '%s\n' "$name"
}

create_release_secrets() {
  env_file=/etc/vermouth/production.env
  [ -f "$env_file" ] && [ "$(stat -c '%U:%G:%a' "$env_file")" = root:root:600 ] ||
    release_fail "$env_file must be root owned with mode 0600"
  set -a
  . "$env_file"
  set +a
  for name in POSTGRES_PASSWORD TOKEN_PUBLIC_KEYS IDENTITY_TOKEN_KID IDENTITY_TOKEN_PRIVATE_KEY \
    IDENTITY_GOOGLE_CLIENT_ID IDENTITY_GOOGLE_CLIENT_SECRET GARAGE_RPC_SECRET GARAGE_ADMIN_TOKEN \
    BILLING_S3_ACCESS_KEY_ID BILLING_S3_SECRET_ACCESS_KEY DOCKERHUB_READ_USERNAME DOCKERHUB_READ_TOKEN; do
    eval "value=\${$name:-}"
    [ -n "$value" ] || release_fail "$env_file is missing $name"
  done

  foundation=$release_work/foundation.json
  jq -n '{POSTGRES_PASSWORD:env.POSTGRES_PASSWORD}' >"$foundation"
  for service in identity teaching billing notifications; do
    apply_secret "postgres-$service-bootstrap" "postgres-$service" "$foundation"
  done
  jq -n '{GARAGE_RPC_SECRET:env.GARAGE_RPC_SECRET,GARAGE_ADMIN_TOKEN:env.GARAGE_ADMIN_TOKEN}' >"$foundation"
  apply_secret garage-bootstrap garage "$foundation"
  jq -n '{BILLING_S3_ACCESS_KEY_ID:env.BILLING_S3_ACCESS_KEY_ID,BILLING_S3_SECRET_ACCESS_KEY:env.BILLING_S3_SECRET_ACCESS_KEY}' >"$foundation"
  apply_secret billing-s3-bootstrap billing-s3 "$foundation"

  jq -n '{TOKEN_PUBLIC_KEYS:env.TOKEN_PUBLIC_KEYS}' >"$release_work/gateway.json"
  gateway_secret=$(content_named_secret gateway-runtime gateway "$release_work/gateway.json")
  encoded_password=$(jq -nr 'env.POSTGRES_PASSWORD | @uri')

  identity_url=postgres://vermouth_identity:$encoded_password@postgres-identity:5432/vermouth_identity?sslmode=disable
  IDENTITY_DATABASE_URL=$identity_url jq -n '{IDENTITY_DATABASE_URL:env.IDENTITY_DATABASE_URL,TOKEN_PUBLIC_KEYS:env.TOKEN_PUBLIC_KEYS,IDENTITY_TOKEN_KID:env.IDENTITY_TOKEN_KID,IDENTITY_TOKEN_PRIVATE_KEY:env.IDENTITY_TOKEN_PRIVATE_KEY,IDENTITY_GOOGLE_CLIENT_ID:env.IDENTITY_GOOGLE_CLIENT_ID,IDENTITY_GOOGLE_CLIENT_SECRET:env.IDENTITY_GOOGLE_CLIENT_SECRET}' >"$release_work/identity.json"
  identity_secret=$(content_named_secret identity-runtime identity "$release_work/identity.json")

  teaching_url=postgres://vermouth_teaching:$encoded_password@postgres-teaching:5432/vermouth_teaching?sslmode=disable
  TEACHING_DATABASE_URL=$teaching_url jq -n '{TEACHING_DATABASE_URL:env.TEACHING_DATABASE_URL,TOKEN_PUBLIC_KEYS:env.TOKEN_PUBLIC_KEYS}' >"$release_work/teaching.json"
  teaching_secret=$(content_named_secret teaching-runtime teaching "$release_work/teaching.json")

  billing_url=postgres://vermouth_billing:$encoded_password@postgres-billing:5432/vermouth_billing?sslmode=disable
  BILLING_DATABASE_URL=$billing_url jq -n '{BILLING_DATABASE_URL:env.BILLING_DATABASE_URL,TOKEN_PUBLIC_KEYS:env.TOKEN_PUBLIC_KEYS,BILLING_S3_ACCESS_KEY_ID:env.BILLING_S3_ACCESS_KEY_ID,BILLING_S3_SECRET_ACCESS_KEY:env.BILLING_S3_SECRET_ACCESS_KEY}' >"$release_work/billing.json"
  billing_secret=$(content_named_secret billing-runtime billing "$release_work/billing.json")

  notifications_url=postgres://vermouth_notifications:$encoded_password@postgres-notifications:5432/vermouth_notifications?sslmode=disable
  NOTIFICATIONS_DATABASE_URL=$notifications_url jq -n '{NOTIFICATIONS_DATABASE_URL:env.NOTIFICATIONS_DATABASE_URL,TOKEN_PUBLIC_KEYS:env.TOKEN_PUBLIC_KEYS}' >"$release_work/notifications.json"
  notifications_secret=$(content_named_secret notifications-runtime notifications "$release_work/notifications.json")

  secrets_values=$release_work/secrets-values.yaml
  {
    printf '%s\n' 'secrets:'
    printf '  gateway: %s\n' "$gateway_secret"
    printf '  identity: %s\n' "$identity_secret"
    printf '  teaching: %s\n' "$teaching_secret"
    printf '  billing: %s\n' "$billing_secret"
    printf '  notifications: %s\n' "$notifications_secret"
  } >"$secrets_values"
}

write_platform_values() {
  platform=/etc/vermouth/platform.json
  [ -f "$platform" ] || release_fail "the platform identity is missing"
  storage_root=$(jq -r '.storage_root' "$platform")
  marker=$storage_root/.vermouth-storage
  [ -f "$marker" ] || release_fail "the locked storage marker is missing"
  marker_hash=$(cat "$marker")
  printf '%s' "$marker_hash" | grep -Eq '^[0-9a-f]{64}$' || release_fail "the locked storage marker is invalid"
  platform_values=$release_work/platform-values.yaml
  {
    printf '%s\n' 'storage:'
    printf '  rootPath: %s\n' "$storage_root"
    printf '  markerSHA256: %s\n' "$marker_hash"
    printf '%s\n' '  guard:'
    printf '%s\n' '    enabled: true'
  } >"$platform_values"
}

previous_application_revision() {
  if ! helm --namespace vermouth status vermouth >/dev/null 2>&1; then
    printf '%s\n' null
    return
  fi
  helm --namespace vermouth history vermouth -o json | jq -r '
    [.[] | select(.status == "deployed" or .status == "superseded") | .revision] | max // null
  '
}

wait_for_internal_release() {
  for deployment in gateway identity teaching billing notifications web; do
    kubectl --namespace vermouth rollout status "deployment/$deployment" --timeout=5m >/dev/null
  done
  for stateful in postgres-identity postgres-teaching postgres-billing postgres-notifications redpanda garage; do
    kubectl --namespace vermouth rollout status "statefulset/$stateful" --timeout=5m >/dev/null
  done
}

production_pod_usage() {
  metrics=$release_work/pod-usage.tsv
  : >"$metrics"
  for namespace in vermouth kube-system; do
    raw=$release_work/pod-usage-$namespace.txt
    kubectl top pod --namespace "$namespace" --no-headers >"$raw" 2>/dev/null ||
      release_fail "kubectl top cannot sample $namespace Pods for the capacity gate"
    awk -v namespace="$namespace" 'NF == 3 {printf "%s\t%s\t%s\t%s\n", namespace, $1, $2, $3}' \
      "$raw" >>"$metrics"
  done
  [ -s "$metrics" ] || release_fail "kubectl top returned no Vermouth or k3s Pod samples"
  LC_ALL=C sort -o "$metrics" "$metrics"
  jq -Rsc '
    split("\n") |
    map(select(length > 0) | split("\t") | {
      namespace:.[0], pod:.[1], cpu:.[2], memory:.[3]
    })
  ' "$metrics"
}

measure_release_capacity() {
  run_id=$1
  reports=/var/lib/vermouth/reports
  install -d -o root -g root -m 0755 "$reports"
  samples=$release_work/resource-samples.jsonl
  : >"$samples"
  sample=1
  while [ "$sample" -le 61 ]; do
    total_kib=$(awk '/^MemTotal:/ {print $2}' /proc/meminfo)
    available_kib=$(awk '/^MemAvailable:/ {print $2}' /proc/meminfo)
    pod_usage=$(production_pod_usage)
    jq -n --argjson sample "$sample" --argjson total "$total_kib" --argjson available "$available_kib" --argjson pods "$pod_usage" \
      '{sample:$sample,total_kib:$total,available_kib:$available,used_kib:($total-$available),pods:$pods}' >>"$samples"
    [ "$sample" -eq 61 ] || sleep 5
    sample=$((sample + 1))
  done
  report=$reports/resource-$run_id.json
  jq -S -c -s '
    (map(.used_kib) | max) as $maximum |
    {
      schema_version:1,
      interval_seconds:5,
      duration_seconds:300,
      maximum_total_memory_kib:$maximum,
      average_total_memory_kib:(map(.used_kib) | add / length),
      limit_total_memory_kib:7340032,
      pass:($maximum < 7340032),
      samples:.
    }
  ' "$samples" >"$report"
  jq -e '.pass == true' "$report" >/dev/null || return 1
  printf '%s\n' "$report"
}

wait_for_external_release() {
  hostname=$1
  deadline=$(( $(date +%s) + 300 ))
  while :; do
    if curl -fsS --max-time 10 "https://$hostname/health" >/dev/null &&
      curl -fsS --max-time 10 "https://$hostname/ready" >/dev/null &&
      curl -fsS --max-time 10 "https://$hostname/config.json" |
        jq -e '.schemaVersion == 1 and .environment == "production" and .googleAuthEnabled == true and .apiBasePath == "/api"' >/dev/null; then
      return
    fi
    [ "$(date +%s)" -lt "$deadline" ] || return 1
    sleep 5
  done
}

record_recovered_application_revision() {
  current_revision=$1
  next=/etc/vermouth/platform.json.next
  jq --argjson current "$current_revision" '.current_application_revision = $current' \
    /etc/vermouth/platform.json >"$next"
  chmod 0600 "$next"
  vermouth-platformconfig atomic-commit "$next" /etc/vermouth/platform.json
  kubectl --namespace vermouth create configmap vermouth-platform-identity \
    --from-file=platform.json=/etc/vermouth/platform.json --dry-run=client -o yaml | kubectl apply -f - >/dev/null
}

recover_application_release() {
  previous_revision=$1
  hostname=$2
  candidate_was_public=$3
  if [ "$previous_revision" = null ]; then
    [ "$candidate_was_public" = true ] || return 0
    if helm upgrade vermouth "$release/chart" --namespace vermouth \
      $values --set application.enabled=true --set ingress.enabled=false \
      --atomic --wait --timeout 10m --history-max 5 >/dev/null; then
      return 0
    fi
    kubectl --namespace vermouth delete ingress vermouth --ignore-not-found --wait=true >/dev/null 2>&1 || true
    return 1
  fi

  if ! helm --namespace vermouth rollback vermouth "$previous_revision" --wait --timeout 10m >/dev/null; then
    kubectl --namespace vermouth delete ingress vermouth --ignore-not-found --wait=true >/dev/null 2>&1 || true
    return 1
  fi
  if ! wait_for_external_release "$hostname"; then
    kubectl --namespace vermouth delete ingress vermouth --ignore-not-found --wait=true >/dev/null 2>&1 || true
    return 1
  fi
  recovered_revision=$(helm --namespace vermouth history vermouth -o json |
    jq '[.[] | select(.status == "deployed") | .revision] | max')
  [ "$recovered_revision" != null ] || return 1
  record_recovered_application_revision "$recovered_revision"
}

write_migration_report() {
  report=$1
  release_identity=$2
  started_at=$3
  status=$4
  previous_versions=$5
  candidate_versions=$6
  reached_versions=$(live_goose_versions)
  finished_at=null
  [ "$status" = running ] || finished_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  next=$report.next
  jq -S -c -n \
    --arg release_identity "$release_identity" --arg started_at "$started_at" \
    --arg status "$status" --arg finished_at "$finished_at" \
    --argjson previous "$previous_versions" --argjson candidate "$candidate_versions" \
    --argjson reached "$reached_versions" '
      {
        schema_version:1,
        release_identity:$release_identity,
        started_at:$started_at,
        finished_at:(if $finished_at == "null" then null else $finished_at end),
        previous_goose_versions:$previous,
        candidate_goose_versions:$candidate,
        reached_goose_versions:$reached,
        status:$status
      }
    ' >"$next"
  chmod 0600 "$next"
  vermouth-platformconfig atomic-commit "$next" "$report"
}

garbage_collect_runtime_secrets() {
  current_revision=$1
  previous_revision=$2
  references=$release_work/runtime-secret-references
  : >"$references"
  for revision in "$current_revision" "$previous_revision"; do
    [ "$revision" != null ] || continue
    helm --namespace vermouth get values vermouth --revision "$revision" -o json |
      jq -r '.secrets | .. | strings | select(length > 0)' >>"$references"
  done
  LC_ALL=C sort -u "$references" -o "$references"
  kubectl --namespace vermouth get secrets \
    -l app.kubernetes.io/name=vermouth,app.kubernetes.io/component=runtime-secret \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' |
    while IFS= read -r secret; do
      [ -n "$secret" ] || continue
      grep -Fx "$secret" "$references" >/dev/null ||
        kubectl --namespace vermouth delete secret "$secret" --wait=true >/dev/null
    done
}

record_platform_release() {
  release=$1
  previous=$2
  current_revision=$3
  images=$release/images.json
  base=$release/deployment-base.json
  migration=$release/migration-compat-evidence.json
  next=/etc/vermouth/platform.json.next
  jq \
    --arg git_sha "$(jq -r '.git_sha' "$images")" \
    --arg run_id "$(jq -r '.github_run_id' "$images")" \
    --arg run_attempt "$(jq -r '.github_run_attempt' "$images")" \
    --arg chart_sha "$(jq -r '.chart_sha256' "$images")" \
    --arg images_sha "$(sha256sum "$images" | awk '{print $1}')" \
    --arg base_sha "$(sha256sum "$base" | awk '{print $1}')" \
    --arg migration_sha "$(sha256sum "$migration" | awk '{print $1}')" \
    --arg rate_sha "$(sha256sum "$release/rate-limit-evidence.json" | awk '{print $1}')" \
    --arg manifest_sha "$(sha256sum "$release/bundle-manifest.json" | awk '{print $1}')" \
    --argjson goose_versions "$(live_goose_versions)" \
    --argjson current "$current_revision" --argjson previous "$previous" \
    --argjson digests "$(jq '[.images[].digest] | sort | unique' "$images")" '
      .current_git_sha = $git_sha |
      .github_run_id = $run_id |
      .github_run_attempt = $run_attempt |
      .release_identity = ($git_sha + "/" + $run_id + "-" + $run_attempt) |
      .chart_sha256 = $chart_sha |
      .images_json_sha256 = $images_sha |
      .deployment_base_sha256 = $base_sha |
      .migration_compat_evidence_sha256 = $migration_sha |
      .rate_limit_evidence_sha256 = $rate_sha |
      .bundle_manifest_sha256 = $manifest_sha |
      .goose_versions = $goose_versions |
      .current_application_revision = $current |
      .previous_deployable_revision = $previous |
      .active_image_digest_set = $digests
    ' /etc/vermouth/platform.json >"$next"
  chmod 0600 "$next"
  vermouth-platformconfig atomic-commit "$next" /etc/vermouth/platform.json
  kubectl --namespace vermouth create configmap vermouth-platform-identity \
    --from-file=platform.json=/etc/vermouth/platform.json --dry-run=client -o yaml | kubectl apply -f - >/dev/null
}

deploy_release() (
  release=$1
  git_sha=$2
  run_id=$3
  run_attempt=$4
  for tool in awk cmp curl diff flock helm install jq kubectl sha256sum sort stat; do
    release_need "$tool"
  done
  release_work=$(mktemp -d /var/tmp/vermouth-release.XXXXXX)
  export release_work
  trap 'rm -rf "$release_work"' EXIT HUP INT TERM
  validate_release_documents "$release" "$git_sha" "$run_id" "$run_attempt"
  verify_live_deployment_base "$release/deployment-base.json"
  verify_registry_images "$release/images.json"
  create_release_secrets
  write_platform_values

  values="--values $release/chart/values-production.yaml --values $release/values-production.yaml --values $release_evidence_values --values $secrets_values --values $platform_values"
  helm upgrade --install vermouth-foundation "$release/chart" --namespace vermouth --create-namespace \
    $values --set foundation.enabled=true --set foundation.bootstrapStorage=false \
    --atomic --wait --timeout 10m --history-max 5 >/dev/null

  job_id=$(production_job_run_id "$run_id" "$run_attempt")
  reports=/var/lib/vermouth/reports
  install -d -o root -g root -m 0755 "$reports"
  migration_report=$reports/migration-$run_id-$run_attempt.json
  migration_started=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  previous_versions=$(jq '.previous_goose_versions' "$release/deployment-base.json")
  candidate_versions=$(jq '.candidate_goose_versions' "$release/migration-compat-evidence.json")
  write_migration_report \
    "$migration_report" "$git_sha/$run_id-$run_attempt" "$migration_started" running \
    "$previous_versions" "$candidate_versions"
  helm upgrade --install vermouth-jobs "$release/chart" --namespace vermouth \
    $values --set-string jobs.runID="$job_id" --set jobs.migrations.enabled=true --set jobs.garageInit.enabled=true \
    --wait --wait-for-jobs --timeout 15m --history-max 5 >/dev/null || {
      write_migration_report \
        "$migration_report" "$git_sha/$run_id-$run_attempt" "$migration_started" failed \
        "$previous_versions" "$candidate_versions"
      release_fail "a migration or Garage initialization Job failed and its logs were retained"
    }
  write_migration_report \
    "$migration_report" "$git_sha/$run_id-$run_attempt" "$migration_started" passed \
    "$previous_versions" "$candidate_versions"

  previous=$(previous_application_revision)
  helm upgrade --install vermouth "$release/chart" --namespace vermouth \
    $values --set application.enabled=true --set ingress.enabled=false \
    --atomic --wait --timeout 10m --history-max 5 >/dev/null
  wait_for_internal_release
  if ! resource_report=$(measure_release_capacity "$run_id-$run_attempt"); then
    hostname=$(jq -r '.hostname' /etc/vermouth/platform.json)
    if recover_application_release "$previous" "$hostname" false; then
      release_fail "the five minute capacity gate failed. The previous application was restored or public traffic remained closed."
    fi
    release_fail "the five minute capacity gate failed. Recovery also failed, so public traffic was closed."
  fi

  helm upgrade vermouth "$release/chart" --namespace vermouth \
    $values --set application.enabled=true --set ingress.enabled=true \
    --atomic --wait --timeout 10m --history-max 5 >/dev/null
  hostname=$(jq -r '.hostname' /etc/vermouth/platform.json)
  if ! wait_for_external_release "$hostname"; then
    if recover_application_release "$previous" "$hostname" true; then
      release_fail "external HTTPS readiness failed. The previous application was restored or public traffic was closed."
    fi
    release_fail "external HTTPS readiness failed. Recovery also failed, so public traffic was closed."
  fi
  current=$(helm --namespace vermouth history vermouth -o json | jq '[.[] | select(.status == "deployed") | .revision] | max')
  record_platform_release "$release" "$previous" "$current"
  garbage_collect_runtime_secrets "$current" "$previous"
  printf '%s\n' "Production ready at https://$hostname"
  printf '%s\n' "Git SHA: $git_sha"
  printf '%s\n' "GitHub run: $run_id attempt $run_attempt"
  jq -r '.images | to_entries[] | "Image digest: \(.key) \(.value.repository)@\(.value.digest)"' \
    "$release/images.json"
  printf '%s\n' "Application Helm revision: $current"
  printf '%s\n' "Capacity report: $resource_report"
  printf 'RESOURCE_REPORT_JSON='
  cat "$resource_report"
)
