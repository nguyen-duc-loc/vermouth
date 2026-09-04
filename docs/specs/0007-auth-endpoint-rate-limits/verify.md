# Verify: auth endpoint rate limits · spec 0007 · updated 2026-09-04

_Steps derived from spec 0007 acceptance criteria. `$check verify` runs these, and `$test` locks the durable ones._

## UI and manual

- [x] Open `/signin?error=rate_limited` in English, then Vietnamese. Confirm the alert says exactly `Too many sign in requests. Wait a moment, then try again.` and `Có quá nhiều yêu cầu đăng nhập. Hãy chờ một lát rồi thử lại.` Unknown error values must show no raw detail. → AC-11
- [x] Mock boot time `POST /api/auth/refresh` as `429` with `Retry-After: 30`. Confirm the current access token and cookie remain untouched, no automatic retry starts, the fixed alert appears, and the retry button is disabled. → AC-4, AC-12
- [x] Keep keyboard focus elsewhere while the countdown reaches zero. Confirm focus does not move, the polite live region announces whole seconds, and the enabled control can be reached and activated with the keyboard. → AC-12
- [x] Inspect the retry state at 390 by 844 and 1280 by 720 CSS pixels in both themes. Confirm there is no overflow, the control is at least 44 by 44 CSS pixels, the focus ring is visible, and the alert remains readable. → AC-12

## Commands

- [x] `task check` → Go format and lint, Biome, and TypeScript all pass. → AC-1 to AC-14
- [x] `task build` → every production binary builds with the gateway rate configuration wired at startup. → AC-9
- [x] `go test ./gateway/internal/ratelimit ./gateway/internal/route` → parser, caller identity, refill, atomic refusal, registry bounds, sampled logs, and route contracts pass. → AC-1 to AC-10
- [x] `go test -race ./gateway/internal/ratelimit ./gateway/internal/route` → concurrent requests and sampled logging report no race. → AC-3, AC-6, AC-8, AC-10
- [x] `go test ./test/authratelimit` with the shared test infrastructure and migrations ready → admitted `start` calls create only admitted pending attempts, caller refusal creates none, and rotating callers stop at the global budget. → AC-1, AC-4, AC-5, AC-8, AC-13
- [x] `corepack pnpm --dir web exec vitest run src/api/session.test.ts src/pages/SignInPage.test.tsx src/routes.test.tsx` → typed refresh outcomes, validated search, bilingual alert, countdown, and manual retry pass. → AC-11, AC-12, AC-13
- [x] On a clean committed checkout, `task test:auth-rate-limit` → all twelve checks pass, then `.tmp/production/rate-limit-evidence.json` is created and both schema and canonical verification succeed. → AC-13, AC-14
- [x] Change one named clean input, then run `task test:auth-rate-limit` → the target refuses to leave passing evidence. Restore the input before continuing. → AC-14
- [x] `helm lint deploy/helm/vermouth --values deploy/helm/vermouth/values-production.yaml` → the chart accepts all required rate and trusted proxy values. → AC-9, AC-14

## Value source checks

