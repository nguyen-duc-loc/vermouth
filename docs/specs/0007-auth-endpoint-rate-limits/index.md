# 0007. Auth endpoint rate limits

**Date**: 2026-08-25
**Updated**: 2026-09-04
**Status**: In Progress

## Summary

The gateway will limit the three public auth paths before they reach `identity`. It will keep small,
bounded counters in memory, apply both caller and whole service budgets, and return a clear retry
time without changing any auth state. This protects the public friend testing deployment without a
new database table or a new infrastructure service.

## Requirements

**User stories**:

1. As a tutor, I want normal sign in and session refresh traffic to keep working even when another
   caller abuses the public auth paths.
2. As the operator, I want abusive auth traffic refused before it can grow `login_attempts`, spend
   Google work, or load the identity database.
3. As the operator, I want limits to be explicit in deployment configuration and visible without
   placing caller identity or session material in logs.

**Acceptance criteria**:

1. **AC-1**: Requests below every applicable budget reach `identity` unchanged. Only
   `GET /api/auth/google/start`, `GET /api/auth/google/callback`, and
   `POST /api/auth/refresh` are limited. `POST /api/auth/signout`, health paths, and authenticated
   API paths never enter this limiter. Go treats `HEAD` as matching a `GET` pattern, so explicit
   `HEAD` handlers for the three auth paths answer `405` through `vermouth.WriteError` before
   limiter lookup and create no caller key.
2. **AC-2**: Each policy is a pair of continuous refill token buckets. The minute bucket starts full,
   has the first count as its capacity, and refills that count per minute. The hour bucket starts
   full, has the second count as its capacity, and refills that count per hour. The required policy
   values are:

   | Endpoint and scope | Minute | Hour |
   |---|---:|---:|
   | `start`, client IP | 5 | 20 |
   | `start`, global | 100 | 500 |
   | `callback`, client IP | 10 | 60 |
   | `callback`, global | 200 | 1,000 |
   | `refresh`, client IP | 30 | 120 |
   | `refresh`, refresh token | 10 | 120 |
   | `refresh`, global | 300 | 3,000 |

3. **AC-3**: One request checks its global bucket pair and every caller bucket pair atomically at one
   instant. While the registry mutex is held, it first confirms that every bucket has at least one
   token, then calls `AllowN` at that same instant on all of them only when every check passed. A
   refusal consumes no token from any bucket. Concurrent requests cannot overspend a bucket, corrupt
   the registry, or race under `go test -race`.
4. **AC-4**: A limited `start` or `callback` answers `302` to
   `/signin?error=rate_limited`, with `Retry-After` set to the whole seconds until every applicable
   budget can admit one request. A limited `refresh` answers `429` through
   `vermouth.WriteError`, with code `rate_limited`, message
   `too many authentication requests; try again later`, its `request_id`, and the same
   `Retry-After` rule. For each empty bucket, the delay is `(1 - tokens) / refill_rate`. The response
   takes the greatest delay, rounds up to whole delta seconds, and clamps the result to at least `1`.
   All refusal responses set `Cache-Control: no-store`.
5. **AC-5**: A refusal happens before forwarding. It creates and consumes no `login_attempts` row,
   performs no Google exchange, and rotates or revokes no refresh token. A flood with rotating
   caller keys is still bounded by the global budgets and the existing ten minute attempt expiry.
6. **AC-6**: The gateway uses `RemoteAddr` unless the immediate peer belongs to one of
   `GATEWAY_TRUSTED_PROXY_CIDRS`. It reads every `X-Forwarded-For` header line, splits comma values,
   trims optional whitespace, and accepts at most 32 nonempty valid addresses. For a trusted peer it
   walks those addresses from right to left and selects the first untrusted address. When all are
   trusted it selects the leftmost address. Any empty or malformed element, or more than 32 values,
   makes the whole header unusable, so the immediate peer becomes the key. `RemoteAddr` is parsed as
   `addr:port`, then as a bare address. An unparseable value uses one fixed `invalid_remote` caller
   key. IPv4 mapped IPv6 is normalized to IPv4. IPv4 uses the exact address. IPv6 uses its `/64`
   prefix.
