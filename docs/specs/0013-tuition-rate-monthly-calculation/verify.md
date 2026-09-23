# Verify: Tuition rate and monthly calculation · spec 0013 · updated 2026-09-22

_Steps derived from spec 0013 acceptance criteria. `$check verify` runs these. `$test` locks the durable ones._

## UI and manual

1. [x] Open one active class in two tutor timezones near a local day boundary. Try rate dates on today and tomorrow. Expect today to be accepted and tomorrow to be refused using the verified timezone. → AC-1
2. [x] Open an active class with retained sessions, an active class without retained sessions, an archived class with retained sessions, and an archived class without retained sessions. Expect each recorded allowed range, and expect `archived_class_has_no_sessions` for the last case. → AC-1, AC-3
3. [x] Save the same amount twice with different idempotency keys, save a backdated correction, then repeat one original key. Expect every new command to advance the revision, the backdated correction not to replace a later current rate, and the receipt retry to return the original response without a new event. → AC-2
4. [x] Inspect the published rate facts. Expect trusted `tutor_id`, the class partition key, VND, effective date, integer amount, and the committed revision. → AC-2, AC-23
5. [x] Pause billing consumption after a rate save. Expect the class page to show syncing until the exact effective date row reaches the returned revision. Stop billing entirely and expect current teaching state with unavailable history. → AC-19, AC-21
6. [x] Change the tutor timezone across a month boundary and open Billing without search values. Expect the server date, verified timezone, and most recent completed month to replace the URL search values. → AC-4
7. [x] Reset certification, change the teaching topic identity, and change its partition set in isolated test infrastructure. Expect preview and issue to refuse until a retained history replay certifies the exact live topic and partitions. → AC-8, AC-24
8. [x] Capture a preview barrier, publish more events, then allow the consumer to reach the original captured ends. Expect the request to complete without chasing the later topic tip. → AC-8
9. [x] Park one failure with the tutor identifier and one unreadable failure without it. Expect the first to block only its tutor and the second to block every tutor, with safe source coordinates and no payload or raw error. → AC-9, AC-10, AC-23
10. [x] Preview sessions on roster start, roster end, rejoin, and outside roster coverage dates. Expect inclusive coverage at both ends and no candidate outside coverage. → AC-5
11. [x] Preview Present, Absent, and unmarked attendance. Expect a line for Present, exclusion for Absent, and an attendance blocker with a recovery destination for unmarked. → AC-5, AC-7
12. [x] Preview sessions before and after a rate change, one zero rate session, and one Present session without a rate. Expect the newest applicable rate, a retained zero line, and a rate blocker rather than a silent zero. → AC-6, AC-7
13. [x] Remove a projected class label and a projected student label in isolated test data. Expect `projection_incomplete` with owned identifiers and no substituted label. → AC-11
14. [x] Remove each required invoice profile field in turn. Expect its stable field blocker and the `/profile` recovery destination. → AC-7
15. [x] Preview exactly 500 students and 10,000 candidates, then exceed each limit. Expect the boundary case to calculate and the larger case to return `period_too_large` without a write. → AC-20
16. [x] Run the canonical fingerprint golden vector containing Vietnamese text, explicit nulls, a zero rate, ordered candidates, blockers, and totals. Expect the fixed digest. → AC-12, AC-24
17. [x] Issue a month, then issue it again. Expect generation 1 and the same run without broker access or another number allocation. → AC-16, AC-17, AC-18, AC-19
18. [x] Inspect new run, invoice, line, and event identifiers. Expect UUIDv7 values generated inside billing. → AC-14
19. [x] Issue invoices in different period years and exercise a counter beyond 9999 in isolated data. Expect one never reused sequence per tutor and period year, with at least four digits and natural expansion. → AC-15
20. [x] Compare the run creation time, invoice issue times, and issued event occurrence times. Expect the one transaction timestamp throughout the run. → AC-14, AC-15
21. [x] Change rate, student name, class name, profile, attendance, session date, and cancellation after issue. Expect every frozen invoice field and total to remain unchanged. → AC-13, AC-15
22. [x] Inspect `billing.invoice.issued`. Expect the frozen invoice fields and JSON null for `pdf_location`. → AC-14
23. [x] Attempt class rate, preview, issue, and period reads with another tutor. Expect no private rate, student, profile, run, invoice, or line data. → AC-1, AC-21, AC-23
24. [x] Drop the successful issue response, reload the period, then repeat issue. Expect the committed run and the same immutable invoices with no duplicate. → AC-17, AC-19

## Commands

1. [x] `task migrate:up` → every additive recovery, rate revision, receipt, and invoice constraint migration applies successfully. → AC-2, AC-8, AC-14, AC-16, AC-24
2. [x] `task replay:billing:teaching` followed by `task consumer:certify -- billing teaching` → the projection is cleared, rebuilt to the captured ends, and certified for the live topic identity and partition set. → AC-8, AC-9, AC-10, AC-24
3. [x] `task generate` and `task web:generate` → generated SQL, gateway Go types, and browser types contain no diff after regeneration. → AC-21
4. [x] `task check` → Go formatting and lint, Biome, and strict TypeScript all pass. → AC-21, AC-22, AC-23
5. [x] `task test` → every Go module and live schema guard passes. → AC-1 to AC-24
6. [x] `cd web && corepack pnpm exec vitest run` → the browser component suite passes, including billing ready, blocked, and issued states. → AC-4, AC-21, AC-22
7. [x] `task build` and `task web:build` → every production Go binary and the production browser bundle compile. → AC-21
8. [x] `task thread:host` → one dated rate reaches billing, stale preview is refused, the refreshed preview issues once, retry returns the same run, and a later rate leaves the invoice unchanged. → AC-1, AC-2, AC-5, AC-6, AC-8, AC-12 to AC-19, AC-21, AC-23, AC-24

## Acceptance criteria coverage

AC-1 is covered by UI steps 1 to 4 and 23. AC-2 is covered by UI steps 3 to 5 and command steps 1 and 8. AC-3 is covered by UI step 2. AC-4 is covered by UI step 6 and command steps 6 and 8. AC-5 is covered by UI steps 10 and 11 and command step 8. AC-6 is covered by UI steps 12 and 16 and command step 8. AC-7 is covered by UI steps 11, 12, and 14. AC-8 is covered by UI steps 7 and 8 and command steps 1, 2, and 8. AC-9 is covered by UI step 9 and command step 2. AC-10 is covered by UI step 9 and command step 2. AC-11 is covered by UI step 13. AC-12 is covered by UI step 16 and command step 8. AC-13 is covered by UI step 21 and command step 8. AC-14 is covered by UI steps 18, 20, and 22 and command steps 1 and 8. AC-15 is covered by UI steps 19 to 21 and command step 8. AC-16 is covered by UI step 17 and command steps 1 and 8. AC-17 is covered by UI steps 17 and 24 and command step 8. AC-18 is covered by UI step 17. AC-19 is covered by UI steps 5, 17, and 24 and command step 8. AC-20 is covered by UI step 15. AC-21 is covered by UI steps 5 and 23 and command steps 3 to 8. AC-22 is covered by command steps 4 and 6 plus the manual keyboard, focus, phone width, and 200 percent zoom pass. AC-23 is covered by UI steps 4, 9, and 23 and command steps 4 and 8. AC-24 is covered by UI steps 7, 9, and 16 and command steps 1, 2, 5, and 8.
