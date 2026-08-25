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
  current=$(env_value "$name")
  [ -z "$current" ] || return 0
  if grep -q "^$name=" "$ROOT/.env"; then
    awk -v key="$name" -v value="$value" 'index($0, key "=") == 1 { print key "=" value; next } { print }' \
      "$ROOT/.env" >"$ROOT/.env.next"
    mv "$ROOT/.env.next" "$ROOT/.env"
  else
    printf '%s=%s\n' "$name" "$value" >>"$ROOT/.env"
  fi
}

ensure_local_configuration() {
  task -d "$ROOT" env
  task -d "$ROOT" dev:keys
  ensure_env_value VERMOUTH_GOOGLE_AUTH_ENABLED false
  ensure_env_value GARAGE_RPC_SECRET "$(openssl rand -hex 32)"
  ensure_env_value GARAGE_ADMIN_TOKEN "$(openssl rand -hex 32)"
  ensure_env_value BILLING_S3_ENDPOINT http://garage:3900
  ensure_env_value BILLING_S3_REGION garage
  ensure_env_value BILLING_S3_BUCKET vermouth-invoices
  ensure_env_value BILLING_S3_ACCESS_KEY_ID "$(openssl rand -hex 12)"
  ensure_env_value BILLING_S3_SECRET_ACCESS_KEY "$(openssl rand -hex 32)"
  printf '%s\n' 'Local configuration is present. Existing values were preserved.'
}