7. **AC-7**: `refresh` uses the global and client IP policies on every request. When exactly one
   usable `vermouth_refresh` cookie is present, it also uses a domain separated `sha256` digest of
   that value as the refresh token key. Zero named cookies skips only the token policy, then reaches
   identity for its normal `401` when the other budgets allow it. An empty named cookie or more than
   one named cookie is malformed. After the global and client IP checks allow it, gateway refuses a
   malformed cookie with the normal `401 unauthenticated` error before forwarding.
8. **AC-8**: The caller registry holds at most 10,000 entries. Each entry is keyed by endpoint, key
   type, and key digest, and contains the minute bucket, hour bucket, and `last_seen`. Entries idle
   for two hours are removed opportunistically. A full registry evicts the least recently used
   caller entry while fixed global entries remain. Gateway restart clears the registry and begins
   with full buckets.
9. **AC-9**: Missing, malformed, duplicate, zero, negative, or otherwise invalid rate and
   trusted proxy configuration stops gateway startup with `vermouth.MissingEnvError` naming the
   variable. The literal `none` means direct traffic with no trusted proxy. No policy has a silent
   default.
10. **AC-10**: The first denial for each endpoint and limiting scope in a one minute interval is a
    structured warning. Later denials in that interval are counted, and the next warning reports the
    suppressed count. When several scopes fail, each failed endpoint and scope pair is sampled
    independently. Scope is exactly `ip`, `refresh_token`, or `global`; minute and hour are not
    separate scopes. Log fields are exactly `endpoint`, `scope`, `count`, `suppressed`, and
    `request_id`. No log or metric contains an IP address, cookie, token digest, or quota key.
11. **AC-11**: `api/openapi.yaml` records the rate limited callback redirects, the `429` refresh
    response, `Retry-After`, and the shared `Error` body. Generated Go and browser types are current.
    The sign in route accepts `rate_limited` as a validated search value and shows a fixed Vietnamese
    or English sentence in its existing alert. English reads
    `Too many sign in requests. Wait a moment, then try again.` Vietnamese reads
    `Có quá nhiều yêu cầu đăng nhập. Hãy chờ một lát rồi thử lại.`
12. **AC-12**: A limited boot time `refresh` leaves the cookie and any current access token
    untouched, shows the same rate limited alert, and offers a keyboard reachable manual retry only
    after `Retry-After`. It starts no automatic retry loop. The retry control has a visible focus
    state, a minimum 44 by 44 CSS pixel target, and its changing wait state is announced without
    moving focus. `session.ts` stores `rateLimitedUntil` in memory. The English labels are
    `Try again` and `Try again in {n}s`. The Vietnamese labels are `Thử lại` and
    `Thử lại sau {n} giây`. The control is disabled before that instant. A polite live region reports
    whole seconds remaining. Reaching zero enables the control without moving focus.
13. **AC-13**: `task test:auth-rate-limit` runs the exact command sequence in **Launch evidence target** and proves the exact policy parser, caller identity rules,
    minute and hour refill, atomic multi bucket refusal, bounded registry, sampled logs, all three
    endpoint contracts, unchanged auth state on refusal, and the global bound on pending login
    attempts. The target runs without a real Google exchange and is suitable for the schema version
    1 evidence required by spec 0006. It also uses the existing Vitest suite against the pure
    browser refresh outcome and retry state, so no new web test library is required.
14. **AC-14**: Before its first check, `task test:auth-rate-limit` removes
    `.tmp/production/rate-limit-evidence.json`. After all tests pass, it invokes
    `gateway/cmd/ratelimitevidence write <repository-root> <evidence-path>`, which reuses the gateway policy parser and atomically writes the
    file with exactly `schema_version`, `git_sha`, `test_target`,
    `threshold_configuration_sha256`, `pass`, and `timestamp`. Values are `1`, the exact 40
    character lowercase Git SHA from `git rev-parse --verify HEAD^{commit}`,
    `task test:auth-rate-limit`, the canonical threshold hash, boolean `true`, and a UTC RFC 3339
    instant with whole seconds and `Z`. The hash input is the seven rate environment names sorted
    bytewise, each followed by `=`, its parser normalized minute first and hour second value with no
    spaces, and `\n`. Trusted proxy CIDRs are not part of this threshold hash. The file uses
    canonical UTF8 JSON with lexical object keys, no insignificant whitespace, and one trailing line
    feed. It rejects any tracked change or untracked file in the clean input set named under
    **Launch evidence target**. The writer uses a unique temporary regular file in the same
    directory, mode `0644`, file and directory sync where supported, and `os.Rename`. It rejects a
    symlink or nonregular destination and removes its temporary file on failure. The timestamp is
    metadata only and never creates a freshness window. A failed test or writer leaves no passing
    evidence.

