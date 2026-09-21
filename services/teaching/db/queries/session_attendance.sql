-- Whole roster attendance reads use retained student labels and never select a
-- phone value. Saves lock the class and session through existing owned queries.

-- name: GetAttendanceSheetSession :one
SELECT
    s.session_id,
    s.class_id,
    c.name AS class_name,
    c.color AS class_color,
    s.starts_at,
    s.ends_at,
    s.local_date,
    s.cancelled_at,
    s.superseded_at,
    c.archived_at AS class_archived_at
FROM sessions s
JOIN classes c
  ON c.tutor_id = s.tutor_id
 AND c.class_id = s.class_id
WHERE s.tutor_id = $1
  AND s.session_id = $2;

-- name: ListAttendanceSheetStudents :many
SELECT
    st.student_id,
    st.name,
    st.removed_at,
    a.state,
    a.marked_at,
    a.updated_at AS attendance_updated_at
FROM sessions sess
JOIN roster_periods rp
  ON rp.tutor_id = sess.tutor_id
 AND rp.class_id = sess.class_id
 AND rp.effective_from <= sess.local_date
 AND (rp.effective_to IS NULL OR sess.local_date <= rp.effective_to)
JOIN students st
  ON st.tutor_id = rp.tutor_id
 AND st.student_id = rp.student_id
LEFT JOIN attendance a
  ON a.tutor_id = sess.tutor_id
 AND a.session_id = sess.session_id
 AND a.student_id = st.student_id
WHERE sess.tutor_id = $1
  AND sess.session_id = $2
ORDER BY lower(st.name), st.name, st.student_id;

-- name: UpsertAttendanceAt :one
INSERT INTO attendance (
    session_id, student_id, tutor_id, state, marked_at, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $5, $5)
ON CONFLICT (session_id, student_id) DO UPDATE
SET state = excluded.state,
    marked_at = excluded.marked_at,
    updated_at = excluded.updated_at
WHERE attendance.tutor_id = excluded.tutor_id
RETURNING session_id, student_id, tutor_id, state, marked_at, created_at, updated_at;
