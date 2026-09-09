#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
evidence=$root/.tmp/production/rate-limit-evidence.json
passed=0

cleanup_evidence() {
  status=$?
  trap - EXIT
  if [ "$passed" -ne 1 ]; then
    rm -f "$evidence"
  fi
  exit "$status"
}
trap cleanup_evidence EXIT

mkdir -p "$root/.tmp/production"
rm -f "$evidence"

platform_value() {
  (
    cd "$root/pkg/vermouth"
    GOWORK=off go run ./cmd/platformconfig get \
      "$root/deploy/helm/vermouth/values-production.yaml" "$1"
  )
}

export GATEWAY_TRUSTED_PROXY_CIDRS=$(platform_value config.gatewayTrustedProxyCIDRs)
export GATEWAY_AUTH_RATE_START_IP=$(platform_value config.gatewayAuthRateStartIP)
export GATEWAY_AUTH_RATE_START_GLOBAL=$(platform_value config.gatewayAuthRateStartGlobal)
export GATEWAY_AUTH_RATE_CALLBACK_IP=$(platform_value config.gatewayAuthRateCallbackIP)
export GATEWAY_AUTH_RATE_CALLBACK_GLOBAL=$(platform_value config.gatewayAuthRateCallbackGlobal)
export GATEWAY_AUTH_RATE_REFRESH_IP=$(platform_value config.gatewayAuthRateRefreshIP)
export GATEWAY_AUTH_RATE_REFRESH_TOKEN=$(platform_value config.gatewayAuthRateRefreshToken)
export GATEWAY_AUTH_RATE_REFRESH_GLOBAL=$(platform_value config.gatewayAuthRateRefreshGlobal)
# The test builds gateway and identity as subprocesses, which are outside its
# Go package dependency graph. A per invocation value prevents stale test cache.
export VERMOUTH_AUTH_RATE_LIMIT_TEST_RUN=$$

cd "$root"
task env
task infra:up
task migrate:up
go test ./gateway/internal/ratelimit ./gateway/internal/route
go test -race ./gateway/internal/ratelimit ./gateway/internal/route
go test ./test/authratelimit
task generate:api
task web:generate
git diff --exit-code -- gateway/internal/apitypes/types.gen.go web/src/api/schema.d.ts
corepack pnpm install --frozen-lockfile
corepack pnpm --dir web exec vitest run \
  src/api/session.test.ts src/pages/SignInPage.test.tsx src/routes.test.tsx
task web:typecheck
go run ./gateway/cmd/ratelimitevidence write "$root" "$evidence"
go run ./pkg/vermouth/cmd/platformconfig validate-document \
  deploy/production/rate-limit-evidence.schema.json "$evidence"
go run ./gateway/cmd/ratelimitevidence verify "$root" "$evidence"

passed=1
