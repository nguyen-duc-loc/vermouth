# 0004. Tutor sign in with Google only, no password

**Date**: 2026-08-22
**Status**: Accepted

## Summary

A tutor signs in with their Google account and Vermouth stores no password at all. The `identity`
service runs the whole Google exchange itself as a confidential client (it holds the Google client
secret, so the browser never sees it), creates a session, and lets the browser exchange that cookie
for the same Ed25519 access token every service verifies. Only an allowlisted email may create a
tutor, so a public address does not become open sign up. A browser binding protects the callback, a
locked session family makes refresh and sign out agree under concurrency, and the browser renews its
short lived token in memory, so there is no password to hash, reset, or leak.

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
  row; every later sign in creates neither. Two valid first sign ins for the same Google subject at
  the same time still produce one tutor, one link, and one registration event, while both browsers
  receive their own session.
- **AC-2**: A first sign in publishes exactly one `identity.tutor.registered` carrying the five
  catalogue fields from spec 0001, written to the outbox in the same transaction as the tutor row. A
  later sign in with no changed Google profile publishes nothing. A changed profile follows AC-9.
- **AC-3**: Reloading the tab leaves the tutor signed in without a Google round trip. The app calls
  `POST /api/auth/refresh` on boot, refreshes once when the access token has at most 60 seconds left,
  and refreshes then retries once after the first protected request answers `401`. Concurrent callers
  share one refresh promise, and a tab left open stays usable without a reload.
- **AC-4**: A request to any `/api/` endpoint other than the four auth endpoints without a valid token
  is refused `401` in the `vermouth.APIError` shape carrying its `request_id`.
- **AC-5**: A Google account that is not on the allowlist and has no tutor yet is refused: no `tutors`
  row and no `tutor_identities` row is written, and the browser lands on `/signin?error=not_allowed`
  with a sentence naming why.
- **AC-6**: `POST /api/auth/signout` revokes the session family and clears the cookie. A refresh racing
  sign out cannot leave a usable refresh token behind, and a later refresh is refused `401`. The
  browser clears its in memory access token as soon as sign out begins. An access token already
  copied elsewhere remains valid only until its existing 15 minute expiry, which is the deliberate
  cost of local verification under INV-14.
- **AC-7**: Presenting a refresh token that was already rotated more than the grace window ago is
  refused and revokes the whole family, so neither the holder of the stolen copy nor the tutor keeps
  the session. Inside the grace window the same token rotates again within the same family and
  answers `200`, so two tabs restored together both stay signed in. Concurrent rotations, sign out,
  and late reuse serialize on the session family, so once its terminal revoked state commits no
  later transaction can add a live token to it.
- **AC-8**: A callback whose `state` is unknown, already consumed, or past its expiry writes no tutor
  and no session, and lands on `/signin?error=expired_state`.
  The callback also requires the `HttpOnly` browser binding cookie created by the matching start call;
  a valid state copied into another browser is refused the same way. Two callbacks may both finish
  Google's exchange, but the transactional attempt delete allows only one to write a session.
- **AC-9**: A known Google subject whose email changed at Google updates `tutors.email` and
  `tutor_identities.provider_email`; a changed display name updates `tutors.display_name`. Either
  change publishes one `identity.tutor.profile.changed`. When a new email already belongs to a
  different tutor, the sign in still succeeds, both email copies and the display name stay as they
  were, no profile event is published, and the conflict is logged with the `request_id`. An unknown
  Google subject whose email already belongs to a tutor is refused with `email_conflict`, and links
  nothing.
- **AC-10**: Timezone and language come from the browser on the start call, are validated in
  `identity`, and fall back to `Asia/Ho_Chi_Minh` and `vi`. Both appear in the token claims and in
  `identity.tutor.registered`. They are creation defaults only; signing in later from another device
  does not overwrite the tutor's saved preferences.
- **AC-11**: One tutor can never read another tutor's data: `tutor_id` comes from the verified token
  and nothing else (INV-8), and `GET /api/me` answers about the token's tutor only.
- **AC-12**: No password column and no password handling code remain anywhere in the repository: no
  Go file imports `golang.org/x/crypto/argon2`, and no `go.mod` requires `golang.org/x/crypto`
  directly. It is already present as an indirect requirement of `pgx` in all six modules, which is
  not this feature's business, so the check is a direct require plus an import, never `go.sum`.
  `task migrate:up` succeeds for all four services from an empty database.
