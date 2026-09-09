-- Google sign in, and the session that follows it (spec 0004). Four tables,
-- none of them a projection: identity owns every fact here.
--
-- tutor_identities keeps the link to the external account off tutors, so a
-- second provider later is one more row rather than an ALTER TABLE plus every
-- query that reads a tutor.
--
-- login_attempts and refresh_tokens have no tutor_id. An attempt exists before
-- the tutor does, while a refresh token reaches its tutor through the locked
-- auth_sessions family that owns revocation and expiry.
--
-- +goose Up
CREATE TABLE tutor_identities (
    tutor_id       uuid        NOT NULL REFERENCES tutors (tutor_id) ON DELETE CASCADE,
    provider       text        NOT NULL,
    provider_subject text      NOT NULL,
    provider_email text        NOT NULL
        CONSTRAINT tutor_identities_provider_email_canonical_check
        CHECK (provider_email = lower(btrim(provider_email))),
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
    state                text        PRIMARY KEY,
    code_verifier        text        NOT NULL,
    nonce                text        NOT NULL,
    browser_binding_hash bytea       NOT NULL,
    redirect_to          text        NOT NULL DEFAULT '/',
    timezone             text        NOT NULL,
    language             text        NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    expires_at           timestamptz NOT NULL
);

CREATE INDEX login_attempts_expires_at_idx ON login_attempts (expires_at);

-- The family row is the one authority for revocation and sliding expiry. Every
-- refresh and sign out locks it before reading or changing a token row.
CREATE TABLE auth_sessions (
    session_id uuid        PRIMARY KEY,
    tutor_id   uuid        NOT NULL REFERENCES tutors (tutor_id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz
);

CREATE INDEX auth_sessions_tutor_id_idx ON auth_sessions (tutor_id);
CREATE INDEX auth_sessions_expires_at_idx ON auth_sessions (expires_at);
CREATE INDEX auth_sessions_revoked_at_idx ON auth_sessions (revoked_at)
    WHERE revoked_at IS NOT NULL;

-- One row per issued refresh token. The raw token is never stored: token_hash
-- is sha256 of it, so a database copy hands nobody a session.
CREATE TABLE refresh_tokens (
    token_hash bytea       PRIMARY KEY,
    session_id uuid        NOT NULL REFERENCES auth_sessions (session_id) ON DELETE CASCADE,
    issued_at  timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    -- Null until rotated. A second use inside the grace window is two tabs
    -- booting together; after it, a stolen copy, and the family ends.
    used_at    timestamptz
);

CREATE INDEX refresh_tokens_session_id_idx ON refresh_tokens (session_id);
CREATE INDEX refresh_tokens_expires_at_idx ON refresh_tokens (expires_at);

-- +goose Down
DROP TABLE refresh_tokens;
DROP TABLE auth_sessions;
DROP TABLE login_attempts;
DROP TABLE tutor_identities;
