# 0004 verify: tutor sign in with Google only, no password

How to prove spec 0004 landed. Every check names the acceptance criterion it covers. The dangerous
session races and malformed Google claims need automated tests with the real Postgres database and
injectable Google adapters. A real Google client is still needed once for the browser round trip.

## Setup

```sh
task infra:clean && task infra:up && task migrate:up
```

Use these values for the by hand checks:

```sh
PSQL='docker exec -i vermouth-postgres-identity psql -U vermouth_identity -d vermouth_identity'
G=http://localhost:8080
APP=http://localhost:5173
```

Set `IDENTITY_COOKIE_SECURE=false` only while checking plain local HTTP. Set
`IDENTITY_SIGNUP_ALLOWLIST` to your Google address for the real provider section.

## Repository gates

- [x] `task check`, `task test`, and `task build` succeed across every module and the web app
      → all
- [x] `task infra:clean && task infra:up && task migrate:up` applies all four service models from
      empty, and identity has `tutors`, `tutor_identities`, `login_attempts`, `auth_sessions`, and
      `refresh_tokens`
      → AC-12
- [x] `task migrate:down -- identity` then `task migrate:up` reverses and reapplies identity
      → AC-12
- [x] `cd test && go test ./model/` proves the table ownership, canonical email checks, the session
      foreign key, the browser binding column, and the allowed statements without `tutor_id`
      → AC-8, AC-9, AC-11, AC-12
- [x] `task dev:token && task thread` completes with no browser and no Google
      → AC-13
- [x] `rg 'argon2' -g '*.go'` and `rg 'golang.org/x/crypto' -g go.mod` find no import and no direct
      requirement; `rg '\.Mint\(' -g '*.go'` finds only refresh and `cmd/devtoken`
      → AC-12, AC-13
- [x] the generated Go and browser API types match `api/openapi.yaml`, including both auth errors and
      `access_expires_at`
      → AC-4, AC-15, AC-16

## Start and callback boundary

- [x] `GET /api/auth/google/start?tz=Europe/Paris&lang=en&redirect_to=/somewhere?day=1` answers `302`
      to Google with `state`, `nonce`, `code_challenge`, `code_challenge_method=S256`, and the
      `openid email profile` scopes
      → AC-1, AC-10
- [x] the response sets `vermouth_login` with `HttpOnly`, `SameSite=Lax`, the configured `Secure`
      value, host only scope, `Path=/api/auth/google`, and a ten minute expiry
      → AC-8
- [x] the attempt stores `sha256` of that cookie and never its raw value; state, nonce, verifier, and
      binding all differ on the next start
      → AC-8
- [x] missing or bad timezone and language store `Asia/Ho_Chi_Minh` and `vi`. A later sign in from a
      different device does not change a tutor preference
      → AC-10
- [x] each of `//evil.example`, `/\\evil.example`, `/%2f%2fevil.example`, a full URL, a control byte,
      and a fragment stores `/`; a clean relative path and query survive
      → AC-15
- [x] malformed `IDENTITY_APP_URL` values stop startup. Production accepts only `https`; local HTTP
      accepts `localhost` or a hostname ending in `.localhost`. A callback hostname different from
      the app hostname stops startup, while different ports on the same local loopback name work.
      The callback also refuses a wrong path, query, fragment, user info, or plain production HTTP
      → AC-15
- [x] a valid state with no binding cookie or another attempt's binding cookie lands on
      `/signin?error=expired_state`, clears the binding cookie, and writes no tutor or session
      → AC-8
- [x] an unknown, expired, or consumed state behaves the same. The expired case uses a real stored
      row whose `expires_at` is in the past
      → AC-8
- [x] a Google error response still needs valid state and binding. With both, it consumes the attempt,
      clears the binding, and lands on `/signin?error=cancelled`; without either, it is
      `expired_state`
      → AC-8
- [x] two callbacks for one valid state may both complete the fake exchange, but exactly one
      transaction consumes the attempt and writes a session
      → AC-8

## Google adapter and profile tests

Use injectable code exchanger and ID token validator fakes. These tests need no network.

- [x] one table test refuses each of: exchange failure, forged signature, bad issuer, wrong audience,
      multiple audiences, stale expiry, wrong nonce, empty or wrong type subject, empty or wrong type
      email, and false or wrong type `email_verified`. Every case writes no tutor, link, event, or
      session
      → AC-14
- [x] a missing, empty, or wrong type `name` uses the email local part; valid name is trimmed
      → AC-14
- [x] two first sign ins for the same subject run concurrently against real Postgres. Both receive a
      session, while one tutor, one link, and one registered outbox event exist
      → AC-1, AC-2
