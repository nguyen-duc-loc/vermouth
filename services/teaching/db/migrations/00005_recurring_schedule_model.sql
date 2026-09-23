-- The first recurrence phase stores weekly rules and keeps every generated
-- occurrence as an ordinary session. Constraint enforcement follows in a
-- separate migration after the read only conflict report is empty.
--
-- +goose Up

ALTER TABLE classes
    ADD COLUMN schedule_revision bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT classes_schedule_revision_nonnegative CHECK (schedule_revision >= 0);

CREATE TABLE schedule_rules (
    schedule_rule_id uuid        PRIMARY KEY,
    tutor_id         uuid        NOT NULL,
    class_id         uuid        NOT NULL,
    revision         bigint      NOT NULL,
    valid_from       date        NOT NULL,
    valid_through    date        NOT NULL,
    time_zone        text        NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    replaced_at      timestamptz,
    ended_at         timestamptz,
    retired_at       timestamptz,
    CONSTRAINT schedule_rules_tutor_rule_unique UNIQUE (tutor_id, schedule_rule_id),
    CONSTRAINT schedule_rules_tutor_class_rule_unique
        UNIQUE (tutor_id, class_id, schedule_rule_id),
    CONSTRAINT schedule_rules_tutor_class_revision_unique
        UNIQUE (tutor_id, class_id, revision),
    CONSTRAINT schedule_rules_tutor_class_fk
        FOREIGN KEY (tutor_id, class_id) REFERENCES classes (tutor_id, class_id),
    CONSTRAINT schedule_rules_valid_range CHECK (valid_through >= valid_from),
    CONSTRAINT schedule_rules_revision_positive CHECK (revision > 0)
);

CREATE INDEX schedule_rules_class_range_idx
    ON schedule_rules (tutor_id, class_id, valid_from, valid_through);

CREATE TABLE schedule_slots (
    schedule_rule_id uuid                     NOT NULL,
    tutor_id         uuid                     NOT NULL,
    weekday          smallint                 NOT NULL,
    start_time       time without time zone   NOT NULL,
    end_time         time without time zone   NOT NULL,
    created_at       timestamptz               NOT NULL DEFAULT now(),
    updated_at       timestamptz               NOT NULL DEFAULT now(),
    PRIMARY KEY (schedule_rule_id, weekday),
    CONSTRAINT schedule_slots_tutor_rule_fk
        FOREIGN KEY (tutor_id, schedule_rule_id)
        REFERENCES schedule_rules (tutor_id, schedule_rule_id),
    CONSTRAINT schedule_slots_weekday_iso CHECK (weekday BETWEEN 1 AND 7),
    CONSTRAINT schedule_slots_ends_after_starts CHECK (end_time > start_time)
);

ALTER TABLE sessions
    ADD COLUMN origin_local_date date,
    ADD COLUMN version bigint NOT NULL DEFAULT 1,
    ADD COLUMN moved_at timestamptz,
    ADD COLUMN superseded_at timestamptz;

UPDATE sessions SET origin_local_date = local_date WHERE origin_local_date IS NULL;

ALTER TABLE sessions
    ALTER COLUMN origin_local_date SET NOT NULL,
    ADD CONSTRAINT sessions_version_positive CHECK (version > 0),
    ADD CONSTRAINT sessions_tutor_class_rule_fk
        FOREIGN KEY (tutor_id, class_id, schedule_rule_id)
        REFERENCES schedule_rules (tutor_id, class_id, schedule_rule_id),
    ADD CONSTRAINT sessions_rule_origin_unique UNIQUE (schedule_rule_id, origin_local_date);

CREATE INDEX sessions_schedule_window_idx
    ON sessions (tutor_id, starts_at, session_id)
    WHERE superseded_at IS NULL;

ALTER TABLE command_receipts
    ADD COLUMN context_snapshot jsonb,
    ADD COLUMN response_snapshot jsonb,
    ADD COLUMN response_status integer,
    DROP CONSTRAINT command_receipts_operation_known,
    DROP CONSTRAINT command_receipts_class_resources,
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

-- +goose Down
ALTER TABLE command_receipts
    DROP CONSTRAINT command_receipts_class_resources,
    DROP CONSTRAINT command_receipts_operation_known,
    DROP COLUMN response_status,
    DROP COLUMN response_snapshot,
    DROP COLUMN context_snapshot,
    ADD CONSTRAINT command_receipts_operation_known
        CHECK (operation IN ('create_class', 'create_student')),
    ADD CONSTRAINT command_receipts_class_resources CHECK (
        (operation = 'create_class' AND related_resource_id IS NOT NULL)
        OR (operation = 'create_student' AND related_resource_id IS NULL)
    );

DROP INDEX sessions_schedule_window_idx;

ALTER TABLE sessions
    DROP CONSTRAINT sessions_rule_origin_unique,
    DROP CONSTRAINT sessions_tutor_class_rule_fk,
    DROP CONSTRAINT sessions_version_positive,
    DROP COLUMN superseded_at,
    DROP COLUMN moved_at,
    DROP COLUMN version,
    DROP COLUMN origin_local_date;

DROP TABLE schedule_slots;
DROP TABLE schedule_rules;

ALTER TABLE classes
    DROP CONSTRAINT classes_schedule_revision_nonnegative,
    DROP COLUMN schedule_revision;
