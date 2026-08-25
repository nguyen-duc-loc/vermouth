# 0004. Tutor sign in with Google only, no password

**Date**: 2026-08-22
**Status**: In Progress

## Summary

A tutor signs in with their Google account and Vermouth stores no password at all. The `identity`
service runs the whole Google exchange itself as a confidential client (it holds the Google client
secret, so the browser never sees it), then mints the same Ed25519 access token every service already
verifies. A first sign in creates the tutor, but only for an email on a configured allowlist, so a
public address does not let a stranger create a tenant. The session survives a reload through a
rotating refresh token in a cookie no script can read, which means there is no password to hash, no
password to reset, and one fewer secret in the database.

This replaces the password decision that specs 0001, 0002 and 0003 recorded but nothing ever built,
and it settles the open `password_hash` blocker in `0003/verify.md`.

## Requirements

**User stories**:

- As a tutor, I want to sign in with my Google account so that I never invent or remember another
  password.
- As a tutor, I want to stay signed in across a reload and across days so that marking attendance on
  a phone does not start with a sign in.
- As a tutor, I want to sign out so that a shared or lost device does not stay signed in.
- As the operator, I want only people I name to be able to create an account so that a public address
  is not an open sign up.

**Acceptance criteria** (the contract, each criterion is IDed and independently checkable):

- **AC-1**: A tutor whose Google email is on the allowlist can sign in from `/signin` and land back in
  the app signed in. The first sign in creates exactly one `tutors` row and one `tutor_identities`
  row; every later sign in creates neither.
- **AC-2**: A first sign in publishes exactly one `identity.tutor.registered` carrying the five
  catalogue fields from spec 0001, written to the outbox in the same transaction as the tutor row. A
  later sign in publishes nothing.
- **AC-3**: Reloading the tab leaves the tutor signed in without a Google round trip: the app calls
  `POST /api/auth/refresh` on boot and gets a fresh access token.
- **AC-4**: A request to any `/api/` endpoint other than the four auth endpoints without a valid token
  is refused `401` in the `vermouth.APIError` shape carrying its `request_id`.
- **AC-5**: A Google account that is not on the allowlist and has no tutor yet is refused: no `tutors`
  row and no `tutor_identities` row is written, and the browser lands on `/signin?error=not_allowed`
  with a sentence naming why.
- **AC-6**: `POST /api/auth/signout` revokes every refresh token in that session family and clears the
  cookie. A later refresh with the same cookie is refused `401`.
- **AC-7**: Presenting a refresh token that was already rotated more than the grace window ago is
  refused and revokes the whole family, so neither the holder of the stolen copy nor the tutor keeps
  the session. Inside the grace window the same token rotates again within the same family and
  answers `200`, so two tabs restored together both stay signed in.
- **AC-8**: A callback whose `state` is unknown, already consumed, or past its expiry writes no tutor
  and no session, and lands on `/signin?error=expired_state`.
- **AC-9**: A known Google subject whose email changed at Google updates `tutors.email` and
  `tutor_identities.provider_email` and publishes `identity.tutor.profile.changed`. When that new
  email already belongs to a different tutor, the sign in still succeeds, the email copy is left as
  it was, and the conflict is logged with the `request_id`. An unknown Google subject whose email
  already belongs to a tutor is refused with `email_conflict`, and links nothing.
- **AC-10**: Timezone and language come from the browser on the start call, are validated in
  `identity`, and fall back to `Asia/Ho_Chi_Minh` and `vi`. Both appear in the token claims and in
  `identity.tutor.registered`.
- **AC-11**: One tutor can never read another tutor's data: `tutor_id` comes from the verified token
  and nothing else (INV-8), and `GET /api/me` answers about the token's tutor only.
- **AC-12**: No password column and no password handling code remain anywhere in the repository: no
  Go file imports `golang.org/x/crypto/argon2`, and no `go.mod` requires `golang.org/x/crypto`
  directly. It is already present as an indirect requirement of `pgx` in all six modules, which is
  not this feature's business, so the check is a direct require plus an import, never `go.sum`.
  `task migrate:up` succeeds for all four services from an empty database.
