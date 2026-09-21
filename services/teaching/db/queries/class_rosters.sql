-- Dated roster reads and writes use the same inclusive date predicate in every
-- statement. The class lock in the handler serialises competing deltas.

-- name: ListClassRosterAtDate :many
SELECT
    st.student_id,
    st.name,
    st.phone,
    st.removed_at,
    rp.effective_from,
    rp.effective_to
FROM roster_periods rp
JOIN students st
  ON st.tutor_id = rp.tutor_id
 AND st.student_id = rp.student_id
WHERE rp.tutor_id = sqlc.arg(tutor_id)
  AND rp.class_id = sqlc.arg(class_id)
  AND rp.effective_from <= sqlc.arg(resolved_date)::date
  AND (rp.effective_to IS NULL OR sqlc.arg(resolved_date)::date <= rp.effective_to)
ORDER BY lower(st.name), st.name, st.student_id;

-- name: LockRosterStudents :many
SELECT student_id, tutor_id, name, phone, created_at, updated_at, removed_at
FROM students
WHERE tutor_id = sqlc.arg(tutor_id)
  AND student_id = ANY(sqlc.arg(student_ids)::uuid[])
ORDER BY student_id
FOR UPDATE;

-- name: ListRosterPeriodsForStudents :many
SELECT class_id, student_id, effective_from, tutor_id, effective_to,
       created_at, updated_at
FROM roster_periods
WHERE tutor_id = sqlc.arg(tutor_id)
  AND class_id = sqlc.arg(class_id)
  AND student_id = ANY(sqlc.arg(student_ids)::uuid[])
ORDER BY student_id, effective_from;

-- name: CountClassRosterAtDate :one
SELECT count(*)::bigint
FROM roster_periods
WHERE tutor_id = sqlc.arg(tutor_id)
  AND class_id = sqlc.arg(class_id)
  AND effective_from <= sqlc.arg(resolved_date)::date
  AND (effective_to IS NULL OR sqlc.arg(resolved_date)::date <= effective_to);

-- name: CloseRosterPeriodAt :one
UPDATE roster_periods
SET effective_to = sqlc.arg(effective_to)::date,
    updated_at = sqlc.arg(updated_at)
WHERE tutor_id = sqlc.arg(tutor_id)
  AND class_id = sqlc.arg(class_id)
  AND student_id = sqlc.arg(student_id)
  AND effective_from = sqlc.arg(effective_from)::date
RETURNING class_id, student_id, effective_from, tutor_id, effective_to,
          created_at, updated_at;
