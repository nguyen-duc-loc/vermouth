-- Canonical DDL for the shared module's own tables (spec 0002, STK-3).
--
-- This file lives in pkg/vermouth and is copied into every service's
-- db/migrations by `task migrate:sync-kit`, so the four services cannot drift.
-- Do not edit the copies: edit this file and run that target.
--
-- +goose Up
CREATE TABLE outbox (
    id            bigserial PRIMARY KEY,
    event_id      uuid        NOT NULL UNIQUE,
    event_name    text        NOT NULL,
    event_version integer     NOT NULL,
    occurred_at   timestamptz NOT NULL,
    tutor_id      uuid        NOT NULL,
    key_kind      text        NOT NULL,
    key_value     uuid        NOT NULL,
    request_id    text        NOT NULL DEFAULT '',
    topic         text        NOT NULL,
    envelope      jsonb       NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    published_at  timestamptz
);

-- The relay only ever reads the unpublished tail, oldest first (STK-18).
CREATE INDEX outbox_unpublished_idx ON outbox (id) WHERE published_at IS NULL;

CREATE TABLE handled_events (
    consumer_name text        NOT NULL,
    event_id      uuid        NOT NULL,
    handled_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (consumer_name, event_id)
);

-- +goose Down
DROP TABLE handled_events;
DROP TABLE outbox;
