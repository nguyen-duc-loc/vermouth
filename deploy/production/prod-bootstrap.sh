#!/bin/sh
set -eu

render_bootstrap_traefik() {
  output=$1
  {
    printf '%s\n' 'apiVersion: helm.cattle.io/v1'
    printf '%s\n' 'kind: HelmChartConfig'
    printf '%s\n' 'metadata:'
    printf '%s\n' '  name: traefik'
    printf '%s\n' '  namespace: kube-system'
    printf '%s\n' 'spec:'
    printf '%s\n' '  failurePolicy: abort'
    printf '%s\n' '  valuesContent: |-'
    sed 's/^/    /' "$PROD_TRAEFIK_COMMON"
    sed "s/__PROD_ACME_EMAIL__/$PROD_ACME_EMAIL/g; s/^/    /" "$PROD_TRAEFIK_VALUES"
  } >"$output"
}

write_bootstrap_runtime_values() {
  output=$1
  cat >"$output" <<EOF
schemaVersion: 1
config:
  environment: production
  apiBasePath: /api
  googleAuthEnabled: true
  identityAppURL: https://$PROD_HOSTNAME
  identityGoogleRedirectURL: https://$PROD_HOSTNAME/api/auth/google/callback
ingress:
  hostname: $PROD_HOSTNAME
storage:
  nodeName: $PROD_NODE_NAME
  rootPath: $PROD_STORAGE_ROOT
  markerSHA256: ""
EOF
}

write_bootstrap_request() {
  output=$1
  chart_hash=$2
  image_lock_hash=$3
  jq -n \
    --arg hostname "$PROD_HOSTNAME" \
    --arg dns_label "$PROD_DNS_LABEL" \
    --arg acme_email "$PROD_ACME_EMAIL" \
    --arg deploy_public_key "$PROD_DEPLOY_PUBLIC_KEY" \
    --arg deploy_key_id "$PROD_DEPLOY_KEY_ID" \
    --arg dockerhub_namespace "$DOCKERHUB_NAMESPACE" \
    --arg age_recipient "$PROD_AGE_RECIPIENT" \
    --arg chart_sha256 "$chart_hash" \
    --arg image_lock_sha256 "$image_lock_hash" \
    --argjson restore_bootstrap "${PROD_RESTORE_BOOTSTRAP:-false}" \
    '{
      schema_version:1,
      hostname:$hostname,
      dns_label:$dns_label,
      acme_email:$acme_email,
      deploy_public_key:$deploy_public_key,
      deploy_key_id:$deploy_key_id,
      dockerhub_namespace:$dockerhub_namespace,
      age_recipient:$age_recipient,
      chart_sha256:$chart_sha256,
      image_lock_sha256:$image_lock_sha256,
      restore_bootstrap:$restore_bootstrap
    }' >"$output"
  chmod 0600 "$output"
}

prepare_disaster_bootstrap() {
  archive=$1
  [ -f "$archive" ] && [ ! -L "$archive" ] || prod_fail "$archive is missing or unsafe"
  prod_validate_age_identity
  age --decrypt --identity "$PROD_AGE_IDENTITY" "$archive" |
    prod_platform_config production-archive-validate >/dev/null
  manifest=$(age --decrypt --identity "$PROD_AGE_IDENTITY" "$archive" |
    zstd -q -dc | tar -xOf - manifest.json)
  printf '%s' "$manifest" | jq -e \
    --arg hostname "$PROD_HOSTNAME" --arg vm "$PROD_VM_NAME" --arg node "$PROD_NODE_NAME" \
    --arg dedicated "$PROD_STORAGE_ROOT" --arg fallback "$PROD_STORAGE_FALLBACK" '
      .schema_version == 1 and .hostname == $hostname and
      .storage_identity.vm_name == $vm and .storage_identity.node_name == $node and
      (.storage_identity.storage_root == $dedicated or .storage_identity.storage_root == $fallback)
    ' >/dev/null || prod_fail "the export belongs to another production target"
  prod_ssh 'sudo --non-interactive install -d -o root -g root -m 0700 /etc/vermouth'
  printf '%s\n' "$manifest" |
    prod_ssh 'sudo --non-interactive install -o root -g root -m 0600 /dev/stdin /etc/vermouth/restore-bootstrap-manifest.json'
  age --decrypt --identity "$PROD_AGE_IDENTITY" "$archive" | zstd -q -dc |
    tar -xOf - config/production.env |
    prod_ssh 'sudo --non-interactive install -o root -g root -m 0600 /dev/stdin /etc/vermouth/production.env'
  PROD_RESTORE_BOOTSTRAP=true
  export PROD_RESTORE_BOOTSTRAP
}

