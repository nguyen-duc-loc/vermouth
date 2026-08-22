-- Google sign in, and the session that follows it (spec 0004). Three tables,
-- none of them a projection: identity owns every fact here.
--
-- tutor_identities keeps the link to the external account off tutors, so a
-- second provider later is one more row rather than an ALTER TABLE plus every
-- query that reads a tutor.
--
-- login_attempts and refresh_tokens are the only tables in the system with no
-- tutor_id, and for the same reason in both cases: an attempt exists before the
-- tutor does, and a refresh token is presented before any token names a tutor.
-- refresh_tokens carries tutor_id all the same, because by the time one is
-- issued the tutor is known; login_attempts cannot.
--
-- +goose Up
CREATE TABLE tutor_identities (
    tutor_id       uuid        NOT NULL REFERENCES tutors (tutor_id) ON DELETE CASCADE,
    provider       text        NOT NULL,
    provider_subject text      NOT NULL,
    provider_email text        NOT NULL,
    linked_at      timestamptz NOT NULL DEFAULT now(),
    -- The Google subject is the identity, so it is the key: one Google account
    -- maps to at most one tutor, and an email match never finds a link.
    PRIMARY KEY (provider, provider_subject),
    -- One account per provider per tutor, since linking is out of scope.
    UNIQUE (tutor_id, provider)
);

-- One in flight sign in, from the start call until the callback consumes it.
-- Single use is a delete inside the callback transaction, so a replayed
-- callback finds nothing rather than a value that still verifies.
CREATE TABLE login_attempts (
    state         text        PRIMARY KEY,
    code_verifier text        NOT NULL,
    nonce         text        NOT NULL,
    redirect_to   text        NOT NULL DEFAULT '/',
    timezone      text        NOT NULL,
    language      text        NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL
);

CREATE INDEX login_attempts_expires_at_idx ON login_attempts (expires_at);

-- One row per issued refresh token, with session_id as the family that survives
-- every rotation. The raw token is never stored: token_hash is sha256 of it, so
-- a database copy hands nobody a session and no comparison has to be timing
-- safe.
CREATE TABLE refresh_tokens (
    token_hash bytea       PRIMARY KEY,
    session_id uuid        NOT NULL,
    tutor_id   uuid        NOT NULL REFERENCES tutors (tutor_id) ON DELETE CASCADE,
    issued_at  timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    -- Null until rotated. A second use inside the grace window is two tabs
    -- booting together; after it, a stolen copy, and the family ends.
    used_at    timestamptz,
    revoked_at timestamptz
);

CREATE INDEX refresh_tokens_session_id_idx ON refresh_tokens (session_id);
CREATE INDEX refresh_tokens_tutor_id_idx ON refresh_tokens (tutor_id);
CREATE INDEX refresh_tokens_expires_at_idx ON refresh_tokens (expires_at);

-- +goose Down
DROP TABLE refresh_tokens;
DROP TABLE login_attempts;
DROP TABLE tutor_identities;