- **AC-13**: `task thread` still drives the end to end thread with no browser and no Google round
  trip, and the only callers of `token.Signer.Mint` in the repository are the callback handler, the
  refresh handler, and the `task dev:token` program that no service binary imports. That list is the
  checkable form of "nothing shipped can mint a token without Google".
- **AC-14**: The callback creates nothing until Google's ID token passes validation: the signature
  against Google's key set, `aud` equal to this client, `iss` a Google issuer, unexpired allowing 60
  seconds of clock skew, `nonce` equal to the one on the `login_attempts` row, and `email_verified`
  true. Any one of those failing writes no tutor, no link, and no session, and lands on
  `/signin?error=provider_error`.

## Decision

**Chosen option**: Option 2: Google OAuth 2.0 Authorization Code with PKCE, run server side by
`identity`

Vermouth authenticates a tutor only through Google, with `identity` acting as the confidential OAuth
client that holds the client secret, exchanges the code, creates or finds the tutor, and mints the
existing Ed25519 access token; the browser holds only that access token in memory plus a rotating
refresh token in an `HttpOnly` cookie, and no password exists anywhere in the system.

**Implementation skills**: `golang-security` (`samber/cc-skills-golang`, `.agents/skills/golang-security/`) · `golang-database` (`samber/cc-skills-golang`, `.agents/skills/golang-database/`) · `golang-error-handling` (`samber/cc-skills-golang`, `.agents/skills/golang-error-handling/`) · `go-goose` (`metalagman/agent-skills`, `.agents/skills/go-goose/`) · `openapi` (`oakoss/agent-skills`, `.agents/skills/openapi/`) · `tanstack-router-best-practices` (`deckardger/tanstack-agent-skills`, `.agents/skills/tanstack-router-best-practices/`) · `tailwindcss-accessibility` (`josiahsiegel/claude-plugin-marketplace`, `.agents/skills/tailwindcss-accessibility/`)

## Rationale

Reasoning, the options weighed, and what was rejected: see [rationale.md](rationale.md).

## Feature design

### Data model sketch

`identity`'s database only. No other service learns anything new (the event catalogue is untouched).

**`tutors`** (existing, from `00002`, plus `updated_at` from the rewritten `00003`)

| Column | Type | Note |
|---|---|---|
| `tutor_id` | `uuid PRIMARY KEY` | UUIDv7 generated in Go |
| `email` | `text NOT NULL UNIQUE` | a copy of Google's `email`, kept in step with it |
| `display_name` | `text NOT NULL` | from Google's `name` |
| `timezone` | `text NOT NULL` | from the browser, validated |
| `language` | `text NOT NULL` | `vi` or `en` |
| `created_at` | `timestamptz NOT NULL DEFAULT now()` | |
| `updated_at` | `timestamptz NOT NULL DEFAULT now()` | the row's own stamp when `identity.tutor.profile.changed` fires; the event carries only spec 0001's five fields, never this column |

`password_hash` is gone: never applied anywhere, and now never added.

**`tutor_identities`** (new): the link to an external account, kept off `tutors` so a second provider
is one more row rather than an `ALTER TABLE` plus every query that reads a tutor.

| Column | Type | Note |
|---|---|---|
| `tutor_id` | `uuid NOT NULL REFERENCES tutors(tutor_id) ON DELETE CASCADE` | |
| `provider` | `text NOT NULL` | only `google` today |
| `provider_subject` | `text NOT NULL` | Google's `sub`, the stable identity |
| `provider_email` | `text NOT NULL` | what Google last told us |
| `linked_at` | `timestamptz NOT NULL DEFAULT now()` | |
| | `PRIMARY KEY (provider, provider_subject)` | one Google account maps to at most one tutor |
| | `UNIQUE (tutor_id, provider)` | one Google account per tutor, since linking is out of scope |

**`login_attempts`** (new): one in flight sign in, from the start call until the callback consumes it.

