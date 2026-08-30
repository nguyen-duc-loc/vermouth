-- The first teaching thread adds stable class identity and durable receipts for
-- the two create commands. A receipt is written in the same transaction as its
-- business rows and outbox facts, so a lost response can recover the original
-- resources without repeating the write.
--
-- +goose Up

ALTER TABLE classes ADD COLUMN color text;

UPDATE classes SET color = 'blue' WHERE color IS NULL;

ALTER TABLE classes
    ALTER COLUMN color SET NOT NULL,
    ADD CONSTRAINT classes_color_known
        CHECK (color IN ('red', 'rose', 'orange', 'green', 'blue', 'yellow', 'violet'));

CREATE TABLE command_receipts (
    tutor_id            uuid        NOT NULL,
    operation           text        NOT NULL,
    idempotency_key     text        NOT NULL,
    request_hash        bytea       NOT NULL,
    primary_resource_id uuid        NOT NULL,
    related_resource_id uuid,
    created_at          timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tutor_id, operation, idempotency_key),
    CONSTRAINT command_receipts_operation_known
        CHECK (operation IN ('create_class', 'create_student')),
    CONSTRAINT command_receipts_key_length
        CHECK (char_length(idempotency_key) BETWEEN 1 AND 128),
    CONSTRAINT command_receipts_hash_sha256
        CHECK (octet_length(request_hash) = 32),
    CONSTRAINT command_receipts_class_resources
        CHECK (
            (operation = 'create_class' AND related_resource_id IS NOT NULL)
            OR (operation = 'create_student' AND related_resource_id IS NULL)
        )
);

-- +goose Down
DROP TABLE command_receipts;

ALTER TABLE classes
    DROP CONSTRAINT classes_color_known,
    DROP COLUMN color;