## Decision

**Chosen option**: Option 1: Gateway memory with dual token buckets

The gateway enforces caller and global budgets with paired `golang.org/x/time/rate` token buckets,
a bounded synchronized registry, and trusted proxy aware client address parsing. Identity, Postgres,
Google, and the edge proxy products gain no rate state.

The launch evidence has one schema owner, one producer, one verifier, and one working path. Spec
0007 owns `deploy/production/rate-limit-evidence.schema.json` and
`gateway/cmd/ratelimitevidence`. `task test:auth-rate-limit` invokes the command's `write` mode only
after every named check passes. Spec 0006 invokes its `verify` mode against candidate production
values, then consumes the exact `.tmp/production/rate-limit-evidence.json` bytes without a copy or
field translation.

**Implementation skills**: `golang-security` (`samber/cc-skills-golang`, `.agents/skills/golang-security/`) · `golang-concurrency` (`samber/cc-skills-golang`, `.agents/skills/golang-concurrency/`) · `golang-observability` (`samber/cc-skills-golang`, `.agents/skills/golang-observability/`) · `golang-testing` (`samber/cc-skills-golang`, `.agents/skills/golang-testing/`) · `golang-error-handling` (`samber/cc-skills-golang`, `.agents/skills/golang-error-handling/`) · `openapi` (`oakoss/agent-skills`, `.agents/skills/openapi/`) · `tailwindcss-accessibility` (`josiahsiegel/claude-plugin-marketplace`, `.agents/skills/tailwindcss-accessibility/`) · `tanstack-router-best-practices` (`deckardger/tanstack-agent-skills`, `.agents/skills/tanstack-router-best-practices/`)

## Rationale

Reasoning and options: see [rationale.md](rationale.md).

## Feature design

### Data model sketch

There is no migration and no durable rate state.

| Entity | Key | Required fields | Relationship and lifecycle |
|---|---|---|---|
| Caller registry | One gateway process | `max_entries = 10000`, `idle_ttl = 2h`, least recently used order, `last_now` | Owns zero to many caller entries. Clears on restart. |
| Caller entry | `endpoint + key_type + key_digest` | minute limiter, hour limiter, `last_seen` | Belongs to the registry. Expires after two idle hours or leaves first at capacity. |
| Global entry | One fixed key per endpoint | minute limiter, hour limiter | One each for `start`, `callback`, and `refresh`. Never evicted. |
| Existing login attempt | Existing random `state` primary key | Existing fields from spec 0004 | Created only after every `start` budget admits the request. Existing expiry and sweep remain unchanged. |
| Rate limit launch evidence | One file at `.tmp/production/rate-limit-evidence.json` | schema version, Git SHA, test target, threshold SHA256, boolean pass state, timestamp | Removed before testing, written atomically after success, then consumed unchanged by spec 0006. |

The registry uses one `sync.Mutex` because one request must inspect and spend from several limiters
as one decision. The critical section performs only address lookup, token inspection, `AllowN`, and
least recently used bookkeeping. It performs no I/O and starts no goroutine. Idle cleanup runs at
most once per minute on the next request.

### State transitions

1. A caller entry moves from absent to active on its first check only when the global pair can admit
   the request, then to removed after two idle hours or least recently used eviction. A denied global
   pair creates no caller entry and does not refresh its least recently used position.
2. Each token bucket moves from full to partly used to empty, then refills continuously from the
   monotonic part of gateway time. Gateway restart returns every bucket to full.
3. One request reads `TokensAt(now)` from every applicable bucket under the registry mutex. When all
   values are at least one, `AllowN(now, 1)` commits them together. Otherwise no `AllowN` call runs.