secret_hash() {
  LC_ALL=C sort "$1" | sha256_text
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
    jq --arg hash "$hash" '.metadata.annotations["vermouth.dev/source-sha256"] = $hash |
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

prepare_secrets() {
  require_context
  load_local_env
  : "${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}"
  : "${IDENTITY_TOKEN_KID:?IDENTITY_TOKEN_KID is required}"
  : "${IDENTITY_TOKEN_PRIVATE_KEY:?IDENTITY_TOKEN_PRIVATE_KEY is required}"
  : "${TOKEN_PUBLIC_KEYS:?TOKEN_PUBLIC_KEYS is required}"
  : "${GARAGE_RPC_SECRET:?GARAGE_RPC_SECRET is required}"
  : "${GARAGE_ADMIN_TOKEN:?GARAGE_ADMIN_TOKEN is required}"
  : "${BILLING_S3_ACCESS_KEY_ID:?BILLING_S3_ACCESS_KEY_ID is required}"
  : "${BILLING_S3_SECRET_ACCESS_KEY:?BILLING_S3_SECRET_ACCESS_KEY is required}"

  kube_cluster create namespace "$NAMESPACE" --dry-run=client -o yaml | kube_cluster apply -f - >/dev/null
  secret_dir=$PLATFORM_TMP/secret-files
  mkdir -p "$secret_dir"
  chmod 0700 "$secret_dir"

  for service in identity teaching billing notifications; do
    printf 'POSTGRES_PASSWORD=%s\n' "$POSTGRES_PASSWORD" >"$secret_dir/postgres-$service.env"
    chmod 0600 "$secret_dir/postgres-$service.env"
    apply_stable_secret "postgres-$service-bootstrap" "$secret_dir/postgres-$service.env"
  done
  {
    printf 'GARAGE_RPC_SECRET=%s\n' "$GARAGE_RPC_SECRET"
    printf 'GARAGE_ADMIN_TOKEN=%s\n' "$GARAGE_ADMIN_TOKEN"
  } >"$secret_dir/garage.env"
  chmod 0600 "$secret_dir/garage.env"
  apply_stable_secret garage-bootstrap "$secret_dir/garage.env"
  {
    printf 'BILLING_S3_ACCESS_KEY_ID=%s\n' "$BILLING_S3_ACCESS_KEY_ID"
    printf 'BILLING_S3_SECRET_ACCESS_KEY=%s\n' "$BILLING_S3_SECRET_ACCESS_KEY"
  } >"$secret_dir/billing-s3.env"
  chmod 0600 "$secret_dir/billing-s3.env"
  apply_stable_secret billing-s3-bootstrap "$secret_dir/billing-s3.env"

  encoded_password=$(jq -nr --arg value "$POSTGRES_PASSWORD" '$value | @uri')
  printf 'TOKEN_PUBLIC_KEYS=%s\n' "$TOKEN_PUBLIC_KEYS" >"$secret_dir/gateway.env"
  gateway_secret=$(apply_runtime_secret gateway "$secret_dir/gateway.env" | tail -1)

  for service in identity teaching billing notifications; do
    upper=$(printf '%s' "$service" | tr '[:lower:]' '[:upper:]')
    {
      printf '%s_DATABASE_URL=postgres://vermouth_%s:%s@postgres-%s:5432/vermouth_%s?sslmode=disable\n' \
        "$upper" "$service" "$encoded_password" "$service" "$service"
      printf 'TOKEN_PUBLIC_KEYS=%s\n' "$TOKEN_PUBLIC_KEYS"
      if [ "$service" = identity ]; then
        printf 'IDENTITY_TOKEN_KID=%s\n' "$IDENTITY_TOKEN_KID"
        printf 'IDENTITY_TOKEN_PRIVATE_KEY=%s\n' "$IDENTITY_TOKEN_PRIVATE_KEY"
        if [ "${VERMOUTH_GOOGLE_AUTH_ENABLED:-false}" = true ]; then
          : "${IDENTITY_GOOGLE_CLIENT_ID:?IDENTITY_GOOGLE_CLIENT_ID is required when Google auth is enabled}"
          : "${IDENTITY_GOOGLE_CLIENT_SECRET:?IDENTITY_GOOGLE_CLIENT_SECRET is required when Google auth is enabled}"
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

  jq -n --arg gateway "$gateway_secret" --arg identity "$identity_secret" \
    --arg teaching "$teaching_secret" --arg billing "$billing_secret" \
    --arg notifications "$notifications_secret" \
    '{gateway:$gateway,identity:$identity,teaching:$teaching,billing:$billing,notifications:$notifications}' \
    >"$SECRETS_JSON"
  chmod 0600 "$SECRETS_JSON"
  rm -f "$secret_dir"/*.env
}

write_runtime_values() {
  [ -f "$IMAGES_JSON" ] || fail "native image digests are missing. Run task images:native."
  [ -f "$SECRETS_JSON" ] || fail "runtime Secret names are missing. Run task platform:secrets."
  load_local_env
  for image in gateway identity teaching billing notifications web identity-migration teaching-migration billing-migration notifications-migration garage-init devtoken; do
    jq -e --arg image "$image" '.images[$image].cluster_ref | strings' "$IMAGES_JSON" >/dev/null || fail "image $image is missing from $IMAGES_JSON"
  done
  google=${VERMOUTH_GOOGLE_AUTH_ENABLED:-false}
  {
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
      printf '  %s: %s\n' "$service" "$(jq -r --arg service "$service" '.[$service]' "$SECRETS_JSON")"
    done
    printf 'config:\n'
    printf '  googleAuthEnabled: %s\n' "$google"
  } >"$RUNTIME_VALUES"
  chmod 0600 "$RUNTIME_VALUES"
}

prune_runtime_secrets() {
  [ -f "$SECRETS_JSON" ] || return 0
  revisions=""
  if helm --kube-context "$CONTEXT" --namespace "$NAMESPACE" status vermouth >/dev/null 2>&1; then
    revisions=$(helm --kube-context "$CONTEXT" --namespace "$NAMESPACE" history vermouth -o json |
      jq -r 'sort_by(.revision) | reverse | .[0:2][] | .revision')
  fi
  for service in gateway identity teaching billing notifications; do
    current=$(jq -r --arg service "$service" '.[$service]' "$SECRETS_JSON")
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
