# 0004. Rationale: tutor sign in with Google only, no password

The build spec is [index.md](index.md). This file is the reasoning, the options weighed, and the
evidence behind them. `/develop` does not read it.

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
`identity` and `0003/verify.md` carries the failure as an open item that names `/architect` as the way
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
finds the tutor, and mints the existing access token. The browser only ever holds a Vermouth token.

**Pros**:

- No password exists, so the whole class of problems in option 1 disappears, including the reset flow.
- The Google client secret sits in the one service that already holds the signing key, and nothing
  about Google leaks past `identity`.
- The exchange happens over a TLS connection `identity` opened itself, so no ID token is ever accepted
  from the browser, which removes the whole family of forged token attacks at the boundary. The token
  is validated anyway, on the reasoning below.
- Ends at the existing `Mint` call, so spec 0001's identity propagation and INV-14 are untouched.
- PKCE (RFC 7636) plus a single use `state` row makes a replayed or forged callback a row that is not
  there.

**Cons**:

- Google is a hard dependency of sign in, with no fallback path if it is down or if the client is
  misconfigured.
- More moving parts than a password check: an in flight attempt row, a callback, a cookie, and a
  refresh path.
- A fresh clone cannot sign in until someone creates a Google client and matches the redirect URL
  exactly.

### Option 3: a Google Identity Services button in the browser

The browser renders Google's own button, receives a Google ID token, and posts it to `identity`, which
verifies it against Google's public keys and mints its own token.

**Pros**:

- Fewest moving parts on the server: no code exchange, no client secret, no in flight attempt row.
- The fastest path to a working sign in, and the button is Google's own so it always looks right.

**Cons**:

- `identity` must accept a token the browser handed it, so the signature check is the only thing
  standing between a forged value and a session, and Google's key set plus its rotation become
  `identity`'s to keep current. Option 2 checks the same claims, but a forged token never reaches the
  check, because the token arrives on a connection `identity` opened to Google.
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

Between the two Google options, the deciding force is where the ID token comes from. In option 3
`identity` accepts a token from the browser, so its signature is the only thing standing between a
forged value and a signed in session. In option 2 `identity` fetches the token itself over a
connection it opened to Google, which is why OpenID Connect Core section 3.1.3.7 lets the TLS server
check stand in for verifying the signature there. This spec validates it regardless, with
`google.golang.org/api/idtoken`: one library call costs less than the argument does, and the claim
checks that are owed either way (`aud` so another client's token cannot be replayed here, `iss`,
expiry, and the `nonce`) then live in one place beside it. What genuinely differs is that option 2
has no browser held credential to forge at all. Option 3's advantage was fewer moving parts, but it
keeps the entire session half of the work, so what it actually saves is the `login_attempts` row and
the code exchange. That is a small saving against the one path where a forged token is the whole
risk.

Option 4 is the right recommendation for most teams and is wrong for this one, for the reason the
premise note gives: it would sit in the sign in path of a system whose whole point is that its
services verify locally and never ask anyone per request.

**The session model** was the second real choice. A single long lived token in `localStorage` is far
less code and survives a reload trivially, and it was rejected because any injected script can read it
and nothing can revoke it before it expires. An opaque session cookie that the gateway swaps for a
token on every request was rejected because it puts a lookup on the request path and makes every
service depend on `identity` being awake, which is INV-14 inverted. What is left is the standard
answer: a short lived access token in memory, and a refresh token in a cookie no script can read.
Rotation with reuse detection then falls out of the table shape rather than needing separate
bookkeeping, and it is what turns a stolen cookie into a visible event instead of a silent one.

**The grace window on rotation** exists because strict reuse detection fires on the app's own normal
behaviour. Two tabs restored together both boot with the same cookie, the first one rotates it, and
the second then presents a token that has already been used, which the row shape cannot tell apart
from a stolen copy. Time can tell them apart: a second tab arrives in the same instant, a thief
arrives minutes or days later. So a rotated token presented within `IDENTITY_REFRESH_GRACE` rotates
again inside the same family and only a later reuse counts as theft. The alternative, a lock or a
single flight guard in the browser, puts the fix in the half of the system an attacker controls, and
would have the tutor signed out of their own tabs to no benefit.

**A conflicting email never moves a tutor.** `provider_subject` decides who signs in, so the awkward
case is a known subject whose new Google email already belongs to a different tutor: the `UNIQUE` on
`tutors.email` refuses the update. Refusing the sign in would lock a real tutor out over a copied
field, so the sign in proceeds, the copy stays stale, and the conflict is logged. An unknown subject
on a taken email keeps its refusal, now with its own code, because that is the case where trusting
the email would hand one Google account another tutor's data.

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
`identity.tutor.registered` with the same five fields, and sign in, refresh, and sign out publish
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
