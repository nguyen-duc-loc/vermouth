# 0004 verify: tutor sign in with Google only, no password

How to prove spec 0004 landed. Every line names the acceptance criterion it covers, so you can run the
list top to bottom or pick the one criterion you care about. `/check verify` can drive this directly.

Read the last section first. Five criteria (**AC-1**, **AC-2**, **AC-5**, **AC-9**, **AC-14**) need a
real Google OAuth client, which this repository cannot carry, so they sit in their own section and stay
unticked until you create one. Everything above that runs from an empty database with no internet.

## Setup

```sh
task infra:up && task migrate:up && task dev     # infra, migrations, then every service
```

Two settings make the by hand checks over plain `http` workable, both in `.env`:

- `IDENTITY_COOKIE_SECURE=false`, so a browser or a `curl` cookie jar keeps the refresh cookie on
  `http://localhost`. Put it back to `true` when you are done.
- `IDENTITY_SIGNUP_ALLOWLIST=you@example.com`, your own Google address, before the Google section.

Shorthand used below:

```sh
PSQL='docker exec -i vermouth-postgres-identity psql -U vermouth_identity -d vermouth_identity'
G=http://localhost:8080
```

## Commands

- [ ] `task check` format, lint, and type check clean across all seven modules and the web app  → all
- [ ] `task test` every module green                                                            → all
- [ ] `task build` every binary builds, `CGO_ENABLED=0`                                         → all
- [ ] `task infra:clean && task infra:up && task migrate:up` all four services apply from empty  → AC-12
- [ ] `task migrate:down -- identity` then `task migrate:up` reverses and reapplies              → AC-12
- [ ] `cd test && go test ./model/` the ownership, tenancy, schema and key guards, now carrying
      `login_attempts` as the one table with no `tutor_id`, seven named statement exemptions, two
      named sweeps, and `identity`'s four tables                                                 → AC-11, AC-12
- [ ] `task dev:token && task thread` the thread completes with no browser and no Google         → AC-13
- [ ] `grep -rn 'argon2' --include='*.go' .` no hit anywhere                                     → AC-12
- [ ] `grep -rn 'golang.org/x/crypto' --include=go.mod .` indirect in all six modules, direct in
      none                                                                                      → AC-12
- [ ] `$PSQL -c "select count(*) from information_schema.columns where column_name like '%password%'"`
      answers `0`                                                                               → AC-12
- [ ] `grep -rn '\.Mint(' --include='*.go' .` only the refresh handler and `cmd/devtoken`, and no
      service binary imports `devtoken`                                                         → AC-13

## By hand, with no Google

### The refusal at the boundary (AC-4, AC-11)

- [ ] `curl -si $G/api/me | head -1` and `curl -si $G/api/thread | head -1` both `401`
- [ ] the body is the `APIError` shape with `code`, `message` and a non empty `request_id`, never a
      bare string or an `http.Error` line

### The start call (AC-10)

- [ ] `curl -si "$G/api/auth/google/start?tz=Europe/Paris&lang=en&redirect_to=/somewhere"` answers
      `302` to `accounts.google.com` carrying `state`, `nonce`, `code_challenge`,
      `code_challenge_method=S256` and the `openid email profile` scopes
- [ ] the stored attempt matches what was sent, and lives ten minutes:

      ```sh
      $PSQL -c "select timezone, language, redirect_to, expires_at - created_at as life
                from login_attempts order by created_at desc limit 1"
      ```

- [ ] `curl -si "$G/api/auth/google/start"` with no query at all stores `Asia/Ho_Chi_Minh`, `vi` and
      `/`, the fallbacks, not an empty string
- [ ] `redirect_to=//evil.example` and `redirect_to=https://evil.example/x` both store `/`, so an off
      origin target can never come back out of the callback
- [ ] every `state` and every `nonce` differs from the last call's

### The refused callback (AC-8)

- [ ] `curl -si "$G/api/auth/google/callback?state=nope&code=x"` lands on
      `/signin?error=expired_state`, and writes no tutor and no session
- [ ] `curl -si "$G/api/auth/google/callback?error=access_denied&state=nope"` lands on
      `/signin?error=cancelled`, checked before the state is even read
- [ ] a `state` that a callback already consumed behaves the same as an unknown one, because the
      attempt is deleted inside the transaction

### The session, from a seeded row (AC-3, AC-6, AC-7, AC-11)

Google is what mints the first refresh token, so seed one instead. `task dev:token` writes the tutor,
and `token_hash` is `sha256` of the raw cookie value, so a known value is one line of SQL:

```sh
TUTOR=$(task dev:token | sed -n 's/.*"tutor_id": "\([^"]*\)".*/\1/p')
RAW=devrefresh
HASH=$(printf '%s' "$RAW" | sha256sum | cut -d' ' -f1)
$PSQL -c "insert into refresh_tokens (token_hash, session_id, tutor_id, expires_at)
          values ('\x$HASH', gen_random_uuid(), '$TUTOR', now() + interval '30 days')"
```

