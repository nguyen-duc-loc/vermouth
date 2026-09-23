# Review, feat/recurring-sessions-exceptions, 2026-09-23

**Reviewed by**: gpt-6-sol (author on gpt-5.6-sol)
**Scope**: 33 relevant files inspected within 188 changed files, branch vs main
**Verdict**: Changes requested

## Summary

The rate command and monthly issue flow keep money writes in one transaction, and the billing tests exercise rollback, concurrent issue, the fixed broker cut, and stale previews. Three gaps still need attention before merge: structurally incomplete broker envelopes can be recorded as handled, a missing class can disappear from the candidate query, and CI does not run the teaching rate integration tests. The web billing tests also need a CI runner.

## Major

### 🟠 Validate required envelope fields before marking an event handled, `pkg/vermouth/consumer.go:197`

**Problem**: `json.Unmarshal` accepts an object without `event_id`. `handleOnce` then inserts the zero UUID into `handled_events`. A second, distinct, otherwise valid teaching event without that field conflicts on the same `(consumer_name, event_id)` and is treated as a safe duplicate at line 290. Neither record is parked or recorded in `consumer_failures`.

**Why it matters**: A malformed source record can silently lose a roster, session, attendance, or rate fact while broker offsets continue to advance. Billing can then certify a projection that lacks the fact and issue from incomplete inputs. The new failure ledger cannot block an event it never records.

**Suggested fix**: You could validate the required envelope identity and structural fields before the handled insert, park incomplete envelopes with their exact source coordinate, and test two distinct malformed records that share a missing event ID against the real broker and database.

### 🟠 Keep sessions visible when no projected roster row survives, `services/billing/db/queries/billing.sql:177`

**Problem**: `ListBillingCandidates` starts from `sessions` but uses an inner `JOIN LATERAL` for roster coverage. A session with no projected coverage disappears before the outer class join and before `calculate` can check its class label. If its class projection is missing too, the expected `projection_incomplete` response never occurs.

**Why it matters**: The month can appear empty, or it can issue the other students' invoices, while a structurally incomplete session is invisible. That breaks the spec's fail closed structural check and makes projection corruption harder to detect.

**Suggested fix**: You could check session and class structure independently of roster coverage, or retain uncovered sessions in the query and distinguish a valid empty roster from a missing class. Add a test with a session whose class is missing and whose roster has no current coverage.

### 🟠 Run the teaching rate integration suite in CI, `.github/workflows/ci.yml:61`

**Problem**: The migration step supplies `TEACHING_DATABASE_URL`, but the `Test` step supplies only `BILLING_DATABASE_URL` and `BROKER_SEEDS`. Task runs tests in every Go module, yet `teachingTestPool` calls `t.Skip` when `TEACHING_DATABASE_URL` is absent. The checkout does not include `.env`, so `TestPutClassRate_RevisesEveryCommandAndReplaysOnlyTheReceipt` and the teaching store integration tests are skipped in CI.

**Why it matters**: The green gate does not exercise rate revisions, idempotency, or the teaching transaction that publishes the rate fact, even though those behaviors determine invoice amounts.

**Suggested fix**: You could supply the teaching database URL to the test step, along with the other service URLs needed by their suites, and make the CI integration target fail when required test infrastructure variables are missing.

## Minor

### 🟡 Run the new browser billing tests in CI, `.github/workflows/ci.yml:101`

**Problem**: The web job runs Biome, TypeScript, generated type validation, and build, but never runs Vitest. The new `BillingPage` and `ClassDetailPage` behavior tests therefore do not contribute to the merge gate.

**Why it matters**: Type checking and build do not exercise the preview, issue recovery, rate sync, or accessible UI states covered by those tests.

**Suggested fix**: You could add a web test command that runs `pnpm exec vitest run` and call it in the web job.

## Strengths

* The billing issue transaction contains the run, counter increments, invoices, frozen lines, and outbox facts, with tests proving rollback at several write points.
* The broker cut and projection readiness protocol have real Postgres and Redpanda tests, including concurrent issue recovery and replay retention loss.

## Test coverage

The billing protocol tests cover rollback, concurrency, stale fingerprints, candidate limits, unresolved failures, and the fixed broker cut. The shared recovery tests use a real broker and database. The teaching rate integration tests exist but are skipped by the CI environment, and the web component tests exist but are not run in CI. The candidate tests cover missing class and student labels only when a covered roster row remains; they do not cover the disappearing session case above.
