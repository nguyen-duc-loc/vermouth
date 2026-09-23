-- The dated rate command uses the shared immutable receipt table. Extending
-- the operation allowlist stays additive because the prior migration is live.
--
-- +goose Up
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
            'save_attendance',
            'put_class_rate'
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
            'save_attendance',
            'put_class_rate'
        )
    );

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM command_receipts WHERE operation = 'put_class_rate') THEN
        RAISE EXCEPTION 'cannot remove put_class_rate while its command receipts exist';
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
