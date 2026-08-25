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