| Column | Type | Note |
|---|---|---|
| `state` | `text PRIMARY KEY` | 32 random bytes from `crypto/rand`, base64url |
| `code_verifier` | `text NOT NULL` | the PKCE verifier, never leaves the server |
| `nonce` | `text NOT NULL` | 32 random bytes, sent to Google and checked against the ID token claim |
| `redirect_to` | `text NOT NULL DEFAULT '/'` | an app path, validated against an open redirect |
| `timezone` | `text NOT NULL` | carried from the start call, because the callback is what creates the tutor |
| `language` | `text NOT NULL` | same reason |
| `created_at` | `timestamptz NOT NULL DEFAULT now()` | |
| `expires_at` | `timestamptz NOT NULL` | ten minutes after creation |

**`refresh_tokens`** (new): one row per issued refresh token, with `session_id` as the family that
survives every rotation.

| Column | Type | Note |
|---|---|---|
| `token_hash` | `bytea PRIMARY KEY` | `sha256` of the raw 32 byte token, so the plain value is never stored and no timing safe comparison is needed |
| `session_id` | `uuid NOT NULL` | the family, constant across rotations |
| `tutor_id` | `uuid NOT NULL REFERENCES tutors(tutor_id) ON DELETE CASCADE` | |
| `issued_at` | `timestamptz NOT NULL DEFAULT now()` | |
| `expires_at` | `timestamptz NOT NULL` | 30 days, set again on every rotation, so the session slides |
| `used_at` | `timestamptz` | null until rotated; a second use is a race inside the grace window and theft after it |
| `revoked_at` | `timestamptz` | null until sign out or a detected reuse |

Indexes: `session_id` (revoke a family), `tutor_id`, `expires_at` (the sweep).

### State transitions

- **A sign in attempt**: `pending` (row written by start) → consumed (deleted inside the callback
  transaction) or expired (past `expires_at`, deleted by the sweep). Single use is a delete, so a
  replay finds nothing rather than a signature that still verifies.
- **One refresh token**: `active` → `rotated` (`used_at` set, a new row joins the same family) →
  `revoked` (`revoked_at` set) or `expired` (past `expires_at`). A `rotated` token presented again
  within `IDENTITY_REFRESH_GRACE` of its `used_at` rotates once more in the same family instead of
  ending it, which is the two tab case; presented later, it ends the family.
- **A session family**: `active` → `revoked`, by sign out or by a reuse of any already rotated member.
  A revoked family never becomes active again; the tutor signs in with Google to start a new one.

### API surface

All four are new on the gateway and carry `security: []` in `api/openapi.yaml`, because they are what
produces a token. `POST /api/tutors` is deleted, along with its two schemas.

| Endpoint | Method | Key inputs | Key outputs | Auth | Key errors |
|---|---|---|---|---|---|
| `/api/auth/google/start` | GET | `tz`, `lang`, `redirect_to` (query, all optional) | `302` to Google's authorize URL, the `login_attempts` row written | none | `302` to `/signin?error=provider_error` |
| `/api/auth/google/callback` | GET | `code`, `state`, or `error` (query, sent by Google) | `302` to the app, `Set-Cookie: vermouth_refresh` | none | `302` to `/signin?error=<code>` |
| `/api/auth/refresh` | POST | the `vermouth_refresh` cookie | `access_token`, `access_expires_at`, a rotated cookie | the cookie | `401 unauthenticated` |
| `/api/auth/signout` | POST | the `vermouth_refresh` cookie | `204`, the cookie cleared | the cookie | none, it is idempotent |

The callback never carries a token. It lands the browser back in the app and the app calls `refresh`,
which is the same call a reload makes, so there is one path to an access token and no token ever
appears in a URL, a redirect log, or browser history.

The cookie is `HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/api/auth`, `Max-Age` matching
`expires_at`. `Secure` works on `http://localhost` because browsers treat localhost as a trustworthy
origin, and `IDENTITY_COOKIE_SECURE` exists for the one case where it does not. Every later
`Set-Cookie` repeats those attributes exactly, the rotation on `refresh` and the clear on `signout`
alike, because a clear whose `Path` does not match leaves the old cookie in place and a sign out then
looks like it worked while the browser keeps sending the token. `refresh` is a `POST` and not a `GET`
for a related reason: the method is half of the cross site protection, so a boot time check must not
be turned into a `GET` later for convenience.

The gateway forwards these four paths to `identity` and adds nothing: it passes the inbound `Cookie`
header on, and copies `Location` and `Set-Cookie` back out. It sets no cookie of its own and holds no
part of the session rule (spec 0001, the gateway owns no business rule).

