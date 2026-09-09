#!/bin/sh
set -eu

load_local_env() {
  [ -f "$ROOT/.env" ] || fail ".env is missing. Run task env first."
  set -a
  # shellcheck disable=SC1091
  . "$ROOT/.env"
  set +a
}

env_value() {
  sed -n "s/^$1=//p" "$ROOT/.env" | tail -1
}

ensure_env_value() {
  name=$1
  value=$2
  if [ -s "$ROOT/.env" ] && [ -n "$(tail -c 1 "$ROOT/.env")" ]; then
    printf '\n' >>"$ROOT/.env"
  fi
  current=$(env_value "$name")
  if ! printf '%s' "$current" | grep -q '[^[:space:]]'; then current=; fi
  [ -z "$current" ] || return 0
  if grep -q "^$name=" "$ROOT/.env"; then
    awk -v key="$name" -v value="$value" 'index($0, key "=") == 1 { print key "=" value; next } { print }' \
      "$ROOT/.env" >"$ROOT/.env.next"
    chmod 0600 "$ROOT/.env.next"
    mv "$ROOT/.env.next" "$ROOT/.env"
  else
    printf '%s=%s\n' "$name" "$value" >>"$ROOT/.env"
  fi
  chmod 0600 "$ROOT/.env"
}

require_env_value() {
  name=$1
  value=$(env_value "$name")
  printf '%s' "$value" | grep -q '[^[:space:]]' || fail "$name is required"
}

ensure_local_configuration() {
  task -d "$ROOT" env
  chmod 0600 "$ROOT/.env"
  task -d "$ROOT" dev:keys
  ensure_env_value VERMOUTH_GOOGLE_AUTH_ENABLED false
  ensure_env_value GARAGE_RPC_SECRET "$(openssl rand -hex 32)"
  ensure_env_value GARAGE_ADMIN_TOKEN "$(openssl rand -hex 32)"
  ensure_env_value BILLING_S3_ENDPOINT http://garage:3900
  ensure_env_value BILLING_S3_REGION garage
  ensure_env_value BILLING_S3_BUCKET vermouth-invoices
  ensure_env_value BILLING_S3_ACCESS_KEY_ID "GK$(openssl rand -hex 12)"
  ensure_env_value BILLING_S3_SECRET_ACCESS_KEY "$(openssl rand -hex 32)"
  validate_local_configuration
  printf '%s\n' 'Local configuration is present. Existing values were preserved.'
}

validate_local_configuration() {
  load_local_env
  case "${VERMOUTH_GOOGLE_AUTH_ENABLED:-false}" in
    false) ;;
    true)
      require_env_value IDENTITY_GOOGLE_CLIENT_ID
      require_env_value IDENTITY_GOOGLE_CLIENT_SECRET
      require_env_value IDENTITY_GOOGLE_REDIRECT_URL
      [ "$IDENTITY_GOOGLE_REDIRECT_URL" = http://vermouth.localhost:8080/api/auth/google/callback ] ||
        fail "IDENTITY_GOOGLE_REDIRECT_URL must equal http://vermouth.localhost:8080/api/auth/google/callback"
      ;;
    *) fail "VERMOUTH_GOOGLE_AUTH_ENABLED must be true or false" ;;
  esac
}

secret_hash() {
  secret_values_json "$1" | platform_config secret-hash
}

secret_values_json() {
  jq -Rn '[inputs | capture("^(?<key>[^=]+)=(?<value>.*)$")] | from_entries' <"$1"
}

secret_keys_json() {
  secret_values_json "$1" | jq -c 'keys | sort'
}

