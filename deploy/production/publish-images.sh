#!/bin/sh
set -eu
umask 077

publish_fail() {
  printf 'production images: %s\n' "$*" >&2
  exit 1
}

for tool in curl docker go jq mktemp; do
  command -v "$tool" >/dev/null 2>&1 || publish_fail "$tool is required"
done
for name in DOCKERHUB_NAMESPACE DOCKERHUB_USERNAME DOCKERHUB_PUSH_TOKEN GITHUB_REPOSITORY GITHUB_RUN_ID GITHUB_RUN_ATTEMPT; do
  eval "value=\${$name:-}"
  [ -n "$value" ] || publish_fail "$name is required"
done

root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
source_revision=$(git -C "$root" rev-parse --verify HEAD^{commit})
printf '%s' "$source_revision" | grep -Eq '^[0-9a-f]{40}$' || publish_fail "the checked out revision is not a full Git SHA"
work=$(mktemp -d)
cleanup_publish() {
  rm -rf "$work"
}
trap cleanup_publish EXIT HUP INT TERM

login_request=$work/dockerhub-login.json
login_response=$work/dockerhub-login-response.json
jq -n '{username:env.DOCKERHUB_USERNAME,password:env.DOCKERHUB_PUSH_TOKEN}' >"$login_request"
curl --fail --silent --show-error --max-time 30 \
  --header 'Content-Type: application/json' --data-binary "@$login_request" \
  https://hub.docker.com/v2/users/login >"$login_response"
hub_token=$(jq -er '.token | strings | select(length > 0)' "$login_response") || publish_fail "Docker Hub API login failed"
printf '%s' "$DOCKERHUB_PUSH_TOKEN" | docker login --username "$DOCKERHUB_USERNAME" --password-stdin >/dev/null

hub_api_get() {
  path=$1
  output=$2
  config=$work/hub-api.config
  {
    printf '%s\n' 'silent'
    printf '%s\n' 'show-error'
    printf '%s\n' 'fail'
    printf '%s\n' 'max-time = 30'
    printf 'header = "Authorization: Bearer %s"\n' "$hub_token"
    printf 'url = "https://hub.docker.com%s"\n' "$path"
  } >"$config"
  curl --config "$config" >"$output"
}

verify_immutable_rule() {
  workload=$1
  repository=vermouth-$workload
  response=$work/$workload-repository.json
  hub_api_get "/v2/namespaces/$DOCKERHUB_NAMESPACE/repositories/$repository" "$response"
  jq -e '
    .immutable_tags_settings.enabled == true and
    (.immutable_tags_settings.rules | index("git-.*") != null)
  ' "$response" >/dev/null ||
    publish_fail "$DOCKERHUB_NAMESPACE/$repository must protect git-* with the exact Docker Hub regex git-.*"
}

