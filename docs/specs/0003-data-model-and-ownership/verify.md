# 0003 verify: data model and data ownership per service

How to prove spec 0003 landed. Every line names the acceptance criterion it covers, so you can run the
list top to bottom or pick the one criterion you care about. `$check verify` can drive this directly.

Read the note at the bottom first: identity's migration is written but not applied, so one command in
the list fails on purpose until that decision is settled.

## Commands

- [ ] `task infra:clean && task infra:up && task migrate:up` all four services apply from empty  → AC-2
- [ ] `task migrate:down -- billing` then `task migrate:up` reverses and reapplies                → AC-2
- [ ] `task generate:sql && task build` no diff in generated code, every binary builds            → AC-5
- [ ] `cd test && go test ./model/` ownership, tenancy, schema and key guards                     → AC-1, AC-3, AC-4, AC-6, AC-7, AC-10, AC-11, AC-12
- [ ] `cd services/billing && go test ./internal/store/ -run TestReplay` replay safety            → AC-7, AC-8
- [ ] `task test` every module green                                                              → all
- [ ] `task lint && task fmt:check` clean across all seven modules                                → AC-5

The guard tests read migration files and `information_schema`, so they need the infra stack up but no
service running. The replay test drives the real Postgres and the real Redpanda from
`test/compose.test.yaml`.

## Value sourcing, by hand

One check per Value sourcing row in the spec's feature design. These are the answers a test cannot
fully pin down, because they are about which number a human sees.

- [ ] Move a Present session from 30 September to 1 October, then it bills in October only, never both → AC-9
- [ ] Mark Present on a student's own leave date, then that session still bills                         → AC-11
- [ ] Put an Absent session beside an unmarked one, then the money matches and only the screen differs  → AC-1
- [ ] Run a December period in January, then the number reads `2026-0001`, not `2027-0001`              → AC-8
- [ ] Blank one invoice profile field, then `is_complete` goes false and the Go list names that field   → AC-12

## Known open item, settled by spec 0004

`services/identity/db/migrations/00003_tutor_credentials.sql` was written and proven against a scratch
empty database, but it was never applied to the dev database and `task migrate:up` failed on it:

```
ERROR: column "password_hash" of relation "tutors" contains null values (SQLSTATE 23502)
```

Two reasons, both outside this build. The dev database already held tutor rows from `task thread`
runs, and the scaffold's registration takes no password, so even an empty database would have broken
at runtime. Spec 0003 chose `password_hash text NOT NULL` on the premise that neither was true.

Settled by spec [0004](../0004-tutor-sign-in-google-oauth/index.md), which removed the column rather than working around it: Vermouth
authenticates through Google only and stores no password. Feature 7 rewrites this migration as
`updated_at` only, wipes the dev identity database, and applies the Google account link in a new
`00004`. Until feature 7 is built, keep treating the first `task migrate:up` line as covering
`teaching`, `billing`, and `notifications` only. Nothing further is owed from this spec.

## Current durable verification, updated 2026-08-25

This section supersedes the open item above. Feature 7 is built. Identity migration
`00003_tutor_timestamps.sql` now adds only `updated_at`, migration `00004_tutor_google_identity.sql`
holds the Google account link, and all four databases migrate from empty without an exception.

### Commands

The first command deletes the local development databases and event log. You may skip it when that
data matters. A clean disposable stack gives the strongest proof for AC-2.

1. [ ] Run `task infra:clean`, then `task infra:up`, then `task migrate:up`. Expect every migration for all four services to apply from empty. This covers AC-2.
2. [ ] Run `task migrate:down -- teaching`, then `task migrate:up`. Repeat for `billing` and `notifications`. For identity, run `task migrate:down -- identity` twice, then `task migrate:up`, because feature 7 has a later migration. Expect every down step and restore to succeed. This covers AC-2.
3. [ ] Run `task migrate:status`. Expect identity at version 4, teaching at version 2, billing at version 2, and notifications at version 3. This covers AC-2.
4. [ ] Run `task generate:sql`, then inspect `git diff -- services/*/internal/store/sqlcgen`. Expect no generated change. This covers AC-5.
5. [ ] Run `task build`. Expect all five production binaries to build. This covers AC-5.
6. [ ] Run `task test` while Postgres and Redpanda are live. Expect every module and `test/model` to pass. This covers AC-1 through AC-12.
7. [ ] Run `task check`. Expect Go format and lint, Biome, and TypeScript checks to pass. This covers AC-5.