4. A denial log scope moves from ready to sampled for one minute. Denials during that minute increase
   a suppressed count. The next denial after the interval reports that count and begins a new one.
5. Launch evidence moves from absent to present only after every check passes. The target removes an
   old file before testing. Any failure keeps it absent.

### API surface

| Endpoint | Method | Key inputs | Key outputs | Auth | Key errors |
|---|---|---|---|---|---|
| `/api/auth/google/start` | GET | `RemoteAddr`, trusted `X-Forwarded-For`, existing query | Existing Google `302`, or `302` to `/signin?error=rate_limited` with `Retry-After` | public | rate limited redirect, existing `503` |
| `/api/auth/google/callback` | GET | `RemoteAddr`, trusted `X-Forwarded-For`, existing Google query | Existing app `302`, or `302` to `/signin?error=rate_limited` with `Retry-After` | public | rate limited redirect, existing provider refusals |
| `/api/auth/refresh` | POST | `RemoteAddr`, trusted `X-Forwarded-For`, optional `vermouth_refresh` cookie | Existing session, or `429 Error` with `Retry-After` | public with cookie | `429 rate_limited`, existing `401` |
| `/api/auth/signout` | POST | Existing optional refresh cookie | Existing `204` | public with cookie | Never rate limited |
| Three limited auth paths | HEAD | None | `405 Error` | public | `405 method_not_allowed`, no limiter key |

All matching happens on the gateway route pattern, not on a caller supplied path string. Requests
with another method do not match these handlers and do not create a limiter key.

The browser API uses a discriminated `RefreshOutcome`: `signed_in` carries the generated `Session`,
`signed_out` carries no data, and `rate_limited` carries `retry_at`. A `429` produces only the third
case and never calls `setAccessToken(null)`. `session.ts` stores the resulting
`rateLimitedUntil = retry_at` in module memory for the first render. The sign in screen copies it
into local countdown state. The manual action calls `refresh` again no earlier than `retry_at`.

### Value sourcing

| Action | Value produced or displayed | Source |
|---|---|---|
| Pick endpoint policy | endpoint name | The exact `ServeMux` route pattern registered in gateway code |
| Pick client address | normalized IPv4 or IPv6 `/64` | `RemoteAddr`, or the rightmost untrusted `X-Forwarded-For` address when the immediate peer matches `GATEWAY_TRUSTED_PROXY_CIDRS` |
| Pick client IP key | domain separated `sha256` digest | `sha256("vermouth:gateway:auth-rate-limit:client-ip:v1\\0" + normalized_address_bytes)` |
| Pick refresh token key | domain separated `sha256` digest | `sha256("vermouth:gateway:auth-rate-limit:refresh-token:v1\\0" + cookie_value_bytes)` for exactly one nonempty `vermouth_refresh` cookie |
| Pick global key | fixed endpoint key | The endpoint policy, never request input |
| Build minute and hour limiters | capacity and refill rate | The matching required environment value parsed as `count/period,count/period` |
| Decide global pressure | early refusal or caller lookup | Global `TokensAt(now)` first. When it is below one, existing caller entries may be read only to compute a truthful delay, but no caller entry is created or touched. |
| Decide one request | allowed or refused | `TokensAt` for every applicable limiter at one clamped `now`, followed by all `AllowN` calls only when every value is at least one |
| Compute `Retry-After` | positive whole seconds | The greatest time for any empty applicable bucket to refill to one token, computed from `TokensAt` and its configured refill rate, rounded up |
| Write refresh refusal | `APIError.code`, message, `request_id` | Fixed code `rate_limited`, fixed generic message, existing request context |
| Write browser refusal | redirect target | Fixed relative path `/signin?error=rate_limited` |
| Produce browser refresh state | `signed_in`, `signed_out`, or `rate_limited` | The generated refresh response status and body, with `retry_at = now + Retry-After` for `429` |
| Show browser wait | alert language and retry time | The `rate_limited` outcome, `browserLanguage()`, and `retry_at` |
| Sample a denial log | endpoint, scope, suppressed count, `request_id` | Fixed endpoint and scope enums, sampler state, existing request context |
| Bound pending attempts | maximum admitted start rate | `GATEWAY_AUTH_RATE_START_GLOBAL` plus spec 0004's ten minute expiry and sweep |
| Build threshold evidence hash | 64 lowercase hexadecimal characters | `gateway/cmd/ratelimitevidence` asks the gateway policy parser for the seven normalized values, then hashes the canonical sorted `NAME=value\n` lines defined by **AC-14** |
| Build launch evidence identity | Git SHA and test target | `git rev-parse --verify HEAD^{commit}` plus fixed string `task test:auth-rate-limit` |
| Build launch evidence result | boolean pass state and timestamp | fixed `true` emitted only after every target check passes, plus the runner UTC clock at whole seconds |
| Store launch evidence | canonical file path and bytes | `gateway/cmd/ratelimitevidence write` targeting `.tmp/production/rate-limit-evidence.json` |
| Verify candidate threshold hash | expected 64 character SHA256 | the same command's `verify` mode over the seven candidate values read from `deploy/helm/vermouth/values-production.yaml` |
| Verify evidence freshness | clean inputs, Git identity, threshold hash, target, schema, canonical bytes, pass state | current checkout and candidate production values; timestamp is metadata only |