- **AC-13**: `task thread` still drives the end to end thread with no browser and no Google round
  trip, and the only callers of `token.Signer.Mint` in the repository are the refresh handler and the
  `task dev:token` program that no service binary imports. That list is the checkable form of "no
  shipped request path can mint an access token without a live refresh session".
- **AC-14**: The callback creates nothing until Google's ID token passes validation: the signature
  against Google's key set, one `aud` equal to this client, `iss` a Google issuer, the validator's
  expiry check, `nonce` equal to the one on the `login_attempts` row, a nonempty string `sub`, a
  nonempty string email, and boolean `email_verified` true. `name` is the only optional claim. A bad
  signature, issuer, audience, expiry, nonce, subject, email, or verified flag writes no tutor, link,
  or session and lands on `/signin?error=provider_error`. An injectable validator makes every case
  testable without a real Google account.
- **AC-15**: `redirect_to` accepts only a clean relative app path and query. Backslashes, control
  bytes, fragments, encoded path separators that become a second leading slash, a scheme, a host,
  or a protocol relative value fall back to `/`. `IDENTITY_APP_URL` is a validated origin, and the
  final redirect always keeps that origin. Plain HTTP is accepted only for `localhost` or a hostname
  ending in `.localhost`, which keeps the local Kubernetes ingress on the loopback namespace while
  every other hostname requires HTTPS. Refresh and sign out require an `Origin` exactly equal to it
  and refuse another or missing origin with `403 forbidden`.
- **AC-16**: A missing, unknown, expired, revoked, or reused refresh cookie answers `401` with code
  `unauthenticated`, message `this session is over, sign in again`, the gateway request id, and a
  clearing `Set-Cookie`. The browser treats all five alike, clears memory, and returns to `/signin`.
  The sign in page accepts only the five known error codes, chooses `vi` or `en` from the first
  supported entry in `navigator.languages`, and displays the fixed sentence recorded below.

## Decision

**Chosen option**: Option 2: Google OAuth 2.0 Authorization Code with PKCE, run server side by
`identity`

Vermouth authenticates a tutor only through Google, with `identity` acting as the confidential OAuth
client that holds the client secret, exchanges the code, creates or finds the tutor, and issues a
rotating refresh token. The refresh endpoint mints the existing Ed25519 access token. The browser
holds that access token in memory plus the refresh token in an `HttpOnly` cookie. A second `HttpOnly`
cookie binds the Google callback to the browser that started it, and a locked `auth_sessions` row is
the authority for refresh and sign out. Small code exchange and ID token validator interfaces keep
the external boundary testable without weakening the production Google adapters. No password exists
anywhere in the system.

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
| `email` | `text NOT NULL UNIQUE CHECK (email = lower(btrim(email)))` | Google's email in the one canonical lowercase form |
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
| `provider_email` | `text NOT NULL CHECK (provider_email = lower(btrim(provider_email)))` | what Google last told us, in the same canonical form |
| `linked_at` | `timestamptz NOT NULL DEFAULT now()` | |
| | `PRIMARY KEY (provider, provider_subject)` | one Google account maps to at most one tutor |
| | `UNIQUE (tutor_id, provider)` | one Google account per tutor, since linking is out of scope |

**`login_attempts`** (new): one in flight sign in, from the start call until the callback consumes it.

| Column | Type | Note |
|---|---|---|
| `state` | `text PRIMARY KEY` | 32 random bytes from `crypto/rand`, base64url |
| `code_verifier` | `text NOT NULL` | the PKCE verifier, never leaves the server |
| `nonce` | `text NOT NULL` | 32 random bytes, sent to Google and checked against the ID token claim |
| `browser_binding_hash` | `bytea NOT NULL` | `sha256` of the independent cookie value created by start |
| `redirect_to` | `text NOT NULL DEFAULT '/'` | an app path, validated against an open redirect |
| `timezone` | `text NOT NULL` | carried from the start call, because the callback is what creates the tutor |
| `language` | `text NOT NULL` | same reason |
| `created_at` | `timestamptz NOT NULL DEFAULT now()` | |
| `expires_at` | `timestamptz NOT NULL` | ten minutes after creation |

**`auth_sessions`** (new): the session family and its terminal state. Every refresh, sign out, and
reuse decision locks this row before it reads or writes a token.

| Column | Type | Note |
|---|---|---|
| `session_id` | `uuid PRIMARY KEY` | UUIDv7 generated in Go |
| `tutor_id` | `uuid NOT NULL REFERENCES tutors(tutor_id) ON DELETE CASCADE` | the tutor every token in the family belongs to |
| `created_at` | `timestamptz NOT NULL DEFAULT now()` | |
| `expires_at` | `timestamptz NOT NULL` | sliding expiry, updated to the newest token expiry while active |
| `revoked_at` | `timestamptz` | the terminal state, set once by sign out or late reuse |

