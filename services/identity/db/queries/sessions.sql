-- The sign in attempt and the refresh token family behind a session (spec
-- 0004). These are the two tables that cannot filter by tutor_id: an attempt
-- exists before the tutor does, and a refresh token is presented before any
-- token names a tutor. Both are identified instead by an unguessable value from
-- crypto/rand, which is what test/model's tenancy guard insists each one names.

-- name: InsertLoginAttempt :exec
INSERT INTO login_attempts (state, code_verifier, nonce, redirect_to, timezone, language, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- The callback reads the attempt before it talks to Google, because the PKCE
-- verifier it holds is what the exchange needs. Claiming it is a separate
-- statement, run inside the transaction that writes the tutor.
-- name: GetLoginAttempt :one
SELECT state, code_verifier, nonce, redirect_to, timezone, language
FROM login_attempts
WHERE state = $1 AND expires_at > now();

-- Single use is the delete itself, inside the callback transaction, so a
-- replayed callback finds nothing. An expired attempt is left for the sweep
-- rather than consumed, which is what makes AC-8 one answer for both cases.
-- name: ConsumeLoginAttempt :one
DELETE FROM login_attempts
WHERE state = $1 AND expires_at > now()
RETURNING state, code_verifier, nonce, redirect_to, timezone, language;

-- name: DeleteExpiredLoginAttempts :execrows
DELETE FROM login_attempts
WHERE expires_at <= now();

-- name: InsertRefreshToken :one
INSERT INTO refresh_tokens (token_hash, session_id, tutor_id, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING token_hash, session_id, tutor_id, issued_at, expires_at, used_at, revoked_at;

-- name: GetRefreshToken :one
SELECT token_hash, session_id, tutor_id, issued_at, expires_at, used_at, revoked_at
FROM refresh_tokens
WHERE token_hash = $1;

-- The first use of a token is a single statement, so two tabs racing cannot
-- both win it: the loser reads used_at back and falls into the grace window
-- branch instead of being treated as theft.
-- name: MarkRefreshTokenUsed :one
UPDATE refresh_tokens
SET used_at = now()
WHERE token_hash = $1 AND used_at IS NULL AND revoked_at IS NULL AND expires_at > now()
RETURNING token_hash, session_id, tutor_id, issued_at, expires_at, used_at, revoked_at;

-- name: RevokeSessionFamily :execrows
UPDATE refresh_tokens
SET revoked_at = now()
WHERE tutor_id = $1 AND session_id = $2 AND revoked_at IS NULL;

-- The sweep. A revoked row is kept for a while on purpose: presenting it again
-- is how a stolen copy shows up, and a deleted row is indistinguishable from
-- one that never existed.
-- name: DeleteFinishedRefreshTokens :execrows
DELETE FROM refresh_tokens
WHERE expires_at <= now() OR (revoked_at IS NOT NULL AND revoked_at <= $1);
