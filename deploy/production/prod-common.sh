#!/bin/sh
set -eu

PROD_DIR=${PROD_SCRIPT_DIR:-$(CDPATH= cd -- "$(dirname "$0")" && pwd)}
PROD_ROOT=$(CDPATH= cd -- "$PROD_DIR/../.." && pwd)
PROD_CONFIG=$PROD_DIR/config.json
PROD_CONFIG_SCHEMA=$PROD_DIR/config.schema.json
PROD_PLATFORM_SCHEMA=$PROD_DIR/platform.schema.json
PROD_CHART=$PROD_ROOT/deploy/helm/vermouth
PROD_VALUES=$PROD_CHART/values-production.yaml
PROD_TRAEFIK_COMMON=$PROD_ROOT/deploy/platform/traefik/values-common.yaml
PROD_TRAEFIK_VALUES=$PROD_ROOT/deploy/platform/traefik/values-production.yaml
PROD_IMAGE_LOCK=$PROD_ROOT/deploy/images.lock.yaml
PROD_TMP=$PROD_ROOT/.tmp/production
PROD_NAMESPACE=vermouth

prod_fail() {
  printf 'production: %s\n' "$*" >&2
  exit "${PROD_FAILURE_CODE:-1}"
}

prod_need() {
  command -v "$1" >/dev/null 2>&1 || prod_fail "$1 is required"
}

prod_platform_config() {
  (
    cd "$PROD_ROOT/pkg/vermouth"
    GOWORK=off go run ./cmd/platformconfig "$@"
  )
}

prod_config_value() {
  prod_platform_config get "$PROD_CONFIG" "$1"
}

prod_sha256_file() {
  shasum -a 256 "$1" | awk '{print $1}'
}

prod_require_env() {
  name=$1
  eval "value=\${$name:-}"
  [ -n "$value" ] || prod_fail "$name is required"
}

prod_expect() {
  name=$1
  expected=$2
  eval "actual=\${$name:-}"
  [ "$actual" = "$expected" ] || prod_fail "$name must be $expected"
}

prod_single_line_matches() {
  value=$1
  pattern=$2
  [ "$(printf '%s' "$value" | wc -l | tr -d '[:space:]')" = 0 ] || return 1
  printf '%s' "$value" | LC_ALL=C grep -Eq "$pattern"
}

prod_operator_init() {
  for tool in go grep jq shasum ssh tar tr wc; do
    prod_need "$tool"
  done
  prod_platform_config validate-document "$PROD_CONFIG_SCHEMA" "$PROD_CONFIG"

  prod_require_env PROD_HOST
  prod_expect PROD_HOST "$(prod_config_value target.host)"
  PROD_OPERATOR_USER=$(prod_config_value target.operator_user)
  PROD_VM_NAME=$(prod_config_value target.vm_name)
  PROD_VM_SIZE=$(prod_config_value target.vm_size)
  PROD_LOCATION=$(prod_config_value target.location)
  PROD_NODE_NAME=$(prod_config_value target.node_name)
  PROD_POD_CIDR=$(prod_config_value k3s.pod_cidr)
  PROD_STORAGE_FALLBACK=$(prod_config_value storage.fallback_root)
  PROD_TRAEFIK_DIGEST=$(prod_config_value traefik.image_digest)
  export PROD_OPERATOR_USER PROD_VM_NAME PROD_VM_SIZE PROD_LOCATION PROD_NODE_NAME PROD_POD_CIDR
  export PROD_STORAGE_FALLBACK PROD_TRAEFIK_DIGEST
}

