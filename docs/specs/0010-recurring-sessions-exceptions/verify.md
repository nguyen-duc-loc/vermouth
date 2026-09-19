# Verify: Recurring sessions and exceptions · spec 0010 · updated 2026-09-19

_Steps derived from spec 0010 acceptance criteria. `$check verify` runs these; `$test` locks the durable ones._

## API and data

- [ ] Create an owned class with one standalone Sunday session, then put a Sunday and Monday schedule on revision 0. Expect revision 1, three candidates, two created sessions, one adopted session, and the original standalone identifier as the first upcoming session. → AC-1, AC-3, AC-11
- [ ] Inspect the adopted session. Expect its rule identifier to match the new rule, its original occurrence date to come from the matching candidate, its version to increment once, and no second scheduled event for that identifier. → AC-3
- [ ] Repeat the put with all seven weekdays from 2024-02-29 through 2026-02-28. Expect 731 candidates in weekday order, and reject 2026-03-01 as beyond the clamped two year bound. → AC-1, AC-9
- [ ] Create a rule in `America/New_York` across the spring clock gap. Expect a missing time to move forward by the exact gap, while the original occurrence date stays unchanged. → AC-10
- [ ] Create a rule in `America/New_York` across the autumn repeated hour. Expect the earlier UTC instant and separate correct start and end offsets. → AC-10
- [ ] Submit duplicate weekdays, zero slots, eight slots, or an end clock no later than its start clock. Expect `400 invalid_input` and no rule, session, receipt, or event. → AC-1, AC-9
- [ ] Put a schedule whose generated time overlaps a different active standalone session. Expect `409`, with the transaction leaving the class revision, rule rows, receipt rows, session rows, and outbox unchanged. → AC-9, AC-11
- [ ] Put the same first schedule concurrently under two command keys at expected revision 0. Expect one revision 1 result and one conflict, with one retained rule version. → AC-9, AC-11
- [ ] Retry the successful command with the same key and sorted equivalent slots after advancing the teaching clock and changing the verified token zone. Expect the exact original status and response snapshot, including identifiers, counts, first upcoming session, command time, and governing zone. → AC-11
- [ ] Reuse the command key with a changed class identifier, expected revision, date, or slot value. Expect `409 idempotency_conflict` without echoing the key or body. → AC-11
- [ ] Use tutor A's token with tutor B's class identifier. Expect the same `404` as a missing class and no receipt, rule, session, or conflict detail. → AC-9
- [ ] Inspect generated sessions. Expect UUIDv7 identifiers, stored rule zone, candidate origin dates, separately resolved UTC endpoints, resolved billing local dates, and the earliest active `(starts_at, session_id)` as first upcoming. → AC-1, AC-10
- [ ] Inspect scheduled outbox rows. Expect one row only for each created session, keyed by `session_id`, with no event for adoption and one transaction time reused for row updates, receipt context, and event occurrence. → AC-3, AC-11

## Commands

- [ ] `task migrate:status` → teaching migrations `00005` and `00006` are applied. → AC-9
- [ ] `docker exec vermouth-postgres-teaching psql -U vermouth_teaching -d vermouth_teaching -X -A -t -c "SELECT to_regclass('schedule_rules'), to_regclass('schedule_slots'), EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'command_receipts' AND column_name = 'response_snapshot'), EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sessions_active_time_no_overlap'), EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'schedule_rules_active_dates_no_overlap');"` → both tables and all three checks return present or true. → AC-9, AC-11
- [ ] `task test` → every Go module and the real Postgres integration tests pass. → AC-1, AC-3, AC-9, AC-10, AC-11
- [ ] `task check && task build && task web:build` → formatting, lint, Biome, TypeScript, all Go binaries, and the production browser build pass. → AC-1, AC-3, AC-9, AC-10, AC-11

## Acceptance criteria coverage

- AC-1 is covered by the seven weekday, inclusive range, candidate count, and input rejection steps.
- AC-3 is covered by exact standalone adoption, identifier retention, version increment, count separation, and event suppression steps.
- AC-9 is covered by uniqueness, overlap rollback, revision race, tenant scope, migration, and live constraint steps.
- AC-10 is covered by stored zones, spring gap, autumn ambiguity, original date, billing date, UTC endpoint, and offset steps.
- AC-11 is covered by class locking, command identity, immutable context and response snapshots, exact replay, timestamp reuse, and conflict steps.