### Value sourcing

| Action | Value produced or displayed | Source |
|---|---|---|
| start | `state` | 32 bytes from `crypto/rand`, base64url |
| start | `code_verifier`, `code_challenge` | `crypto/rand`, then `sha256` per PKCE (RFC 7636) |
| start | `nonce` | 32 bytes from `crypto/rand`, base64url, stored on the attempt and sent to Google |
| start | `timezone` on the stored attempt | the `tz` query param, validated with `time.LoadLocation`, else `Asia/Ho_Chi_Minh` |
| start | `language` on the stored attempt | the `lang` query param, `vi` or `en`, else `vi` |
| start | `redirect_to` | the query param, kept only if it starts with a single `/`, else `/` |
| start | the Google authorize URL | `IDENTITY_GOOGLE_CLIENT_ID` plus `IDENTITY_GOOGLE_REDIRECT_URL`, scopes `openid email profile` |
| callback | whether Google's ID token is genuine | `google.golang.org/api/idtoken`'s `Validate` for the signature and key set, then `aud` equal to `IDENTITY_GOOGLE_CLIENT_ID`, `iss` one of `https://accounts.google.com` or `accounts.google.com`, unexpired within 60 seconds of skew, and `nonce` equal to the attempt's |
| callback | `provider_subject` | the `sub` claim of Google's ID token |
| callback | `email` | the `email` claim, refused unless `email_verified` is true |
| callback | `display_name` | the `name` claim, trimmed; when it is missing or empty after trimming, the part of the email before `@` |
| callback | `tutor_id` | UUIDv7 generated in Go |
| callback | `timezone`, `language` on the new tutor | the `login_attempts` row, not the request |
| callback | the `identity.tutor.registered` fields | the inserted row, through the existing conversion, so no column can leak into an event |
| callback | `session_id` | UUIDv7 generated in Go |
| callback | the refresh token value | 32 bytes from `crypto/rand`, base64url, stored only as `sha256` |
| callback | `expires_at` | `now` plus `IDENTITY_REFRESH_TTL` (default 30 days) |
| callback | the redirect target | `IDENTITY_APP_URL` plus `login_attempts.redirect_to` |
| callback | the error code in `?error=` | one of `not_allowed`, `email_conflict`, `cancelled`, `expired_state`, `provider_error` |
| refresh | `access_token`, `access_expires_at` | `token.Signer.Mint` from the tutor row, `IDENTITY_TOKEN_TTL` (15m) |
| refresh | the rotated cookie | a new random token in the same `session_id` |
| refresh | whether a rotated token is a race or a theft | `used_at` against `now`, compared to `IDENTITY_REFRESH_GRACE` (default `10s`): inside it rotate again in the same family, outside it revoke the family |
| the app on boot | whether a tutor is signed in | whether `refresh` answered `200`; never a stored flag |
| the sign in screen | the error sentence | a fixed map from the five `?error=` codes, in the tutor's language |
| `/api/me` | the tutor | `tutors` by `tutor_id` from the token (INV-8) |

The allowlist needs no value of its own, but it does need one normal form. Both sides are trimmed and
lowercased before they are compared, an empty entry from a double comma is dropped, and an entry with
no `@` in it stops startup rather than sitting in the list matching nothing, because a typo in a
gate should be loud (STK-8).

### Key invariants

- No password exists in the system: no column, no hash, no reset path, no `golang.org/x/crypto`.
- `tutor_id` comes from a verified token everywhere except the four auth endpoints, and those take
  the tutor from Google's `sub`, never from an input (INV-8).
- The Google client secret exists only in `identity`'s environment. The gateway and the browser never
  hold it, and no ID token is ever accepted from the browser.
- `provider_subject` is the identity; `email` is a copy of it. An email match never creates or finds a
  link.
- One `login_attempts` row is single use: consumed by a delete inside the callback transaction.
- A refresh token is stored only as `sha256` of its value, and reusing a rotated one revokes the whole
  family.