- [x] a later unchanged sign in publishes nothing. Changed canonical email or display name updates
      the owned rows and publishes one profile event
      → AC-2, AC-9
- [x] a known subject whose new email belongs to another tutor signs in with both profile copies and
      display name unchanged, no event, and a request id in the conflict log
      → AC-9
- [x] an unknown subject on an existing email returns `email_conflict` and links nothing, including
      when the competing insert commits concurrently
      → AC-9

## Session and concurrency tests

These tests run against real Postgres with barriers that hold transactions at the named points. A
sequence of curl calls is not evidence for a race.

- [x] refresh with the exact app `Origin` rotates the cookie and returns an access token. Missing or
      foreign `Origin` returns `403 forbidden` with the gateway request id and changes no row
      → AC-3, AC-15
- [x] two refreshes using the same token inside grace both answer `200`, create tokens in one active
      family, and share one tutor
      → AC-7
- [x] a forced signing failure rolls the rotation back, leaves the presented token active, and sends
      neither a replacement cookie nor an access token
      → AC-3, AC-7
- [x] hold refresh after it locks the session, start sign out, then release refresh. Sign out runs
      next and no live token can refresh afterward
      → AC-6, AC-7
- [x] hold refresh before it inserts the new token, start a late reuse revocation, then release both.
      The session ends revoked and no inserted token remains usable
      → AC-7
- [x] sign out twice with the correct origin answers `204`; it clears the cookie and leaves the
      session terminal. A copied access token remains valid only until its original 15 minute expiry
      and no refresh extends it
      → AC-6
- [x] missing, unknown, expired, revoked, and late reused cookies all answer `401`, code
      `unauthenticated`, message `this session is over, sign in again`, one request id, and a clearing
      cookie with the original attributes
      → AC-16
- [x] the sweep deletes expired attempts and expired or seven day revoked session rows, with token
      rows removed by the foreign key cascade. It never deletes an active family
      → AC-7, AC-8

## Browser behavior

- [x] initial checking renders no protected route and starts no protected request. Refresh `200`
      becomes authenticated; `401` becomes anonymous; a network or `5xx` failure with no valid token
      shows unavailable with a retry control
      → AC-3, AC-16
- [x] one timer refreshes 60 seconds before `access_expires_at`. Returning to a visible tab performs
      the same check, and transient failures retry after one second then double to at most five
      seconds only while the old token is valid
      → AC-3
- [x] many protected calls waiting on expiry share one refresh promise. The first protected `401`
      refreshes and retries once; a second `401` clears memory and navigates to sign in
      → AC-3, AC-16
- [x] sign out clears the in memory token before the request finishes. `204` moves to anonymous; a
      server failure shows unavailable and offers retry without claiming the server session ended
      → AC-6
- [x] an anonymous protected route preserves its clean relative path and query in `redirect`; an
      invalid redirect becomes `/`; the callback target remains hidden until boot refresh succeeds
      → AC-3, AC-15
- [x] `navigator.languages` selects the first `vi` or `en` entry, with `vi` fallback. The five known
      codes render the exact ten sentences in `index.md`; an unknown error is ignored
      → AC-16
- [x] keyboard reach, visible focus, alert announcement, and the phone target meet WCAG AA
      → AC-1, AC-5

## Tenant boundary

- [x] no token on `/api/me` or `/api/thread` answers `401` in `vermouth.APIError` with the gateway
      request id
      → AC-4
- [x] for every protected route that accepts a tutor owned identifier, tutor A presents tutor B's
      identifier and receives no row. `/api/me` ignores all caller identifiers and reads only the
      verified `sub`
      → AC-11

## Real Google client

Create an OAuth client whose authorized callback exactly matches `IDENTITY_GOOGLE_REDIRECT_URL`.
Set the client id, client secret, app URL, and allowlist. This section proves integration only; the
fake adapter tests above prove the hostile claims.

- [x] sign in from `/signin`, return to the preserved app path, and receive one browser bound session
      with no token in a URL or history
      → AC-1, AC-8, AC-15
- [x] first sign in creates one tutor and link and publishes one registered event; second unchanged
      sign in creates and publishes nothing
      → AC-1, AC-2
- [x] reload and leave the tab open past one access expiry. Both remain signed in without another
      Google round trip
      → AC-3
- [x] an account outside the allowlist lands on the exact `not_allowed` sentence and writes nothing
      → AC-5

## Public deployment gate

- [ ] spec 0007 and scope feature 21 are verified before feature 16 exposes these endpoints. Start,
      callback, and refresh then have both caller and global limits, and attempt growth is bounded
      → prerequisite outside AC-1 through AC-16
