-- Canonical additive DDL for prior resolutions when a source fails again.
-- A new projection generation may encounter a coordinate repaired earlier.
-- Keep the old audit fields while the current failure becomes unresolved.

-- +goose Up
ALTER TABLE consumer_failures
    ADD COLUMN resolution_history jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD CONSTRAINT consumer_failures_resolution_history_array
        CHECK (jsonb_typeof(resolution_history) = 'array');

-- +goose Down
ALTER TABLE consumer_failures
    DROP CONSTRAINT consumer_failures_resolution_history_array,
    DROP COLUMN resolution_history;