Indexes: `tutor_id`, `expires_at`, and a partial index on `revoked_at` where it is not null.

**`refresh_tokens`** (new): one row per issued refresh token. The session row owns the family state,
so a token cannot disagree with sign out.

| Column | Type | Note |
|---|---|---|
| `token_hash` | `bytea PRIMARY KEY` | `sha256` of the raw 32 byte token, so the plain value is never stored and no timing safe comparison is needed |
| `session_id` | `uuid NOT NULL REFERENCES auth_sessions(session_id) ON DELETE CASCADE` | the family, constant across rotations |
| `issued_at` | `timestamptz NOT NULL DEFAULT now()` | |
| `expires_at` | `timestamptz NOT NULL` | 30 days, set again on every rotation, so the session slides |
| `used_at` | `timestamptz` | null until rotated; a second use is a race inside the grace window and theft after it |

Indexes: `session_id` and `expires_at`.

### State transitions

- **A sign in attempt**: `pending` (row and browser binding cookie written by start) → consumed
  (deleted inside the callback transaction) or expired (past `expires_at`, deleted by the sweep).
  The callback needs both the state and the matching binding cookie. Single use is the transactional
  delete, so concurrent callbacks may both finish Google's exchange but only one can write.
- **One refresh token**: `active` → `rotated` (`used_at` set and a new row joins the locked family) or
  `expired`. A rotated token presented again inside `IDENTITY_REFRESH_GRACE` rotates once more in the
  same family, which is the two tab case. Presented later, it revokes the session row.
- **A session family**: `active` → `revoked` by sign out or late reuse, or `expired` at its sliding
  `expires_at`. A transaction reads the token's immutable `session_id`, locks `auth_sessions` with
  `SELECT FOR UPDATE`, then locks and rereads the token before deciding. Revoked and expired are
  terminal, and no transaction may insert another token after either state is observed.
- **The browser**: `checking` → `authenticated` after refresh succeeds, `anonymous` after `401`, or
  `unavailable` after a network or `5xx` failure leaves no valid access token. An authenticated
  browser returns to `checking` 60 seconds before expiry, when a protected request first answers
  `401`, or when a hidden tab becomes visible with at most 60 seconds left. All callers share one
  refresh promise. A transient failure retries after one second, then doubles to at most five seconds
  while the current token is valid.
  Sign out clears the in memory token immediately; it becomes `anonymous` on `204` or `unavailable`
  with a retry control when identity could not confirm revocation.

### API surface

All four are new on the gateway and carry `security: []` in `api/openapi.yaml`, because they are what
produces a token. `POST /api/tutors` is deleted, along with its two schemas.

| Endpoint | Method | Key inputs | Key outputs | Auth | Key errors |
|---|---|---|---|---|---|
| `/api/auth/google/start` | GET | `tz`, `lang`, `redirect_to` (query, all optional) | `302` to Google, the attempt row, `vermouth_login` binding cookie | none | `302` to `/signin?error=provider_error` |
| `/api/auth/google/callback` | GET | `code`, `state`, or `error` query, plus `vermouth_login` cookie | `302` to the app, binding cookie cleared, `vermouth_refresh` set | matching state and binding | `302` to `/signin?error=<code>` |
| `/api/auth/refresh` | POST | `vermouth_refresh` cookie, `Origin` header | `access_token`, `access_expires_at`, rotated cookie | same origin plus live session | `401 unauthenticated`, `403 forbidden` |
| `/api/auth/signout` | POST | `vermouth_refresh` cookie, `Origin` header | `204`, cookie cleared | same origin; an unknown cookie is allowed | `403 forbidden`, `500 internal` |

The callback never carries a token. It lands the browser back in the app and the app calls `refresh`,
which is the same call a reload makes, so there is one path to an access token and no token ever
appears in a URL, a redirect log, or browser history.

The refresh cookie is `HttpOnly`, `Secure`, `SameSite=Lax`, host only, `Path=/api/auth`, with
`Max-Age` matching `expires_at`. Local development may use `localhost` or a hostname ending in
`.localhost`; `IDENTITY_COOKIE_SECURE` exists for the plain HTTP case where the browser will not send
a Secure cookie.
Every later `Set-Cookie` repeats those attributes exactly, including a clear. Refresh and sign out
also require the exact configured `Origin`; the method and cookie flag are supporting layers, not the
whole cross site defense.