apply_stable_secret() {
  name=$1
  file=$2
  hash=$(secret_hash "$file")
  if kube get secret "$name" >/dev/null 2>&1; then
    live=$(kube get secret "$name" -o jsonpath='{.metadata.annotations.vermouth\.dev/source-sha256}')
    [ "$live" = "$hash" ] || fail "$name changed from its stored foundation values. Run task platform:recreate after confirming data replacement."
    return
  fi
  kube create secret generic "$name" --from-env-file="$file" --dry-run=client -o json |
    jq --arg hash "$hash" '.immutable = true |
      .metadata.annotations["vermouth.dev/source-sha256"] = $hash |
      .metadata.labels["app.kubernetes.io/name"] = "vermouth" |
      .metadata.labels["app.kubernetes.io/component"] = "foundation-secret"' |
    kube apply -f - >/dev/null
  printf 'Created stable Secret %s\n' "$name"
}

apply_runtime_secret() {
  service=$1
  file=$2
  hash=$(secret_hash "$file")
  short=$(printf '%s' "$hash" | cut -c1-12)
  name=$service-runtime-$short
  if ! kube get secret "$name" >/dev/null 2>&1; then
    kube create secret generic "$name" --from-env-file="$file" --dry-run=client -o json |
      jq --arg service "$service" --arg hash "$hash" '.immutable = true |
        .metadata.annotations["vermouth.dev/source-sha256"] = $hash |
        .metadata.labels["app.kubernetes.io/name"] = "vermouth" |
        .metadata.labels["app.kubernetes.io/component"] = "runtime-secret" |
        .metadata.labels["vermouth.dev/runtime-service"] = $service' |
      kube apply -f - >/dev/null
    printf 'Created content named Secret %s\n' "$name"
  fi
  printf '%s' "$name"
}

append_secret_record() {
  state=$1
  section=$2
  key=$3
  name=$4
  file=$5
  hash=$(secret_hash "$file")
  keys=$(secret_keys_json "$file")
  next=$state.record
  jq --arg section "$section" --arg key "$key" --arg name "$name" --arg hash "$hash" --argjson keys "$keys" '.[$section][$key] = {name:$name,source_hash:$hash,keys:$keys}' "$state" >"$next"
  mv "$next" "$state"
}