Production uses `time.Now`. The limiter receives a clock function so unit tests advance time without
sleeping. The registry clamps any value before `last_now` to `last_now`, then stores the clamped
value. This preserves monotonic decisions under a backward test clock or wall clock correction. It
is simpler and more deterministic than tests that wait for real minute and hour windows.

With a fresh full `start` global pair and no gateway restart, at most 583 unexpired attempts can be
admitted in one ten minute attempt lifetime. Because the sweep runs every ten minutes, worst case
tick alignment can leave at most 666 physical attempt rows just before deletion. A remote flood
cannot reset either bound. An operator controlled gateway restart begins a new bound as recorded in
**AC-8**.

### Key invariants

1. The gateway makes the limit decision before opening an upstream request.
2. A caller cannot choose its endpoint name, scope kind, global key, trusted proxy set, or cookie
   name.
3. Untrusted request headers never override `RemoteAddr`.
4. One request either calls `AllowN` for every applicable bucket or for none of them.
5. A refusal changes no auth row, Google state, refresh cookie, access token, or session family.
6. Global entries are never evicted. Caller entry eviction cannot bypass the whole service bound.
7. No raw refresh token enters registry state. No caller identifier enters logs or metric labels.
8. The one process registry is valid only while the gateway deployment has one replica. Scaling the
   gateway without revisiting this decision multiplies every effective budget.
9. Global refusal creates no caller entry, changes no caller least recently used position, and cannot
   be used to churn the caller registry.
10. There is no second rate limit evidence path, copy step, alternate success field, writer, or
    canonical encoder.
11. Evidence cannot name a Git commit while testing different tracked or untracked rate limit input.

### Security model

The three routes remain public because they create or refresh authentication. Rate limiting is an
edge availability control, not authentication or authorization. It never makes a caller trusted and
never changes spec 0004's session rules.

The direct peer is trusted only through explicit CIDRs. A public caller cannot forge
`X-Forwarded-For` into a new budget. IPv6 `/64` grouping and global endpoint budgets reduce address
rotation bypass. There are no bypass CIDRs and no valid session exemption.

The refresh cookie is inspected only far enough to compute an in memory digest. Its raw value is
forwarded under the existing contract, never logged, never placed in an error, and never stored by
the limiter. This feature introduces no PCI, health, or other regulated data scope.

### Configuration required

1. `GATEWAY_TRUSTED_PROXY_CIDRS`: comma separated CIDRs whose direct requests may supply
   `X-Forwarded-For`, or the exact trimmed lowercase literal `none` for direct traffic. CIDRs are
   masked to canonical form. Duplicate or overlapping canonical prefixes are invalid.
2. `GATEWAY_AUTH_RATE_START_IP`: required `5/1m,20/1h` initially.
3. `GATEWAY_AUTH_RATE_START_GLOBAL`: required `100/1m,500/1h` initially.
4. `GATEWAY_AUTH_RATE_CALLBACK_IP`: required `10/1m,60/1h` initially.
5. `GATEWAY_AUTH_RATE_CALLBACK_GLOBAL`: required `200/1m,1000/1h` initially.
6. `GATEWAY_AUTH_RATE_REFRESH_IP`: required `30/1m,120/1h` initially.
7. `GATEWAY_AUTH_RATE_REFRESH_TOKEN`: required `10/1m,120/1h` initially.
8. `GATEWAY_AUTH_RATE_REFRESH_GLOBAL`: required `300/1m,3000/1h` initially.