write_bootstrap_manifest() {
  directory=$1
  output=$directory/bundle-manifest.json
  files=$directory/files.json
  printf '{}\n' >"$files"
  find "$directory" -type f ! -name bundle-manifest.json ! -name files.json -print | LC_ALL=C sort |
    while IFS= read -r file; do
      relative=${file#"$directory/"}
      hash=$(prod_sha256_file "$file")
      next=$files.next
      jq --arg name "$relative" --arg hash "$hash" '. + {($name):$hash}' "$files" >"$next"
      mv "$next" "$files"
    done
  jq -n --argjson files "$(cat "$files")" '{schema_version:1,files:$files}' >"$output"
  rm -f "$files"
  chmod 0600 "$output"
}

install_production_program() {
  source=$1
  target=$2
  prod_ssh "sudo --non-interactive install -o root -g root -m 0755 /dev/stdin $target" <"$source"
}

install_production_file() {
  source=$1
  target=$2
  prod_ssh "sudo --non-interactive install -o root -g root -m 0644 /dev/stdin $target" <"$source"
}

prod_bootstrap_tools() {
  for tool in awk cp find grep id jq stat tar uname; do
    prod_need "$tool"
  done
}

prod_restore_bootstrap_tools() {
  for tool in age age-keygen zstd; do
    prod_need "$tool"
  done
}

prod_bootstrap() {
  PROD_FAILURE_CODE=3
  prod_init
  PROD_RESTORE_BOOTSTRAP=false
  export PROD_RESTORE_BOOTSTRAP
  prod_bootstrap_tools
  case "$#" in
    0) ;;
    2)
      [ "$1" = --from-export ] || prod_fail "usage: task prod:bootstrap -- --from-export <archive.tar.zst.age>"
      prod_restore_bootstrap_tools
      prepare_disaster_bootstrap "$2"
      ;;
    *) prod_fail "usage: task prod:bootstrap -- --from-export <archive.tar.zst.age>" ;;
  esac

  mkdir -p -m 0700 "$PROD_TMP"
  chmod 0700 "$PROD_TMP"
  PROD_WORK_DIR=$(mktemp -d "$PROD_TMP/bootstrap.XXXXXX")
  export PROD_WORK_DIR
  trap prod_cleanup EXIT HUP INT TERM
  bundle=$PROD_WORK_DIR/bundle
  mkdir -p "$bundle/chart"
  cp -R "$PROD_CHART"/. "$bundle/chart/"
  cp "$PROD_CONFIG" "$bundle/config.json"
  render_bootstrap_traefik "$bundle/traefik-config.yaml"
  write_bootstrap_runtime_values "$bundle/runtime-values.yaml"
  chart_hash=$(prod_platform_config tree-sha256 "$PROD_CHART")
  image_lock_hash=$(prod_sha256_file "$PROD_IMAGE_LOCK")
  write_bootstrap_request "$bundle/request.json" "$chart_hash" "$image_lock_hash"
  write_bootstrap_manifest "$bundle"

  (
    cd "$PROD_ROOT/pkg/vermouth"
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off go build \
      -buildvcs=false -trimpath -o "$PROD_WORK_DIR/vermouth-platformconfig" ./cmd/platformconfig
  )

  cat "$PROD_DIR/azure-metadata.sh" "$PROD_DIR/remote-doctor.sh" >"$PROD_WORK_DIR/remote-doctor"
  cat "$PROD_DIR/azure-metadata.sh" "$PROD_DIR/bootstrap-root.sh" >"$PROD_WORK_DIR/bootstrap-root"
  remote_environment=$(prod_remote_environment)
  prod_ssh "sudo --non-interactive env BOOTSTRAP_MODE=true $remote_environment /bin/sh -s" <"$PROD_WORK_DIR/remote-doctor"
  install_production_program "$PROD_DIR/vermouth-ssh-entrypoint" /usr/local/sbin/vermouth-ssh-entrypoint
  install_production_program "$PROD_DIR/vermouth-deploy-root" /usr/local/sbin/vermouth-deploy-root
  install_production_program "$PROD_WORK_DIR/bootstrap-root" /usr/local/sbin/vermouth-bootstrap-root
  install_production_program "$PROD_WORK_DIR/vermouth-platformconfig" /usr/local/sbin/vermouth-platformconfig
  prod_ssh 'sudo --non-interactive install -d -o root -g root -m 0755 /usr/local/share/vermouth'
  prod_ssh 'sudo --non-interactive install -d -o root -g root -m 0755 /usr/local/lib/vermouth'
  install_production_program "$PROD_DIR/deploy-root-lib.sh" /usr/local/lib/vermouth/deploy-root-lib.sh
  install_production_program "$PROD_DIR/prod-ops-root.sh" /usr/local/sbin/vermouth-prod-ops
  for schema in images deployment-base rate-limit-evidence migration-compat-evidence export-manifest platform; do
    install_production_file "$PROD_DIR/$schema.schema.json" "/usr/local/share/vermouth/$schema.schema.json"
  done

  COPYFILE_DISABLE=1 tar --no-xattrs -C "$bundle" -czf - . |
    prod_ssh 'sudo --non-interactive /usr/local/sbin/vermouth-bootstrap-root'
  printf '%s\n' "Production bootstrap completed for $PROD_HOSTNAME without deploying the application"
}