validate_complete_secret_state() {
  platform_config validate-document "$ROOT/deploy/platform/secrets.schema.json" "$SECRETS_JSON"
  inventory=$ROOT/deploy/platform/secret-inventory.yaml
  for section in foundation runtime; do
    expected=$(platform_config get "$inventory" "$section" | jq -c 'keys | sort')
    actual=$(jq -c --arg section "$section" '.[$section] | keys | sort' "$SECRETS_JSON")
    [ "$actual" = "$expected" ] || fail "generated $section Secret inventory differs from the committed inventory"
    for key in $(printf '%s' "$expected" | jq -r '.[]'); do
      name=$(jq -r --arg section "$section" --arg key "$key" '.[$section][$key].name' "$SECRETS_JSON")
      hash=$(jq -r --arg section "$section" --arg key "$key" '.[$section][$key].source_hash' "$SECRETS_JSON")
      keys=$(jq -c --arg section "$section" --arg key "$key" '.[$section][$key].keys' "$SECRETS_JSON")
      inventory_entry=$(platform_config get "$inventory" "$section.$key")
      name_base=$(printf '%s' "$inventory_entry" | jq -r '.nameBase')
      google_enabled=${VERMOUTH_GOOGLE_AUTH_ENABLED:-false}
      expected_keys=$(printf '%s' "$inventory_entry" | jq -c --argjson google "$google_enabled" '
        [.keys | to_entries[] | select((.value.optional // false) == false or $google) | .key] | sort
      ')
      if [ "$section" = foundation ]; then
        [ "$name" = "$name_base" ] || fail "generated Secret $name differs from inventory name $name_base"
      else
        case "$name" in
          "$name_base"-[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]) ;;
          *) fail "generated Secret $name does not use inventory base $name_base" ;;
        esac
      fi
      [ "$keys" = "$expected_keys" ] || fail "generated Secret $name keys differ from the committed inventory"
      live_hash=$(kube get secret "$name" -o jsonpath='{.metadata.annotations.vermouth\.dev/source-sha256}')
      live_immutable=$(kube get secret "$name" -o jsonpath='{.immutable}')
      live_keys=$(kube get secret "$name" -o json | jq -c '.data | keys | sort')
      [ "$live_hash" = "$hash" ] || fail "Secret $name source hash differs from generated state"
      [ "$live_immutable" = true ] || fail "Secret $name is not immutable"
      [ "$live_keys" = "$keys" ] || fail "Secret $name keys differ from generated state"
    done
  done
}

prepare_secrets() {
  PLATFORM_FAILURE_CODE=4
  require_context
  validate_local_configuration
  for name in POSTGRES_PASSWORD IDENTITY_TOKEN_KID IDENTITY_TOKEN_PRIVATE_KEY TOKEN_PUBLIC_KEYS \
    GARAGE_RPC_SECRET GARAGE_ADMIN_TOKEN BILLING_S3_ACCESS_KEY_ID BILLING_S3_SECRET_ACCESS_KEY; do
    require_env_value "$name"
  done
  [ "$IDENTITY_TOKEN_KID" = dev-1 ] || fail "IDENTITY_TOKEN_KID must be dev-1 for the local platform"
  printf '%s' "$GARAGE_RPC_SECRET" | grep -Eq '^[0-9a-f]{64}$' ||
    fail "GARAGE_RPC_SECRET must contain 64 lowercase hexadecimal characters"
  printf '%s' "$GARAGE_ADMIN_TOKEN" | grep -Eq '^[0-9a-f]{64}$' ||
    fail "GARAGE_ADMIN_TOKEN must contain 64 lowercase hexadecimal characters"
  printf '%s' "$BILLING_S3_ACCESS_KEY_ID" | grep -Eq '^GK[0-9a-f]{24}$' ||
    fail "BILLING_S3_ACCESS_KEY_ID must start with GK and contain 24 lowercase hexadecimal characters"
  printf '%s' "$BILLING_S3_SECRET_ACCESS_KEY" | grep -Eq '^[0-9a-f]{64}$' ||
    fail "BILLING_S3_SECRET_ACCESS_KEY must contain 64 lowercase hexadecimal characters"

  kube_cluster create namespace "$NAMESPACE" --dry-run=client -o yaml | kube_cluster apply -f - >/dev/null
  secret_dir=$PLATFORM_TMP/secret-files
  mkdir -p "$secret_dir"
  chmod 0700 "$secret_dir"
  secret_state=$SECRETS_JSON.next
  printf '{"schema_version":1,"foundation":{},"runtime":{}}\n' >"$secret_state"
  chmod 0600 "$secret_state"

  for service in identity teaching billing notifications; do
    printf 'POSTGRES_PASSWORD=%s\n' "$POSTGRES_PASSWORD" >"$secret_dir/postgres-$service.env"
    chmod 0600 "$secret_dir/postgres-$service.env"
    name=postgres-$service-bootstrap
    apply_stable_secret "$name" "$secret_dir/postgres-$service.env"
    append_secret_record "$secret_state" foundation "postgres-$service" "$name" "$secret_dir/postgres-$service.env"
  done
  {
    printf 'GARAGE_RPC_SECRET=%s\n' "$GARAGE_RPC_SECRET"
    printf 'GARAGE_ADMIN_TOKEN=%s\n' "$GARAGE_ADMIN_TOKEN"
  } >"$secret_dir/garage.env"
  chmod 0600 "$secret_dir/garage.env"
  apply_stable_secret garage-bootstrap "$secret_dir/garage.env"
  append_secret_record "$secret_state" foundation garage garage-bootstrap "$secret_dir/garage.env"
  {
    printf 'BILLING_S3_ACCESS_KEY_ID=%s\n' "$BILLING_S3_ACCESS_KEY_ID"
    printf 'BILLING_S3_SECRET_ACCESS_KEY=%s\n' "$BILLING_S3_SECRET_ACCESS_KEY"
  } >"$secret_dir/billing-s3.env"
  chmod 0600 "$secret_dir/billing-s3.env"
  apply_stable_secret billing-s3-bootstrap "$secret_dir/billing-s3.env"
  append_secret_record "$secret_state" foundation billing-s3 billing-s3-bootstrap "$secret_dir/billing-s3.env"

  printf 'TOKEN_PUBLIC_KEYS=%s\n' "$TOKEN_PUBLIC_KEYS" >"$secret_dir/gateway.env"
  gateway_secret=$(apply_runtime_secret gateway "$secret_dir/gateway.env" | tail -1)

  for service in identity teaching billing notifications; do
    upper=$(printf '%s' "$service" | tr '[:lower:]' '[:upper:]')
    database=$(platform_config get "$ROOT/deploy/platform/secret-inventory.yaml" "runtime.$service.database")
    service_dns=$(printf '%s' "$database" | jq -r '.serviceDNS')
    port=$(printf '%s' "$database" | jq -r '.port')
    database_name=$(printf '%s' "$database" | jq -r '.database')
    role=$(printf '%s' "$database" | jq -r '.role')
    template=$(printf '%s' "$database" | jq -r '.urlTemplate')
    encoded_password=$(jq -nr --arg value "$POSTGRES_PASSWORD" '$value | @uri')
    database_url=$(jq -nr --arg template "$template" --arg role "$role" --arg password "$encoded_password" \
      --arg serviceDNS "$service_dns" --arg port "$port" --arg database "$database_name" '
        $template |
        gsub("\\{role\\}"; $role) |
        gsub("\\{password\\}"; $password) |
        gsub("\\{serviceDNS\\}"; $serviceDNS) |
        gsub("\\{port\\}"; $port) |
        gsub("\\{database\\}"; $database)
      ')
    {
      printf '%s_DATABASE_URL=%s\n' "$upper" "$database_url"
      printf 'TOKEN_PUBLIC_KEYS=%s\n' "$TOKEN_PUBLIC_KEYS"
      if [ "$service" = identity ]; then
        printf 'IDENTITY_TOKEN_KID=%s\n' "$IDENTITY_TOKEN_KID"
        printf 'IDENTITY_TOKEN_PRIVATE_KEY=%s\n' "$IDENTITY_TOKEN_PRIVATE_KEY"
        if [ "${VERMOUTH_GOOGLE_AUTH_ENABLED:-false}" = true ]; then
          printf 'IDENTITY_GOOGLE_CLIENT_ID=%s\n' "$IDENTITY_GOOGLE_CLIENT_ID"
          printf 'IDENTITY_GOOGLE_CLIENT_SECRET=%s\n' "$IDENTITY_GOOGLE_CLIENT_SECRET"
        fi
      fi
      if [ "$service" = billing ]; then
        printf 'BILLING_S3_ACCESS_KEY_ID=%s\n' "$BILLING_S3_ACCESS_KEY_ID"
        printf 'BILLING_S3_SECRET_ACCESS_KEY=%s\n' "$BILLING_S3_SECRET_ACCESS_KEY"
      fi
    } >"$secret_dir/$service.env"
    chmod 0600 "$secret_dir/$service.env"
  done

  identity_secret=$(apply_runtime_secret identity "$secret_dir/identity.env" | tail -1)
  teaching_secret=$(apply_runtime_secret teaching "$secret_dir/teaching.env" | tail -1)
  billing_secret=$(apply_runtime_secret billing "$secret_dir/billing.env" | tail -1)
  notifications_secret=$(apply_runtime_secret notifications "$secret_dir/notifications.env" | tail -1)

  append_secret_record "$secret_state" runtime gateway "$gateway_secret" "$secret_dir/gateway.env"
  append_secret_record "$secret_state" runtime identity "$identity_secret" "$secret_dir/identity.env"
  append_secret_record "$secret_state" runtime teaching "$teaching_secret" "$secret_dir/teaching.env"
  append_secret_record "$secret_state" runtime billing "$billing_secret" "$secret_dir/billing.env"
  append_secret_record "$secret_state" runtime notifications "$notifications_secret" "$secret_dir/notifications.env"
  platform_config validate-document "$ROOT/deploy/platform/secrets.schema.json" "$secret_state"
  platform_config atomic-commit "$secret_state" "$SECRETS_JSON"
  rm -f "$secret_dir"/*.env
  validate_complete_secret_state
}

write_runtime_values() {
  PLATFORM_FAILURE_CODE=4
  [ -f "$IMAGES_JSON" ] || fail "native image digests are missing. Run task images:native."
  [ -f "$SECRETS_JSON" ] || fail "runtime Secret names are missing. Run task platform:secrets."
  validate_complete_image_state
  platform_config validate-document "$ROOT/deploy/platform/images.schema.json" "$IMAGES_JSON"
  platform_config validate-document "$ROOT/deploy/platform/secrets.schema.json" "$SECRETS_JSON"
  load_local_env
  run_id=$1
  for image in gateway identity teaching billing notifications web identity-migration teaching-migration billing-migration notifications-migration garage-init devtoken; do
    jq -e --arg image "$image" '.images[$image].cluster_ref | strings' "$IMAGES_JSON" >/dev/null || fail "image $image is missing from $IMAGES_JSON"
  done
  google=${VERMOUTH_GOOGLE_AUTH_ENABLED:-false}
  next=$RUNTIME_VALUES.next
  {
    printf 'schemaVersion: 1\n'
    printf 'jobs:\n'
    printf '  runID: %s\n' "$run_id"
    printf 'images:\n'
    for image in gateway identity teaching billing notifications web; do
      printf '  %s: %s\n' "$image" "$(jq -r --arg image "$image" '.images[$image].cluster_ref' "$IMAGES_JSON")"
    done
    printf '  garageInit: %s\n' "$(jq -r '.images["garage-init"].cluster_ref' "$IMAGES_JSON")"
    printf '  devtoken: %s\n' "$(jq -r '.images.devtoken.cluster_ref' "$IMAGES_JSON")"
    printf '  migrations:\n'
    for service in identity teaching billing notifications; do
      printf '    %s: %s\n' "$service" "$(jq -r --arg image "$service-migration" '.images[$image].cluster_ref' "$IMAGES_JSON")"
    done
    printf 'secrets:\n'
    for service in gateway identity teaching billing notifications; do
      printf '  %s: %s\n' "$service" "$(jq -r --arg service "$service" '.runtime[$service].name' "$SECRETS_JSON")"
    done
    printf 'config:\n'
    printf '  environment: local\n'
    printf '  apiBasePath: /api\n'
    printf '  googleAuthEnabled: %s\n' "$google"
  } >"$next"
  chmod 0600 "$next"
  platform_config validate-document "$ROOT/deploy/helm/vermouth/values.schema.json" "$next"
  platform_config atomic-commit "$next" "$RUNTIME_VALUES"
}

prune_runtime_secrets() {
  [ -f "$SECRETS_JSON" ] || return 0
  revisions=""
  if helm --kube-context "$CONTEXT" --namespace "$NAMESPACE" status vermouth >/dev/null 2>&1; then
    revisions=$(helm --kube-context "$CONTEXT" --namespace "$NAMESPACE" history vermouth -o json |
      jq -r '[.[] | select(.status == "deployed" or .status == "superseded")] | sort_by(.revision) | reverse | .[0:2][] | .revision')
  fi
  for service in gateway identity teaching billing notifications; do
    current=$(jq -r --arg service "$service" '.runtime[$service].name' "$SECRETS_JSON")
    keep=" $current "
    for revision in $revisions; do
      previous=$(helm --kube-context "$CONTEXT" --namespace "$NAMESPACE" get values vermouth \
        --revision "$revision" -o json 2>/dev/null | jq -r --arg service "$service" '.secrets[$service] // empty' || true)
      [ -z "$previous" ] || keep="$keep$previous "
    done
    for name in $(kube get secret -l "vermouth.dev/runtime-service=$service" -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}'); do
      case "$keep" in
        *" $name "*) ;;
        *) kube delete secret "$name" --ignore-not-found >/dev/null ;;
      esac
    done
  done
}
