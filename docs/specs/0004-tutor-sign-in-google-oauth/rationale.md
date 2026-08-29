# 0004. Rationale: tutor sign in with Google only, no password

The build spec is [index.md](index.md). This file is the reasoning, the options weighed, and the
evidence behind them. `$develop` does not read it.

## Context

> ⚠️ Premise note: hand writing refresh token rotation, reuse detection, cross site protection, and
> cookie handling is the classic reinventing auth pattern, and it is where breaches live. The
> honest alternative is a hosted identity provider that owns all of it. It is being rejected here for
> a specific reason, not by oversight: spec 0001 already fixed local Ed25519 verification with no per
> request call to any identity service (INV-14), `identity` already mints and already holds the
> private key, and the stated purpose of this repository is learning the distributed design. A hosted
> provider would put a second identity system inside the sign in path and leave the minting code in
> place anyway. The tradeoff being accepted consciously is that the session code below is Vermouth's
> to get right, which is why rotation with reuse detection is specified rather than left to the build,
> and why the refresh secret never reaches a script.

Vermouth has no sign in. Three accepted specs say a tutor will have a password: spec 0001 puts a
password hash in `identity`'s ownership row, spec 0002 picks `argon2id` for storing it, and spec 0003
adds `tutors.password_hash text NOT NULL` in migration `00003_tutor_credentials.sql`. None of it was
ever built. There is no password code in any Go file, no `golang.org/x/crypto` in any `go.mod`, and
the one endpoint that creates a tutor, `POST /api/tutors`, takes an email, a display name, a timezone,
and a language, then hands back an access token to anyone who asks.

That gap is already blocking work. `00003` cannot apply: the development database holds 26 tutor rows
from `task thread` runs and the column is `NOT NULL` with no default, so `task migrate:up` fails on
`identity` and `0003/verify.md` carries the failure as an open item that names `$architect` as the way
out. Meanwhile feature 7 in the scope is tagged as needing a decision, and its "Done when" line still
says passwords are stored hashed.

Three forces shape the answer. The tutor is one person, a friend or two at most during testing, and
the target date for friends testing the real money loop is 7 November 2026, so anything that costs a
week of work has to earn it. Feature 16 puts this on a public address, so whatever gates account
creation has to hold up outside localhost. And the token design is already fixed and already built:
`identity` alone holds the Ed25519 private key, every other service verifies locally against a public
key it already has, and the claims are exactly `sub`, `tz`, `language`, `iat`, `exp`. Whatever
authenticates a tutor has to end at that existing minting call, not replace it.

There is also a plain product force. The tutor already has a Google account, and the least useful
thing this system could ask them to invent is one more password.

## Options considered

### Option 1: build the password path the existing specs describe

Implement what specs 0001 to 0003 already promise: `argon2id` hashing through
`golang.org/x/crypto/argon2`, a password on registration, a sign in endpoint that verifies the hash,
and later a reset flow.

**Pros**:

- No change to three accepted specs, and no documentation to correct.
- No external dependency in the sign in path: it works offline and in a test with no network.
- Nothing new to learn; the migration is already written.

**Cons**:

- Vermouth becomes responsible for the worst secret in the system, and for everything that hangs off
  it: hash parameters, timing safe verification, throttling, and a reset flow that needs email
  delivery, which `notifications` cannot send yet.
- A password reset by email is real work that buys nothing a tutor wants.
- It still does not answer who is allowed to create an account on a public address.
- The `NOT NULL` blocker stays: the column still lands on a table with rows and a registration that
  takes no password.

### Option 2: Google OAuth 2.0 Authorization Code with PKCE, run server side by `identity` (chosen)

`identity` is a confidential OAuth client. The browser navigates to `identity`'s start endpoint, which
stores `state` and a PKCE verifier and redirects to Google. Google returns to `identity`'s callback,
which exchanges the code with the client secret, reads `sub` and `email` from the ID token, creates or
finds the tutor, and creates a refresh session. The browser then calls `refresh`, which mints the
existing access token. The browser only ever holds a Vermouth access token in memory and the refresh
token in a cookie no script can read.

**Pros**:

- No password exists, so the whole class of problems in option 1 disappears, including the reset flow.
- The Google client secret sits in the one service that already holds the signing key, and nothing
  about Google leaks past `identity`.
- The exchange, provider credential, and claim validation stay inside `identity`; the browser only
  follows redirects and never integrates a provider SDK or handles a Google token.
- Keeps the existing `Mint` call behind one refresh path, so spec 0001's identity propagation and
  INV-14 are untouched.
- PKCE (RFC 7636), a single use `state` row, and the independent browser binding make a replayed,
  forged, or cross browser callback a row and cookie pair that is not there.

**Cons**:

- Google is a hard dependency of sign in, with no fallback path if it is down or if the client is
  misconfigured.
