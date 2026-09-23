# Review, feat/recurring-sessions-exceptions, 2026-09-23

**Reviewed by**: gpt-5.6-sol (author on GPT-6)
**Scope**: 31 files, committed range `ec23cc1..HEAD`
**Verdict**: Approve

## Summary

The three fix commits close all four reported defects. Repeated source failures become unresolved again while retaining prior resolution audit data, missing event identifiers are parked before idempotency, active sessions with a missing class are checked before roster coverage can hide them, and CI now runs the teaching integration and browser Vitest suites. I found no actionable correctness, security, resilience, performance, contract, migration, or test coverage issue in this range.

## Strengths

* `pkg/vermouth/consumer_failure.go:219` reopens the exact source coordinate and clears only its current resolution fields. It appends the former resolution to `resolution_history`, so certification sees the new unresolved failure without losing operator audit evidence.
* `pkg/vermouth/consumer.go:196` rejects a nil `event_id` before `handleOnce` can write `handled_events`. The real broker regression at `pkg/vermouth/consumer_test.go:119` proves two such records produce two unresolved source failures and do not collide as duplicates.
* `services/billing/db/queries/billing.sql:159` checks active sessions and class ownership independently of roster coverage. `services/billing/internal/handler/billing_periods.go:488` turns that result into the safe `projection_incomplete` response before candidate calculation.
* The canonical migration has byte identical copies at the next migration number for identity, teaching, billing, and notifications. Its default upgrades existing rows safely, and regenerated SQLc models include the new JSON column.
* `.github/workflows/ci.yml:61` gives the Go test step all four database URLs and the broker address. `.github/workflows/ci.yml:95` runs Vitest after installation, and `test/model/ci_workflow_test.go:14` guards both additions.

## Test coverage

The new tests directly cover the repaired behavior. `pkg/vermouth/consumer_test.go:119` covers distinct missing event identifiers, `pkg/vermouth/consumer_test.go:165` covers reopening a resolved coordinate while retaining its audit history, `pkg/vermouth/cmd/consumerfailures/main_test.go:125` proves certification refuses that repeated failure, and `services/billing/internal/handler/billing_periods_integration_test.go:537` covers a missing class with no roster coverage.

Focused review runs passed the three recovery regressions, the missing class billing regression, the complete teaching integration suite, the CI and live recovery schema guards, and 33 Vitest files with 189 tests. The parent run also passed `task check`, full `task test`, production `task build`, the targeted regressions, and all 189 Vitest tests. `git diff --check` passed, and the four service migration copies match the canonical DDL byte for byte.
