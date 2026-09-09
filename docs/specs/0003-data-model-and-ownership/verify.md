# 0003 verify: data model and data ownership per service

This checklist proves the surfaces feature 4 builds: migrations, typed queries, live schema guards,
replay safety, and store behavior. Spec 0003 also decides value sources for later actions. Those
decisions remain binding, but their runtime checks belong to the later feature that builds each
action and do not gate feature 4.

The data layer is the runtime surface for this feature. It adds no HTTP endpoint. Run the checks
against the real Postgres and Redpanda stack in `test/compose.test.yaml`.

## Commands

1. [x] Create one temporary empty database for each service. Apply every migration with `goose`.
   Expect identity at version 4, teaching at version 3, billing at version 3, and notifications at
   version 3. Drop only the temporary databases when done. This covers AC-2.
2. [x] In the temporary databases, reverse teaching and billing twice so
   `00003_tenant_references.sql`, `00003_model_integrity.sql`, and each `00002` model migration are
   exercised. Reverse notifications once for `00003_digest_model.sql`. Reverse identity twice so
   `00003_tutor_timestamps.sql` is exercised beneath feature 7 migration `00004`, then reapply every
   service. Expect every step to succeed. This covers AC-2.
3. [x] Run `task generate:sql`, then inspect
   `git diff -- services/*/internal/store/sqlcgen`. Expect no generated change. Run `task build` and
   expect all five production binaries to build. This covers AC-5.
4. [x] Run `task test` with Postgres and Redpanda live and with the Go test result cache cleared.
   Expect every module, every store package, and `test/model` to pass with no skip. This covers
   AC-1 through AC-12.
5. [x] Run `task check`. Expect Go format and lint, Biome, and TypeScript checks to pass. This covers
   AC-5.

## Store behavior

The tests named below are the observable query and broker surface for this schema feature.

1. [x] Deliver the same projection facts twice, then replay the topic from its earliest offset.
   Expect one row per event key, identical billable results, and no change to invoices, invoice
   lines, number counters, billing runs, digest runs, or profile fields. Compare projection keys and
   business columns rather than bookkeeping timestamps. This covers AC-7, AC-8, and AC-9.
2. [x] Join, leave, then rejoin one student. Expect two periods with exactly one open row. Expect the
   membership gap not to bill. This covers AC-11.
3. [x] Move a Present session into another month. Expect it to leave the first period and enter the
   second based only on `local_date`. This covers AC-9.
4. [x] Compare Present, Absent, and unmarked attendance. Expect only Present to bill. Expect no
   invoice number to be spent when nothing bills. This covers AC-1, AC-8, and AC-9.
5. [x] Change an invoice profile after issue. Expect the stored payee block to remain byte for byte
   unchanged. This covers AC-8.
6. [x] Blank each invoice profile field in turn. Expect `is_complete` and the Go missing field list
   to become complete together. This covers AC-12.
7. [x] Count roster membership on its first day, leave day, a gap, and a one day membership. Expect
   the inclusive coverage predicate to decide both billing and digest results. This covers AC-9 and
   AC-11.
8. [x] Run the tenant scoped reads with another tutor ID. Expect no row from teaching, billing, or
   notifications. Confirm every handwritten query names `tutor_id` in its filter and every projection
   join matches it. Attempt a teaching and billing child write using tutor A with a parent id owned by
   tutor B. Expect the database to reject both. This covers AC-4 and AC-6.
9. [x] Apply a projection insert and update. Expect `recorded_at` to remain the first consumer
   transaction time and `updated_at` to reflect the applied update. Expect replay safety to ignore
   both while still comparing every key and business column. This covers AC-3 and AC-7.
10. [x] Deliver an event whose payload `tutor_id` conflicts with the envelope. Expect the consumer
    to use the envelope tenant and never write a row for the payload tenant. This covers AC-4.

## Deferred behavior checks

These decisions stay in the Value sourcing table in `index.md`. They are not feature 4 acceptance
steps because their production actions do not exist yet. The feature that builds each action must
place the corresponding runtime check in its own `verify.md`.

1. Session schedule and move derive `local_date` from `starts_at` in the verified token timezone.
   Feature 10 stores the recurrence timezone before a background scheduler creates sessions.
2. Projection end times come from each event envelope `occurred_at`.
3. Class archive time comes from the teaching transaction clock.
4. A teaching rate change updates the class and inserts its outbox event in one transaction.
5. Class creation and rate change event fields both write `class_rates.effective_from`.
6. An invoice number uses the billed `period_year`, even when issue happens in another year.
7. Issued invoice and line names remain frozen after projected names change.
8. A billing profile handler seeds the empty profile idempotently before read or update, so an
   identity event still in flight returns an incomplete profile rather than not found.
9. The profile screen and month end refusal read `is_complete` rather than implementing another
   completeness predicate.
10. A void raises the next billing run generation by one, and concurrent reissue attempts resolve to
   one winner.
11. A reissue reads current projections while the voided invoice keeps its frozen values.
12. Each digest local date comes from that recipient's timezone.

## Acceptance criteria coverage

The command checks and store behavior checks cover AC-1 through AC-12 for the schema and query surface
feature 4 builds. Deferred behavior checks preserve later design obligations and do not participate in
this feature's verdict.
