#!/bin/sh
set -eu
umask 077

bundle_fail() {
  printf 'production bundle: %s\n' "$*" >&2
  exit 1
}

[ "$#" -eq 3 ] || bundle_fail "usage: release-bundle.sh <images.json> <deployment-base.json> <release.tar.zst>"
images_input=$1
base_input=$2
output=$3
for tool in cp find go helm jq mktemp sha256sum tar zstd; do
  command -v "$tool" >/dev/null 2>&1 || bundle_fail "$tool is required"
done
[ -n "${PROD_HOSTNAME:-}" ] || bundle_fail "PROD_HOSTNAME is required"

root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
production=$root/deploy/production
rate_evidence=$root/.tmp/production/rate-limit-evidence.json
migration_evidence=$root/.tmp/production/migration-compat-evidence.json
for file in "$images_input" "$base_input" "$rate_evidence" "$migration_evidence"; do
  [ -f "$file" ] && [ ! -L "$file" ] || bundle_fail "$file is missing or unsafe"
done

platform_config() {
  (
    cd "$root/pkg/vermouth"
    GOWORK=off go run ./cmd/platformconfig "$@"
  )
}
platform_config validate-document "$production/images.schema.json" "$images_input"
platform_config validate-document "$production/deployment-base.schema.json" "$base_input"
platform_config validate-document "$production/rate-limit-evidence.schema.json" "$rate_evidence"
platform_config validate-document "$production/migration-compat-evidence.schema.json" "$migration_evidence"

git_sha=$(git -C "$root" rev-parse --verify HEAD^{commit})
[ "$(jq -r '.git_sha' "$images_input")" = "$git_sha" ] || bundle_fail "images.json does not match the checked out Git SHA"
[ "$(jq -r '.candidate_git_sha' "$base_input")" = "$git_sha" ] || bundle_fail "deployment base does not match the checked out Git SHA"
[ "$(jq -r '.git_sha' "$rate_evidence")" = "$git_sha" ] || bundle_fail "rate limit evidence does not match the checked out Git SHA"
[ "$(jq -r '.git_sha' "$migration_evidence")" = "$git_sha" ] || bundle_fail "migration evidence does not match the checked out Git SHA"
chart_hash=$(platform_config tree-sha256 "$root/deploy/helm/vermouth")
[ "$(jq -r '.chart_sha256' "$images_input")" = "$chart_hash" ] || bundle_fail "images.json does not match the committed chart"

work=$(mktemp -d)
cleanup_bundle() {
  rm -rf "$work"
}
trap cleanup_bundle EXIT HUP INT TERM
bundle=$work/bundle
mkdir -p "$bundle/chart"
cp -R "$root/deploy/helm/vermouth"/. "$bundle/chart/"
cp "$images_input" "$bundle/images.json"
cp "$base_input" "$bundle/deployment-base.json"
cp "$rate_evidence" "$bundle/rate-limit-evidence.json"
cp "$migration_evidence" "$bundle/migration-compat-evidence.json"

images_sha=$(sha256sum "$bundle/images.json" | awk '{print $1}')
base_sha=$(sha256sum "$bundle/deployment-base.json" | awk '{print $1}')
migration_sha=$(sha256sum "$bundle/migration-compat-evidence.json" | awk '{print $1}')
rate_sha=$(sha256sum "$bundle/rate-limit-evidence.json" | awk '{print $1}')
run_id=$(jq -r '.github_run_id' "$bundle/images.json")
run_attempt=$(jq -r '.github_run_attempt' "$bundle/images.json")
identity=$git_sha/$run_id-$run_attempt
values=$bundle/values-production.yaml
{
  printf '%s\n' 'release:'
  printf '  gitSHA: %s\n' "$git_sha"
  printf '  githubRunID: "%s"\n' "$run_id"
  printf '  githubRunAttempt: "%s"\n' "$run_attempt"
  printf '  identity: %s\n' "$identity"
  printf '  imagesJSONSHA256: %s\n' "$images_sha"
  printf '  deploymentBaseSHA256: %s\n' "$base_sha"
  printf '  migrationCompatEvidenceSHA256: %s\n' "$migration_sha"
  printf '  rateLimitEvidenceSHA256: %s\n' "$rate_sha"
  printf '%s\n' '  bundleManifestSHA256: ""'
  printf '%s\n' 'ingress:'
  printf '  hostname: %s\n' "$PROD_HOSTNAME"
  printf '%s\n' 'config:'
  printf '  identityAppURL: https://%s\n' "$PROD_HOSTNAME"
  printf '  identityGoogleRedirectURL: https://%s/api/auth/google/callback\n' "$PROD_HOSTNAME"
  printf '%s\n' 'images:'
  for workload in gateway identity teaching billing notifications web garage-init; do
    key=$workload
    [ "$workload" != garage-init ] || key=garageInit
    printf '  %s: %s@%s\n' "$key" \
      "$(jq -r --arg name "$workload" '.images[$name].repository' "$bundle/images.json")" \
      "$(jq -r --arg name "$workload" '.images[$name].digest' "$bundle/images.json")"
  done
  printf '%s\n' '  migrations:'
  for service in identity teaching billing notifications; do
    workload=$service-migration
    printf '    %s: %s@%s\n' "$service" \
      "$(jq -r --arg name "$workload" '.images[$name].repository' "$bundle/images.json")" \
      "$(jq -r --arg name "$workload" '.images[$name].digest' "$bundle/images.json")"
  done
} >"$values"

helm lint "$bundle/chart" --values "$bundle/chart/values-production.yaml" --values "$values" >/dev/null
manifest=$bundle/bundle-manifest.json
files=$work/files.json
printf '{}\n' >"$files"
find "$bundle" -type f ! -name bundle-manifest.json -print | LC_ALL=C sort | while IFS= read -r file; do
  relative=${file#"$bundle/"}
  hash=$(sha256sum "$file" | awk '{print $1}')
  next=$files.next
  jq --arg name "$relative" --arg hash "$hash" '. + {($name):$hash}' "$files" >"$next"
  mv "$next" "$files"
done
jq -S -c -n --argjson files "$(cat "$files")" '{schema_version:1,files:$files}' >"$manifest"

next_output=$output.next
tar -C "$bundle" -cf - . | zstd -q -T0 -19 -o "$next_output"
zstd -q -t "$next_output"
mv "$next_output" "$output"
printf '%s\n' "$identity"