## Rule replacement and ending

- [ ] Replace an active rule from a future local date. Expect the old version to close on the preceding date, the new rule to capture the current verified token zone, the class revision to increment once, and both versions to remain readable. → AC-4, AC-10, AC-11
- [ ] Move one future occurrence and tutor cancel another across two retained rule versions, then replace from an origin date covering both. Expect both rows to remain unchanged, both origin dates to suppress regeneration, and `preserved_count` to equal the retained exception row count. → AC-4, AC-7
- [ ] Inspect the replacement result. Expect `candidate_count` before suppression, disjoint created and adopted counts, the exact superseded and preserved row counts, and the earliest active owned class session as `first_session`. → AC-2, AC-3, AC-4
- [ ] Replace a planned latest rule before its start and exactly on its start. Expect that rule to retire without an invalid range, an overlapping predecessor to close when needed, and one new monotonic class revision. → AC-4, AC-9
- [ ] Replace after a completed rule ends. Expect the completed rule to remain unchanged and the date gap to remain intentional. → AC-4
- [ ] End an active rule with an inclusive last date. Expect its stored zone to govern today, `ended_at` and `updated_at` to share the command instant, later untouched sessions to become replaced, and explicit exceptions to remain. → AC-5, AC-10
- [ ] End a planned latest rule before it starts. Expect the latest rule to retire, an overlapping predecessor to shorten when applicable, and no replacement rule to be created. → AC-5
- [ ] Retry successful replace and end commands after changing the clock, token zone, and current rule state. Expect the original status, command context, and response snapshots. → AC-11
- [ ] Submit replace or end with a stale expected revision. Expect `409 stale_schedule` with the owned class identifier, current revision, and nullable latest rule summary, with no receipt or write. → AC-11, AC-13
- [ ] Attempt an invalid rule transition. Expect `409 invalid_schedule_state` with the current owned state and allowed actions. → AC-4, AC-5, AC-13
- [ ] Inspect outbox rows from replace and end. Expect one `teaching.session.cancelled` fact per newly superseded session, keyed by `session_id`, in the same transaction as the rule and receipt changes. → AC-4, AC-5, AC-16

## Session exceptions

- [ ] Move an active recurring session twice. Expect the source rule zone and rule `valid_from` to govern both moves, the identifier and attendance to remain stable, the billing local date to update, and the version to increment once per move. → AC-6, AC-10
- [ ] Move a standalone session after changing the verified token zone. Expect the current token zone to govern the local clocks and the immutable original local date to anchor the two year interval. → AC-6, AC-10
- [ ] Move from a 29 February anchor to a non leap year boundary. Expect the inclusive anniversary bound to clamp to 28 February. Reject one day outside either bound. → AC-6
- [ ] Move through a forward clock gap and a backward repeated hour. Expect the exact gap shift, the earlier ambiguous instant, separate start and end offsets, and rejection when normalization inverts the interval or leaves the local date. → AC-6, AC-10
- [ ] Cancel an active session that already has attendance. Expect attendance to remain, the session version to increment, and billing plus notifications to become inactive after `teaching.session.cancelled`. → AC-7, AC-16
- [ ] Restore a tutor cancelled moved session. Expect cancellation to clear, the moved marker and stored current time to remain, the same identifier to publish `teaching.session.scheduled`, and both projections to reactivate. → AC-7, AC-16
- [ ] Retry move, cancel, and restore with their original keys after later edits. Expect the exact original canonical session response and captured display zone. → AC-11
- [ ] Reuse a session command key with changed input. Expect `409 idempotency_conflict` naming only the operation and path resource identifier. → AC-11, AC-13
- [ ] Submit a stale session version. Expect `409 stale_session` with the current owned canonical session. Submit a terminal or otherwise invalid transition and expect `409 invalid_session_state` with state, version, and allowed actions. → AC-7, AC-11, AC-13
- [ ] Race a move and a restore into one occupied interval. Expect the database constraint to select one winner and every loser to return `409 session_overlap` with the earliest owned conflicting row formatted in the request display zone. → AC-8
- [ ] Remove the selected conflicting row between rollback and recovery. Expect one bounded full command retry, then nullable conflict row fields rather than an untyped failure. → AC-8
- [ ] Use tutor A with tutor B's session identifier or overlapping row. Expect the same `404` as a missing session and no foreign class label, time, receipt, write, or event. → AC-13
- [ ] Issue an invoice, then move, cancel, restore, or replace one contributing session. Expect the issued invoice to remain unchanged while future calculation inputs converge. → AC-16