Every rate value contains exactly two positive entries. One entry must use `1m`, one must use `1h`,
and their order does not matter. Duplicate periods, extra entries, unsupported periods, or overflow
stop startup. Local direct development sets the proxy value to `none`. Local k3d and production k3s
set it to the committed pod CIDR `10.42.0.0/16`. That broad network value is trusted only together
with the NetworkPolicy that admits gateway ingress from the Envoy or Traefik edge path and no other
Pod. `task prod:doctor` confirms the live cluster pod CIDR is exactly `10.42.0.0/16`; a mismatch
blocks deployment rather than widening trust.

### Launch evidence target

`task test:auth-rate-limit` reads the seven `config.gatewayAuthRate*` values from
`deploy/helm/vermouth/values-production.yaml`, maps them to their exact `GATEWAY_AUTH_RATE_*`
environment names, creates `.tmp/production` when absent, and removes only
`.tmp/production/rate-limit-evidence.json`. It then runs these commands in order. Any nonzero result
stops the target and leaves the evidence absent.

| Check | Exact command | Coverage |
|---|---|---|
| Development environment | `task env` | local database connection values for the isolated test stack |
| Local infrastructure | `task infra:up` | ready Postgres and Redpanda dependencies |
| Candidate migrations | `task migrate:up` | current identity schema and pending attempt storage |
| Unit and route behavior | `go test ./gateway/internal/ratelimit ./gateway/internal/route` | parser, caller identity, refill, atomic decisions, bounds, logs, and endpoint contracts |
| Concurrency | `go test -race ./gateway/internal/ratelimit ./gateway/internal/route` | registry and sampler race safety |
| Gateway to identity | `go test ./test/authratelimit` | refusal before forwarding, unchanged auth state, pending attempt bound, and dummy Google path |
| Gateway API generation | `task generate:api` | Go contract generation from OpenAPI |
| Browser API generation | `task web:generate` | browser contract generation from OpenAPI |
| Generated contract cleanliness | `git diff --exit-code -- gateway/internal/apitypes/types.gen.go web/src/api/schema.d.ts` | committed generated types match the contract |
| Browser dependencies | `corepack pnpm install --frozen-lockfile` | exact lockfile dependency set for browser checks |
| Browser behavior | `corepack pnpm --dir web exec vitest run src/api/session.test.ts src/pages/SignInPage.test.tsx src/routes.test.tsx` | refresh outcome, alert, countdown, retry, and route validation |
| Browser types | `task web:typecheck` | strict generated and application type agreement |

Only after all twelve checks pass, the target runs
`go run ./gateway/cmd/ratelimitevidence write <repository-root> .tmp/production/rate-limit-evidence.json`.
The command derives `git_sha`, normalizes and hashes the exported policy values, writes boolean
`pass: true`, and captures the runner UTC timestamp. The target then runs
`go run ./pkg/vermouth/cmd/platformconfig validate-document deploy/production/rate-limit-evidence.schema.json .tmp/production/rate-limit-evidence.json`
and `go run ./gateway/cmd/ratelimitevidence verify <repository-root> .tmp/production/rate-limit-evidence.json`.
If either validation fails, the target removes the exact evidence file before returning failure.

The production value mapping is fixed:

| Helm value | Gateway environment |
|---|---|
| `config.gatewayAuthRateStartIP` | `GATEWAY_AUTH_RATE_START_IP` |
| `config.gatewayAuthRateStartGlobal` | `GATEWAY_AUTH_RATE_START_GLOBAL` |
| `config.gatewayAuthRateCallbackIP` | `GATEWAY_AUTH_RATE_CALLBACK_IP` |
| `config.gatewayAuthRateCallbackGlobal` | `GATEWAY_AUTH_RATE_CALLBACK_GLOBAL` |
| `config.gatewayAuthRateRefreshIP` | `GATEWAY_AUTH_RATE_REFRESH_IP` |
| `config.gatewayAuthRateRefreshToken` | `GATEWAY_AUTH_RATE_REFRESH_TOKEN` |
| `config.gatewayAuthRateRefreshGlobal` | `GATEWAY_AUTH_RATE_REFRESH_GLOBAL` |

