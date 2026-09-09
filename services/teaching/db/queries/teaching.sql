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
INSERT INTO classes (class_id, tutor_id, name, color, rate_amount, currency, rate_effective_from)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING class_id, tutor_id, name, color, rate_amount, currency, rate_effective_from,
          created_at, updated_at, archived_at;

-- name: InsertSession :one
INSERT INTO sessions (session_id, class_id, tutor_id, starts_at, ends_at, local_date, schedule_rule_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING session_id, class_id, tutor_id, starts_at, ends_at, local_date,
          schedule_rule_id, created_at, updated_at, cancelled_at;

-- ListSessionsForLocalDate is what the home screen asks for. The tutor's own day
-- is a date this service computed in their timezone and stored, so reading it
-- back needs no timezone arithmetic here.
-- name: ListSessionsForLocalDate :many
SELECT session_id, class_id, tutor_id, starts_at, ends_at, local_date
FROM sessions
WHERE tutor_id = $1
  AND local_date = $2
  AND cancelled_at IS NULL
ORDER BY starts_at, session_id;

-- The receipt is claimed before its business rows are inserted. A concurrent
-- loser receives no row, rolls back, then reads the committed winner.
-- name: InsertCommandReceipt :one
INSERT INTO command_receipts (
    tutor_id, operation, idempotency_key, request_hash,
    primary_resource_id, related_resource_id
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (tutor_id, operation, idempotency_key) DO NOTHING
RETURNING tutor_id, operation, idempotency_key, request_hash,
          primary_resource_id, related_resource_id, created_at;

-- name: GetCommandReceipt :one
SELECT tutor_id, operation, idempotency_key, request_hash,
       primary_resource_id, related_resource_id, created_at
FROM command_receipts
WHERE tutor_id = $1
  AND operation = $2
  AND idempotency_key = $3;

-- name: GetOwnedClass :one
SELECT class_id, tutor_id, name, color, rate_amount, currency,
       rate_effective_from, created_at, updated_at, archived_at
FROM classes
WHERE tutor_id = $1
  AND class_id = $2;

-- name: GetOwnedSession :one
SELECT session_id, class_id, tutor_id, starts_at, ends_at, local_date,
       schedule_rule_id, created_at, updated_at, cancelled_at
FROM sessions
WHERE tutor_id = $1
  AND session_id = $2;

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
  AND c.archived_at IS NULL
  AND (
      NOT sqlc.arg(has_cursor)::boolean
      OR (s.starts_at, s.session_id) >
         (sqlc.arg(cursor_starts_at)::timestamptz, sqlc.arg(cursor_session_id)::uuid)
  )
ORDER BY s.starts_at, s.session_id
LIMIT sqlc.arg(page_size);

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