- On a first sign in the tutor row, the link row, the outbox row, the first refresh token row, and
  the `login_attempts` delete all go into one `pgx.Tx` in one function (one aggregate, INV-3,
  STK-4). `token.Signer.Mint` runs after that commit, which is where `handler.Register` already
  mints today (`services/identity/internal/handler/tutor.go:168` commits, `:179` mints), so a
  signing failure cannot undo a tutor who already exists. The refresh and signout paths publish
  nothing.
- The event catalogue does not change. `identity.tutor.registered` and
  `identity.tutor.profile.changed` keep the exact fields spec 0001 names.

### Security model

- The four auth endpoints are deliberately unauthenticated, because they are what produces a token.
  Everything else under `/api/` stays behind the gateway's existing verification, and each service
  verifies again locally (INV-14).
- **Who may sign in**: any Google account may attempt it. Only an email in
  `IDENTITY_SIGNUP_ALLOWLIST` may create a tutor; an existing linked subject signs in regardless of
  the allowlist, so removing an address does not lock out a tutor who already exists. An empty or
  unset allowlist means no new tutor can be created, so it fails closed.
- **What makes Google's answer trustworthy**: the code is exchanged over a TLS connection `identity`
  opened itself, and the ID token that comes back is validated as well, because the transport tells
  you who you are talking to while the claims tell you who the token was minted for.
  `google.golang.org/api/idtoken`'s `Validate` covers the signature and Google's key set, and the
  handler then checks `aud`, `iss`, expiry with 60 seconds of skew, the `nonce` from the attempt row,
  and `email_verified`. No ID token is ever accepted from the browser.
- **A taken email never moves a tutor**: the callback looks the account up by
  `(provider, provider_subject)` and only that lookup decides who signs in. A known subject whose new
  email already belongs to a different tutor still signs in, with the email copy left stale and the
  conflict logged, because locking a real tutor out over a copied field is worse than a stale copy. An
  unknown subject on a taken email is refused with `email_conflict`, since linking is out of scope.
- **Who may read what**: a tutor reads only their own rows. There is no admin role and no second role.
- **Cross site protection on the callback** is the `login_attempts` row itself: `state` is
  unguessable, single use, and expires in ten minutes, so a forged callback has no row to match.
- **Cross site protection on refresh and signout** is `SameSite=Lax` plus the method being `POST`,
  which a cross site page cannot make with the cookie attached. No separate token is kept in step.
- **Open redirect**: `redirect_to` is accepted only as a path starting with a single `/`, and the
  redirect target is always built from `IDENTITY_APP_URL`, never from anything the caller sends.
- Every random value uses `crypto/rand`. Failures answer in generic terms through
  `vermouth.WriteError` while the reason goes to the log with the `request_id`.
- No compliance scope applies: one tutor, their own data, no card data and no health data.

### Configuration required

- `IDENTITY_GOOGLE_CLIENT_ID`: the OAuth client, required, startup fails without it (STK-8).
- `IDENTITY_GOOGLE_CLIENT_SECRET`: required, held only by `identity`.
- `IDENTITY_GOOGLE_REDIRECT_URL`: the public callback address, for example
  `http://localhost:8080/api/auth/google/callback`. Required, and it must match the Google console
  entry exactly.
- `IDENTITY_APP_URL`: where the callback sends the browser back to, for example
  `http://localhost:5173` in development. Required.
- `IDENTITY_SIGNUP_ALLOWLIST`: comma separated emails allowed to create a tutor. Optional, and empty
  means nobody new, which is the safe reading rather than a silent default. Entries are trimmed and
  lowercased at startup, an empty entry is dropped, and an entry with no `@` stops startup with
  `vermouth.MissingEnvError` carrying a `Reason`, the same way `TOKEN_PUBLIC_KEYS` already reports a
  malformed entry (`pkg/vermouth/config.go:168`), rather than being ignored.
- `IDENTITY_REFRESH_TTL`: optional, default `720h`.
- `IDENTITY_REFRESH_GRACE`: optional, default `10s`. How long after a rotation the old token still
  rotates instead of ending the family, which is what keeps two tabs booting together from looking
  like theft. Shorter is stricter; zero turns the grace off entirely.
- `IDENTITY_SWEEP_INTERVAL`: optional, default `10m`. Its own value, not the relay's `750ms`, because
  these rows live ten minutes and thirty days.
