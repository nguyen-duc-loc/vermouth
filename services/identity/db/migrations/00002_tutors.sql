-- The tutor identity itself, held to what spec 0001 fixes: identity owns
-- tutor_id, email, display name, timezone and language, and holds a copy of
-- nothing. There is no password column here and none anywhere else: spec 0004
-- replaced passwords with Google sign in, so the link to the Google account in
-- 00004 is what proves who a tutor is.
--
-- +goose Up
CREATE TABLE tutors (
    tutor_id     uuid        PRIMARY KEY,
    email        text        NOT NULL UNIQUE
        CONSTRAINT tutors_email_canonical_check CHECK (email = lower(btrim(email))),
    display_name text        NOT NULL,
    timezone     text        NOT NULL,
    language     text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE tutors;
