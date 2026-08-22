-- notifications holds a copy of the recipient (email, display name, timezone,
-- language) because at 6:00 nobody is asking and there is nobody to ask
-- (spec 0001, local copies). It is a projection: built only from identity
-- events, never written by a request, and rebuildable by a replay.
--
-- Written by identity.tutor.registered, which creates the row, and
-- identity.tutor.profile.changed, which updates it: every column here comes from
-- one of those two and from nothing else (spec 0003, AC-3).
--
-- Feature 17 adds the digest records and today's sessions. Feature 4 owns the
-- full data model.
--
-- +goose Up
CREATE TABLE recipients (
    tutor_id     uuid        PRIMARY KEY,
    email        text        NOT NULL,
    display_name text        NOT NULL,
    timezone     text        NOT NULL,
    language     text        NOT NULL,
    recorded_at  timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE recipients;
