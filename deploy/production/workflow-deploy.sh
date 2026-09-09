#!/bin/sh
set -eu
umask 077

transport_fail() {
  printf 'production transport: %s\n' "$*" >&2
  exit 1
}

[ "$#" -ge 1 ] || transport_fail "usage: workflow-deploy.sh <inspect-base|preflight|deploy> [release.tar.zst]"
action=$1
case "$action" in
  inspect-base) [ "$#" -eq 1 ] || transport_fail "inspect-base accepts no archive" ;;
  preflight|deploy)
    [ "$#" -eq 2 ] || transport_fail "$action requires one release archive"
    archive=$2
    ;;
  *) transport_fail "the action must be inspect-base, preflight, or deploy" ;;
esac
for name in PROD_HOST PROD_SSH_USER PROD_SSH_PRIVATE_KEY PROD_SSH_HOST_KEY; do
  eval "value=\${$name:-}"
  [ -n "$value" ] || transport_fail "$name is required"
done
[ "$PROD_HOST" = 4.194.251.123 ] || transport_fail "PROD_HOST must be 4.194.251.123"
[ "$PROD_SSH_USER" = vermouth-deploy ] || transport_fail "PROD_SSH_USER must be vermouth-deploy"
[ "$action" = inspect-base ] || { [ -f "$archive" ] && [ ! -L "$archive" ]; } ||
  transport_fail "$archive is missing or unsafe"

work=$(mktemp -d)
cleanup_transport() {
  rm -rf "$work"
}
trap cleanup_transport EXIT HUP INT TERM
key=$work/deploy-key
known_hosts=$work/known-hosts
printf '%s\n' "$PROD_SSH_PRIVATE_KEY" >"$key"
printf '%s\n' "$PROD_SSH_HOST_KEY" >"$known_hosts"
chmod 0600 "$key" "$known_hosts"

if [ "$action" = inspect-base ]; then
  git_sha=$(git rev-parse --verify HEAD^{commit})
  printf '%s' "${GITHUB_RUN_ID:-}" | grep -Eq '^[1-9][0-9]*$' || transport_fail "GITHUB_RUN_ID is invalid"
  printf '%s' "${GITHUB_RUN_ATTEMPT:-}" | grep -Eq '^[1-9][0-9]*$' || transport_fail "GITHUB_RUN_ATTEMPT is invalid"
  ssh \
    -i "$key" \
    -o BatchMode=yes \
    -o ConnectTimeout=15 \
    -o IdentitiesOnly=yes \
    -o StrictHostKeyChecking=yes \
    -o UserKnownHostsFile="$known_hosts" \
    "$PROD_SSH_USER@$PROD_HOST" \
    "inspect-base $git_sha $GITHUB_RUN_ID $GITHUB_RUN_ATTEMPT" </dev/null
  exit 0
fi

images=$work/images.json
zstd -q -dc "$archive" | tar -xOf - ./images.json >"$images"
git_sha=$(jq -r '.git_sha' "$images")
run_id=$(jq -r '.github_run_id' "$images")
run_attempt=$(jq -r '.github_run_attempt' "$images")
[ "$run_id" = "${GITHUB_RUN_ID:-}" ] || transport_fail "the bundle run ID does not match this workflow"
[ "$run_attempt" = "${GITHUB_RUN_ATTEMPT:-}" ] || transport_fail "the bundle run attempt does not match this workflow"
bundle_sha=$(sha256sum "$archive" | awk '{print $1}')

ssh \
  -i "$key" \
  -o BatchMode=yes \
  -o ConnectTimeout=15 \
  -o IdentitiesOnly=yes \
  -o StrictHostKeyChecking=yes \
  -o UserKnownHostsFile="$known_hosts" \
  "$PROD_SSH_USER@$PROD_HOST" \
  "$action $git_sha $bundle_sha" <"$archive"