The separate trusted proxy mapping is `config.gatewayTrustedProxyCIDRs` to
`GATEWAY_TRUSTED_PROXY_CIDRS`. It is not hashed into threshold evidence. Spec 0006 validates it as a
separate production launch input.

Both `write` and `verify` reject a dirty clean input set. That set is `api/openapi.yaml`,
`api/oapi-codegen.yaml`, `gateway/`, `web/src/api/`, `web/src/pages/SignInPage.tsx`,
`web/src/pages/SignInPage.test.tsx`, `web/src/routes.tsx`, `web/src/routes.test.tsx`,
`web/package.json`, root `package.json`, `pnpm-lock.yaml`, `pnpm-workspace.yaml`, `Taskfile.yml`,
`.env.example`, `deploy/helm/vermouth/`,
`deploy/production/rate-limit-evidence.schema.json`, and `test/authratelimit/`. The evidence output
is outside this set. The verifier rejects a symlink or nonregular document, validates its schema and
field derivations, reserializes it with the writer's canonical encoder, and byte compares the result
with the original file. Spec 0006 supplies the candidate production values and bundles the original
bytes only after verification succeeds.

### Critical test scenarios

1. Happy path: one request below every applicable budget crosses the gateway and receives the
   unchanged identity response, verifies **AC-1**, **AC-2**.
2. Burst and sustained refusal: controlled time exhausts each minute and hour policy independently,
   checks the exact retry delay, advances time, then succeeds, verifies **AC-2**, **AC-3**, **AC-4**.
3. Atomic concurrency: many goroutines hit one caller and global policy together, the admitted count
   never exceeds either budget, denied reservations consume nothing, and the race detector stays
   clean, verifies **AC-3**.
4. Auth state: limited start, callback, and refresh requests reach no upstream. A real test identity
   database shows no extra attempt, no consumed state, and no rotated or revoked token, verifies
   **AC-5**, **AC-13**.
5. Proxy trust: direct, trusted, untrusted, malformed, excessive, IPv4, and IPv6 chains resolve to the
   expected bounded keys. Cases include several header lines, empty elements, all trusted chains,
   IPv4 mapped IPv6, invalid `RemoteAddr`, duplicate CIDRs, and overlapping CIDRs, verifies
   **AC-6**, **AC-7**.
6. Registry pressure: 10,001 distinct caller keys trigger least recently used eviction while global
   limits remain and idle entries leave after two controlled hours, verifies **AC-8**.
7. Configuration: every missing and malformed environment value returns the named
   `MissingEnvError`, verifies **AC-9**.
8. Observability: a denial flood creates one warning per endpoint and scope per minute, then one
   suppressed count, with no sensitive or unbounded value, verifies **AC-10**.
9. Contract and browser: generated types include `429`, the two redirects accept `rate_limited`, and
   a limited refresh preserves the current session view until a keyboard user retries after the
   wait, verifies **AC-11**, **AC-12**.
10. Global pressure: an empty global bucket plus 10,001 rotating caller values creates no caller
    entries, while controlled time proves the 583 live and 666 physical pending attempt bounds,
    verifies **AC-5**, **AC-8**.
11. Evidence: the exact twelve commands pass before the writer produces the canonical threshold hash and schema version 1 file at
    `.tmp/production/rate-limit-evidence.json`. The deployment schema accepts boolean `pass: true`
    and rejects `pass_state`, the old path, stale Git identity, noncanonical bytes, and a changed
    threshold. Verification recomputes the expected hash from candidate production Helm values and
    byte compares canonical encoding. One forced failure leaves no passing file. A timestamp change
    alone does not make otherwise matching evidence stale, verifies **AC-13**, **AC-14**.

## Build plan

The Tracer Bullet approach first proves one limited `start` request across the final gateway,
identity, OpenAPI, configuration, and test path. The next slices reuse that thread for callback and
refresh, then add pressure and browser depth.