- [x] Send requests to `start`, `callback`, and `refresh`, then send the same traffic to `signout`, health, and an authenticated API path. Confirm only the three exact `ServeMux` route patterns consume rate budgets. → AC-1
- [x] Exercise direct, trusted proxy, untrusted proxy, malformed chain, 33 address chain, IPv4 mapped IPv6, exact IPv4, and two addresses in one IPv6 `/64`. Confirm the selected caller follows `RemoteAddr` and the rightmost untrusted forwarding rule. → AC-6
- [x] Compare caller entries for two ports on one IPv4 address, two IPv6 addresses in one `/64`, and a different `/64`. Confirm the domain separated client digest groups only the intended callers and no raw address enters state. → AC-6, AC-8
- [x] Repeat `refresh` with one token across different IP addresses, then with different tokens from one IP. Confirm the domain separated refresh digest enforces the token policy independently and no raw token enters state or logs. → AC-7, AC-10
- [x] Rotate more than 10,000 caller identities after exhausting one endpoint global budget. Confirm the fixed endpoint global key still refuses every request and no new caller entry appears. → AC-5, AC-8
- [x] Reverse the minute and hour entries in each environment value, then parse them. Confirm the same minute capacity, hour capacity, refill rates, and normalized minute first value result. → AC-2, AC-9, AC-14
- [x] Exhaust the global pair before presenting a new caller. Confirm refusal occurs before caller lookup or creation, while an existing empty caller may still increase the truthful retry delay without changing its recent position. → AC-3, AC-8
- [x] Run concurrent requests against one caller and global pair. Confirm each request observes one clamped instant, admitted requests spend every applicable bucket, refused requests spend none, and admissions never exceed either budget. → AC-3
- [x] Exhaust minute and hour buckets separately with a controlled clock. Confirm `Retry-After` uses the greatest empty bucket delay, rounds up to whole seconds, and never falls below `1`. → AC-2, AC-4
- [x] Limit `refresh` with request id `request-refresh`. Confirm status `429`, code `rate_limited`, the fixed generic message, the same request id, `Retry-After`, `Cache-Control: no-store`, and no `Set-Cookie`. → AC-4, AC-5
- [x] Limit `start` and `callback`. Confirm each returns `302` to the fixed relative `/signin?error=rate_limited` with `Retry-After` and `Cache-Control: no-store`, without forwarding. → AC-4, AC-5
- [x] Return refresh statuses `200`, `401`, and `429`. Confirm the browser produces only `signed_in` with its generated `Session`, `signed_out`, and `rate_limited` with `retry_at`. → AC-11, AC-12
- [x] Change `navigator.languages` between English and Vietnamese while `rateLimitedUntil` is set. Confirm the alert, retry label, and remaining whole seconds come only from `browserLanguage()` and `retry_at`. → AC-11, AC-12
- [x] Deny the same endpoint and scope several times inside one minute, then deny once after it. Confirm one first warning, suppression inside the window, and the next warning reports the suppressed count with only `endpoint`, `scope`, `count`, `suppressed`, and `request_id`. → AC-10
- [x] Run the production `start` global policy through the ten minute attempt lifetime and the worst case twenty minute physical row window calculation. Confirm the bounds are 583 live attempts and 666 physical rows. → AC-5, AC-13
- [x] Hash the seven parser normalized `NAME=value\n` lines in bytewise name order. Confirm the evidence contains the resulting 64 character lowercase SHA256 and excludes trusted proxy CIDRs. → AC-14
- [x] Compare evidence `git_sha` with `git rev-parse --verify HEAD^{commit}` and `test_target` with `task test:auth-rate-limit`. Confirm both match exactly. → AC-14
- [x] Inspect a passing document. Confirm `pass` is boolean `true` and `timestamp` is a UTC RFC 3339 instant with whole seconds and `Z`. Change only the timestamp to another canonical instant and confirm verification still succeeds. → AC-14
- [x] Remove the evidence path, run the clean target, and inspect its file mode and bytes. Confirm the writer uses `.tmp/production/rate-limit-evidence.json`, mode `0644`, lexical JSON keys, no insignificant whitespace, and one trailing line feed. → AC-14
- [x] Change one production Helm threshold while keeping old evidence. Confirm `gateway/cmd/ratelimitevidence verify` recomputes the candidate hash and refuses the document. → AC-14
- [x] Try a symlink destination, a nonregular destination, noncanonical JSON, an extra field, a stale Git SHA, and a dirty named input. Confirm each is refused and no temporary file or passing replacement remains. → AC-14

## Acceptance criteria coverage

- AC-1 is covered by the exact route scope, `HEAD`, signout exemption, route unit tests, and the real gateway to identity thread.
- AC-2 is covered by strict policy parsing plus independent minute and hour exhaustion.
- AC-3 is covered by atomic refusal tests and the race detector.
- AC-4 is covered by redirect and API refusal contracts plus whole second retry checks.
- AC-5 is covered by no forwarding assertions and the real pending attempt count.
- AC-6 is covered by direct and proxy caller identity cases, including IPv4 and IPv6 normalization.
- AC-7 is covered by refresh IP, token, zero cookie, empty cookie, and duplicate cookie cases.
- AC-8 is covered by idle cleanup, least recently used eviction, the 10,000 entry cap, restart state, and global first refusal.
- AC-9 is covered by every missing and malformed configuration case plus Helm values.
- AC-10 is covered by per endpoint and scope sampling with sensitive value exclusion.
- AC-11 is covered by generated OpenAPI types, validated search, and exact bilingual copy.
- AC-12 is covered by preserved auth memory, no automatic retry, the announced countdown, keyboard behavior, focus stability, and touch size.
- AC-13 is covered by the named evidence target and its Go and Vitest command sequence.
- AC-14 is covered by clean input rejection, canonical schema version 1 writing, candidate threshold verification, and atomic file safety.
