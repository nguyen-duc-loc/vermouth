-- Hand written SQL for teaching, compiled to typed Go by sqlc (STK-3). No ORM,
-- and no SQL assembled by string concatenation at runtime.
--
-- Every statement here names tutor_id (INV-8, AC-4). The value comes from the
-- sub claim of the verified token and never from an input, so there is no cross
-- tutor read path and no table is reachable without the filter. Every list
-- filters on the end timestamp being empty, because nothing is ever deleted
-- (AC-11).

-- name: InsertStudent :one
INSERT INTO students (student_id, tutor_id, name, phone)
VALUES ($1, $2, $3, $4)
RETURNING student_id, tutor_id, name, phone, created_at, updated_at, removed_at;

-- name: InsertClass :one
INSERT INTO classes (
    class_id, tutor_id, name, color, rate_amount, currency, rate_effective_from,
    schedule_revision, rate_revision
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, 1
)
RETURNING class_id, tutor_id, name, color, rate_amount, currency, rate_effective_from,
          schedule_revision, rate_revision, created_at, updated_at, archived_at;

-- name: InsertScheduleRule :one
INSERT INTO schedule_rules (
    schedule_rule_id, tutor_id, class_id, revision, valid_from, valid_through, time_zone
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING schedule_rule_id, tutor_id, class_id, revision, valid_from, valid_through,
          time_zone, created_at, updated_at, replaced_at, ended_at, retired_at;

-- name: InsertScheduleSlot :one
INSERT INTO schedule_slots (
    schedule_rule_id, tutor_id, weekday, start_time, end_time
)
VALUES (
    sqlc.arg(schedule_rule_id),
    sqlc.arg(tutor_id),
    sqlc.arg(weekday),
    sqlc.arg(start_time)::text::time,
    sqlc.arg(end_time)::text::time
)
RETURNING schedule_rule_id, tutor_id, weekday,
          to_char(start_time, 'HH24:MI') AS start_time,
          to_char(end_time, 'HH24:MI') AS end_time,
          created_at, updated_at;

-- name: InsertSession :one
INSERT INTO sessions (
    session_id, class_id, tutor_id, starts_at, ends_at, local_date,
    schedule_rule_id, origin_local_date
)
VALUES (
    sqlc.arg(session_id),
    sqlc.arg(class_id),
    sqlc.arg(tutor_id),
    sqlc.arg(starts_at),
    sqlc.arg(ends_at),
    sqlc.arg(local_date),
    sqlc.arg(schedule_rule_id),
    coalesce(sqlc.narg(origin_local_date)::date, sqlc.arg(local_date)::date)
)
RETURNING session_id, class_id, tutor_id, starts_at, ends_at, local_date,
          schedule_rule_id, origin_local_date, version, moved_at, superseded_at,
          created_at, updated_at, cancelled_at;

-- ListSessionsForLocalDate is what the home screen asks for. The tutor's own day
-- is a date this service computed in their timezone and stored, so reading it
-- back needs no timezone arithmetic here.
-- name: ListSessionsForLocalDate :many
SELECT session_id, class_id, tutor_id, starts_at, ends_at, local_date
FROM sessions
WHERE tutor_id = $1
  AND local_date = $2
  AND cancelled_at IS NULL
  AND superseded_at IS NULL
ORDER BY starts_at, session_id;

-- The receipt is claimed before its business rows are inserted. A concurrent
-- loser receives no row, rolls back, then reads the committed winner.
-- name: InsertCommandReceipt :one
INSERT INTO command_receipts (
    tutor_id, operation, idempotency_key, request_hash,
    primary_resource_id, related_resource_id, context_snapshot
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (tutor_id, operation, idempotency_key) DO NOTHING
RETURNING tutor_id, operation, idempotency_key, request_hash,
          primary_resource_id, related_resource_id, created_at,
          context_snapshot, response_snapshot, response_status;

-- name: CompleteCommandReceipt :one
UPDATE command_receipts
SET response_snapshot = $4,
    response_status = $5
WHERE tutor_id = $1
  AND operation = $2
  AND idempotency_key = $3
  AND response_snapshot IS NULL
  AND response_status IS NULL
RETURNING tutor_id, operation, idempotency_key, request_hash,
          primary_resource_id, related_resource_id, created_at,
          context_snapshot, response_snapshot, response_status;

-- name: GetCommandReceipt :one
SELECT tutor_id, operation, idempotency_key, request_hash,
       primary_resource_id, related_resource_id, created_at,
       context_snapshot, response_snapshot, response_status
FROM command_receipts
WHERE tutor_id = $1
  AND operation = $2
  AND idempotency_key = $3;

-- name: GetOwnedClass :one
SELECT class_id, tutor_id, name, color, rate_amount, currency,
       rate_effective_from, schedule_revision, rate_revision, created_at, updated_at, archived_at
FROM classes
WHERE tutor_id = $1
  AND class_id = $2;

-- name: LockOwnedClass :one
SELECT class_id, tutor_id, name, color, rate_amount, currency,
       rate_effective_from, schedule_revision, rate_revision, created_at, updated_at, archived_at
FROM classes
WHERE tutor_id = $1
  AND class_id = $2
FOR UPDATE;

-- name: SetClassScheduleRevision :one
UPDATE classes
SET schedule_revision = $3,
    updated_at = $4
WHERE tutor_id = $1
  AND class_id = $2
RETURNING class_id, tutor_id, name, color, rate_amount, currency,
          rate_effective_from, schedule_revision, rate_revision, created_at, updated_at, archived_at;

-- name: GetEarliestRetainedClassSessionDate :one
SELECT min(local_date)::date
FROM sessions
WHERE tutor_id = $1
  AND class_id = $2;

-- SetOwnedClassRate always advances the monotonic rate revision. A backdated
-- correction changes history through its event but changes current class rate
-- only when its date is at least the current effective date.
-- name: SetOwnedClassRate :one
UPDATE classes
SET rate_revision = rate_revision + 1,
    rate_amount = CASE
        WHEN sqlc.arg(effective_from)::date >= rate_effective_from
            THEN sqlc.arg(rate_amount)::bigint
        ELSE rate_amount
    END,
    currency = CASE
        WHEN sqlc.arg(effective_from)::date >= rate_effective_from
            THEN sqlc.arg(currency)::text
        ELSE currency
    END,
    rate_effective_from = CASE
        WHEN sqlc.arg(effective_from)::date >= rate_effective_from
            THEN sqlc.arg(effective_from)::date
        ELSE rate_effective_from
    END,
    updated_at = sqlc.arg(updated_at)
WHERE tutor_id = sqlc.arg(tutor_id)
  AND class_id = sqlc.arg(class_id)
RETURNING class_id, tutor_id, name, color, rate_amount, currency,
          rate_effective_from, schedule_revision, rate_revision, created_at, updated_at, archived_at;

-- name: GetOwnedSession :one
SELECT session_id, class_id, tutor_id, starts_at, ends_at, local_date,
       schedule_rule_id, origin_local_date, version, moved_at, superseded_at,
       created_at, updated_at, cancelled_at
FROM sessions
WHERE tutor_id = $1
  AND session_id = $2;

-- name: GetOwnedSessionForUpdate :one
SELECT session_id, class_id, tutor_id, starts_at, ends_at, local_date,
       schedule_rule_id, origin_local_date, version, moved_at, superseded_at,
       created_at, updated_at, cancelled_at
FROM sessions
WHERE tutor_id = $1
  AND session_id = $2
FOR UPDATE;

-- name: GetSessionSourceRule :one
SELECT r.schedule_rule_id, r.tutor_id, r.class_id, r.revision, r.valid_from,
       r.valid_through, r.time_zone, r.created_at, r.updated_at,
       r.replaced_at, r.ended_at, r.retired_at
FROM schedule_rules r
JOIN sessions s
  ON s.tutor_id = r.tutor_id
 AND s.class_id = r.class_id
 AND s.schedule_rule_id = r.schedule_rule_id
WHERE s.tutor_id = $1
  AND s.session_id = $2;

-- name: GetOwnedStudent :one
SELECT student_id, tutor_id, name, phone, created_at, updated_at, removed_at
FROM students
WHERE tutor_id = $1
  AND student_id = $2;

-- ListHomeSessions pages session rows before roster students are joined, so a
-- large roster cannot consume the 51 row page proof.
-- name: ListHomeSessions :many
SELECT s.session_id, s.class_id, c.name AS class_name, c.color AS class_color,
       s.starts_at, s.ends_at, s.local_date
FROM sessions s
JOIN classes c
  ON c.tutor_id = s.tutor_id
 AND c.class_id = s.class_id
WHERE s.tutor_id = sqlc.arg(tutor_id)
  AND s.local_date = sqlc.arg(local_date)
  AND s.cancelled_at IS NULL
  AND s.superseded_at IS NULL
  AND c.archived_at IS NULL
  AND (
      NOT sqlc.arg(has_cursor)::boolean
      OR (s.starts_at, s.session_id) >
         (sqlc.arg(cursor_starts_at)::timestamptz, sqlc.arg(cursor_session_id)::uuid)
  )
ORDER BY s.starts_at, s.session_id
LIMIT sqlc.arg(page_size);

-- name: ListOwnedClassSessions :many
SELECT session_id, class_id, tutor_id, starts_at, ends_at, local_date,
       schedule_rule_id, origin_local_date, version, moved_at, superseded_at,
       created_at, updated_at, cancelled_at
FROM sessions
WHERE tutor_id = $1
  AND class_id = $2
ORDER BY starts_at, session_id;

-- name: FindAdoptableStandaloneSession :one
SELECT session_id, class_id, tutor_id, starts_at, ends_at, local_date,
       schedule_rule_id, origin_local_date, version, moved_at, superseded_at,
       created_at, updated_at, cancelled_at
FROM sessions
WHERE tutor_id = sqlc.arg(tutor_id)
  AND class_id = sqlc.arg(class_id)
  AND schedule_rule_id IS NULL
  AND starts_at = sqlc.arg(starts_at)
  AND ends_at = sqlc.arg(ends_at)
  AND local_date = sqlc.arg(local_date)
  AND moved_at IS NULL
  AND cancelled_at IS NULL
  AND superseded_at IS NULL
ORDER BY session_id
LIMIT 1
FOR UPDATE;

-- name: AdoptStandaloneSession :one
UPDATE sessions
SET schedule_rule_id = sqlc.arg(schedule_rule_id),
    origin_local_date = sqlc.arg(origin_local_date),
    version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE tutor_id = sqlc.arg(tutor_id)
  AND class_id = sqlc.arg(class_id)
  AND session_id = sqlc.arg(session_id)
  AND schedule_rule_id IS NULL
  AND moved_at IS NULL
  AND cancelled_at IS NULL
  AND superseded_at IS NULL
RETURNING session_id, class_id, tutor_id, starts_at, ends_at, local_date,
          schedule_rule_id, origin_local_date, version, moved_at, superseded_at,
          created_at, updated_at, cancelled_at;

-- name: GetFirstUpcomingClassSession :one
SELECT session_id, class_id, tutor_id, starts_at, ends_at, local_date,
       schedule_rule_id, origin_local_date, version, moved_at, superseded_at,
       created_at, updated_at, cancelled_at
FROM sessions
WHERE tutor_id = sqlc.arg(tutor_id)
  AND class_id = sqlc.arg(class_id)
  AND starts_at >= sqlc.arg(command_time)
  AND cancelled_at IS NULL
  AND superseded_at IS NULL
ORDER BY starts_at, session_id
LIMIT 1;

-- name: GetOwnedLatestScheduleRule :one
SELECT schedule_rule_id, tutor_id, class_id, revision, valid_from, valid_through,
       time_zone, created_at, updated_at, replaced_at, ended_at, retired_at
FROM schedule_rules
WHERE tutor_id = $1
  AND class_id = $2
ORDER BY revision DESC
LIMIT 1;

-- name: ListOwnedScheduleRulesForUpdate :many
SELECT schedule_rule_id, tutor_id, class_id, revision, valid_from, valid_through,
       time_zone, created_at, updated_at, replaced_at, ended_at, retired_at
FROM schedule_rules
WHERE tutor_id = $1
  AND class_id = $2
ORDER BY revision DESC
FOR UPDATE;

-- name: RetireScheduleRule :one
UPDATE schedule_rules
SET retired_at = $4,
    updated_at = $4
WHERE tutor_id = $1
  AND class_id = $2
  AND schedule_rule_id = $3
  AND retired_at IS NULL
RETURNING schedule_rule_id, tutor_id, class_id, revision, valid_from, valid_through,
          time_zone, created_at, updated_at, replaced_at, ended_at, retired_at;

-- name: ReplaceScheduleRule :one
UPDATE schedule_rules
SET valid_through = $4,
    replaced_at = $5,
    updated_at = $5
WHERE tutor_id = $1
  AND class_id = $2
  AND schedule_rule_id = $3
  AND retired_at IS NULL
RETURNING schedule_rule_id, tutor_id, class_id, revision, valid_from, valid_through,
          time_zone, created_at, updated_at, replaced_at, ended_at, retired_at;

-- name: EndScheduleRule :one
UPDATE schedule_rules
SET valid_through = $4,
    ended_at = $5,
    updated_at = $5
WHERE tutor_id = $1
  AND class_id = $2
  AND schedule_rule_id = $3
  AND retired_at IS NULL
RETURNING schedule_rule_id, tutor_id, class_id, revision, valid_from, valid_through,
          time_zone, created_at, updated_at, replaced_at, ended_at, retired_at;

-- name: ListRetainedScheduleExceptions :many
SELECT session_id, origin_local_date
FROM sessions
WHERE tutor_id = $1
  AND class_id = $2
  AND origin_local_date >= $3
  AND superseded_at IS NULL
  AND (moved_at IS NOT NULL OR cancelled_at IS NOT NULL)
ORDER BY origin_local_date, session_id;

-- name: SupersedeUntouchedScheduleSessions :many
UPDATE sessions
SET superseded_at = sqlc.arg(command_time),
    version = version + 1,
    updated_at = sqlc.arg(command_time)
WHERE tutor_id = sqlc.arg(tutor_id)
  AND class_id = sqlc.arg(class_id)
  AND schedule_rule_id IS NOT NULL
  AND origin_local_date >= sqlc.arg(affected_from)
  AND starts_at >= sqlc.arg(command_time)
  AND moved_at IS NULL
  AND cancelled_at IS NULL
  AND superseded_at IS NULL
RETURNING session_id, class_id, tutor_id, starts_at, ends_at, local_date,
          schedule_rule_id, origin_local_date, version, moved_at, superseded_at,
          created_at, updated_at, cancelled_at;

-- name: MoveOwnedSession :one
UPDATE sessions
SET starts_at = $3,
    ends_at = $4,
    local_date = $5,
    moved_at = $6,
    version = version + 1,
    updated_at = $6
WHERE tutor_id = $1
  AND session_id = $2
  AND cancelled_at IS NULL
  AND superseded_at IS NULL
RETURNING session_id, class_id, tutor_id, starts_at, ends_at, local_date,
          schedule_rule_id, origin_local_date, version, moved_at, superseded_at,
          created_at, updated_at, cancelled_at;

-- name: CancelOwnedSession :one
UPDATE sessions
SET cancelled_at = $3,
    version = version + 1,
    updated_at = $3
WHERE tutor_id = $1
  AND session_id = $2
  AND cancelled_at IS NULL
  AND superseded_at IS NULL
RETURNING session_id, class_id, tutor_id, starts_at, ends_at, local_date,
          schedule_rule_id, origin_local_date, version, moved_at, superseded_at,
          created_at, updated_at, cancelled_at;

-- name: RestoreOwnedSession :one
UPDATE sessions
SET cancelled_at = NULL,
    version = version + 1,
    updated_at = $3
WHERE tutor_id = $1
  AND session_id = $2
  AND cancelled_at IS NOT NULL
  AND superseded_at IS NULL
RETURNING session_id, class_id, tutor_id, starts_at, ends_at, local_date,
          schedule_rule_id, origin_local_date, version, moved_at, superseded_at,
          created_at, updated_at, cancelled_at;

-- name: FindOwnedSessionConflict :one
SELECT s.session_id, c.name AS class_name, s.starts_at, s.ends_at
FROM sessions s
JOIN classes c
  ON c.tutor_id = s.tutor_id
 AND c.class_id = s.class_id
WHERE s.tutor_id = sqlc.arg(tutor_id)
  AND s.session_id <> sqlc.arg(excluded_session_id)
  AND s.cancelled_at IS NULL
  AND s.superseded_at IS NULL
  AND tstzrange(s.starts_at, s.ends_at, '[)')
      && tstzrange(sqlc.arg(starts_at)::timestamptz, sqlc.arg(ends_at)::timestamptz, '[)')
ORDER BY s.starts_at, s.session_id
LIMIT 1;

-- name: ListScheduleClasses :many
SELECT class_id, name, color, schedule_revision, archived_at
FROM classes
WHERE tutor_id = $1
  AND archived_at IS NULL
ORDER BY name, class_id;

-- name: ListScheduleRules :many
SELECT schedule_rule_id, class_id, revision, valid_from, valid_through, time_zone,
       replaced_at, ended_at, retired_at
FROM schedule_rules
WHERE schedule_rules.tutor_id = sqlc.arg(tutor_id)
  AND (
      NOT sqlc.arg(has_class_filter)::boolean
      OR schedule_rules.class_id = ANY(sqlc.arg(class_ids)::uuid[])
  )
  AND (
      (schedule_rules.valid_from <= sqlc.arg(through_date)
       AND schedule_rules.valid_through >= sqlc.arg(from_date))
      OR schedule_rules.revision = (
          SELECT max(latest.revision)
          FROM schedule_rules latest
          WHERE latest.tutor_id = schedule_rules.tutor_id
            AND latest.class_id = schedule_rules.class_id
      )
      OR schedule_rules.schedule_rule_id IN (
          SELECT session_rule.schedule_rule_id
          FROM sessions session_rule
          WHERE session_rule.tutor_id = schedule_rules.tutor_id
            AND session_rule.schedule_rule_id IS NOT NULL
            AND (
                session_rule.origin_local_date BETWEEN sqlc.arg(from_date) AND sqlc.arg(through_date)
                OR session_rule.local_date BETWEEN sqlc.arg(from_date) AND sqlc.arg(through_date)
            )
      )
  )
ORDER BY schedule_rules.class_id, schedule_rules.revision;

-- name: ListScheduleSlots :many
SELECT schedule_rule_id, weekday,
       to_char(start_time, 'HH24:MI') AS start_time,
       to_char(end_time, 'HH24:MI') AS end_time
FROM schedule_slots
WHERE tutor_id = sqlc.arg(tutor_id)
  AND schedule_rule_id = ANY(sqlc.arg(schedule_rule_ids)::uuid[])
ORDER BY schedule_rule_id, weekday;

-- name: ListScheduleSessions :many
SELECT s.session_id, s.class_id, c.name AS class_name, c.color AS class_color,
       c.archived_at, s.starts_at, s.ends_at, s.local_date, s.origin_local_date,
       s.schedule_rule_id, r.time_zone AS source_time_zone, s.version,
       s.moved_at, s.cancelled_at, s.superseded_at, s.updated_at
FROM sessions s
JOIN classes c
  ON c.tutor_id = s.tutor_id
 AND c.class_id = s.class_id
LEFT JOIN schedule_rules r
  ON r.tutor_id = s.tutor_id
 AND r.class_id = s.class_id
 AND r.schedule_rule_id = s.schedule_rule_id
WHERE s.tutor_id = sqlc.arg(tutor_id)
  AND s.starts_at >= sqlc.arg(from_instant)
  AND s.starts_at < sqlc.arg(through_instant)
  AND s.superseded_at IS NULL
  AND (
      NOT sqlc.arg(has_class_filter)::boolean
      OR s.class_id = ANY(sqlc.arg(class_ids)::uuid[])
  )
ORDER BY s.starts_at, s.session_id;

-- name: ListReplacedScheduleSessions :many
SELECT s.session_id, s.class_id, c.name AS class_name, c.color AS class_color,
       c.archived_at, s.starts_at, s.ends_at, s.local_date, s.origin_local_date,
       s.schedule_rule_id, r.time_zone AS source_time_zone, s.version,
       s.moved_at, s.cancelled_at, s.superseded_at, s.updated_at
FROM sessions s
JOIN classes c
  ON c.tutor_id = s.tutor_id
 AND c.class_id = s.class_id
LEFT JOIN schedule_rules r
  ON r.tutor_id = s.tutor_id
 AND r.class_id = s.class_id
 AND r.schedule_rule_id = s.schedule_rule_id
WHERE s.tutor_id = sqlc.arg(tutor_id)
  AND s.origin_local_date >= sqlc.arg(from_date)
  AND s.origin_local_date <= sqlc.arg(through_date)
  AND s.superseded_at IS NOT NULL
  AND (
      NOT sqlc.arg(has_class_filter)::boolean
      OR s.class_id = ANY(sqlc.arg(class_ids)::uuid[])
  )
  AND (
      NOT sqlc.arg(has_cursor)::boolean
      OR (s.origin_local_date, s.session_id) >
         (sqlc.arg(cursor_origin_date)::date, sqlc.arg(cursor_session_id)::uuid)
  )
ORDER BY s.origin_local_date, s.session_id
LIMIT sqlc.arg(page_size);

-- ListSessionConflicts is the read only upgrade report. Each conflicting pair
-- appears once in stable tutor, time, and identifier order.
-- name: ListSessionConflicts :many
SELECT left_session.tutor_id,
       left_session.session_id AS first_session_id,
       left_session.class_id AS first_class_id,
       left_session.starts_at AS first_starts_at,
       left_session.ends_at AS first_ends_at,
       right_session.session_id AS second_session_id,
       right_session.class_id AS second_class_id,
       right_session.starts_at AS second_starts_at,
       right_session.ends_at AS second_ends_at
FROM sessions left_session
JOIN sessions right_session
  ON right_session.tutor_id = left_session.tutor_id
 AND right_session.session_id > left_session.session_id
 AND tstzrange(right_session.starts_at, right_session.ends_at, '[)')
     && tstzrange(left_session.starts_at, left_session.ends_at, '[)')
WHERE left_session.cancelled_at IS NULL
  AND left_session.superseded_at IS NULL
  AND right_session.cancelled_at IS NULL
  AND right_session.superseded_at IS NULL
ORDER BY left_session.tutor_id, left_session.starts_at, left_session.session_id,
         right_session.session_id;

-- name: ListHomeStudents :many
SELECT rp.class_id, sess.session_id, st.student_id, st.name,
       a.state AS attendance_state, a.marked_at
FROM sessions sess
JOIN roster_periods rp
  ON rp.tutor_id = sess.tutor_id
 AND rp.class_id = sess.class_id
 AND rp.effective_from <= sess.local_date
 AND (rp.effective_to IS NULL OR sess.local_date <= rp.effective_to)
JOIN students st
  ON st.tutor_id = rp.tutor_id
 AND st.student_id = rp.student_id
 AND st.removed_at IS NULL
LEFT JOIN attendance a
  ON a.tutor_id = sess.tutor_id
 AND a.session_id = sess.session_id
 AND a.student_id = st.student_id
WHERE sess.tutor_id = sqlc.arg(tutor_id)
  AND sess.session_id = ANY(sqlc.arg(session_ids)::uuid[])
ORDER BY sess.starts_at, sess.session_id, st.name, st.student_id;

-- Locking the owned session serialises corrections, including the first mark
-- where no attendance row exists yet. The roster flag uses the same inclusive
-- coverage predicate as every home and billing read.
-- name: GetAttendanceWriteContext :one
SELECT s.session_id, s.class_id, s.local_date,
       EXISTS (
           SELECT 1
           FROM roster_periods rp
           WHERE rp.tutor_id = s.tutor_id
             AND rp.class_id = s.class_id
             AND rp.student_id = st.student_id
             AND rp.effective_from <= s.local_date
             AND (rp.effective_to IS NULL OR s.local_date <= rp.effective_to)
       )::boolean AS rostered
FROM sessions s
JOIN students st
  ON st.tutor_id = s.tutor_id
 AND st.student_id = sqlc.arg(student_id)
 AND st.removed_at IS NULL
WHERE s.tutor_id = sqlc.arg(tutor_id)
  AND s.session_id = sqlc.arg(session_id)
  AND s.cancelled_at IS NULL
FOR UPDATE;

-- name: InsertRosterPeriod :one
INSERT INTO roster_periods (class_id, student_id, effective_from, tutor_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (class_id, student_id, effective_from) DO NOTHING
RETURNING class_id, student_id, effective_from, tutor_id, effective_to,
          created_at, updated_at;

-- UpsertAttendance keeps at most one row per session and student, so a
-- correction updates it in place rather than adding a second opinion, and a
-- redelivered mark lands on the same row (INV-6).
-- name: UpsertAttendance :one
INSERT INTO attendance (session_id, student_id, tutor_id, state, marked_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (session_id, student_id) DO UPDATE
SET state      = excluded.state,
    marked_at  = excluded.marked_at,
    updated_at = now()
WHERE attendance.tutor_id = excluded.tutor_id
RETURNING session_id, student_id, tutor_id, state, marked_at, created_at, updated_at;

-- name: GetAttendance :one
SELECT session_id, student_id, tutor_id, state, marked_at, created_at, updated_at
FROM attendance
WHERE tutor_id = $1
  AND session_id = $2
  AND student_id = $3;

-- OpenRosterPeriod starts a membership. Joining a class the student is already
-- in on the same day changes nothing, which is what makes a repeated join safe.
-- name: OpenRosterPeriod :exec
INSERT INTO roster_periods (class_id, student_id, effective_from, tutor_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (class_id, student_id, effective_from) DO NOTHING;

-- CloseRosterPeriod closes the one open row for the pair, which the partial
-- unique index makes well defined: teaching.roster.left carries no
-- effective_from, so the open row is the only possible target. A second left for
-- the same pair updates nothing.
-- name: CloseRosterPeriod :exec
UPDATE roster_periods
SET effective_to = $4,
    updated_at   = now()
WHERE tutor_id = $1
  AND class_id = $2
  AND student_id = $3
  AND effective_to IS NULL;

-- name: FindOpenRosterPeriod :one
SELECT class_id, student_id, effective_from, tutor_id, effective_to
FROM roster_periods
WHERE tutor_id = $1
  AND class_id = $2
  AND student_id = $3
  AND effective_to IS NULL;

-- ListRosterPeriods is the whole membership history of one pair, oldest first,
-- so a rejoin reads as two periods rather than one edited row.
-- name: ListRosterPeriods :many
SELECT class_id, student_id, effective_from, tutor_id, effective_to
FROM roster_periods
WHERE tutor_id = $1
  AND class_id = $2
  AND student_id = $3
ORDER BY effective_from;