- [ ] `curl -si -X POST $G/api/auth/refresh -H "Cookie: vermouth_refresh=$RAW"` answers `200` with
      `access_token` and `access_expires_at`, and a rotated cookie whose value is not `$RAW`  → AC-3
- [ ] that access token reads its own tutor and only its own: `curl -s $G/api/me -H "Authorization:
      Bearer <token>"` answers about `$TUTOR`                                                 → AC-11
- [ ] presenting `$RAW` again straight away answers `200` and rotates again inside the same
      `session_id`, which is two tabs restored together                                       → AC-7
- [ ] presenting `$RAW` again after `IDENTITY_REFRESH_GRACE` has passed answers `401`, and every row
      in that `session_id` now carries `revoked_at`, including the newest one, so the token the
      tutor holds is refused too                                                              → AC-7
- [ ] on a fresh seeded row: `curl -si -X POST $G/api/auth/signout -H "Cookie: vermouth_refresh=$RAW"`
      answers `204` with the cookie cleared, and the next `refresh` with the same cookie `401` → AC-6
- [ ] signing out twice answers `204` both times, because it is idempotent                    → AC-6

### The sweep

- [ ] set `IDENTITY_SWEEP_INTERVAL=2s`, insert an attempt with `expires_at` in the past, and the log
      reports `"msg":"Swept"` with a non zero `login_attempts` count. Put `10m` back afterwards.
- [ ] a `revoked_at` row is kept seven days before the sweep takes it, so a reuse stays detectable
      for a week rather than being deleted the moment it is refused

### Configuration (STK-8)

- [ ] an entry with no `@` in `IDENTITY_SIGNUP_ALLOWLIST` stops startup naming that entry
- [ ] a missing `IDENTITY_GOOGLE_CLIENT_ID` stops startup with `vermouth.MissingEnvError`
- [ ] a bad duration in `IDENTITY_REFRESH_TTL` stops startup naming the variable
- [ ] an empty `IDENTITY_SIGNUP_ALLOWLIST` starts, and means nobody new can sign up

## With a real Google client

Create an OAuth client at the Google console with `http://localhost:8080/api/auth/google/callback` as
an authorized redirect, then set `IDENTITY_GOOGLE_CLIENT_ID`, `IDENTITY_GOOGLE_CLIENT_SECRET` and your
own address in `IDENTITY_SIGNUP_ALLOWLIST`. None of these has been run.

- [ ] Happy path: sign in from `/signin`, land back in the app signed in, exactly one `tutors` row and
      one `tutor_identities` row appear                                                → AC-1
- [ ] Exactly one `identity.tutor.registered` with spec 0001's five fields reaches `notifications`,
      written to the outbox in the same transaction as the tutor row                    → AC-2
- [ ] Second sign in with the same Google account writes no new row and publishes nothing → AC-1, AC-2
- [ ] Reload the tab, the app stays signed in with no Google round trip                 → AC-3
- [ ] Timezone and language from the browser appear in the token claims and in the event → AC-10
- [ ] An account off the allowlist with no tutor yet lands on `/signin?error=not_allowed` with a
      sentence naming why, and writes nothing                                          → AC-5
- [ ] A known subject whose Google email changed updates `tutors.email` and
      `tutor_identities.provider_email` and publishes `identity.tutor.profile.changed`  → AC-9
- [ ] A known subject whose new Google email already belongs to another tutor still signs in, the
      email copy is left alone, and the conflict is logged with its `request_id`        → AC-9
- [ ] An unknown subject whose email already belongs to a tutor is refused with `email_conflict` and
      links nothing                                                                    → AC-9
- [ ] A bad ID token writes nothing and lands on `/signin?error=provider_error`, once each for `aud`
      for another client, a stale expiry, a `nonce` that does not match the attempt, and
      `email_verified` false                                                           → AC-14

## Value sourcing, by hand

One check per Value sourcing row the automated list does not already cover. These are the answers a
test cannot fully pin down, because they are about which value a tutor ends up carrying.

- [ ] `tz=Not/AZone` falls back to `Asia/Ho_Chi_Minh` rather than storing the bad name  → AC-10
- [ ] `lang=fr` falls back to `vi`, since only `vi` and `en` exist                       → AC-10
- [ ] A Google account with no `name` claim gets the part of its email before `@` as its display name
- [ ] The new tutor's timezone and language come from the `login_attempts` row, not from the callback
      request, which carries none                                                       → AC-10
- [ ] The sign in screen shows the right sentence for each of the five `?error=` codes, in both `vi`
      and `en`

## Known open item

**The spec's AC-13 names three `Mint` callers, the code has two.** The API surface section says the
callback carries no token and the app calls `refresh`, while Key invariants says minting happens after
that commit. Both cannot hold, so the build followed the endpoint contract: only the refresh handler
and `cmd/devtoken` mint. The invariant AC-13 is protecting gets stronger, not weaker, but the list to
check is one shorter than the spec's. Worth `/architect` correcting in the spec.
