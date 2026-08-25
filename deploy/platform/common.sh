#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
VERSIONS=$ROOT/deploy/platform/versions.yaml
IMAGE_LOCK=$ROOT/deploy/images.lock.yaml
CHART=$ROOT/deploy/helm/vermouth
LOCAL_VALUES=$CHART/values-local.yaml
PLATFORM_TMP=$ROOT/.tmp/platform
IMAGES_JSON=$PLATFORM_TMP/images.json
SECRETS_JSON=$PLATFORM_TMP/secrets.json
RUNTIME_VALUES=$PLATFORM_TMP/runtime-values.yaml
CONTEXT=k3d-vermouth
CLUSTER=vermouth
NAMESPACE=vermouth
REGISTRY=vermouth-registry
HOST_REGISTRY=k3d-vermouth-registry.localhost:5111
CLUSTER_REGISTRY=k3d-vermouth-registry:5000

mkdir -p "$PLATFORM_TMP"

fail() {
  printf 'platform: %s\n' "$*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required. Run task platform:doctor for the full list."
}

kube() {
  kubectl --context "$CONTEXT" --namespace "$NAMESPACE" --request-timeout=15s "$@"
}

kube_cluster() {
  kubectl --context "$CONTEXT" --request-timeout=15s "$@"
}

cluster_exists() {
  k3d cluster list --no-headers 2>/dev/null | awk '{print $1}' | grep -Fx "$CLUSTER" >/dev/null
}

registry_exists() {
  k3d registry list --no-headers 2>/dev/null | awk '{print $1}' | grep -Fx "k3d-$REGISTRY" >/dev/null
}

require_context() {
  current=$(kubectl config current-context 2>/dev/null || true)
  [ "$current" = "$CONTEXT" ] || fail "current context is ${current:-unset}, expected $CONTEXT. Select it explicitly, then retry."
  cluster_exists || fail "cluster $CLUSTER is absent. Run task platform:bootstrap."
}

yaml_value() {
  file=$1
  wanted=$2
  awk -v wanted="$wanted" '
    function trim(value) {
      sub(/^[[:space:]]+/, "", value)
      sub(/[[:space:]]+$/, "", value)
      gsub(/^"|"$/, "", value)
      return value
    }
    /^[[:space:]]*(#|$)/ { next }
    {
      match($0, /[^ ]/)
      level = int((RSTART - 1) / 2)
      line = substr($0, RSTART)
      split(line, parts, ":")
      key[level] = trim(parts[1])
      for (i = level + 1; i < 12; i++) delete key[i]
      value = line
      sub(/^[^:]+:[[:space:]]*/, "", value)
      if (value == "") next
      path = key[0]
      for (i = 1; i <= level; i++) path = path "." key[i]
      if (path == wanted) print trim(value)
    }
  ' "$file"
}

version_at_least() {
  actual=$1
  minimum=$2
  awk -v actual="$actual" -v minimum="$minimum" 'BEGIN {
    split(actual, a, "."); split(minimum, b, ".")
    for (i = 1; i <= 4; i++) {
      av = a[i] + 0; bv = b[i] + 0
      if (av > bv) exit 0
      if (av < bv) exit 1
    }
    exit 0
  }'
}

require_series() {
  label=$1
  actual=$2
  series=$3
  minimum=$4
  case "$actual" in
    "$series"|"$series".*) ;;
    *) fail "$label $actual is outside supported series $series.x" ;;
  esac
  version_at_least "$actual" "$minimum" || fail "$label $actual is older than $minimum"
  printf '%-12s %s\n' "$label" "$actual"
}

sha256_file() {
  shasum -a 256 "$1" | awk '{print $1}'
}

sha256_text() {
  shasum -a 256 | awk '{print $1}'
}

confirm_vermouth() {
  action=$1
  printf '%s targets cluster %s, registry %s, and their local data.\n' "$action" "$CLUSTER" "$REGISTRY"
  printf 'Type vermouth to continue: '
  IFS= read -r answer
  [ "$answer" = vermouth ] || fail "confirmation did not match vermouth, nothing changed"
}

job_succeeded() {
  kube get job "$1" -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}' 2>/dev/null | grep -Fx True >/dev/null
}
