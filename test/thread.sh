#!/usr/bin/env sh
# Drive the first real teaching path through every runtime boundary.
set -e

if [ "${VERMOUTH_DEV_MODE:-platform}" = host ]; then
  GATEWAY="http://localhost${GATEWAY_HTTP_ADDR:-:8080}"
  TOKEN_TASK=dev:token:host
else
  GATEWAY=http://vermouth.localhost:8080
  TOKEN_TASK=dev:token
fi

printf 'Creating a tutor with task dev:token, no browser and no Google\n'
REGISTERED=$(task --silent "$TOKEN_TASK")

TOKEN=$(printf '%s' "$REGISTERED" | jq -er '
  select(type == "object" and .schema_version == 1) |
  .access_token | select(type == "string" and length > 0)
' 2>/dev/null || true)
TUTOR=$(printf '%s' "$REGISTERED" | jq -er '
  select(type == "object" and .schema_version == 1) |
  .tutor_id | select(type == "string" and length > 0)
' 2>/dev/null || true)
if [ -z "$TOKEN" ] || [ -z "$TUTOR" ]; then
  echo "devtoken did not answer with a tutor and a token:"
  echo "$REGISTERED"
  exit 1
fi

AUTH="Authorization: Bearer $TOKEN"
printf 'The gateway is at %s\n' "$GATEWAY"
printf 'tutor_id %s is ready for the teaching thread\n' "$TUTOR"

printf 'Waiting for the authenticated home read'
i=0
HOME=
while [ $i -lt 40 ]; do
  if HOME=$(curl -fsS "$GATEWAY/api/home" -H "$AUTH"); then
    if printf '%s' "$HOME" | jq -e '.local_date and .setup_defaults' >/dev/null 2>&1; then
      printf '\n'
      break
    fi
  fi
  printf '.'
  i=$((i + 1))
  sleep 1
done
if [ $i -ge 40 ]; then
  printf '\nThe home read never became available.\n'
  exit 1
fi

LOCAL_DATE=$(printf '%s' "$HOME" | jq -er '.local_date')
CLASS_KEY="thread-class-$TUTOR"
STUDENT_KEY="thread-student-$TUTOR"
CLASS_BODY=$(jq -n \
  --arg name "Thread class $TUTOR" \
  --arg local_date "$LOCAL_DATE" \
  '{name:$name,color:null,rate_amount:150000,first_session:{local_date:$local_date,start_time:"10:00",end_time:"11:00"}}')

printf 'Creating one class and first session\n'
CLASS_RESULT=$(curl -fsS -X POST "$GATEWAY/api/classes" \
  -H "$AUTH" -H 'Content-Type: application/json' -H "Idempotency-Key: $CLASS_KEY" \
  --data "$CLASS_BODY")
CLASS_ID=$(printf '%s' "$CLASS_RESULT" | jq -er '.class.class_id')
SESSION_ID=$(printf '%s' "$CLASS_RESULT" | jq -er '.first_session.session_id')

CLASS_REPLAY=$(curl -fsS -X POST "$GATEWAY/api/classes" \
  -H "$AUTH" -H 'Content-Type: application/json' -H "Idempotency-Key: $CLASS_KEY" \
  --data "$CLASS_BODY")
test "$(printf '%s' "$CLASS_REPLAY" | jq -er '.class.class_id')" = "$CLASS_ID"
test "$(printf '%s' "$CLASS_REPLAY" | jq -er '.first_session.session_id')" = "$SESSION_ID"

printf 'Creating one student and replaying the safe command\n'
STUDENT_BODY=$(jq -n --arg name "Thread student $TUTOR" '{name:$name,phone:null}')
STUDENT_RESULT=$(curl -fsS -X POST "$GATEWAY/api/students" \
  -H "$AUTH" -H 'Content-Type: application/json' -H "Idempotency-Key: $STUDENT_KEY" \
  --data "$STUDENT_BODY")
STUDENT_ID=$(printf '%s' "$STUDENT_RESULT" | jq -er '.student_id')
STUDENT_REPLAY=$(curl -fsS -X POST "$GATEWAY/api/students" \
  -H "$AUTH" -H 'Content-Type: application/json' -H "Idempotency-Key: $STUDENT_KEY" \
  --data "$STUDENT_BODY")
test "$(printf '%s' "$STUDENT_REPLAY" | jq -er '.student_id')" = "$STUDENT_ID"

printf 'Joining the roster and marking Present\n'
ROSTER_BODY=$(jq -n --arg student_id "$STUDENT_ID" --arg effective_from "$LOCAL_DATE" \
  '{student_id:$student_id,effective_from:$effective_from}')
curl -fsS -X POST "$GATEWAY/api/classes/$CLASS_ID/roster" \
  -H "$AUTH" -H 'Content-Type: application/json' --data "$ROSTER_BODY" >/dev/null
curl -fsS -X PUT "$GATEWAY/api/sessions/$SESSION_ID/attendance/$STUDENT_ID" \
  -H "$AUTH" -H 'Content-Type: application/json' --data '{"state":"Present"}' >/dev/null

printf 'Waiting for billing to project all five facts'
START=$(date +%s)
i=0
FINAL=
while [ $i -lt 40 ]; do
  if FINAL=$(curl -fsS "$GATEWAY/api/home" -H "$AUTH"); then
    if printf '%s' "$FINAL" | jq -e \
      --arg session_id "$SESSION_ID" --arg student_id "$STUDENT_ID" '
        .billing_projection.state == "active" and
        any(.sessions[];
          .session_id == $session_id and
          any(.students[]; .student_id == $student_id and .attendance_state == "Present"))
      ' >/dev/null 2>&1; then
      ELAPSED=$(( $(date +%s) - START ))
      printf '\n\nThe teaching thread is complete after about %ss.\n' "$ELAPSED"
      printf 'class_id %s\nsession_id %s\nstudent_id %s\n' "$CLASS_ID" "$SESSION_ID" "$STUDENT_ID"
      exit 0
    fi
  fi
  printf '.'
  i=$((i + 1))
  sleep 1
done

printf '\n\nThe teaching facts never converged in home and billing. The last answer was:\n%s\n' "$FINAL"
if [ "${VERMOUTH_DEV_MODE:-platform}" = host ]; then
  echo "Look at .tmp/logs/teaching.log and .tmp/logs/billing.log."
else
  echo "Run task platform:logs -- teaching and task platform:logs -- billing."
fi
exit 1
