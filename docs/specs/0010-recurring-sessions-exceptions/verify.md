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
