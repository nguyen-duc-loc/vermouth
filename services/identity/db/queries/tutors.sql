-- Hand written SQL, compiled to typed Go by sqlc (STK-3). No ORM, and no SQL
-- assembled by string concatenation at runtime.

-- name: InsertTutor :one
INSERT INTO tutors (tutor_id, email, display_name, timezone, language)
VALUES ($1, $2, $3, $4, $5)
RETURNING tutor_id, email, display_name, timezone, language, created_at;

-- name: GetTutor :one
SELECT tutor_id, email, display_name, timezone, language, created_at
FROM tutors
WHERE tutor_id = $1;

-- name: GetTutorByEmail :one
SELECT tutor_id, email, display_name, timezone, language, created_at
FROM tutors
WHERE email = $1;

-- The email copy is kept in step with Google's, which owns it. updated_at is
-- what identity.tutor.profile.changed stamps; the event itself carries only
-- spec 0001's five fields.
-- name: UpdateTutorFromProvider :one
UPDATE tutors
SET email = $2, display_name = $3, updated_at = now()
WHERE tutor_id = $1
RETURNING tutor_id, email, display_name, timezone, language, created_at;

-- The link to an external account. provider_subject is the identity; the email
-- beside it is only a copy of what Google last said.
-- name: InsertTutorIdentity :one
INSERT INTO tutor_identities (tutor_id, provider, provider_subject, provider_email)
VALUES ($1, $2, $3, $4)
RETURNING tutor_id, provider, provider_subject, provider_email, linked_at;

-- name: GetTutorByProviderSubject :one
SELECT t.tutor_id, t.email, t.display_name, t.timezone, t.language, t.created_at,
       i.provider_email
FROM tutor_identities i
JOIN tutors t ON t.tutor_id = i.tutor_id
WHERE i.provider = $1 AND i.provider_subject = $2;

-- name: UpdateTutorIdentityEmail :exec
UPDATE tutor_identities
SET provider_email = $3
WHERE tutor_id = $1 AND provider = $2;
