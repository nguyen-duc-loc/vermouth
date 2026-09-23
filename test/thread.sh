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

printf 'Saving and reloading the tutor invoice profile\n'
PROFILE=$(curl -fsS "$GATEWAY/api/invoice-profile" -H "$AUTH")
PROFILE_REVISION=$(printf '%s' "$PROFILE" | jq -er '.revision')
BANKS=$(curl -fsS "$GATEWAY/api/banks" -H "$AUTH")
printf '%s' "$BANKS" | jq -e 'any(.banks[]; .code == "970436" and .short_name == "Vietcombank")' >/dev/null
PROFILE_BODY=$(jq -n --argjson revision "$PROFILE_REVISION" '{
  expected_revision:$revision,
  legal_name:"Thread Tutor",
  contact_line:"thread@example.com",
  bank_code:"970436",
  bank_account_number:"THREAD123",
  bank_account_holder:"THREAD TUTOR"
}')
SAVED_PROFILE=$(curl -fsS -X PUT "$GATEWAY/api/invoice-profile" \
  -H "$AUTH" -H 'Content-Type: application/json' --data "$PROFILE_BODY")
printf '%s' "$SAVED_PROFILE" | jq -e '
  .is_complete == true and .bank_status == "active" and .bank_name == "Vietcombank"
' >/dev/null
RETRIED_PROFILE=$(curl -fsS -X PUT "$GATEWAY/api/invoice-profile" \
  -H "$AUTH" -H 'Content-Type: application/json' --data "$PROFILE_BODY")
test "$(printf '%s' "$RETRIED_PROFILE" | jq -er '.revision')" = \
  "$(printf '%s' "$SAVED_PROFILE" | jq -er '.revision')"
RELOADED_PROFILE=$(curl -fsS "$GATEWAY/api/invoice-profile" -H "$AUTH")
test "$(printf '%s' "$RELOADED_PROFILE" | jq -er '.revision')" = \
  "$(printf '%s' "$SAVED_PROFILE" | jq -er '.revision')"

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

printf 'Adopting the first session into a seven day weekly rule\n'
SCHEDULE_KEY="thread-schedule-$TUTOR"
SCHEDULE_BODY=$(jq -n --arg date "$LOCAL_DATE" \
  '{expected_revision:0,effective_from:$date,valid_through:$date,slots:[
    {weekday:1,start_time:"10:00",end_time:"11:00"},
    {weekday:2,start_time:"10:00",end_time:"11:00"},
    {weekday:3,start_time:"10:00",end_time:"11:00"},
    {weekday:4,start_time:"10:00",end_time:"11:00"},
    {weekday:5,start_time:"10:00",end_time:"11:00"},
    {weekday:6,start_time:"10:00",end_time:"11:00"},
    {weekday:7,start_time:"10:00",end_time:"11:00"}
  ]}')
SCHEDULE_RESULT=$(curl -fsS -X PUT "$GATEWAY/api/classes/$CLASS_ID/schedule" \
  -H "$AUTH" -H 'Content-Type: application/json' -H "Idempotency-Key: $SCHEDULE_KEY" \
  --data "$SCHEDULE_BODY")
test "$(printf '%s' "$SCHEDULE_RESULT" | jq -er '.candidate_count')" = 1
test "$(printf '%s' "$SCHEDULE_RESULT" | jq -er '.adopted_count')" = 1

printf 'Moving, cancelling, and restoring the recurring occurrence\n'
MOVE_BODY=$(jq -n --arg date "$LOCAL_DATE" \
  '{expected_version:2,local_date:$date,start_time:"00:00",end_time:"23:59"}')
