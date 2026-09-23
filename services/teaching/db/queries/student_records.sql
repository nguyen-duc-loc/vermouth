-- Student record reads and writes remain inside teaching. Every statement
-- includes the tutor identifier supplied by verified token claims.

-- name: ListStudents :many
SELECT
    st.student_id,
    st.name,
    lower(st.name) AS lower_name,
    st.phone,
    count(c.class_id)::bigint AS active_class_count,
    st.updated_at
FROM students st
LEFT JOIN roster_periods rp
  ON rp.tutor_id = st.tutor_id
 AND rp.student_id = st.student_id
 AND rp.effective_from <= sqlc.arg(local_date)::date
 AND (rp.effective_to IS NULL OR sqlc.arg(local_date)::date <= rp.effective_to)
LEFT JOIN classes c
  ON c.tutor_id = rp.tutor_id
 AND c.class_id = rp.class_id
 AND c.archived_at IS NULL
WHERE st.tutor_id = sqlc.arg(tutor_id)
  AND st.removed_at IS NULL
  AND (
      sqlc.arg(search_query)::text = ''
      OR strpos(lower(st.name), lower(sqlc.arg(search_query)::text)) > 0
      OR strpos(coalesce(st.phone, ''), sqlc.arg(search_query)::text) > 0
  )
  AND (
      NOT sqlc.arg(has_cursor)::boolean
      OR (lower(st.name), st.name, st.student_id) > (
          sqlc.arg(cursor_lower_name)::text,
          sqlc.arg(cursor_name)::text,
          sqlc.arg(cursor_student_id)::uuid
      )
  )
GROUP BY st.student_id, st.name, st.phone, st.updated_at
ORDER BY lower(st.name), st.name, st.student_id
LIMIT sqlc.arg(page_size);

-- name: GetActiveOwnedStudent :one
SELECT student_id, tutor_id, name, phone, created_at, updated_at, removed_at
FROM students
WHERE tutor_id = $1
  AND student_id = $2
  AND removed_at IS NULL;

-- name: LockActiveOwnedStudent :one
SELECT student_id, tutor_id, name, phone, created_at, updated_at, removed_at
FROM students
WHERE tutor_id = $1
  AND student_id = $2
  AND removed_at IS NULL
FOR UPDATE;

-- name: UpdateStudentRecord :one
UPDATE students
SET name = $3,
    phone = $4,
    updated_at = $5
WHERE tutor_id = $1
  AND student_id = $2
  AND removed_at IS NULL
RETURNING student_id, tutor_id, name, phone, created_at, updated_at, removed_at;

-- name: ArchiveStudentRecord :one
UPDATE students
SET removed_at = $3,
    updated_at = $3
WHERE tutor_id = $1
  AND student_id = $2
  AND removed_at IS NULL
RETURNING student_id, tutor_id, name, phone, created_at, updated_at, removed_at;

-- name: ListStudentMemberships :many
SELECT
    c.class_id,
    c.name AS class_name,
    c.color AS class_color,
    rp.effective_from,
    rp.effective_to,
    (
        c.archived_at IS NULL
        AND rp.effective_from <= sqlc.arg(local_date)::date
        AND (rp.effective_to IS NULL OR sqlc.arg(local_date)::date <= rp.effective_to)
    )::boolean AS active
FROM roster_periods rp
JOIN classes c
  ON c.tutor_id = rp.tutor_id
 AND c.class_id = rp.class_id
WHERE rp.tutor_id = sqlc.arg(tutor_id)
  AND rp.student_id = sqlc.arg(student_id)
ORDER BY active DESC, rp.effective_from DESC, lower(c.name), c.name, c.class_id;

-- name: ListActiveStudentClasses :many
SELECT c.class_id, c.name
FROM roster_periods rp
JOIN classes c
  ON c.tutor_id = rp.tutor_id
 AND c.class_id = rp.class_id
 AND c.archived_at IS NULL
WHERE rp.tutor_id = sqlc.arg(tutor_id)
  AND rp.student_id = sqlc.arg(student_id)
  AND rp.effective_from <= sqlc.arg(local_date)::date
  AND (rp.effective_to IS NULL OR sqlc.arg(local_date)::date <= rp.effective_to)
ORDER BY lower(c.name), c.name, c.class_id;
