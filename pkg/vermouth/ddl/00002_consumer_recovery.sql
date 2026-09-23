-- Canonical additive DDL for durable consumer recovery state (spec 0013).
--
-- Each service receives this file at its next local migration number through
-- `task migrate:sync-kit`. Applied migrations stay unchanged.
--
-- +goose Up
CREATE TABLE consumer_failures (
    consumer_name     text        NOT NULL,
    source_topic      text        NOT NULL,
    source_partition  integer     NOT NULL,
    source_offset     bigint      NOT NULL,
    event_id          uuid,
    tutor_id          uuid,
    failure_category  text        NOT NULL,
    failed_at         timestamptz NOT NULL DEFAULT now(),
    resolved_at       timestamptz,
    resolution        text,
    resolution_code   text,
    resolved_by       text,
    repair_reference  text,
    PRIMARY KEY (consumer_name, source_topic, source_partition, source_offset),
    CONSTRAINT consumer_failures_source_position_valid
        CHECK (source_partition >= 0 AND source_offset >= 0),
    CONSTRAINT consumer_failures_category_known
        CHECK (failure_category IN ('decode_failed', 'version_unknown', 'handler_failed')),
    CONSTRAINT consumer_failures_resolution_known
        CHECK (resolution IS NULL OR resolution IN ('replayed', 'acknowledged')),
    CONSTRAINT consumer_failures_resolution_code_known
        CHECK (
            resolution_code IS NULL
            OR resolution_code IN ('source_repaired', 'projection_restored')
        ),
    CONSTRAINT consumer_failures_repair_reference_valid
        CHECK (
            repair_reference IS NULL
            OR repair_reference ~ '^[A-Za-z0-9._:/-]{1,200}$'
        ),
    CONSTRAINT consumer_failures_resolution_complete
        CHECK (
            (resolved_at IS NULL AND resolution IS NULL AND resolution_code IS NULL
                AND resolved_by IS NULL AND repair_reference IS NULL)
            OR
            (resolved_at IS NOT NULL AND resolution = 'replayed' AND resolution_code IS NULL
                AND resolved_by IS NULL AND repair_reference IS NULL)
            OR
            (resolved_at IS NOT NULL AND resolution = 'acknowledged'
                AND resolution_code IS NOT NULL AND resolved_by IS NOT NULL
                AND repair_reference IS NOT NULL AND failure_category = 'decode_failed')
        )
);

CREATE INDEX consumer_failures_unresolved_barrier_idx
    ON consumer_failures (
        consumer_name,
        source_topic,
        tutor_id,
        source_partition,
        source_offset
    )
    WHERE resolved_at IS NULL;

CREATE TABLE consumer_readiness (
    consumer_name          text        PRIMARY KEY,
    projection_generation  uuid        NOT NULL,
    state                  text        NOT NULL,
    updated_at             timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT consumer_readiness_state_known
        CHECK (state IN ('uncertified', 'replaying', 'certified'))
);

CREATE TABLE consumer_replay_manifests (
    consumer_name          text        NOT NULL,
    projection_generation  uuid        NOT NULL,
    source_topic           text        NOT NULL,
    topic_identity         text        NOT NULL,
    partition_set          jsonb       NOT NULL,
    earliest_offsets       jsonb       NOT NULL,
    captured_end_offsets   jsonb       NOT NULL,
    completed_offsets      jsonb,
    started_at             timestamptz NOT NULL,
    completed_at           timestamptz,
    operator_identity      text        NOT NULL,
    PRIMARY KEY (consumer_name, projection_generation),
    CONSTRAINT consumer_replay_manifests_completed_together
        CHECK (
            (completed_at IS NULL AND completed_offsets IS NULL)
            OR (completed_at IS NOT NULL AND completed_offsets IS NOT NULL)
        )
);

ALTER TABLE consumer_replay_manifests
    ADD CONSTRAINT consumer_replay_manifests_readiness_fk
    FOREIGN KEY (consumer_name)
    REFERENCES consumer_readiness (consumer_name);

-- +goose Down
DROP TABLE consumer_replay_manifests;
DROP TABLE consumer_readiness;
DROP TABLE consumer_failures;