MOVE_RESULT=$(curl -fsS -X POST "$GATEWAY/api/sessions/$SESSION_ID/move" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: thread-move-$TUTOR" --data "$MOVE_BODY")
test "$(printf '%s' "$MOVE_RESULT" | jq -er '.version')" = 3
test "$(printf '%s' "$MOVE_RESULT" | jq -er '.state')" = active
CANCEL_RESULT=$(curl -fsS -X POST "$GATEWAY/api/sessions/$SESSION_ID/cancel" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: thread-cancel-$TUTOR" --data '{"expected_version":3}')
test "$(printf '%s' "$CANCEL_RESULT" | jq -er '.state')" = cancelled
RESTORE_RESULT=$(curl -fsS -X POST "$GATEWAY/api/sessions/$SESSION_ID/restore" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: thread-restore-$TUTOR" --data '{"expected_version":4}')
test "$(printf '%s' "$RESTORE_RESULT" | jq -er '.state')" = active

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

printf 'Saving one roster delta and one whole roster attendance pass\n'
ROSTER_BODY=$(jq -n --arg student_id "$STUDENT_ID" --arg change_date "$LOCAL_DATE" \
  '{change_date:$change_date,additions:[$student_id],removals:[]}')
curl -fsS -X PUT "$GATEWAY/api/classes/$CLASS_ID/roster" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: thread-roster-$TUTOR" --data "$ROSTER_BODY" >/dev/null
ATTENDANCE_SHEET=$(curl -fsS "$GATEWAY/api/sessions/$SESSION_ID/attendance" -H "$AUTH")
ATTENDANCE_REVISION=$(printf '%s' "$ATTENDANCE_SHEET" | jq -er '.revision')
ATTENDANCE_BODY=$(jq -n --arg revision "$ATTENDANCE_REVISION" --arg student_id "$STUDENT_ID" \
  '{revision:$revision,marks:[{student_id:$student_id,state:"Present"}]}')
curl -fsS -X PUT "$GATEWAY/api/sessions/$SESSION_ID/attendance" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: thread-attendance-$TUTOR" --data "$ATTENDANCE_BODY" >/dev/null

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
      printf '\nThe teaching thread is complete after about %ss.\n' "$ELAPSED"
      printf 'Driving one dated rate through monthly preview, issue, retry, and immutable reload\n'

      DEFAULT_PERIOD=$(curl -fsS "$GATEWAY/api/billing-periods/default" -H "$AUTH")
      BILLING_YEAR=$(printf '%s' "$DEFAULT_PERIOD" | jq -er '.year')
      BILLING_MONTH=$(printf '%s' "$DEFAULT_PERIOD" | jq -er '.month')
      BILLING_DATE=$(printf '%04d-%02d-15' "$BILLING_YEAR" "$BILLING_MONTH")

      BILLING_CLASS_BODY=$(jq -n \
        --arg name "Billing thread class $TUTOR" \
        --arg local_date "$BILLING_DATE" \
        '{name:$name,color:"green",rate_amount:150000,first_session:{local_date:$local_date,start_time:"10:00",end_time:"11:00"}}')
      BILLING_CLASS_RESULT=$(curl -fsS -X POST "$GATEWAY/api/classes" \
        -H "$AUTH" -H 'Content-Type: application/json' \
        -H "Idempotency-Key: billing-class-$TUTOR" --data "$BILLING_CLASS_BODY")
      BILLING_CLASS_ID=$(printf '%s' "$BILLING_CLASS_RESULT" | jq -er '.class.class_id')
      BILLING_SESSION_ID=$(printf '%s' "$BILLING_CLASS_RESULT" | jq -er '.first_session.session_id')

      RATE_RESULT=$(curl -fsS -X PUT \
        "$GATEWAY/api/classes/$BILLING_CLASS_ID/rates/$BILLING_DATE" \
        -H "$AUTH" -H 'Content-Type: application/json' \
        -H "Idempotency-Key: billing-rate-$TUTOR" --data '{"rate_amount":175000}')
      RATE_REVISION=$(printf '%s' "$RATE_RESULT" | jq -er '.rate_revision')
      test "$(printf '%s' "$RATE_RESULT" | jq -er '.issued_invoices_unchanged')" = true

      BILLING_STUDENT_RESULT=$(curl -fsS -X POST "$GATEWAY/api/students" \
        -H "$AUTH" -H 'Content-Type: application/json' \
        -H "Idempotency-Key: billing-student-$TUTOR" \
        --data "$(jq -n --arg name "Billing student $TUTOR" '{name:$name,phone:null}')")
      BILLING_STUDENT_ID=$(printf '%s' "$BILLING_STUDENT_RESULT" | jq -er '.student_id')
      curl -fsS -X PUT "$GATEWAY/api/classes/$BILLING_CLASS_ID/roster" \
        -H "$AUTH" -H 'Content-Type: application/json' \
        -H "Idempotency-Key: billing-roster-$TUTOR" \
        --data "$(jq -n --arg student "$BILLING_STUDENT_ID" --arg date "$BILLING_DATE" \
          '{change_date:$date,additions:[$student],removals:[]}')" >/dev/null
      BILLING_ATTENDANCE=$(curl -fsS "$GATEWAY/api/sessions/$BILLING_SESSION_ID/attendance" -H "$AUTH")
      BILLING_ATTENDANCE_REVISION=$(printf '%s' "$BILLING_ATTENDANCE" | jq -er '.revision')
      curl -fsS -X PUT "$GATEWAY/api/sessions/$BILLING_SESSION_ID/attendance" \
        -H "$AUTH" -H 'Content-Type: application/json' \
        -H "Idempotency-Key: billing-attendance-$TUTOR" \
        --data "$(jq -n --arg revision "$BILLING_ATTENDANCE_REVISION" --arg student "$BILLING_STUDENT_ID" \
          '{revision:$revision,marks:[{student_id:$student,state:"Present"}]}')" >/dev/null

      printf 'Waiting for the exact dated rate revision to reach billing'
      j=0
      while [ $j -lt 20 ]; do
        RATE_HISTORY=$(curl -fsS "$GATEWAY/api/classes/$BILLING_CLASS_ID/rates" -H "$AUTH")
        if printf '%s' "$RATE_HISTORY" | jq -e \
          --arg date "$BILLING_DATE" --argjson revision "$RATE_REVISION" '
            .history_state == "synced" and
            any(.rates[]; .effective_from == $date and .rate_revision >= $revision)
          ' >/dev/null 2>&1; then
          printf '\n'
          break
        fi
        printf '.'
        j=$((j + 1))
        sleep 1
      done
      if [ $j -ge 20 ]; then
        printf '\nThe dated rate did not reach billing.\n'
        exit 1
      fi

      printf 'Waiting for the complete monthly preview'
      j=0
      PREVIEW=
      while [ $j -lt 20 ]; do
        if PREVIEW=$(curl -fsS -X POST \
          "$GATEWAY/api/billing-periods/$BILLING_YEAR/$BILLING_MONTH/preview" -H "$AUTH"); then
          if printf '%s' "$PREVIEW" | jq -e \
            --arg student "$BILLING_STUDENT_ID" --arg session "$BILLING_SESSION_ID" '
              .status == "ready" and .grand_total == 175000 and
              (.blockers | length) == 0 and
              any(.students[];
                .student_id == $student and .total_amount == 175000 and
                any(.lines[]; .session_id == $session and .amount == 175000))
            ' >/dev/null 2>&1; then
            printf '\n'
            break
          fi
        fi
        printf '.'
        j=$((j + 1))
        sleep 1
      done
      if [ $j -ge 20 ]; then
        printf '\nThe complete monthly preview did not become ready. The last answer was:\n%s\n' "$PREVIEW"
        exit 1
      fi
      FINGERPRINT=$(printf '%s' "$PREVIEW" | jq -er '.preview_fingerprint')
      ISSUE_BODY=$(jq -n --arg fingerprint "$FINGERPRINT" '{preview_fingerprint:$fingerprint}')

      RATE_BEFORE_ISSUE=$(curl -fsS -X PUT "$GATEWAY/api/classes/$BILLING_CLASS_ID/rates/$BILLING_DATE" \
        -H "$AUTH" -H 'Content-Type: application/json' \
        -H "Idempotency-Key: billing-rate-before-issue-$TUTOR" \
        --data '{"rate_amount":180000}')
      RATE_BEFORE_ISSUE_REVISION=$(printf '%s' "$RATE_BEFORE_ISSUE" | jq -er '.rate_revision')
      j=0
      while [ $j -lt 20 ]; do
        RATE_HISTORY=$(curl -fsS "$GATEWAY/api/classes/$BILLING_CLASS_ID/rates" -H "$AUTH")
        if printf '%s' "$RATE_HISTORY" | jq -e \
          --arg date "$BILLING_DATE" --argjson revision "$RATE_BEFORE_ISSUE_REVISION" '
            any(.rates[]; .effective_from == $date and .rate_revision >= $revision)
          ' >/dev/null 2>&1; then
          break
        fi
        j=$((j + 1))
        sleep 1
      done
      if [ $j -ge 20 ]; then
        echo "The changed rate did not reach billing before the stale preview check."
        exit 1
      fi
      STALE_STATUS=$(curl -sS -o .tmp/thread-stale-preview.json -w '%{http_code}' -X POST \
        "$GATEWAY/api/billing-periods/$BILLING_YEAR/$BILLING_MONTH/issue" \
        -H "$AUTH" -H 'Content-Type: application/json' --data "$ISSUE_BODY")
      test "$STALE_STATUS" = 409
      jq -e '.error.code == "preview_stale"' .tmp/thread-stale-preview.json >/dev/null

      PREVIEW=$(curl -fsS -X POST \
        "$GATEWAY/api/billing-periods/$BILLING_YEAR/$BILLING_MONTH/preview" -H "$AUTH")
      printf '%s' "$PREVIEW" | jq -e '.status == "ready" and .grand_total == 180000' >/dev/null
      FINGERPRINT=$(printf '%s' "$PREVIEW" | jq -er '.preview_fingerprint')
      ISSUE_BODY=$(jq -n --arg fingerprint "$FINGERPRINT" '{preview_fingerprint:$fingerprint}')
      ISSUED=$(curl -fsS -X POST \
        "$GATEWAY/api/billing-periods/$BILLING_YEAR/$BILLING_MONTH/issue" \
        -H "$AUTH" -H 'Content-Type: application/json' --data "$ISSUE_BODY")
      RUN_ID=$(printf '%s' "$ISSUED" | jq -er '.billing_run_id')
      printf '%s' "$ISSUED" | jq -e '.invoices | length == 1' >/dev/null

      RETRIED_ISSUE=$(curl -fsS -X POST \
        "$GATEWAY/api/billing-periods/$BILLING_YEAR/$BILLING_MONTH/issue" \
        -H "$AUTH" -H 'Content-Type: application/json' --data "$ISSUE_BODY")
      test "$(printf '%s' "$RETRIED_ISSUE" | jq -er '.billing_run_id')" = "$RUN_ID"

      curl -fsS -X PUT "$GATEWAY/api/classes/$BILLING_CLASS_ID/rates/$BILLING_DATE" \
        -H "$AUTH" -H 'Content-Type: application/json' \
        -H "Idempotency-Key: billing-rate-after-issue-$TUTOR" \
        --data '{"rate_amount":200000}' >/dev/null
      RELOADED_RUN=$(curl -fsS \
        "$GATEWAY/api/billing-periods/$BILLING_YEAR/$BILLING_MONTH" -H "$AUTH")
      printf '%s' "$RELOADED_RUN" | jq -e \
        --arg run "$RUN_ID" '
          .status == "already_issued" and .run.billing_run_id == $run and
          .run.grand_total == 180000 and .run.invoices[0].lines[0].amount == 180000
        ' >/dev/null

      printf '\nThe money thread is complete.\n'
      printf 'class_id %s\nsession_id %s\nstudent_id %s\nbilling_run_id %s\n' \
        "$BILLING_CLASS_ID" "$BILLING_SESSION_ID" "$BILLING_STUDENT_ID" "$RUN_ID"
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
