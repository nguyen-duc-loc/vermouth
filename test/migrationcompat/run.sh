#!/bin/sh
set -eu
umask 077

compat_fail() {
  printf 'migration compatibility: %s\n' "$*" >&2
  exit 1
}

[ "$#" -eq 1 ] || compat_fail "usage: task test:migration-compat -- deployment-base.json"
base_input=$1
for tool in awk basename cmp docker find git go goose jq mktemp shasum sort task tr; do
  command -v "$tool" >/dev/null 2>&1 || compat_fail "$tool is required"
done
[ -f "$base_input" ] && [ ! -L "$base_input" ] || compat_fail "$base_input is missing or unsafe"

root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
production=$root/deploy/production
output=$root/.tmp/production/migration-compat-evidence.json
work=$(mktemp -d)
previous_worktree=
databases=

cleanup_compatibility() {
  for service in identity teaching billing notifications; do
    database=$(printf '%s\n' "$databases" | awk -F: -v service="$service" '$1 == service {print $2}')
    [ -z "$database" ] || docker exec "vermouth-postgres-$service" \
      dropdb --if-exists --force -U "vermouth_$service" "$database" >/dev/null 2>&1 || true
  done
  [ -z "$previous_worktree" ] || git -C "$root" worktree remove --force "$previous_worktree" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup_compatibility EXIT HUP INT TERM

platform_config() {
  (
    cd "$root/pkg/vermouth"
    GOWORK=off go run ./cmd/platformconfig "$@"
  )
}

platform_config validate-document "$production/deployment-base.schema.json" "$base_input"
jq -S -c . "$base_input" >"$work/base-canonical.json"
cmp -s "$base_input" "$work/base-canonical.json" || compat_fail "deployment-base.json is not canonical JSON"

git_sha=$(git -C "$root" rev-parse --verify HEAD^{commit})
[ "$(jq -r '.candidate_git_sha' "$base_input")" = "$git_sha" ] ||
  compat_fail "deployment-base.json does not name the checked out Git SHA"
if [ -n "${GITHUB_RUN_ID:-}" ]; then
  [ "$(jq -r '.github_run_id' "$base_input")" = "$GITHUB_RUN_ID" ] ||
    compat_fail "deployment-base.json does not name this GitHub run"
fi
if [ -n "${GITHUB_RUN_ATTEMPT:-}" ]; then
  [ "$(jq -r '.github_run_attempt' "$base_input")" = "$GITHUB_RUN_ATTEMPT" ] ||
    compat_fail "deployment-base.json does not name this GitHub run attempt"
fi

for migration in "$root"/services/*/db/migrations/*.sql; do
  grep -Eiq '^--[[:space:]]+\+goose[[:space:]]+NO[[:space:]]+TRANSACTION([[:space:]]|$)' "$migration" &&
    compat_fail "production migration ${migration#"$root/"} disables transactions"
done
migration_set_sha256=$(platform_config production-migration-set-sha256 "$root")

candidate_version() {
  service=$1
  find "$root/services/$service/db/migrations" -type f -name '*.sql' -exec basename {} \; |
    awk -F_ '$1 ~ /^[0-9]+$/ {version=$1+0} version > maximum {maximum=version} END {print maximum+0}'
}

candidate_versions=$(jq -n \
  --argjson identity "$(candidate_version identity)" \
  --argjson teaching "$(candidate_version teaching)" \
  --argjson billing "$(candidate_version billing)" \
  --argjson notifications "$(candidate_version notifications)" \
  '{identity:$identity,teaching:$teaching,billing:$billing,notifications:$notifications}')

task infra:up >/dev/null
run_key=${GITHUB_RUN_ID:-local$$}
run_key=$(printf '%s' "$run_key" | tr -cd 'A-Za-z0-9_')
for service in identity teaching billing notifications; do
  database=vermouth_compat_${run_key}_$service
  databases=$(printf '%s\n%s:%s' "$databases" "$service" "$database")
  docker exec "vermouth-postgres-$service" dropdb --if-exists --force \
    -U "vermouth_$service" "$database" >/dev/null
  docker exec "vermouth-postgres-$service" createdb -U "vermouth_$service" "$database"
  case "$service" in
    identity) IDENTITY_DATABASE_URL="postgres://vermouth_identity:vermouth@localhost:5433/$database?sslmode=disable" ;;
    teaching) TEACHING_DATABASE_URL="postgres://vermouth_teaching:vermouth@localhost:5434/$database?sslmode=disable" ;;
    billing) BILLING_DATABASE_URL="postgres://vermouth_billing:vermouth@localhost:5435/$database?sslmode=disable" ;;
    notifications) NOTIFICATIONS_DATABASE_URL="postgres://vermouth_notifications:vermouth@localhost:5436/$database?sslmode=disable" ;;
  esac
done
export IDENTITY_DATABASE_URL TEACHING_DATABASE_URL BILLING_DATABASE_URL NOTIFICATIONS_DATABASE_URL

service_url() {
  case "$1" in
    identity) printf '%s\n' "$IDENTITY_DATABASE_URL" ;;
    teaching) printf '%s\n' "$TEACHING_DATABASE_URL" ;;
    billing) printf '%s\n' "$BILLING_DATABASE_URL" ;;
    notifications) printf '%s\n' "$NOTIFICATIONS_DATABASE_URL" ;;
    *) compat_fail "unknown migration service $1" ;;
  esac
}

apply_to_version() {
  source_root=$1
  service=$2
  version=$3
  [ "$version" -eq 0 ] || goose -dir "$source_root/services/$service/db/migrations" \
    postgres "$(service_url "$service")" up-to "$version" >/dev/null
}

run_service_tests() {
  source_root=$1
  service=$2
  (
    cd "$source_root/services/$service"
    GOWORK="$source_root/go.work" go test ./...
  )
}

previous_source=$(jq -r '.previous_source_revision // empty' "$base_input")
if [ -n "$previous_source" ]; then
  git -C "$root" cat-file -e "$previous_source^{commit}" 2>/dev/null ||
    compat_fail "previous source revision $previous_source is unavailable"
  previous_worktree=$work/previous
  git -C "$root" worktree add --detach "$previous_worktree" "$previous_source" >/dev/null

  for service in identity teaching billing notifications; do
    previous_version=$(jq -r --arg service "$service" '.previous_goose_versions[$service]' "$base_input")
    current_version=$(printf '%s' "$candidate_versions" | jq -r --arg service "$service" '.[$service]')
    [ "$current_version" -ge "$previous_version" ] ||
      compat_fail "$service candidate migrations end before the deployed Goose version"
    for previous_migration in "$previous_worktree/services/$service/db/migrations"/*.sql; do
      name=$(basename "$previous_migration")
      version=${name%%_*}
      printf '%s' "$version" | grep -Eq '^[0-9]+$' || compat_fail "migration $name has no numeric version"
      [ "$version" -le "$previous_version" ] || continue
      candidate_migration=$root/services/$service/db/migrations/$name
      [ -f "$candidate_migration" ] && cmp -s "$previous_migration" "$candidate_migration" ||
        compat_fail "$service migration $name changed after deployment"
    done
    for candidate_migration in "$root/services/$service/db/migrations"/*.sql; do
      name=$(basename "$candidate_migration")
      version=${name%%_*}
      [ "$version" -le "$previous_version" ] || continue
      [ -f "$previous_worktree/services/$service/db/migrations/$name" ] ||
        compat_fail "$service migration $name was inserted before the deployed Goose version"
    done
    apply_to_version "$previous_worktree" "$service" "$previous_version"
  done

  for service in identity teaching billing notifications; do
    previous_version=$(jq -r --arg service "$service" '.previous_goose_versions[$service]' "$base_input")
    for candidate_migration in $(find "$root/services/$service/db/migrations" -type f -name '*.sql' | LC_ALL=C sort); do
      name=$(basename "$candidate_migration")
      version=${name%%_*}
      [ "$version" -gt "$previous_version" ] || continue
      apply_to_version "$root" "$service" "$version"
      run_service_tests "$previous_worktree" "$service"
    done
  done
else
  for service in identity teaching billing notifications; do
    version=$(printf '%s' "$candidate_versions" | jq -r --arg service "$service" '.[$service]')
    apply_to_version "$root" "$service" "$version"
  done
fi

for service in identity teaching billing notifications; do
  run_service_tests "$root" "$service"
done

mkdir -p -m 0700 "$root/.tmp/production"
chmod 0700 "$root/.tmp/production"
base_sha256=$(shasum -a 256 "$base_input" | awk '{print $1}')
temporary=$(mktemp "$root/.tmp/production/migration-compat-evidence.XXXXXX")
trap 'rm -f "$temporary"; cleanup_compatibility' EXIT HUP INT TERM
jq -S -c \
  --arg git_sha "$git_sha" \
  --arg deployment_base_sha256 "$base_sha256" \
  --arg migration_set_sha256 "$migration_set_sha256" \
  --arg timestamp "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --argjson candidate_goose_versions "$candidate_versions" '
    {
      schema_version:1,
      git_sha:$git_sha,
      deployment_base_sha256:$deployment_base_sha256,
      previous_application_revision:.previous_application_revision,
      previous_release_identity:.previous_release_identity,
      previous_source_revision:.previous_source_revision,
      previous_images_json_sha256:.previous_images_json_sha256,
      previous_goose_versions:.previous_goose_versions,
      candidate_goose_versions:$candidate_goose_versions,
      migration_set_sha256:$migration_set_sha256,
      test_target:"task test:migration-compat",
      pass:true,
      timestamp:$timestamp
    }
  ' "$base_input" >"$temporary"
chmod 0644 "$temporary"
platform_config validate-document "$production/migration-compat-evidence.schema.json" "$temporary"
mv "$temporary" "$output"
trap cleanup_compatibility EXIT HUP INT TERM
printf '%s\n' "Migration compatibility passed and wrote ${output#"$root/"}"