prod_init() {
  prod_operator_init

  for name in PROD_DNS_LABEL PROD_HOSTNAME PROD_SSH_USER PROD_K3S_LIVE_VERSION \
    PROD_K3S_IMAGE_TAG PROD_TRAEFIK_IMAGE PROD_TRAEFIK_CHART PROD_STORAGE_UUID \
    PROD_STORAGE_ROOT PROD_AGE_RECIPIENT DOCKERHUB_NAMESPACE PROD_ACME_EMAIL \
    PROD_DEPLOY_PUBLIC_KEY PROD_DEPLOY_KEY_ID; do
    prod_require_env "$name"
  done

  prod_expect PROD_SSH_USER "$(prod_config_value target.deploy_user)"
  prod_expect PROD_K3S_LIVE_VERSION "$(prod_config_value k3s.live_version)"
  prod_expect PROD_K3S_IMAGE_TAG "$(prod_config_value k3s.image_tag)"
  prod_expect PROD_TRAEFIK_IMAGE "$(prod_config_value traefik.image)"
  prod_expect PROD_TRAEFIK_CHART "$(prod_config_value traefik.chart)"
  prod_expect PROD_STORAGE_UUID "$(prod_config_value storage.uuid)"
  prod_expect PROD_STORAGE_ROOT "$(prod_config_value storage.dedicated_root)"

  prod_single_line_matches "$PROD_DNS_LABEL" '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$' ||
    prod_fail "PROD_DNS_LABEL must be a valid Azure DNS label"
  expected_hostname=$PROD_DNS_LABEL.$(prod_config_value dns_suffix)
  [ "$PROD_HOSTNAME" = "$expected_hostname" ] || prod_fail "PROD_HOSTNAME must be $expected_hostname"
  prod_single_line_matches "$DOCKERHUB_NAMESPACE" '^[a-z0-9][a-z0-9_-]*$' ||
    prod_fail "DOCKERHUB_NAMESPACE is invalid"
  prod_single_line_matches "$PROD_ACME_EMAIL" '^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$' ||
    prod_fail "PROD_ACME_EMAIL is invalid"
  prod_single_line_matches "$PROD_DEPLOY_KEY_ID" '^[A-Za-z0-9._@-]{1,80}$' ||
    prod_fail "PROD_DEPLOY_KEY_ID is invalid"
  prod_single_line_matches "$PROD_DEPLOY_PUBLIC_KEY" '^ssh-ed25519 [A-Za-z0-9+/]+={0,3}( [A-Za-z0-9._@-]+)?$' ||
    prod_fail "PROD_DEPLOY_PUBLIC_KEY must be one Ed25519 public key"
  prod_single_line_matches "$PROD_AGE_RECIPIENT" '^age1[0-9a-z]{20,}$' ||
    prod_fail "PROD_AGE_RECIPIENT must be an age recipient"

}

prod_ssh() {
  remote_command=$1
  ssh \
    -o BatchMode=yes \
    -o ConnectTimeout=10 \
    -o StrictHostKeyChecking=yes \
    "$PROD_OPERATOR_USER@$PROD_HOST" \
    "$remote_command"
}

prod_remote_environment() {
  printf '%s' \
    "EXPECTED_HOST=$PROD_HOST EXPECTED_HOSTNAME=$PROD_HOSTNAME EXPECTED_VM_NAME=$PROD_VM_NAME EXPECTED_VM_SIZE=$PROD_VM_SIZE EXPECTED_LOCATION=$PROD_LOCATION EXPECTED_OPERATOR=$PROD_OPERATOR_USER EXPECTED_NODE=$PROD_NODE_NAME EXPECTED_K3S=$PROD_K3S_LIVE_VERSION EXPECTED_POD_CIDR=$PROD_POD_CIDR EXPECTED_TRAEFIK_IMAGE=$PROD_TRAEFIK_IMAGE EXPECTED_TRAEFIK_DIGEST=$PROD_TRAEFIK_DIGEST EXPECTED_TRAEFIK_CHART=$PROD_TRAEFIK_CHART EXPECTED_STORAGE_UUID=$PROD_STORAGE_UUID EXPECTED_STORAGE_ROOT=$PROD_STORAGE_ROOT EXPECTED_FALLBACK_ROOT=$PROD_STORAGE_FALLBACK EXPECTED_NAMESPACE=$PROD_NAMESPACE DOCKERHUB_NAMESPACE=$DOCKERHUB_NAMESPACE"
}

prod_cleanup() {
  [ -z "${PROD_WORK_DIR:-}" ] || rm -rf "$PROD_WORK_DIR"
}