The browser binding cookie is a separate random value. It is `HttpOnly`, `Secure`, `SameSite=Lax`,
host only, `Path=/api/auth/google`, and expires with the ten minute attempt. The database stores only
its `sha256`. The callback clears it on success or refusal. It is never a session credential and is
never reused.

The gateway forwards these four paths to `identity` and adds nothing: it passes the inbound `Cookie`
header on, and copies `Location` and `Set-Cookie` back out. It sets no cookie of its own and holds no
part of the session rule (spec 0001, the gateway owns no business rule).

### Value sourcing

| Action | Value produced or displayed | Source |
|---|---|---|
| start | `state` | 32 bytes from `crypto/rand`, base64url |
| start | `code_verifier`, `code_challenge` | `crypto/rand`, then `sha256` per PKCE (RFC 7636) |
| start | `nonce` | 32 bytes from `crypto/rand`, base64url, stored on the attempt and sent to Google |
| start | browser binding cookie and hash | an independent 32 bytes from `crypto/rand`; raw base64url in the cookie, `sha256` on the attempt |
| start | `timezone` on the stored attempt | the `tz` query param, validated with `time.LoadLocation`, else `Asia/Ho_Chi_Minh` |
| start | `language` on the stored attempt | the `lang` query param, `vi` or `en`, else `vi` |
| start | `redirect_to` | the query param parsed as a relative URL, decoded and cleaned under AC-15, else `/` |
| start | the Google authorize URL | `IDENTITY_GOOGLE_CLIENT_ID` plus `IDENTITY_GOOGLE_REDIRECT_URL`, scopes `openid email profile` |
| callback | whether the browser started this attempt | constant time comparison of `sha256(vermouth_login)` with `login_attempts.browser_binding_hash` |
| callback | whether Google's ID token is genuine | injectable validator backed by `idtoken.Validate` for signature, keys, one exact audience, and expiry; handler checks issuer, nonce, and typed required claims |
| callback | `provider_subject` | nonempty string `sub` claim, otherwise `provider_error` |
| callback | `email` | nonempty string `email`, trimmed and lowercased, with boolean `email_verified` true |
| callback | `display_name` | the `name` claim, trimmed; when it is missing or empty after trimming, the part of the email before `@` |
| callback | `tutor_id` | UUIDv7 generated in Go |
| callback | `timezone`, `language` on the new tutor | the `login_attempts` row, not the request |
| callback | the `identity.tutor.registered` fields | the inserted row, through the existing conversion, so no column can leak into an event |
| callback | `session_id` | UUIDv7 generated in Go |
| callback | the refresh token value | 32 bytes from `crypto/rand`, base64url, stored only as `sha256` |
| callback | token and session `expires_at` | the same `now` plus `IDENTITY_REFRESH_TTL` (default 30 days) |
| callback | the redirect target | the validated `IDENTITY_APP_URL` origin resolving the cleaned `login_attempts.redirect_to` |
| callback | the error code in `?error=` | one of `not_allowed`, `email_conflict`, `cancelled`, `expired_state`, `provider_error` |
| refresh | `access_token`, `access_expires_at` | `token.Signer.Mint` from the session tutor row, `IDENTITY_TOKEN_TTL` (15m) |
| refresh or sign out | whether the caller is same origin | `Origin` parsed and compared by scheme, host, and port with `IDENTITY_APP_URL` |
| refresh | the rotated cookie | a new random token inserted only while the locked `auth_sessions` row is active |
| refresh | whether a rotated token is a race or a theft | `used_at` against one transaction timestamp and `IDENTITY_REFRESH_GRACE` (default `10s`); inside it rotates, outside it revokes the locked session |
| refresh or sign out refusal | `request_id` | gateway `RequestIDMiddleware`, forwarded to identity under INV-15 |
| the app | session state | `checking` while the single refresh promise runs, `authenticated` on `200`, `anonymous` on `401`, `unavailable` on network or `5xx` with no valid token |
| the app | next refresh time | `access_expires_at` minus 60 seconds, checked again when the tab becomes visible |
| the app | transient refresh retry | one second after the first network or `5xx` failure, then doubled to at most five seconds while the current token is valid |
| the sign in screen | language | first `navigator.languages` entry beginning `vi` or `en`, otherwise `vi` |
| the sign in screen | the error sentence | the fixed bilingual map below, selected only for one of the five validated search values |
| `/api/me` | the tutor | `tutors` by `tutor_id` from the token (INV-8) |