verify_image() {
  plan=$1
  reference=$2
  record=$3
  workload=$(jq -r '.workload' "$plan")
  repository=$(jq -r '.repository' "$plan")
  source=$(jq -r '.source_revision' "$plan")
  input_hash=$(jq -r '.build_input_sha256' "$plan")
  source_url=https://github.com/$GITHUB_REPOSITORY
  root_json=$work/$workload-root.json
  docker buildx imagetools inspect --raw "$reference" >"$root_json"
  root_digest=$(docker buildx imagetools inspect "$reference" | awk '$1 == "Digest:" {print $2; exit}')
  printf '%s' "$root_digest" | grep -Eq '^sha256:[0-9a-f]{64}$' || publish_fail "$workload root digest is invalid"
  jq -e \
    --arg source "$source_url" --arg revision "$source" --arg workload "$workload" --arg input "$input_hash" '
      .mediaType == "application/vnd.oci.image.index.v1+json" and
      (.manifests | length) == 2 and
      ([.manifests[].mediaType] | all(. == "application/vnd.oci.image.manifest.v1+json")) and
      ([.manifests[].platform | (.os + "/" + .architecture)] | sort == ["linux/amd64","linux/arm64"]) and
      .annotations["org.opencontainers.image.source"] == $source and
      .annotations["org.opencontainers.image.revision"] == $revision and
      .annotations["vermouth.dev/workload"] == $workload and
      .annotations["vermouth.dev/build-input-sha256"] == $input and
      (.annotations["org.opencontainers.image.created"] | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$"))
    ' "$root_json" >/dev/null || publish_fail "$workload root index does not match its production plan"
  created_at=$(jq -r '.annotations["org.opencontainers.image.created"]' "$root_json")
  jq -r '.manifests[].digest' "$root_json" | while IFS= read -r child_digest; do
    child=$work/$workload-${child_digest#sha256:}.json
    docker buildx imagetools inspect --raw "$repository@$child_digest" >"$child"
    jq -e \
      --arg source "$source_url" --arg revision "$source" --arg workload "$workload" \
      --arg input "$input_hash" --arg created "$created_at" '
        .mediaType == "application/vnd.oci.image.manifest.v1+json" and
        .annotations["org.opencontainers.image.source"] == $source and
        .annotations["org.opencontainers.image.revision"] == $revision and
        .annotations["org.opencontainers.image.created"] == $created and
        .annotations["vermouth.dev/workload"] == $workload and
        .annotations["vermouth.dev/build-input-sha256"] == $input
      ' "$child" >/dev/null || publish_fail "$workload child manifest $child_digest does not match its production plan"
  done
  jq -c \
    --arg workload "$workload" --arg repository "$repository" --arg tag "$(jq -r '.tag' "$plan")" \
    --arg digest "$root_digest" --arg input "$input_hash" --arg revision "$source" --arg created "$created_at" \
    '{workload:$workload,repository:$repository,tag:$tag,digest:$digest,platforms:["linux/amd64","linux/arm64"],build_input_sha256:$input,source_revision:$revision,created_at:$created}' \
    >"$record"
}

publish_image() {
  workload=$1
  plan=$work/$workload-plan.json
  record=$work/$workload-record.json
  (
    cd "$root/pkg/vermouth"
    GOWORK=off go run ./cmd/platformconfig production-image-plan \
      "$root" "$workload" "$source_revision" "$DOCKERHUB_NAMESPACE" >"$plan"
  )
  verify_immutable_rule "$workload"
  reference=$(jq -r '.repository + ":" + .tag' "$plan")
  if docker buildx imagetools inspect --raw "$reference" >/dev/null 2>&1; then
    verify_image "$plan" "$reference" "$record"
    cat "$record" >>"$records"
    return
  fi

  [ "$(jq '.build_args | length' "$plan")" -eq 0 ] || publish_fail "$workload has build arguments that the production publisher does not yet support"
  created_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  source_url=https://github.com/$GITHUB_REPOSITORY
  set +e
  docker buildx build \
    --file "$root/$(jq -r '.dockerfile' "$plan")" \
    --target "$(jq -r '.target' "$plan")" \
    --platform linux/amd64,linux/arm64 \
    --tag "$reference" \
    --provenance=false \
    --sbom=false \
    --annotation "index:org.opencontainers.image.source=$source_url" \
    --annotation "index:org.opencontainers.image.revision=$source_revision" \
    --annotation "index:org.opencontainers.image.created=$created_at" \
    --annotation "index:vermouth.dev/workload=$workload" \
    --annotation "index:vermouth.dev/build-input-sha256=$(jq -r '.build_input_sha256' "$plan")" \
    --annotation "manifest:org.opencontainers.image.source=$source_url" \
    --annotation "manifest:org.opencontainers.image.revision=$source_revision" \
    --annotation "manifest:org.opencontainers.image.created=$created_at" \
    --annotation "manifest:vermouth.dev/workload=$workload" \
    --annotation "manifest:vermouth.dev/build-input-sha256=$(jq -r '.build_input_sha256' "$plan")" \
    --push "$root"
  build_status=$?
  set -e
  if [ "$build_status" -ne 0 ]; then
    printf '%s\n' "$workload push did not win. Inspecting the immutable tag for a matching publisher."
  fi
  verify_image "$plan" "$reference" "$record"
  cat "$record" >>"$records"
}

records=$work/verified-records.jsonl
: >"$records"
for workload in billing billing-migration garage-init gateway identity identity-migration notifications notifications-migration teaching teaching-migration web; do
  publish_image "$workload"
done

chart_sha256=$(
  cd "$root/pkg/vermouth"
  GOWORK=off go run ./cmd/platformconfig tree-sha256 "$root/deploy/helm/vermouth"
)
metadata=$work/metadata.json
jq -n \
  --arg git_sha "$source_revision" --arg github_run_id "$GITHUB_RUN_ID" --arg github_run_attempt "$GITHUB_RUN_ATTEMPT" \
  --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --arg chart_sha256 "$chart_sha256" \
  '{git_sha:$git_sha,github_run_id:$github_run_id,github_run_attempt:$github_run_attempt,generated_at:$generated_at,chart_sha256:$chart_sha256}' \
  >"$metadata"
(
  cd "$root/pkg/vermouth"
  GOWORK=off go run ./cmd/platformconfig production-images-document "$metadata" "$records"
)
