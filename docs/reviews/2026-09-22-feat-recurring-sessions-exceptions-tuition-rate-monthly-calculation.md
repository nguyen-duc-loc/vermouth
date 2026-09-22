# Review, feat/recurring-sessions-exceptions, 2026-09-22

**Reviewed by**: gpt-6-astra (author on gpt-5.6-sol)
**Scope**: 46 implementation, test, contract, and supporting files, branch vs main at `731fc3d3f57b8c8aa57ec24495f73829bd7ef2ea`; focused on spec 0013. Core new files were read in full; larger files shared with earlier features were inspected in their relevant sections and diff. Repository context files are additional references, not included in this count.
**Verdict**: Changes requested

## Summary

The change implements dated rate revisions, certified projection readiness, monthly previews, and atomic immutable invoice issuance. The money transaction and its adversarial tests are substantial, and the gateway accommodates the largest supported responses. Recovery still has a poison-record failure, initial billing read errors can leave the page permanently loading, and the main money-protocol suite is omitted from the documented test command.

## Major

### 🟠 Invalid JSON cannot reach the dead letter topic, `pkg/vermouth/consumer.go:359`

**Problem**: The decode-failure path records the failure and calls `park`, but `DeadLetter.Envelope` is `json.RawMessage` and receives the undecodable source bytes. For a record such as `invalid envelope` or truncated JSON, `json.Marshal(letter)` fails before anything is produced to the dead letter topic. The new `drainRecords` retry loop repeats this permanent encoding failure indefinitely.

**Why it matters**: One malformed broker value prevents the consumer from committing that record or processing subsequent records in the batch. Billing's barrier cannot catch up, and acknowledgement does not help because the source is still retried through the same failing serialization. This directly breaks the unreadable-event recovery path required by AC-9, AC-10, and AC-24. The current cancellation test uses invalid bytes but fails the ledger insert first, so it does not exercise this case.

**Suggested fix**: Preserve arbitrary source bytes in a representation that is always valid JSON, with an explicit encoding contract that recovery understands. Add a real Postgres/Redpanda regression proving a non-JSON record is parked, its durable failure precedes offset advancement, and a following valid record is handled.

### 🟠 Initial billing failures are hidden behind permanent loading, `web/src/pages/BillingPage.tsx:165`

**Problem**: `loading` includes `defaultQuery.isPending`, `selected === undefined`, and `periodQuery.isPending`, and the render checks it before `pageError`. If `/api/billing-periods/default` fails when opening `/billing` without search values, selection never becomes defined and the disabled period query remains pending. If the tutor read fails, both dependent queries remain pending. The error surface is therefore unreachable. With explicit search values, a default-query error can become visible, but its Try again action only refetches the period query.

**Why it matters**: A failed initial request leaves the billing screen announcing loading indefinitely, without an actionable error or working retry. The new tests always resolve the tutor and default reads, so this ordinary recovery path is uncovered.

**Suggested fix**: Give prerequisite errors precedence over dependent loading states, distinguish disabled queries from active initial fetches, and retry the failed prerequisite. Cover failing tutor and default reads both with and without valid period search values.

### 🟠 The money-protocol integration suite is excluded from the normal test gate, `services/billing/internal/handler/billing_periods_integration_test.go:1`

**Problem**: The only suite exercising `BillingService` rollback at every money write, concurrent issue, repeatable snapshots, fixed broker cuts, and exact volume bounds is behind `//go:build integration`. `Taskfile.yml:231` runs `go test ./...` without that tag, and CI invokes that task. There is no task or CI invocation enabling the tag. Even running `task test` with the required live database and broker variables does not discover these tests.

**Why it matters**: The documented verification command can pass while all of the new high-risk money protocol regressions are omitted. This undermines AC-24 and the verification record's claim that the normal Go test command covers the monthly flow.

**Suggested fix**: Include this suite in the real-infrastructure test target used for the feature gate, either by removing the tag and retaining the existing environment checks or by explicitly enabling it in a documented target. Ensure the gate actually supplies Postgres and Redpanda, and record the command that executes these tests.

## Minor

### 🟡 Rate history synchronization is lost on page reload, `web/src/pages/ClassDetailPage.tsx:83`

**Problem**: Polling and the syncing badge depend solely on local `pendingRate`. A fresh visit receiving `history_state: syncing` or `unavailable` neither announces that state nor polls for recovery. In addition, success retains only the returned date/revision and discards `saved.current`, so the authoritative saved values are not rendered immediately if the following GET fails. A greater revision on the same date is announced as ordinary synchronization instead of a superseding correction.

**Why it matters**: The page can silently display incomplete dated history after navigation or reload, and a successful write can continue showing an older current rate during a read outage. These are gaps in AC-19 and the explicit browser synchronization contract.

**Suggested fix**: Render and recover from the server's history state independently of a locally submitted command, retain the confirmed teaching portion of the mutation response, and distinguish an exact revision match from a newer same-date correction. Test reload during lag, unavailable history, and a superseding correction.

### 🟡 Automatic session loss leaves private billing state behind, `web/src/main.tsx:24`

**Problem**: The global anonymous-session subscriber clears profile state and teaching drafts but does not call `clearBillingClientState`. Billing cleanup is wired only into the AccountPanel button. Session expiration, a refused refresh, or a sign-out retry completed through the session error surface bypass that button's cleanup.

**Why it matters**: Billing/rate query data and independently tracked mutation requests are not consistently cleared at the session boundary, contrary to AC-21. Tutor-scoped keys reduce accidental reuse, but do not fulfill cancellation and removal on session loss.

**Suggested fix**: Connect billing cancellation/removal to the central session transition as well as explicit sign-out, and test an anonymous transition while a billing request is pending and private query data is populated.

## Strengths

- Issue keeps the run, number allocation, invoices, frozen lines, and outbox facts in one repeatable-read transaction. Database uniqueness and fresh winner reads provide a concrete duplicate-issue defense.
- The integration tests inject failures across every money write, synchronize concurrent issue attempts, exercise a fixed broker cut, and cover the exact supported volume boundary. Gateway tests also cover large escaped-label responses and preservation of the five-second barrier outcome.
- Dated rate application uses per-date positive revisions and legacy source coordinates, and the preview fingerprint uses ordered typed data with explicit nulls and a golden vector.

## Test coverage

During this review, the billing handler, gateway aggregate, and gateway route package tests passed. The gateway route tests initially encountered the sandbox's local-port restriction and passed when rerun with local test-server access. The installed Vitest entry point passed all 23 tests across `billing.test.ts`, `BillingPage.test.tsx`, and `ClassDetailPage.test.tsx`; direct invocation with Node avoided a pnpm registry-signature lookup that could not complete under restricted networking.

The real Postgres/Redpanda integration suite was inspected but was not executed in this review, and the ordinary Go command excludes the tagged billing suite. The specific malformed-record, initial prerequisite-error, rate-reload, and automatic-session-loss paths described above need regression coverage. The recovery CLI suite tests certification extensively but does not exercise the acknowledgement command or successful exact-coordinate failure resolution, which remain additional AC-10 coverage gaps.
