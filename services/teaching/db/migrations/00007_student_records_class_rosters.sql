-- Student records and dated rosters gain the constraints that make their
-- existing meanings safe under concurrent commands. Existing invalid rows are
-- reported before any constraint is added, so this migration never rewrites
-- teaching history.
--
-- +goose Up

-- +goose StatementBegin
DO $$
DECLARE
    invalid_students text;
    overlapping_periods text;
BEGIN
    SELECT string_agg(student_id::text, ', ' ORDER BY student_id)
    INTO invalid_students
    FROM students
    WHERE name <> btrim(name)
       OR char_length(name) NOT BETWEEN 1 AND 160
       OR (
           phone IS NOT NULL
           AND (
               phone <> btrim(phone)
               OR phone = ''
               OR char_length(phone) > 40
           )
       );

    IF invalid_students IS NOT NULL THEN
        RAISE EXCEPTION 'invalid teaching students remain: %', invalid_students;
    END IF;

    SELECT string_agg(
        format(
            '%s:%s:%s<->%s',
            left_period.tutor_id,
            left_period.class_id,
            left_period.student_id,
            right_period.effective_from
        ),
        ', ' ORDER BY left_period.tutor_id, left_period.class_id,
                     left_period.student_id, left_period.effective_from
    )
    INTO overlapping_periods
    FROM roster_periods left_period
    JOIN roster_periods right_period
      ON right_period.tutor_id = left_period.tutor_id
     AND right_period.class_id = left_period.class_id
     AND right_period.student_id = left_period.student_id
     AND right_period.effective_from > left_period.effective_from
     AND daterange(
             right_period.effective_from,
             right_period.effective_to,
             '[]'
         ) && daterange(
             left_period.effective_from,
             left_period.effective_to,
             '[]'
         );

    IF overlapping_periods IS NOT NULL THEN
        RAISE EXCEPTION 'overlapping teaching roster periods remain: %', overlapping_periods;
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE students
    ADD CONSTRAINT students_name_valid
        CHECK (name = btrim(name) AND char_length(name) BETWEEN 1 AND 160),
    ADD CONSTRAINT students_phone_valid
        CHECK (
            phone IS NULL
            OR (
                phone = btrim(phone)
                AND phone <> ''
                AND char_length(phone) <= 40
            )
        );

CREATE INDEX students_active_name_idx
    ON students (tutor_id, lower(name), name, student_id)
    WHERE removed_at IS NULL;

ALTER TABLE roster_periods
    ADD CONSTRAINT roster_periods_dates_no_overlap
    EXCLUDE USING gist (
        tutor_id WITH =,
        class_id WITH =,
        student_id WITH =,
        daterange(effective_from, effective_to, '[]') WITH &&
    );

ALTER TABLE command_receipts
    DROP CONSTRAINT command_receipts_class_resources,
    DROP CONSTRAINT command_receipts_operation_known,
    ADD CONSTRAINT command_receipts_operation_known CHECK (
        operation IN (
            'create_class',
            'create_student',
            'put_schedule',
            'end_schedule',
            'move_session',
            'cancel_session',
            'restore_session',
            'update_student',
            'remove_student',
            'change_roster',
            'save_attendance'
        )
    ),
    ADD CONSTRAINT command_receipts_class_resources CHECK (
        (operation = 'create_class' AND related_resource_id IS NOT NULL)
        OR (operation = 'create_student' AND related_resource_id IS NULL)
        OR operation IN (
            'put_schedule',
            'end_schedule',
            'move_session',
            'cancel_session',
            'restore_session',
            'update_student',
            'remove_student',
            'change_roster',
            'save_attendance'
        )
    );

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM command_receipts
        WHERE operation IN (
            'update_student',
            'remove_student',
            'change_roster',
            'save_attendance'
        )
    ) THEN
        RAISE EXCEPTION 'student record command receipts prevent migration reversal';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE command_receipts
    DROP CONSTRAINT command_receipts_class_resources,
    DROP CONSTRAINT command_receipts_operation_known,
    ADD CONSTRAINT command_receipts_operation_known CHECK (
        operation IN (
            'create_class',
            'create_student',
            'put_schedule',
            'end_schedule',
            'move_session',
            'cancel_session',
            'restore_session'
        )
    ),
    ADD CONSTRAINT command_receipts_class_resources CHECK (
        (operation = 'create_class' AND related_resource_id IS NOT NULL)
        OR (operation = 'create_student' AND related_resource_id IS NULL)
        OR operation IN (
            'put_schedule',
            'end_schedule',
            'move_session',
            'cancel_session',
            'restore_session'
        )
    );

ALTER TABLE roster_periods DROP CONSTRAINT roster_periods_dates_no_overlap;
DROP INDEX students_active_name_idx;
ALTER TABLE students
    DROP CONSTRAINT students_phone_valid,
    DROP CONSTRAINT students_name_valid;
