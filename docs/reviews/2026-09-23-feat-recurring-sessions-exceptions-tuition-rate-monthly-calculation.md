# Review, feat/recurring-sessions-exceptions, 2026-09-23

**Reviewed by**: gpt-6-astra (author on gpt-5.6-sol)
**Scope**: 41 implementation, test, contract, and supporting files, branch vs main at `731fc3d3f57b8c8aa57ec24495f73829bd7ef2ea`. The branch has 188 changed files; this review focuses on feature 13, its shared recovery machinery, and the later CI fix. Core feature files were read in full; larger files shared with prerequisite features were inspected in their relevant sections and diff. Specs, context files, and the prior review are additional references, excluded from the 41-file count.
**Verdict**: Blocked

## Summary

The change implements dated tuition rates, certified monthly previews, and atomic immutable invoice issue. The transaction boundary, retry recovery, and adversarial money tests are strong, and the previously reported malformed-record and browser recovery defects have corresponding fixes. A resolved failure can nevertheless become invisible when it fails again during a later projection rebuild, allowing certification of incomplete billing data. CI also still skips the teaching rate integration tests because their database environment is absent from the test step.

## Blockers

### 🔴 A repeated failure inherits an obsolete resolution, `pkg/vermouth/consumer_failure.go:219`

**Problem**: `recordConsumerFailure` uses `ON CONFLICT ... DO NOTHING` for an existing source coordinate, including a row whose `resolved_at` is already set. The billing replay task clears the projection tables and handled events, but retains these failure resolutions. If that source record fails again during the new rebuild, parking succeeds and its offset advances while the ledger continues to describe it as resolved. For example, an operator can restore a projection and acknowledge its unreadable source with `projection_restored`; the next full replay deletes that repair, encounters the same unreadable record, and retains the old acknowledgement. A previously replay-resolved record that fails in a subsequent rebuild has the same problem.

**Why it matters**: Both certification and monthly calculation filter only `resolved_at IS NULL`. They can therefore accept a generation that has skipped a known failed record. Missing roster/session facts can disappear from the candidate set, or a missing rate correction can leave an older rate in force, producing incomplete or incorrect immutable invoices. The readiness generation does not scope failure resolution, so it does not prevent this violation of AC-9, AC-10, and AC-24.

**Suggested fix**: Make failure resolution apply to the projection generation it actually repaired, or reopen a resolved coordinate when it is parked again, while preserving the operator audit history. Before committing the failed record's offset, the current generation must expose an unresolved failure. Add a real database/broker regression covering acknowledge or successful resolution, a fresh projection rebuild, the same record failing again, and refusal of certification and billing until that generation is repaired.

## Major

### 🟠 CI silently skips the teaching half of the tuition feature, `.github/workflows/ci.yml:62`

**Problem**: The migration step supplies all four database URLs, but its environment is scoped to that step. The separate `Test` step supplies only `BILLING_DATABASE_URL` and `BROKER_SEEDS`. `Taskfile.yml` loads only `.env`, which is not tracked or created by this workflow. Consequently `teachingTestPool` skips on a fresh runner, including the new `TestPutClassRate_RevisesEveryCommandAndReplaysOnlyTheReceipt` test. The identity and notifications tests that require their database environment are affected as well.

**Why it matters**: CI can remain green without exercising the rate revisions, receipt replay, ownership checks, and archive restrictions that feed the invoice calculation. Starting and migrating the databases does not make those tests execute. I reproduced the exact skip condition: running the named rate test with `TEACHING_DATABASE_URL` unset prints `SKIP` and then exits successfully with `PASS`. The new CI guard checks the migration-step variables but does not check the test-step environment.

**Suggested fix**: Provide all required database URLs to the test step, or move the shared test-stack environment to the Go job so migrations and tests inherit the same values. Extend the workflow guard to cover the execution environment, and confirm the rate integration test executes rather than skips in CI.

## Strengths

- Issue keeps the run, invoice numbers, frozen invoices and lines, and outbox facts in one repeatable-read transaction. Unique constraints and fresh winner reads protect repeated and concurrent requests.
- The money tests inject failure at every write stage, coordinate competing issue transactions, check repeatable snapshots and fixed broker cuts, and exercise the exact 500-student/10,000-candidate boundary.
- The malformed-record regression now checks a real dead letter publication followed by successful processing of a valid record. Browser tests cover prerequisite errors, lost-response recovery, navigation during issue, and dated rate synchronization.

## Test coverage

The existing money protocol, recovery, gateway, and browser tests were inspected. This review ran the focused teaching rate test with its database variable unset and confirmed the CI skip described above; it did not rerun the full suite or add tests. The reported earlier successful local and CI runs do not cover that missing CI environment.

No existing regression exercises a resolved failure being parked again after a new projection generation. The acknowledgement command and successful exact-coordinate failure resolution also lack direct behavioral tests; include those paths when adding the recovery regression so database constraints, projection writes, handled-event writes, and resolution are verified together.
