-- Dated tuition rate commands need a monotonic class revision so billing can
-- reject stale delivery without depending on arrival order (spec 0013).
--
-- +goose Up
ALTER TABLE classes
    ADD COLUMN rate_revision bigint;

UPDATE classes SET rate_revision = 0 WHERE rate_revision IS NULL;

ALTER TABLE classes
    ALTER COLUMN rate_revision SET NOT NULL,
    ALTER COLUMN rate_revision SET DEFAULT 1,
    ADD CONSTRAINT classes_rate_revision_nonnegative CHECK (rate_revision >= 0),
    ADD CONSTRAINT classes_rate_bounded CHECK (rate_amount <= 1000000000);

-- +goose Down
ALTER TABLE classes
    DROP CONSTRAINT classes_rate_bounded,
    DROP CONSTRAINT classes_rate_revision_nonnegative,
    DROP COLUMN rate_revision;
