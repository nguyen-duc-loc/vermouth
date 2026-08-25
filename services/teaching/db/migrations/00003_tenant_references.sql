-- Same service references must match the tenant as well as the entity id.
-- The original entity foreign keys remain in place. These additive composite
-- keys close the cross tutor path without rewriting an applied migration.
--
-- +goose Up

-- +goose StatementBegin
DO $$
DECLARE
    mismatches text;
BEGIN
    SELECT string_agg(problem, ', ' ORDER BY problem)
    INTO mismatches
    FROM (
        SELECT format(
            'sessions(session_id=%s,tutor_id=%s,class_id=%s,parent_tutor_id=%s)',
            s.session_id,
            s.tutor_id,
            s.class_id,
            c.tutor_id
        ) AS problem
        FROM sessions s
        JOIN classes c ON c.class_id = s.class_id
        WHERE s.tutor_id <> c.tutor_id

        UNION ALL

        SELECT format(
            'roster_periods(class_id=%s,student_id=%s,tutor_id=%s,class_tutor_id=%s)',
            rp.class_id,
            rp.student_id,
            rp.tutor_id,
            c.tutor_id
        )
        FROM roster_periods rp
        JOIN classes c ON c.class_id = rp.class_id
        WHERE rp.tutor_id <> c.tutor_id

        UNION ALL

        SELECT format(
            'roster_periods(class_id=%s,student_id=%s,tutor_id=%s,student_tutor_id=%s)',
            rp.class_id,
            rp.student_id,
            rp.tutor_id,
            s.tutor_id
        )
        FROM roster_periods rp
        JOIN students s ON s.student_id = rp.student_id
        WHERE rp.tutor_id <> s.tutor_id

        UNION ALL

        SELECT format(
            'attendance(session_id=%s,student_id=%s,tutor_id=%s,session_tutor_id=%s)',
            a.session_id,
            a.student_id,
            a.tutor_id,
            s.tutor_id
        )
        FROM attendance a
        JOIN sessions s ON s.session_id = a.session_id
        WHERE a.tutor_id <> s.tutor_id

        UNION ALL

        SELECT format(
            'attendance(session_id=%s,student_id=%s,tutor_id=%s,student_tutor_id=%s)',
            a.session_id,
            a.student_id,
            a.tutor_id,
            s.tutor_id
        )
        FROM attendance a
        JOIN students s ON s.student_id = a.student_id
        WHERE a.tutor_id <> s.tutor_id
    ) AS found;

    IF mismatches IS NOT NULL THEN
        RAISE EXCEPTION 'teaching tenant reference mismatches: %', mismatches;
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE students
    ADD CONSTRAINT students_tutor_student_unique UNIQUE (tutor_id, student_id);

ALTER TABLE classes
    ADD CONSTRAINT classes_tutor_class_unique UNIQUE (tutor_id, class_id);

ALTER TABLE sessions
    ADD CONSTRAINT sessions_tutor_session_unique UNIQUE (tutor_id, session_id),
    ADD CONSTRAINT sessions_tutor_class_fk
        FOREIGN KEY (tutor_id, class_id) REFERENCES classes (tutor_id, class_id);

ALTER TABLE roster_periods
    ADD CONSTRAINT roster_periods_tutor_class_fk
        FOREIGN KEY (tutor_id, class_id) REFERENCES classes (tutor_id, class_id),
    ADD CONSTRAINT roster_periods_tutor_student_fk
        FOREIGN KEY (tutor_id, student_id) REFERENCES students (tutor_id, student_id);

ALTER TABLE attendance
    ADD CONSTRAINT attendance_tutor_session_fk
        FOREIGN KEY (tutor_id, session_id) REFERENCES sessions (tutor_id, session_id),
    ADD CONSTRAINT attendance_tutor_student_fk
        FOREIGN KEY (tutor_id, student_id) REFERENCES students (tutor_id, student_id);

-- +goose Down
ALTER TABLE attendance
    DROP CONSTRAINT attendance_tutor_student_fk,
    DROP CONSTRAINT attendance_tutor_session_fk;

ALTER TABLE roster_periods
    DROP CONSTRAINT roster_periods_tutor_student_fk,
    DROP CONSTRAINT roster_periods_tutor_class_fk;

ALTER TABLE sessions
    DROP CONSTRAINT sessions_tutor_class_fk,
    DROP CONSTRAINT sessions_tutor_session_unique;

ALTER TABLE classes
    DROP CONSTRAINT classes_tutor_class_unique;

ALTER TABLE students
    DROP CONSTRAINT students_tutor_student_unique;