The allowlist needs no value of its own, but it does need one normal form. Both sides are trimmed and
lowercased before they are compared, an empty entry from a double comma is dropped, and an entry with
no `@` in it stops startup rather than sitting in the list matching nothing, because a typo in a
gate should be loud (STK-8).

### Browser session and refusal copy

The protected route redirects an anonymous browser to `/signin?redirect={relative path and query}`.
The sign in route validates `redirect` with the same relative URL rule as AC-15 and passes it to
`start`; an absent or invalid value becomes `/`. After the callback, the target page stays behind the
`checking` state until refresh answers. No protected request starts and no protected screen renders
while that check is pending.

The session coordinator owns the one refresh promise, the access token, its expiry timer, the
visibility check, and the one refresh then retry response to a protected `401`. A second `401` after
a successful refresh clears memory and moves to `anonymous`. A network or `5xx` refresh failure
retries after one second, then doubles to at most five seconds while the current token remains valid;
once it expires, the app shows an
`unavailable` state with a retry control instead of pretending the tutor signed out.

The sign in search schema accepts only `redirect` plus these five error values. Unknown values are
ignored. The first supported `navigator.languages` entry selects the sentence.

| Code | Vietnamese | English |
|---|---|---|
| `not_allowed` | Email này chưa được mời. Hãy nhờ chủ hệ thống thêm địa chỉ của bạn, rồi thử lại. | That email is not invited yet. Ask the operator to add your address, then try again. |
| `email_conflict` | Email này đã thuộc về một tài khoản khác. Hãy đăng nhập bằng tài khoản Google đã dùng lần đầu. | That email already belongs to another account. Sign in with the Google account you used first. |
| `cancelled` | Bạn đã dừng ở bước Google. Không có gì được lưu lại. | You stopped at the Google step. Nothing was saved. |
| `expired_state` | Lần đăng nhập đó đã quá cũ. Hãy bắt đầu lại từ đây. | That sign in took too long. Start again from here. |
| `provider_error` | Google không trả lời được lúc này. Hãy thử lại sau một phút. | Google could not answer just now. Try again in a minute. |

### Key invariants

- No password exists in the system: no column, no hash, no reset path, no `golang.org/x/crypto`.
- `tutor_id` comes from a verified token everywhere except the four auth endpoints, and those take
  the tutor from Google's `sub`, never from an input (INV-8).
- The Google client secret exists only in `identity`'s environment. The gateway and the browser never
  hold it, and no ID token is ever accepted from the browser.
- `provider_subject` is the identity; canonical lowercase `email` is a copy of it. An email match
  never creates or finds a link. The database check and unique constraint enforce the same normal
  form the handler uses.
- One `login_attempts` row is single use and bound to one browser cookie. The callback may exchange
  with Google before the transaction, but only the transaction that deletes the live matching row
  may write a tutor or session.
- A refresh token is stored only as `sha256` of its value. The locked `auth_sessions` row is the one
  authority for family state, and no token may be inserted after it is revoked or expired.
- On a first sign in the tutor row, the link row, the outbox row, the `auth_sessions` row, the first
  refresh token row, and the `login_attempts` delete all go into one `pgx.Tx` in one function (one
  aggregate, INV-3, STK-4). A concurrent unique violation for the same provider subject rolls back
  and retries the transaction once as an existing tutor; the named email constraint becomes
  `email_conflict` instead of a generic failure.
- The callback never mints an access token. The browser calls `refresh` after the redirect. Refresh
  locks and writes the rotation, mints locally, then commits; a signing failure rolls the rotation
  back, while a commit failure sends neither token. The first browser load and every reload therefore
  use the same access token path. Refresh and sign out publish nothing.
- Revoking a session prevents future refreshes but does not call `identity` from a protected request.
  An already minted access token can remain valid for at most `IDENTITY_TOKEN_TTL`, 15 minutes, under
  INV-14.
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
  The production `IDTokenValidator` delegates to `google.golang.org/api/idtoken.Validate` for the
  signature, Google's key set, one exact audience, and expiry with the library's clock semantics.
  The handler then requires a Google issuer, the attempt nonce, nonempty string subject and email,
  and boolean `email_verified` true. A multiple audience token is refused. No ID token is accepted
  from the browser, and the interface exists so malformed claims and signatures are testable.
- **A taken email never moves a tutor**: the callback looks the account up by
  `(provider, provider_subject)` and only that lookup decides who signs in. A known subject whose new
  email already belongs to a different tutor still signs in, with both profile copies left stale, no
  event, and the conflict logged, because locking a real tutor out over a copied field is worse than a
  stale copy. An unknown subject on a taken email is refused with `email_conflict`, since linking is
  out of scope.