1. Add `golang.org/x/time/rate` to the gateway module. Build the typed rate parser, required proxy
   parser, bounded registry, injected clock, atomic probe and `AllowN` commit, and client address
   normalizer. Cover exact limits, backward time, every forwarding header case, IPv6 `/64`, idle
   cleanup, least recently used eviction, global first lookup, and concurrent atomic checks with
   table tests, satisfies **AC-2**, **AC-3**, **AC-6**, **AC-8**, **AC-9**.
2. Wrap only `GET /api/auth/google/start` in the gateway. Wire its caller and global policies,
   sampled warning, redirect, headers, Helm values, `.env.example`, Compose values, and one real
   gateway to identity test proving an allowed request and a refusal with no inserted attempt. This
   is the first complete thread, satisfies **AC-1**, **AC-4**, **AC-5**, **AC-9**, **AC-10**.
3. Reuse the same middleware for callback and refresh. Add token cookie digesting, missing cookie
   behavior, malformed and duplicate cookie refusal, unchanged callback state, unchanged refresh
   family, explicit `HEAD` refusal, and the signout exemption, satisfies **AC-1**, **AC-3**,
   **AC-4**, **AC-5**, **AC-7**.
4. Update `api/openapi.yaml`, regenerate gateway and browser types, add the validated
   `rate_limited` search value and bilingual alert, then make boot time refresh return the typed
   `RefreshOutcome`, keep its session view, and expose one announced manual retry after
   `Retry-After`. Document optional `Retry-After` and `Cache-Control` headers on the shared `302`,
   add a reusable `RateLimited` response for refresh, and test the pure refresh outcome plus retry
   countdown with the existing Vitest suite. Run the web type check and build, satisfies **AC-4**,
   **AC-11**, **AC-12**, **AC-13**.
5. Add `task test:auth-rate-limit` with controlled clock unit coverage, a race run for gateway, a
   real Postgres and gateway integration path using a local dummy Google configuration, the global
   pending attempt bounds, sampled multi scope logs, and the Vitest browser state checks. Remove stale
   `.tmp/production/rate-limit-evidence.json` before testing. Add
   `gateway/cmd/ratelimitevidence` with exact `write` and `verify` modes, reuse the production policy
   parser, reject dirty named inputs, and atomically write exact schema version 1 evidence only after
   every named command passes. Own `deploy/production/rate-limit-evidence.schema.json`, validate and
   canonically byte compare the same file, and treat timestamp as metadata only, satisfies **AC-3**, **AC-5**, **AC-6**,
   **AC-7**, **AC-8**, **AC-10**, **AC-13**, **AC-14**.

## Consequences

**Positive**:

1. Abusive traffic is refused before it spends identity, Postgres, or Google work.
2. Caller limits preserve fairness while global limits bound rotating address and token attacks.
3. The feature adds no database write on the attack path and no infrastructure service.
4. The exact public behavior and production launch evidence are reproducible in one test target.

**Negative and tradeoffs**:

1. Restarting the gateway restores full budgets, so a caller can gain a new burst during an operator
   controlled restart.
2. The one process registry does not coordinate replicas. Adding a gateway replica would multiply
   effective limits.
3. Shared IPv4 addresses and IPv6 `/64` networks share caller budgets. A busy school network could
   therefore delay an innocent tutor.
4. The gateway now understands the refresh cookie name and trusted proxy chain instead of forwarding
   every auth detail opaquely.
5. Seven policy values plus one trusted proxy value add deployment configuration that must stay in
   step across local and production environments.
6. The feature writes one production named temporary artifact even when its target runs outside the
   deployment workflow. That shared path is deliberate because it removes a copy and translation
   boundary from the public launch decision.

**Neutral**:

1. There is no database migration and no change to the event catalogue.
2. A small existing sign in screen gains one error sentence and one manual retry state.
3. Sampled structured logs are the only new production signal until feature 9 adds central metrics
   and alerts.

## Follow-up

1. Before the gateway may run more than one replica, choose a shared limiter or divide budgets by a
   fixed replica count. Do not scale this local registry silently.
2. Feature 9 should export admitted and refused counters plus registry size with endpoint and scope
   as the only labels, then alert on sustained global refusal.
