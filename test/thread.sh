#!/usr/bin/env sh
# Drive the skeleton's one end to end thread and watch it complete.
#
# A request from the browser's point of view, through the gateway, into
# identity, out of its outbox, through the relay into Redpanda, and consumed by
# notifications, which records it. That is the thread spec 0002 asks the
# skeleton to prove, and feature 8 thickens into real behaviour.
set -e

if [ "${VERMOUTH_DEV_MODE:-platform}" = host ]; then
  GATEWAY="http://localhost${GATEWAY_HTTP_ADDR:-:8080}"
  TOKEN_TASK=dev:token:host
else
  GATEWAY=http://vermouth.localhost
  TOKEN_TASK=dev:token
fi

# Sign in needs Google, a browser and an internet connection since spec 0004, so
# the thread starts from the development only program instead (AC-13). It writes
# the tutor and its event in one transaction, exactly as the callback does, which
# is the part of the thread this script is here to prove.
printf 'Creating a tutor with task dev:token, no browser and no Google\n'
REGISTERED=$(task --silent "$TOKEN_TASK")

TOKEN=$(printf '%s' "$REGISTERED" | sed -n 's/.*"access_token": "\([^"]*\)".*/\1/p')
TUTOR=$(printf '%s' "$REGISTERED" | sed -n 's/.*"tutor_id": "\([^"]*\)".*/\1/p')
if [ -z "$TOKEN" ] || [ -z "$TUTOR" ]; then
  echo "devtoken did not answer with a tutor and a token:"
  echo "$REGISTERED"
  exit 1
fi
printf 'The gateway is at %s\n' "$GATEWAY"
printf 'tutor_id %s written in identity, with its event in the same transaction\n' "$TUTOR"

printf 'Waiting for the relay to publish it and notifications to record it'
START=$(date +%s)
i=0
while [ $i -lt 40 ]; do
  THREAD=$(curl -fsS "$GATEWAY/api/thread" -H "Authorization: Bearer $TOKEN")
  case "$THREAD" in
    *'"recorded":true'*)
      ELAPSED=$(( $(date +%s) - START ))
      printf '\n\nThe thread is complete after about %ss:\n%s\n' "$ELAPSED" "$THREAD"
      exit 0
      ;;
  esac
  printf '.'
  i=$((i + 1))
  sleep 1
done

printf '\n\nThe event never reached notifications. The last answer was:\n%s\n' "$THREAD"
if [ "${VERMOUTH_DEV_MODE:-platform}" = host ]; then
  echo "Look at .tmp/logs/identity.log for the relay, and .tmp/logs/notifications.log for the consumer."
else
  echo "Run task platform:logs -- identity and task platform:logs -- notifications."
fi
exit 1