- **Who may read what**: a tutor reads only their own rows. There is no admin role and no second role.
- **Cross site protection on the callback** requires the unguessable single use `state` and the
  independent browser binding cookie whose hash is on the attempt. A callback URL copied to another
  browser has no matching cookie and cannot sign that browser into the attacker's account.
- **Cross site protection on refresh and sign out** requires an `Origin` whose parsed scheme, host,
  and port exactly equal the parsed `IDENTITY_APP_URL`. `SameSite=Lax`, a host only cookie, and `POST`
  provide further layers. A missing origin is refused because these are browser only endpoints.
- **Open redirect**: `IDENTITY_APP_URL` must be one origin with no path, query, fragment, or user info.
  Plain HTTP is limited to `localhost` and its `.localhost` namespace, including
  `vermouth.localhost`; every other hostname requires HTTPS.
  A redirect value is parsed, decoded, and cleaned. It is refused when it has a scheme, host, user
  info, fragment, backslash, control byte, or decoded second leading slash. The cleaned relative path
  and query are resolved against the configured origin, then the final origin is compared again.
- **Session termination** is strong for refresh tokens and bounded for access tokens. The session row
  lock makes revocation win every race, while a minted token remains locally verifiable for at most
  15 minutes. That bound is accepted rather than adding a per request revocation call that breaks
  INV-14.
- Every random value uses `crypto/rand`. Failures answer in generic terms through
  `vermouth.WriteError` while the reason goes to the log with the `request_id`.
- Spec 0007 and scope feature 21 are a public deployment gate. The unauthenticated endpoints may run
  on localhost before that rate limit is verified, but feature 16 must not expose them first.
- No compliance scope applies: one tutor, their own data, no card data and no health data.

### Configuration required

- `IDENTITY_GOOGLE_CLIENT_ID`: the OAuth client, required, startup fails without it (STK-8).
- `IDENTITY_GOOGLE_CLIENT_SECRET`: required, held only by `identity`.
- `IDENTITY_GOOGLE_REDIRECT_URL`: the public callback address, for example
  `http://localhost:8080/api/auth/google/callback`. Required, and it must match the Google console
  entry exactly. It uses `https`, except for plain HTTP on `localhost` or a hostname ending in
  `.localhost`, has the exact
  `/api/auth/google/callback` path and no query, fragment, or user info. Its hostname must match
  `IDENTITY_APP_URL`; different ports are allowed only on those local loopback names for the Vite
  proxy development path.
- `IDENTITY_APP_URL`: where the callback sends the browser back to, for example
  `http://localhost:5173` for Vite or `http://vermouth.localhost:8080` for the local Kubernetes
  ingress. Required and parsed at startup as one local loopback HTTP origin or HTTPS production
  origin with no path, query, fragment, or user info. It is also the only allowed `Origin` on refresh
  and sign out.
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
- Concurrent first sign in: two browser bound attempts for one new subject complete together, both
  get sessions, one tutor, one link, and one event exist, verifies **AC-1**, **AC-2**.
- Reload and expiry: a fresh boot gets a token, one open tab renews 60 seconds before expiry, and many
  waiting requests cause one refresh, verifies **AC-3**.
- Second sign in: the same subject signs in again, no new rows and no new event, verifies **AC-1**,
  **AC-2**.
- Refused sign up: a Google account off the allowlist, no rows written, `?error=not_allowed`,
  verifies **AC-5**.
- Callback binding: a valid state with no cookie or the cookie from another attempt is refused, and
  two callbacks for one valid pair allow one transaction to write, verifies **AC-8**.
- Stolen cookie: a rotated refresh token presented again well after the grace window, the family
  revoked, the tutor's next refresh also refused, verifies **AC-7**.
- Session races: concurrent refreshes inside grace both succeed, refresh racing sign out leaves the
  family revoked, and rotation racing late reuse cannot insert after revocation, verifies **AC-6**,
  **AC-7**.
- Sign out then access: refresh is `401`, while an access token copied before sign out verifies only
  until its original 15 minute expiry, verifies **AC-6**.
- Changed Google profile: changed email and display name update the canonical copies and publish one
  profile event; a later device does not overwrite timezone or language, verifies **AC-9**, **AC-10**.
- Email takeover attempt: a new subject whose email matches an existing tutor, refused with
  `email_conflict`, nothing linked, verifies **AC-9**.
- Email collision on a real tutor: a known subject whose new Google email already belongs to another
  tutor, still signed in, both profile copies unchanged, no event, and a logged conflict, verifies
  **AC-9**.
