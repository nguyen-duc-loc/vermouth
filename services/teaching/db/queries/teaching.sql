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
INSERT INTO classes (class_id, tutor_id, name, rate_amount, currency, rate_effective_from)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING class_id, tutor_id, name, rate_amount, currency, rate_effective_from,
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