- More moving parts than a password check: an in flight attempt row, a callback, two cookies, a
  locked session family, and a refresh path.
- A fresh clone cannot sign in until someone creates a Google client and matches the redirect URL
  exactly.

### Option 3: a Google Identity Services button in the browser

The browser renders Google's own button, receives a Google ID token, and posts it to `identity`, which
verifies it against Google's public keys and mints its own token.

**Pros**:

- Fewest moving parts on the server: no code exchange, no client secret, no in flight attempt row.
- The fastest path to a working sign in, and the button is Google's own so it always looks right.

**Cons**:

- `identity` accepts an untrusted credential from the browser and must validate its signature,
  audience, issuer, expiry, nonce, subject, and verified email before any write. That is a normal and
  sound auth boundary when implemented correctly, but it gives the browser and Google's JavaScript
  library a larger role than option 2.
- Still needs its own refresh path, so the session code below is not saved.
- The flow is tied to Google's browser library, which makes a second provider later a different flow
  rather than a second configuration.

### Option 4: a hosted identity provider in front (Auth0, Clerk, Keycloak, Zitadel, Dex)

Put a dedicated identity product in front of Vermouth. It talks to Google, owns sessions, refresh, and
revocation, and hands `identity` a verified user.

**Pros**:

- Someone else owns rotation, reuse detection, cookie handling, and the security patches, which is the
  premise note's concern answered properly.
- Comes with a second provider, account linking, and multi factor for free when they are ever wanted.

**Cons**:

- A second identity system beside the one this repository already implements and already mints from,
  which means either two token shapes or a rewrite of INV-14.
- A live third party in the sign in path (or, for the self hosted ones, a whole extra service with its
  own database to operate on a single Azure VM against a measured memory budget).
- It works against the stated purpose of the project: the machinery being learned is exactly what it
  hides.

## Rationale

Option 1 was rejected on cost against value. It is the only option that requires nothing to be
rewritten, and it is still the wrong answer, because the password's real cost is not the hash: it is
the reset flow, the throttling, and the fact that the highest severity secret in the system would
exist at all, all to give one tutor a credential they did not ask for. The `NOT NULL` blocker settles
by deleting the column, which is the cleanest of the four exits `0003/verify.md` lists.

Between the two Google options, option 3 is materially simpler and is not inherently less secure. A
properly validated signed credential is a normal authentication boundary. It removes the client
secret, PKCE, callback exchange, and `login_attempts`, while keeping the same refresh session work.
Option 2 remains chosen because the implemented boundary keeps every provider detail and credential
inside `identity`, leaves the browser as a plain redirect client, and gives a later provider the same
server owned shape. That isolation is worth the extra login machinery here, but it is a tradeoff, not
a unique security requirement. If this decision were unbuilt and shortest delivery time were the
only force, option 3 would be the runner up and a credible choice.

Option 4 is the right recommendation for most teams and is wrong for this one, for the reason the
premise note gives: it would sit in the sign in path of a system whose whole point is that its
services verify locally and never ask anyone per request.

**The session model** was the second real choice. A single long lived token in `localStorage` is far
less code and survives a reload trivially, and it was rejected because any injected script can read it
and nothing can revoke it before it expires. An opaque session cookie that the gateway swaps for a
token on every request was rejected because it puts a lookup on the request path and makes every
service depend on `identity` being awake, which is INV-14 inverted. What is left is the standard
answer: a short lived access token in memory, and a refresh token in a cookie no script can read.
Rotation with reuse detection turns a stolen cookie into a visible event instead of a silent one.
The `auth_sessions` row is the authority for the family because per token rows alone cannot make sign
out win a race against rotation. Locking the family before every token decision makes revoked a real
terminal state rather than a best effort update.

**The browser binding cookie** exists because `state` stored only on the server proves that a callback
belongs to a start, but not that both happened in the same browser. Without the second value, an
attacker can start with their Google account and hand the callback to a victim, signing the victim
into the attacker's tutor. The independent cookie closes that login CSRF path without making a
script readable token.

**Access token revocation remains bounded rather than immediate.** Sign out and reuse end refresh at
once, while a token already copied from memory can verify for the rest of its 15 minute life. Checking
revocation on every request would restore immediate termination and break INV-14 by putting identity
back on the request path. The short bound is the deliberate choice, and the browser clears its own
copy as soon as sign out begins.

**The browser renews before expiry** because a boot only refresh leaves an open attendance screen dead
after 15 minutes. One shared promise avoids turning many waiting requests into refresh token reuse,
and the visibility check covers timers throttled while a phone tab is hidden.