- A bad ID token: fake validator cases cover a forged signature, bad issuer, multiple or wrong
  audience, stale expiry, wrong nonce, empty or wrong type subject and email, false or wrong type
  verified flag, and missing name fallback, verifies **AC-14**.
- Redirect and origin: malicious relative forms fall back to `/`, a foreign or missing refresh and
  sign out origin gets `403`, and the configured final redirect stays on the app origin, verifies
  **AC-15**.
- Session refusal: every dead cookie case returns the same `APIError`, request id, and cookie clear;
  the browser becomes anonymous, verifies **AC-16**.
- Auth and permission: no token on `/api/me` and `/api/thread` is `401`, and a tutor token cannot use
  another tutor identifier on any protected resource, verifies **AC-4**, **AC-11**.
- Browser contract: checking blocks protected rendering, `5xx` becomes retryable unavailable, the
  redirect is preserved, unknown errors are ignored, and all ten refusal sentences match, verifies
  **AC-3**, **AC-16**.
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
2. Before this feature is accepted, rewrite the unshipped `00004_tutor_google_identity.sql` target
   with `tutor_identities`, browser bound `login_attempts`, `auth_sessions`, and `refresh_tokens`.
   Add the canonical email checks. Do not preserve the known unsafe per token family shape in a new
   migration.
3. `task infra:clean`, then `task migrate:up` on an empty identity database, which proves both the
   `0003/verify.md` blocker and the replaced `00004` shape are gone.
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
- Rewriting `00004` requires `task infra:clean`. This is allowed only because the feature is not
  accepted, there is no production database, and every current row is disposable development data.
- Deleting `POST /api/tutors` breaks `test/thread.sh` until `task dev:token` exists. They land
  together for that reason.

## Build plan

Tracer Bullet: the first ten tasks are one thin sign in thread through browser, gateway, identity,
Google, and the database, and back to a signed in screen. Nothing is thickened until that thread runs.

1. Clear the password out of the repository: rewrite `00003` as `00003_tutor_timestamps.sql` with
   `updated_at` only, drop the password comment from `00002`'s header, confirm no Go code and no
   `go.mod` mentions `argon2` or `golang.org/x/crypto`, then `task infra:clean` and `task migrate:up`,
   satisfies **AC-12**.
2. Rewrite the unshipped `00004_tutor_google_identity.sql` target with canonical email checks,
   `tutor_identities`, browser bound `login_attempts`, `auth_sessions`, and `refresh_tokens` linked
   to their family; then clean and migrate, satisfies **AC-1**, **AC-6**, **AC-7**, **AC-8**,
   **AC-9**, **AC-12**.
3. Hand write the queries in `db/queries/`: the provider lookups and writes, bound attempt insert and
   consume, session creation and `SELECT FOR UPDATE`, token lookup and rotation, terminal family
   revocation, and the sweeps. Every refresh and sign out uses the lock order session then token.
   Run `task generate:sql`, satisfies **AC-1**, **AC-6**, **AC-7**, **AC-8**, **AC-9**.
4. The contract first, since both halves generate from it: add the four auth endpoints to
   `api/openapi.yaml` with `security: []`, document the cookie and `Origin` requirements plus the
   `401` and `403` shapes, delete `POST /api/tutors` and its schemas, then run `task generate:api`
   and `task web:generate`, satisfies **AC-1**, **AC-4**, **AC-15**, **AC-16**.
5. `identity`: validate the app and callback URLs at startup, then build
   `GET /api/auth/google/start`. Clean `redirect_to`, write the state, verifier, nonce, and browser
   binding hash, set the short lived binding cookie, and answer `302` with the nonce on the authorize
   URL, satisfies **AC-1**, **AC-8**, **AC-10**, **AC-14**, **AC-15**.
6. `identity`: `GET /api/auth/google/callback`. Require the matching binding cookie, exchange the
   code through an injectable exchanger, validate the raw ID token through an injectable validator,
   enforce every typed claim in AC-14, and apply the allowlist. In one `pgx.Tx`, consume the attempt,
   resolve the concurrent first sign in, write the tutor, link, and first event once, create the
   session and first token, then clear the binding,
   set the refresh cookie, and redirect, satisfies **AC-1**, **AC-2**, **AC-5**, **AC-8**,
   **AC-9**, **AC-14**, **AC-15**.