### Value sources

Each step checks one row from the spec Value sourcing table. You may drive these through the store
tests or through the later feature that owns the action.

1. [ ] Deliver the same projection event twice. Expect one row at the event identifier, never a second row. Check `student_id`, `class_id`, `session_id`, `(session_id, student_id)`, and `(class_id, student_id, effective_from)`. This covers AC-7.
2. [ ] Join, leave, then rejoin one student. Expect the leave to close only the row whose `effective_to` is null, and the rejoin to create a new row. This covers AC-11.
3. [ ] Deliver `teaching.student.removed` and `teaching.session.cancelled` with known envelope times. Expect `removed_at` and `cancelled_at` to equal each envelope `occurred_at`. This covers AC-3 and AC-11.
4. [ ] Archive a class in teaching. Expect `archived_at` to come from the teaching transaction clock and no consumer projection to store it. This covers AC-1 and AC-11.
5. [ ] Change a class rate. Expect the teaching class row and its outbox event to be written in one transaction, with no billing write back. This covers AC-1 and AC-3.
6. [ ] Consume `teaching.class.created` with `rate_effective_from`, then `teaching.class.rate.changed` with `effective_from`. Expect both values to land in `class_rates.effective_from` and the second date to add or correct only its own keyed row. This covers AC-3 and AC-7.
7. [ ] Move a Present session from 30 September to 1 October. Expect it to bill in October only, based on `sessions.local_date`, with no stored month column. This covers AC-9.
8. [ ] Compare Present, Absent, and unmarked attendance for the same period. Expect only Present to bill. Expect Absent and unmarked to produce the same money. This covers AC-1 and AC-9.
9. [ ] Keep a student on the roster for a period with no Present session. Expect no invoice and no invoice number to be spent. This covers AC-1 and AC-8.
10. [ ] Run a December 2026 period in January 2027. Expect the invoice number year to be `2026`, from `period_year`. This covers AC-8.
11. [ ] Issue an invoice, then change the invoice profile. Expect the issued invoice payee block to remain byte for byte unchanged. This covers AC-8.
12. [ ] Issue an invoice, then change the projected student and class names. Expect the issued invoice and its lines to retain the names frozen at issue. This covers AC-8.
13. [ ] Blank each invoice profile field in turn. Expect `is_complete` to be false and the Go missing field list to name that field. Expect both answers to become complete together. This covers AC-12.
14. [ ] Void the live billing run for a period, then attempt a reissue. Expect the next generation to be the current generation plus one, and a concurrent attempt to lose on the unique key. This covers AC-1 and AC-8.
15. [ ] Move a session or change its dated rate after voiding an invoice, then reissue. Expect the replacement to use current projections while the voided invoice keeps its frozen values. This covers AC-8 and AC-9.
16. [ ] Count a roster on its first day, leave day, a gap, and a one day membership. Expect the digest count to use the inclusive roster coverage predicate and never a stored counter. This covers AC-9 and AC-11.
17. [ ] Give two recipients different timezones around a UTC date boundary. Expect each digest local date to come from that recipient row timezone. This covers AC-9 and AC-10.
18. [ ] Run one query with tutor A from the verified token against rows owned by tutor B. Expect no row. Confirm no request body or query parameter can replace that `tutor_id`. This covers AC-4.

### Acceptance criteria coverage

AC-1 through AC-12 are covered by the command checks and the eighteen value source checks above.
`$check verify data model & data ownership per service` may run them directly, and `$test` may lock
the durable behavior into automated tests.
