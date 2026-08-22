-- name: UpsertRecipient :exec
INSERT INTO recipients (tutor_id, email, display_name, timezone, language)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (tutor_id) DO UPDATE
SET email        = excluded.email,
    display_name = excluded.display_name,
    timezone     = excluded.timezone,
    language     = excluded.language,
    updated_at   = now();

-- name: GetRecipient :one
SELECT tutor_id, email, display_name, timezone, language, recorded_at, updated_at
FROM recipients
WHERE tutor_id = $1;