7. `identity`: `POST /api/auth/refresh`. Require the exact origin, lock the session then the token,
   apply rotation or terminal revocation, mint before commit so signing failure rolls back the
   rotation, and clear the cookie on every `401`, satisfies **AC-3**, **AC-7**, **AC-15**, **AC-16**.
8. `gateway`: forward the four auth paths unauthenticated, passing `Cookie`, `Origin`, and request id
   inbound and `Location` plus every `Set-Cookie` outbound, and delete the `POST /api/tutors` route;
   everything else stays behind the existing verification, satisfies **AC-1**, **AC-3**, **AC-4**,
   **AC-11**, **AC-15**, **AC-16**. Shape: the four
   paths are registered on the outer mux exactly where `POST /api/tutors` is carved out today
   (`gateway/internal/route/routes.go:40`), while `/api/` keeps going to the inner mux behind
   `auth.Middleware`. Go's `ServeMux` picks the more specific pattern, so nothing else needs
   rearranging.
9. `web`: a typed session coordinator and route context with `checking`, `authenticated`,
   `anonymous`, and `unavailable`; one refresh promise, expiry and visibility renewal, one protected
   `401` retry, and no protected render before checking ends. Keep the token only in memory,
   satisfies **AC-3**, **AC-6**, **AC-16**.
10. `web`: a `/signin` route whose validated search holds one known error and one clean relative
    redirect. Send the first supported browser language and timezone to start, render the ten fixed
    sentences, and keep the Google control keyboard reachable with a phone first target size,
    satisfies **AC-1**, **AC-3**, **AC-5**, **AC-10**, **AC-15**, **AC-16**.
    The thread runs end to end from here. The rest thickens it.
11. Thicken the session races: prove two rotations inside grace succeed, and make refresh versus sign
    out plus rotation versus late reuse serialize on the family so no token survives a terminal
    state, satisfies **AC-6**, **AC-7**.
12. `POST /api/auth/signout`: require the exact origin, lock and revoke the family, clear the cookie,
    answer `204`, and make the browser clear memory before it waits. A server failure becomes a
    retryable unavailable state, satisfies **AC-6**, **AC-15**.
13. `task dev:token`: a small program outside every service binary that inserts a tutor and signs a
    token with the development key, and `test/thread.sh` using it instead of the deleted endpoint,
    satisfies **AC-13**.
14. Normalize every provider email before a write. Retry one concurrent provider subject conflict as
    an existing tutor; map the named email constraint to `email_conflict`; synchronize changed email
    or display name once; leave both profile copies and the event untouched on collision, satisfies
    **AC-1**, **AC-2**, **AC-9**, **AC-10**.
15. The sweep: delete expired `login_attempts`, then delete expired or seven day revoked
    `auth_sessions` with token rows cascading on the same ticker shape the relay uses, but on
    `IDENTITY_SWEEP_INTERVAL` (default `10m`), satisfies **AC-7**, **AC-8**.
16. `.env.example` gains the nine variables with the comments that say what each is for, and
    `task dev:keys` stays untouched, satisfies **AC-1**, **AC-12**.

## Consequences

**Positive**:

- No password anywhere: nothing to hash, store, rotate, reset, leak, or explain in a spec. The
  highest severity thing a tutor could hand over does not exist.
- The blocker in `0003/verify.md` closes by deletion rather than by a workaround, and
  `task migrate:up` covers all four services again.
- Google carries the hard parts: password strength, breach checks, two factor, and account recovery.
- The access token design already in the code stays exactly as it is. Ed25519 minting in `identity`,
  local verification everywhere, `sub`, `tz`, `language`, `iat`, `exp`, unchanged claims.
- The event catalogue does not move, so `billing` and `notifications` need no change at all.
- `tutor_identities` makes a second provider later a row and a branch, not a schema change.
- Browser binding prevents login CSRF, and the session row lock makes sign out and reuse revocation
  terminal even when requests race.

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
- Four new tables, four new endpoints, two cookie paths, a browser session coordinator, and a sweep,
  all of it code that exists only to hold a session. A single long lived token in `localStorage`
  would have been perhaps a fifth of it.
- Sign out cannot revoke an access token already copied from memory. Local verification deliberately
  leaves at most 15 minutes of residual access instead of adding an identity lookup to every request.
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
- These are the first auth cookies in the system, so the gateway learns to preserve multiple
  `Set-Cookie` values plus `Cookie`, `Origin`, and request id across the proxy boundary.

## Follow-up

- [ ] Finish and verify spec 0007, scope feature 21, before feature 16 puts the gateway on a public
      address. It rate limits `start`, `callback`, and `refresh`, which remain intentionally separate
      from this identity decision.
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