**The grace window on rotation** exists because strict reuse detection fires on the app's own normal
behaviour. Two tabs restored together both boot with the same cookie, the first one rotates it, and
the second then presents a token that has already been used, which the row shape cannot tell apart
from a stolen copy. Time can tell them apart: a second tab arrives in the same instant, a thief
arrives minutes or days later. So a rotated token presented within `IDENTITY_REFRESH_GRACE` rotates
again inside the same family and only a later reuse counts as theft. The alternative, a lock or a
single flight guard in the browser, is not sufficient by itself. The family lock orders writes but
cannot tell a second tab from a stolen copy, while one browser promise cannot coordinate another tab.
The grace window settles that remaining ambiguity.

**A conflicting email never moves a tutor.** `provider_subject` decides who signs in, so the awkward
case is a known subject whose new Google email already belongs to a different tutor: the `UNIQUE` on
`tutors.email` refuses the update. Refusing the sign in would lock a real tutor out over a copied
field, so the sign in proceeds, both profile copies stay stale, no event is published, and the
conflict is logged. An unknown subject on a taken email keeps its refusal, now with its own code,
because that is the case where trusting the email would hand one Google account another tutor's data.
Email is trimmed and lowercased before every write, with the same form enforced in Postgres. A
concurrent first sign in relies on the provider subject constraint, rolls back the losing insert, and
retries once as the linked tutor, which preserves one tutor and one registration event without an
application lock.

**The allowlist** exists because feature 16 makes this public and any Google account would otherwise
create a tenant. An environment variable was chosen over a table because configuration only is already
the rule (STK-8), it needs no screen, and it fails closed when empty. It gates creation only, never an
existing tutor's sign in, so removing an address cannot lock out someone who already exists.

**Timezone and language** come from the browser because Google supplies neither, both are `NOT NULL`
on `tutors`, and both are already in the event that `billing` and `notifications` consume. Asking for
them on an onboarding screen would be more explicit and would add a screen and a state to the flow;
fixed defaults would be silently wrong for anyone outside Vietnam. Reading them from the browser and
validating them with the check `cleanRegisterInput` already performs gets a correct timezone with no
form at all.

**The event catalogue is untouched on purpose.** A first sign in publishes the same
`identity.tutor.registered` with the same five fields, a changed provider profile publishes the
existing `identity.tutor.profile.changed`, and an unchanged sign in, refresh, and sign out publish
nothing. An `identity.tutor.signed_in` event was considered for an audit trail and rejected: it would
add a row to spec 0001's catalogue that two services would ignore.

## Evidence: what the password decision actually touches

Gathered from the repository on 2026-08-22, and the reason this change is documentation plus one
migration rather than a code removal.

**In prose, four lines across three accepted specs:**

| Where | What it says today |
|---|---|
| `docs/specs/0001-service-boundaries-and-communication/index.md:42` | `identity` owns "`tutor_id`, email, password hash, display name, timezone, language" |
| `docs/specs/0002-stack-and-scaffold/index.md:66` | Password storage is `argon2id` through `golang.org/x/crypto/argon2` |
| `docs/specs/0002-stack-and-scaffold/rationale.md:118` | "Argon2id as the current default for password hashing" |
| `docs/specs/0003-data-model-and-ownership/index.md:138` | `tutors` gains `password_hash text NOT NULL` |

Spec 0003 mentions it twice more, at `index.md:400` (the hash never leaves `identity`) and
`index.md:462` (the migration list), and `0003/verify.md:36` to `:52` carries the open blocker. The
scope's feature 7 line still ends "passwords are stored hashed".

**In code, one unapplied migration and nothing else.** `services/identity/db/migrations/00003_tutor_credentials.sql`
adds `password_hash` and `updated_at`. `updated_at` is what `identity.tutor.profile.changed` stamps,
so the file is rewritten rather than deleted. A search for `argon2` and `password` across every `.go`
file returns four comment lines and no implementation, and no module requires `golang.org/x/crypto`.

**What the current sign in path really is.** `POST /api/tutors` at `gateway/internal/route/routes.go:40`
forwards to `services/identity/internal/http/routes.go:36`, which calls `handler.Register` and returns
a tutor plus an access token to any caller. `test/thread.sh:14` is that endpoint's only real user, and
it greps `access_token` out of the answer, which is why deleting the endpoint and adding
`task dev:token` have to land together.

**What already works and is deliberately left alone.** `identity` alone holds the private key and
mints (`services/identity/internal/token/signer.go`); the gateway verifies every `/api/` request and
passes the token onward unchanged (`gateway/internal/auth/auth.go:22`); the access lifetime is already
configuration (`IDENTITY_TOKEN_TTL`, default `15m`); the browser already keeps the token in memory only
(`web/src/api/client.ts:16`); and the Vite dev proxy puts the app and the gateway on one origin
(`web/vite.config.ts:13`), so a `SameSite=Lax` cookie works in development with no CORS anywhere.