- `IDENTITY_COOKIE_SECURE`: optional, default true. Only for an origin a browser does not treat as
  trustworthy.
- `IDENTITY_TOKEN_TTL` already exists and stays at `15m`.

### Critical test scenarios

- Happy path: an allowlisted Google account signs in, a tutor and a link appear, one
  `identity.tutor.registered` reaches `notifications`, and `/api/me` answers, verifies **AC-1**,
  **AC-2**, **AC-10**.
- Reload: after sign in, a fresh boot with only the cookie gets an access token, verifies **AC-3**.
- Second sign in: the same subject signs in again, no new rows and no new event, verifies **AC-1**,
  **AC-2**.
- Refused sign up: a Google account off the allowlist, no rows written, `?error=not_allowed`,
  verifies **AC-5**.
- Replayed callback: the same `state` twice, the second refused, verifies **AC-8**.
- Stolen cookie: a rotated refresh token presented again well after the grace window, the family
  revoked, the tutor's next refresh also refused, verifies **AC-7**.
- Two tabs at once: two `refresh` calls with the same cookie inside the grace window, both answered
  `200`, one family still active, nothing revoked, verifies **AC-7**. This is the scenario the reuse
  rule breaks without a grace window, so it is the one that must exist.
- Sign out then refresh: `401`, verifies **AC-6**.
- Changed Google email: the same subject with a new email, `tutors.email` updated and
  `identity.tutor.profile.changed` published, verifies **AC-9**.
- Email takeover attempt: a new subject whose email matches an existing tutor, refused with
  `email_conflict`, nothing linked, verifies **AC-9**.
- Email collision on a real tutor: a known subject whose new Google email already belongs to another
  tutor, still signed in, the email copy unchanged, the conflict logged, verifies **AC-9**.
- A bad ID token: `aud` for another client, a stale expiry, a `nonce` that does not match the attempt,
  and `email_verified` false, each refused with no rows written, verifies **AC-14**.
- Auth and permission: no token on `/api/me` and on `/api/thread`, `401` in the one error shape,
  verifies **AC-4**, **AC-11**.
- Migrations: `task infra:clean` then `task migrate:up`, all four services succeed, verifies
  **AC-12**.
- No browser: `task dev:token` then `task thread`, the thread completes, verifies **AC-13**.

## Migration plan

**Strategy**: big bang, deliberately. There is no live system to strangle: no user, no production
database, no password ever stored, and the 26 `tutors` rows in the development database are output
from `task thread` runs. Running an old and a new sign in side by side would mean building the
password path that this decision exists to avoid.

**Phases**:

1. Take the password out of the repository before anything new goes in: rewrite
   `00003_tutor_credentials.sql` as `00003_tutor_timestamps.sql` keeping only `updated_at`, and
   correct the four documentation lines that promise a password (spec 0001's ownership row, spec
   0002's stack row and its rationale bullet, spec 0003's column row plus its migration list).
   Nothing has ever applied `00003`, so rewriting it in place keeps the migration history truthful
   instead of adding a column whose whole life is two migrations.
2. `task infra:clean`, then `task migrate:up` on an empty identity database, which is also the proof
   that the blocker in `0003/verify.md` is gone.
3. Add `00004_tutor_google_identity.sql` with the three new tables, then build the thread in the
   order the build plan gives.
4. Delete `POST /api/tutors` from `api/openapi.yaml`, the gateway, and `identity`, in the same change
   that adds `task dev:token`, so `task thread` never has a broken window.

**Rollback**: revert the commit range and run `task infra:clean`. `goose` down steps exist for both
migrations, but a rollback wants a clean database rather than a stepwise unwind, because the tutor
rows created after the cutover have no meaning without their Google link.

**Risks**:

- Forgetting `task infra:clean` leaves the 26 rows in place; `00004` then applies, but every one of
  those tutors is unreachable, since no Google subject links to them. The symptom is a sign in that
  refuses with `email_conflict` on an email that already exists (AC-9's refusal path), which reads as
  a bug and is not one.
- `IDENTITY_GOOGLE_REDIRECT_URL` must match the Google console entry character for character, and
  the failure is a Google error page rather than anything in the logs.
- Deleting `POST /api/tutors` breaks `test/thread.sh` until `task dev:token` exists. They land
  together for that reason.

## Build plan

Tracer Bullet: the first eight tasks are one thin sign in thread through browser, gateway, identity,
Google, and the database, and back to a signed in screen. Nothing is thickened until that thread runs.

1. Clear the password out of the repository: rewrite `00003` as `00003_tutor_timestamps.sql` with
   `updated_at` only, drop the password comment from `00002`'s header, confirm no Go code and no
   `go.mod` mentions `argon2` or `golang.org/x/crypto`, then `task infra:clean` and `task migrate:up`,
   satisfies **AC-12**.
2. `00004_tutor_google_identity.sql`: `tutor_identities`, `login_attempts`, `refresh_tokens` with
   their constraints and indexes, satisfies **AC-1**, **AC-6**, **AC-7**, **AC-8**.
3. Hand write the queries in `db/queries/` (find a link by subject, insert a tutor, insert a link,
   update the email pair, insert and consume an attempt, insert, read, rotate and revoke a refresh
   token, revoke a family, and the two sweeps) and run `task generate:sql`, satisfies **AC-1**,
   **AC-6**, **AC-7**, **AC-9**.
4. The contract first, since both halves generate from it: add the four auth endpoints to
   `api/openapi.yaml` with `security: []`, delete `POST /api/tutors` and its schemas, then
   `task generate:api` and `task web:generate`, satisfies **AC-1**, **AC-4**.
5. `identity`: the OAuth client from configuration plus `GET /api/auth/google/start`, writing the
   attempt with its `state`, verifier and `nonce`, and answering `302` with the `nonce` on the
   authorize URL, satisfies **AC-1**, **AC-10**, **AC-14**.
6. `identity`: `GET /api/auth/google/callback`. Exchange the code, validate the ID token with
   `idtoken.Validate` plus the `aud`, `iss`, expiry, `nonce` and `email_verified` checks, read `sub`,
   `email` and `name`, apply the allowlist, then in one `pgx.Tx` consume the attempt, insert or find
   the tutor and its link, write the outbox row on a first sign in only, and issue the first refresh
   token; mint after that commit, then set the cookie and redirect, satisfies **AC-1**, **AC-2**,
   **AC-5**, **AC-8**, **AC-14**.
7. `identity`: `POST /api/auth/refresh`, rotating within the family and minting the access token,
   satisfies **AC-3**.
8. `gateway`: forward the four auth paths unauthenticated, passing `Cookie` inbound and `Location`
   plus `Set-Cookie` outbound, and delete the `POST /api/tutors` route; everything else stays behind
   the existing verification, satisfies **AC-1**, **AC-3**, **AC-4**, **AC-11**. Shape: the four
   paths are registered on the outer mux exactly where `POST /api/tutors` is carved out today
   (`gateway/internal/route/routes.go:40`), while `/api/` keeps going to the inner mux behind
   `auth.Middleware`. Go's `ServeMux` picks the more specific pattern, so nothing else needs
   rearranging.
9. `web`: a `/signin` route with one Google button linking to `start` with the browser's timezone and
   language, a boot time `refresh` that decides whether the app is signed in, the token kept in
   memory as today, and the four error sentences. WCAG AA with visible focus and a phone first target
   size, satisfies **AC-1**, **AC-3**, **AC-5**, **AC-10**.
   The thread runs end to end from here. The rest thickens it.
10. Reuse detection with the grace window: a token whose row already carries `used_at` rotates again
    in the same family when `used_at` is within `IDENTITY_REFRESH_GRACE`, and otherwise revokes the
    whole family and answers `401`, satisfies **AC-7**.
11. `POST /api/auth/signout`: revoke the family, clear the cookie, answer `204`, plus the sign out
    control in the app, satisfies **AC-6**.
12. `task dev:token`: a small program outside every service binary that inserts a tutor and signs a
    token with the development key, and `test/thread.sh` using it instead of the deleted endpoint,
    satisfies **AC-13**.
13. A known subject with a changed email updates both rows and publishes
    `identity.tutor.profile.changed`, unless the new email belongs to another tutor, in which case
    the sign in proceeds with the copy untouched and the conflict logged; an unknown subject on a
    taken email is refused with `email_conflict`, satisfies **AC-9**.
14. The sweep: delete expired `login_attempts` and expired or long revoked `refresh_tokens` on the
    same ticker shape the relay already uses, but on `IDENTITY_SWEEP_INTERVAL` (default `10m`), not
    the relay's `750ms`, satisfies **AC-8**.
15. `.env.example` gains the nine new variables with the comments that say what each is for, and
    `task dev:keys` stays untouched, satisfies **AC-1**, **AC-12**.

## Consequences

**Positive**:

- No password anywhere: nothing to hash, store, rotate, reset, leak, or explain in a spec. The
  highest severity thing a tutor could hand over does not exist.
- The blocker in `0003/verify.md` closes by deletion rather than by a workaround, and
  `task migrate:up` covers all four services again.
- Google carries the hard parts: password strength, breach checks, two factor, and account recovery.
- The token design already in the code stays exactly as it is. Ed25519 minting in `identity`, local
  verification everywhere, `sub`, `tz`, `language`, `iat`, `exp`, unchanged claims.
- The event catalogue does not move, so `billing` and `notifications` need no change at all.
- `tutor_identities` makes a second provider later a row and a branch, not a schema change.

**Negative or tradeoffs**:

- Google becomes a hard dependency of sign in with no fallback. A Google outage means nobody new can
  sign in, though an existing session keeps working for up to 30 days because `refresh` never talks
  to Google.
- Sign in now needs internet and a real Google client, so an offline demo is impossible and a fresh
  clone needs console setup before the first sign in. This is a real step backwards from `curl`
  against `POST /api/tutors`.
- Email becomes a copy of Google's value. Feature 12's profile screen must not offer an editable email
  field, or the copy and the source drift apart.
- Adding a tester means editing `IDENTITY_SIGNUP_ALLOWLIST` and restarting `identity`. Acceptable at
  one tutor plus friends, annoying beyond that.
- Three new tables, four new endpoints, a cookie path, and a sweep, all of it code that exists only to
  hold a session. A single long lived token in `localStorage` would have been perhaps a fifth of it.
- The auth endpoints have no rate limit, so `start` can be spammed to grow `login_attempts` until the
  sweep runs. Fine behind localhost, not fine on the public address feature 16 creates.
- `task dev:token` is a second way a tutor row can appear. It lives outside every service binary on
  purpose, but it is still a door that has to stay outside.
- The 26 development tutor rows are deleted rather than migrated.

**Neutral**:

- `POST /api/tutors` disappears from the public surface, so the skeleton's own thread changes shape
  even though what it proves is unchanged.
- `golang.org/x/oauth2` and `google.golang.org/api/idtoken` join `identity`'s dependencies, the
  second one so the ID token checks live in a library rather than in hand written claim code.
  `golang.org/x/crypto` never becomes a direct one, though `pgx` already pulls it in indirectly,
  which is what AC-12 is careful about.
- The refresh cookie is the first cookie in the system, so the gateway learns to pass two headers it
  did not pass before.

## Follow-up

- [ ] Rate limit `start`, `callback` and `refresh` before feature 16 puts the gateway on a public
      address. Nothing bounds them today.
- [ ] A stub Google provider in `test/`, pointed at through the `IDENTITY_GOOGLE_*` variables, so the
      browser path can be driven end to end. The runner up in this design, and what the empty
      `test/e2e/` will want when `$test` covers the money path with Playwright.
- [ ] `$sync` owes three context files an update once this is built: `services/identity/AGENTS.md`
      (the `golang.org/x/crypto/argon2` dependency line, the password hash convention, and the
      password hash gotcha), root `AGENTS.md` if it grows an auth line, and `web/AGENTS.md` (the token
      storage note in `src/api/client.ts`).
- [ ] Feature 12 must leave email read only on the profile screen, since Google owns it.
- [ ] Sign out everywhere (revoking every family of one tutor) is deliberately out of this MVP. The
      `session_id` shape already supports it if a lost phone ever makes it worth a control.
- [ ] Account linking and a second provider stay closed until a real tutor asks. `tutor_identities`
      allows both, and the refusal in AC-9 is what a tutor hits meanwhile.
