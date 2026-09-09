-- The sign in attempt and the locked refresh token family behind a session
-- (spec 0004). Every value is parameterized, and every family transition locks
-- auth_sessions before it locks the presented token.

-- name: InsertLoginAttempt :exec
INSERT INTO login_attempts (
    state, code_verifier, nonce, browser_binding_hash,
    redirect_to, timezone, language, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- The callback reads the attempt before it talks to Google, because the PKCE
-- verifier it holds is what the exchange needs. Claiming it is a separate
-- statement, run inside the transaction that writes the tutor.
-- name: GetLoginAttempt :one
SELECT state, code_verifier, nonce, browser_binding_hash,
       redirect_to, timezone, language
FROM login_attempts
WHERE state = $1 AND expires_at > now();

-- Single use is the delete itself, inside the callback transaction, so a
-- replayed callback finds nothing. An expired attempt is left for the sweep
-- rather than consumed, which is what makes AC-8 one answer for both cases.
-- name: ConsumeLoginAttempt :one
DELETE FROM login_attempts
WHERE state = $1 AND expires_at > now()
RETURNING state, code_verifier, nonce, browser_binding_hash,
          redirect_to, timezone, language;

-- name: DeleteExpiredLoginAttempts :execrows
DELETE FROM login_attempts
WHERE expires_at <= now();

-- name: InsertAuthSession :one
INSERT INTO auth_sessions (session_id, tutor_id, expires_at)
VALUES ($1, $2, $3)
RETURNING session_id, tutor_id, created_at, expires_at, revoked_at;

-- name: InsertRefreshToken :one
INSERT INTO refresh_tokens (token_hash, session_id, expires_at)
VALUES ($1, $2, $3)
RETURNING token_hash, session_id, issued_at, expires_at, used_at;

-- This lookup takes no lock. session_id is immutable, and it only tells the
-- caller which auth_sessions row must be locked before it may inspect the token.
-- name: GetRefreshTokenSessionID :one
SELECT session_id
FROM refresh_tokens
WHERE token_hash = $1;

-- name: LockAuthSession :one
SELECT session_id, tutor_id, created_at, expires_at, revoked_at
FROM auth_sessions s
WHERE s.session_id = $1
FOR UPDATE;

-- name: LockRefreshToken :one
SELECT token_hash, session_id, issued_at, expires_at, used_at
FROM refresh_tokens
WHERE token_hash = $1 AND session_id = $2
FOR UPDATE;

-- name: MarkRefreshTokenUsed :exec
UPDATE refresh_tokens
SET used_at = $3
WHERE token_hash = $1 AND session_id = $2 AND used_at IS NULL;

-- name: ExtendAuthSession :exec
UPDATE auth_sessions
SET expires_at = $2
WHERE session_id = $1 AND revoked_at IS NULL;

-- name: RevokeAuthSession :execrows
UPDATE auth_sessions
SET revoked_at = $2
WHERE session_id = $1 AND revoked_at IS NULL;

-- The sweep. A revoked row is kept for a while on purpose: presenting it again
-- is how a stolen copy shows up, and a deleted row is indistinguishable from
-- one that never existed.
-- name: DeleteFinishedAuthSessions :execrows
DELETE FROM auth_sessions
WHERE expires_at <= transaction_timestamp()
   OR (revoked_at IS NOT NULL AND revoked_at <= $1);