## Calendar and browser

- [ ] Read one Day, seven Monday first Week dates, and a 42 date Month window in two verified token zones. Expect UTC selection boundaries to follow local midnights while display dates, clocks, and offsets follow the request zone. → AC-12
- [ ] Filter by repeated class identifiers in different orders. Expect sorted cache identity, classes ordered by normalized name and identifier, sessions ordered by start and identifier, and a foreign identifier to match no owned session. → AC-12, AC-13
- [ ] Read a window where the latest rule lies outside the visible dates and where returned sessions reference older rules. Expect the intersecting rules, each returned class latest rule, and every referenced source rule. → AC-12
- [ ] Request replaced history with a limit below 50 and walk every cursor page. Expect stable origin date and session identifier order with no duplicate or gap. → AC-12
- [ ] Reuse a history cursor after changing tutor, window, sorted filters, limit, or ordering version. Expect `400 invalid_input`. → AC-12, AC-13
- [ ] Inspect a calendar session whose source zone, display zone, and billing local date differ. Expect each fact to keep its separate label and source, with class name beside its class color and text plus an icon for exception state. → AC-12, AC-15
- [ ] Reload `/schedule` with Day, Week, Month, date, and class search values. Expect validated URL state to restore the same view, every week to begin Monday, and invalid values to fall back safely. → AC-14
- [ ] At phone width, expect a compact seven date strip and chronological Day agenda. At desktop width, expect the compact month control, class filters, and seven column Week view without unrelated dashboard chrome. → AC-14
- [ ] Open Add schedule, choose an existing class, select one through seven weekdays, and vary dates and clocks. Expect the provisional candidate estimate to change from explicit form values and the committed summary to replace it with authoritative counts. → AC-1, AC-14
- [ ] Open Manage schedule. Expect the latest class revision, rule, slots, and stored zone. Replace or end it and expect the confirmation copy to explain that explicit exceptions remain. → AC-4, AC-5, AC-15
- [ ] Select an active, cancelled, moved, and replaced session. Expect the details sheet to show rule origin, current time, source zone, billing date, state, and only the valid Move, Cancel, or Restore actions. → AC-6, AC-7, AC-15
- [ ] Complete each calendar and sheet flow using only the keyboard. Expect visible focus, focus trapping and return, 44 pixel targets, labelled fields, polite success announcements, alert errors, and usable content at 200 percent zoom in both themes. → AC-15
- [ ] Exercise loading, empty, validation, stale conflict, overlap conflict, retry, and paged history states. Expect each state to explain the next available action while retaining mutation values and command keys for deliberate retry. → AC-11, AC-15
- [ ] Change one session so it leaves the visible window. Expect every filtered and unfiltered schedule query for that tutor, zone, and affected class to invalidate, with no optimistic invented time. → AC-12, AC-15

## End to end evidence

- [ ] `task schedule:conflicts` → prints `[]` in stable JSON order against the migrated teaching database and performs no write. → AC-8, AC-9
- [ ] Apply every teaching migration to an empty temporary Postgres database, confirm `schedule_rules`, `schedule_slots`, and both exclusion constraints are live, then migrate back to version 0. Expect every step to succeed cleanly. → AC-9
- [ ] `task thread:host` → adopts the first session into a weekly rule, moves it, cancels it, restores it, marks attendance, and reports billing convergence through the real gateway and Redpanda. → AC-1, AC-3, AC-6, AC-7, AC-16, AC-17
- [ ] Deliver scheduled, moved, cancelled, and restoration scheduled facts twice, then reset offsets and handled rows and replay. Expect one correct billing row and one correct notifications row for the stable session identifier. → AC-7, AC-16
- [ ] `task generate && task web:generate && git diff --check` → generated sqlc, gateway, and browser types match their SQL and OpenAPI sources with no hand written drift. → AC-17
- [ ] `task check && task test && (cd web && pnpm exec vitest run) && task build && task web:build` → every repository gate, real database test, component test, binary build, and production browser build passes. → AC-1 through AC-17
