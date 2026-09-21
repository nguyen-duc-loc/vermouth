#!/usr/bin/env bash
# Prove the recurring schedule paths that need controlled clocks, token context,
# consumer offsets, or authoritative billing records without adding production hooks.
set -euo pipefail

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"

: "${GATEWAY_TRUSTED_PROXY_CIDRS:=none}"
: "${GATEWAY_AUTH_RATE_START_IP:=5/1m,20/1h}"
: "${GATEWAY_AUTH_RATE_START_GLOBAL:=100/1m,500/1h}"
: "${GATEWAY_AUTH_RATE_CALLBACK_IP:=10/1m,60/1h}"
: "${GATEWAY_AUTH_RATE_CALLBACK_GLOBAL:=200/1m,1000/1h}"
: "${GATEWAY_AUTH_RATE_REFRESH_IP:=30/1m,120/1h}"
: "${GATEWAY_AUTH_RATE_REFRESH_TOKEN:=10/1m,120/1h}"
: "${GATEWAY_AUTH_RATE_REFRESH_GLOBAL:=300/1m,3000/1h}"
export GATEWAY_TRUSTED_PROXY_CIDRS GATEWAY_AUTH_RATE_START_IP GATEWAY_AUTH_RATE_START_GLOBAL
export GATEWAY_AUTH_RATE_CALLBACK_IP GATEWAY_AUTH_RATE_CALLBACK_GLOBAL
export GATEWAY_AUTH_RATE_REFRESH_IP GATEWAY_AUTH_RATE_REFRESH_TOKEN GATEWAY_AUTH_RATE_REFRESH_GLOBAL

cleanup() {
  rtk task stop >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

rtk task infra:up
rtk task migrate:up
rtk task build
rtk task stop >/dev/null 2>&1 || true
rtk task start
rtk task status

thread_output=$(VERMOUTH_DEV_MODE=host rtk proxy ./test/thread.sh)
rtk printf '%s\n' "$thread_output"
tutor_id=$(rtk printf '%s\n' "$thread_output" | rtk awk '/^tutor_id / { print $2 }')
session_id=$(rtk printf '%s\n' "$thread_output" | rtk awk '/^session_id / { print $2 }')
if [[ -z $tutor_id || -z $session_id ]]; then
  rtk printf 'the recurring thread did not report its tutor and session identifiers\n'
  exit 1
fi

rtk sleep 2
billing_before=$(rtk docker exec vermouth-postgres-billing psql -U vermouth_billing -d vermouth_billing -X -A -t -c "
SELECT row_to_json(projected)::text
FROM (
  SELECT session_id, class_id, tutor_id, starts_at, ends_at, local_date, cancelled_at
  FROM sessions
  WHERE tutor_id = '$tutor_id' AND session_id = '$session_id'
) projected;")
notifications_before=$(rtk docker exec vermouth-postgres-notifications psql -U vermouth_notifications -d vermouth_notifications -X -A -t -c "
SELECT row_to_json(projected)::text
FROM (
  SELECT session_id, class_id, tutor_id, starts_at, ends_at, local_date, cancelled_at
  FROM sessions
  WHERE tutor_id = '$tutor_id' AND session_id = '$session_id'
) projected;")
billing_handled=$(rtk docker exec vermouth-postgres-billing psql -U vermouth_billing -d vermouth_billing -X -A -t -c \
  "SELECT count(*) FROM handled_events WHERE consumer_name = 'billing.teaching';")
notifications_handled=$(rtk docker exec vermouth-postgres-notifications psql -U vermouth_notifications -d vermouth_notifications -X -A -t -c \
  "SELECT count(*) FROM handled_events WHERE consumer_name = 'notifications.teaching';")
if [[ -z $billing_before || -z $notifications_before || $billing_handled -lt 1 || $notifications_handled -lt 1 ]]; then
  rtk printf 'both teaching projections must exist before replay\n'
  exit 1
fi

rtk task svc:stop -- billing
rtk task svc:stop -- notifications
REPLAY_DATABASE_URL="$BILLING_DATABASE_URL" rtk go run ./pkg/vermouth/cmd/replay \
  -service billing -consumer teaching -topics teaching.events
REPLAY_DATABASE_URL="$NOTIFICATIONS_DATABASE_URL" rtk go run ./pkg/vermouth/cmd/replay \
  -service notifications -consumer teaching -topics teaching.events
rtk task svc:start -- billing
rtk task svc:start -- notifications

attempt=0
while [[ $attempt -lt 40 ]]; do
  billing_after_count=$(rtk docker exec vermouth-postgres-billing psql -U vermouth_billing -d vermouth_billing -X -A -t -c \
    "SELECT count(*) FROM handled_events WHERE consumer_name = 'billing.teaching';")
  notifications_after_count=$(rtk docker exec vermouth-postgres-notifications psql -U vermouth_notifications -d vermouth_notifications -X -A -t -c \
    "SELECT count(*) FROM handled_events WHERE consumer_name = 'notifications.teaching';")
  if [[ $billing_after_count -ge $billing_handled && $notifications_after_count -ge $notifications_handled ]]; then
    break
  fi
  attempt=$((attempt + 1))
  rtk sleep 1
done
if [[ $attempt -ge 40 ]]; then
  rtk printf 'billing and notifications did not finish their replay within 40 seconds\n'
  exit 1
fi

billing_after=$(rtk docker exec vermouth-postgres-billing psql -U vermouth_billing -d vermouth_billing -X -A -t -c "
SELECT row_to_json(projected)::text
FROM (
  SELECT session_id, class_id, tutor_id, starts_at, ends_at, local_date, cancelled_at
  FROM sessions
  WHERE tutor_id = '$tutor_id' AND session_id = '$session_id'
) projected;")
notifications_after=$(rtk docker exec vermouth-postgres-notifications psql -U vermouth_notifications -d vermouth_notifications -X -A -t -c "
SELECT row_to_json(projected)::text
FROM (
  SELECT session_id, class_id, tutor_id, starts_at, ends_at, local_date, cancelled_at
  FROM sessions
  WHERE tutor_id = '$tutor_id' AND session_id = '$session_id'
) projected;")
[[ $billing_after == "$billing_before" ]] || { rtk printf 'billing session changed after replay\n'; exit 1; }
[[ $notifications_after == "$notifications_before" ]] || { rtk printf 'notifications session changed after replay\n'; exit 1; }
rtk printf 'billing and notifications replay kept one stable active session\n'

rtk proxy sh -c '. ./.env; export TEACHING_DATABASE_URL; cd services/teaching; rtk proxy go test ./internal/handler ./internal/http -count=1 -v'
rtk proxy sh -c '. ./.env; export BILLING_DATABASE_URL BROKER_SEEDS; cd services/billing; rtk proxy go test ./internal/store -run "^TestReplayRebuildsProjectionsAndNeverTouchesAnInvoice$" -count=1 -v'
rtk proxy sh -c '. ./.env; export NOTIFICATIONS_DATABASE_URL; cd services/notifications; rtk proxy go test ./internal/store -run "^TestProjectionReplayPreservesDigestRuns$" -count=1 -v'

rtk task schedule:conflicts
rtk printf 'recurring schedule verification harness passed\n'
