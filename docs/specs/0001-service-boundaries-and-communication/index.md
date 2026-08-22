# 0001. Service boundaries and event driven communication

**Date**: 2026-08-22
**Status**: Accepted

## Summary

Vermouth is split into four services (`identity`, `teaching`, `billing`, `notifications`) behind one gateway, each with its own database that nobody else reads. Services never call each other directly. They learn about each other only through events on a message broker (a queue that carries facts between programs), and each keeps the small local copy of other people's data it needs to do its own job alone. That is what lets month end billing run while `teaching` is restarting, and it is the tradeoff this project exists to learn: you gain independence and pay in duplicated fields plus data that catches up a moment later. This spec fixes the boundaries, the events, the two hard flows, and the rules every service obeys. It picks no tools; feature 2 does that, against the requirements listed here.

## Requirements

This is a decision spec, so it carries no build tasks and no acceptance criteria of its own. What it carries instead is a set of invariants: rules every later feature must satisfy, and the first thing to check when a later design feels awkward.

- **INV-1**: No service calls another service. Only the gateway calls services. A service reaches another service's data only through events it consumed earlier.
- **INV-2**: Each service owns its own database. No shared schema, no cross database read, no shared ORM models.
- **INV-3**: Any state change other services must know about is written to that service's outbox table inside the same transaction as the business change, and published from there by a relay.
- **INV-4**: Every event carries an envelope: `event_id`, `event_name`, `event_version`, `occurred_at`, `tutor_id`, `key`, `request_id`, plus the named fields in the catalogue below. Every event in the catalogue starts at `event_version` 1, and the number rises only when a change is one a tolerant reader cannot absorb (a field removed, or a field whose meaning changed).
- **INV-5**: Every consumer is idempotent (handling the same message twice changes nothing the second time), enforced by a `handled_events` table keyed by consumer name plus `event_id`.
- **INV-6**: Ordering is guaranteed only per key (`class_id` or `session_id`), never across keys and never system wide.
- **INV-7**: A projection (a local copy built from events) stores the facts as they arrive and derives its answers at read time. No handler computes a running total or overwrites a derived value in arrival order, so ordering across keys never matters.
- **INV-8**: `tutor_id` comes from the verified token and nothing else. No endpoint accepts `tutor_id` as an input, and every query filters by it.
- **INV-9**: An issued invoice never changes. A correction voids it and issues a new invoice with a new number, linked to the voided one.
- **INV-10**: A screen reads a fact from the service that owns it, never from another service's copy.
- **INV-11**: Broker retention is long enough to rebuild any projection from the beginning, and every consumer can be reset to replay from scratch.
- **INV-12**: Consumers are tolerant readers: unknown fields are ignored, and no consumer demands a field that was not in the event on day one. An `event_version` the consumer does not recognise is the one thing it must not absorb: it parks the message in the dead letter queue (INV-13) rather than guess at it.
- **INV-13**: A message that keeps failing is retried a bounded number of times with growing delay, then parked in a dead letter queue with an alert. It never blocks the stream and it is never dropped. Parked messages are replayable.
- **INV-14**: Token verification is local, using a public key the service already holds. No service asks `identity` anything per request.
- **INV-15**: The `request_id` created at the gateway travels through every handler and is copied onto every event that handler publishes.

## Decision

**Chosen option**: Option 2: four event driven services behind an aggregating gateway

Split Vermouth into `identity`, `teaching`, `billing`, and `notifications`, each with its own database; forbid synchronous service to service calls entirely; carry every cross boundary fact as a fat event through the broker; and let the gateway do routing, token verification, and read only aggregation for screens that need more than one service (basis: database per service, plus event carried state transfer so a consumer never needs a follow up question).

## Service and communication design

### The service map

| Service | Owns (one sentence) | Must never own | Holds copies of |
|---|---|---|---|
| `identity` | Proof of who you are: `tutor_id`, email, the linked Google account, display name, timezone, language. No password: spec [0004](../0004-tutor-sign-in-google-oauth/index.md) replaced the password with Google sign in. | Bank details, invoice profile, or anything about teaching or money. | Nothing. |
| `teaching` | What is taught: classes, schedules, sessions, students, roster membership, attendance, and the current tuition rate on a class. | Invoices, money arithmetic, rate history for billing, email sending. | Nothing (see the timezone note below). |
| `billing` | Money: dated rate history, the billable session projection, the tutor's invoice profile and bank details, invoices, invoice lines, invoice numbers, voids, paid state, and the rendered invoice PDF. | Attendance truth, the schedule, student management, email sending. | Student labels, class labels, sessions, roster periods, attendance. |
| `notifications` | Delivery: sending email, digest run records, alert delivery. | Any business truth, schedule generation, invoice content. | Tutor recipient (email, display name, timezone, language), class label plus roster count, today's sessions. |
| gateway | No data. Routing, token verification, `request_id` creation, one error shape, and read only aggregation for screens. | A database, business rules, or any write logic of its own. | Nothing (routing config plus the identity public key). |

Two placements that would otherwise be argued again later:

- The **invoice PDF belongs to `billing`**, not to the future `documents` service. The PDF is the rendered form of an invoice, and `billing` owns invoices. `documents` (feature 18) owns files a tutor uploads against a session.
- **Placement rule for anything new**: a new capability joins the service that owns the thing it hangs off, unless it needs its own storage engine or its own scaling, in which case it becomes its own service. Under that rule `documents` (feature 18) becomes its own service, because it owns object storage, and `search` (feature 19) becomes its own service fed entirely by events. The system therefore tops out at six services.

### Interaction styles, each with its reason

| Interaction | Style | Reason |
|---|---|---|
| Browser to gateway | Synchronous | A person is waiting for an answer. |
| Gateway to one service (command or query) | Synchronous | The gateway is the only caller allowed, and the user is waiting. |
| Gateway read aggregation to two or more services | Synchronous, in parallel, read only | A phone first screen should not pay several round trips. Read only keeps business logic out of the gateway. One slow service degrades only its own panel. |
| Service to service | Never | INV-1. This is the rule the whole design rests on: it is what makes a service independently deployable and restartable. |
| Service to service through the broker | Asynchronous | Every fact that crosses a boundary travels this way, so the publisher does not care whether the consumer is running. |
| Month end billing run | Synchronous inside `billing` | All the data is already local, so the work is a local read. Asynchronous machinery would add polling for nothing. |
| Invoice PDF render and store | Synchronous inside the issue request | The tutor is waiting for the file. |
| Daily digest | Scheduled inside `notifications`, local read, then a retried send | Nothing outside `notifications` needs to be awake at 6:00. |
| Email provider call | Synchronous inside the digest job, retried | It is an outbound call from a background job, not from a user request. |

### Event catalogue

Fat events: each event carries the fields its consumers need, named here. Consumers ignore fields they do not know (INV-12).

**Published by `identity`**

| Event | Key | Fields | Consumers |
|---|---|---|---|
| `identity.tutor.registered` | `tutor_id` | `tutor_id`, `email`, `display_name`, `timezone`, `language` | `billing` creates an empty invoice profile row so month end can fail with "profile incomplete" rather than "unknown tutor". `notifications` creates a recipient row. |
| `identity.tutor.profile.changed` | `tutor_id` | `tutor_id`, `email`, `display_name`, `timezone`, `language` | `notifications` updates the recipient. `billing` ignores it: the invoice profile is written straight to `billing` by the tutor. |

**Published by `teaching`**

| Event | Key | Fields | Consumers |
|---|---|---|---|
| `teaching.class.created` | `class_id` | `class_id`, `tutor_id`, `name`, `rate_amount`, `currency`, `rate_effective_from` | `billing` stores the class label and the first row of rate history. `notifications` stores the class label. |
| `teaching.class.changed` | `class_id` | `class_id`, `tutor_id`, `name` | `billing` and `notifications` update the class label. |
| `teaching.class.rate.changed` | `class_id` | `class_id`, `tutor_id`, `rate_amount`, `currency`, `effective_from` | `billing` appends a rate history row. Never updates an existing one. |
| `teaching.session.scheduled` | `session_id` | `session_id`, `class_id`, `tutor_id`, `starts_at`, `ends_at`, `local_date` | `billing` stores a session row. `notifications` stores a today's session row. |
| `teaching.session.moved` | `session_id` | `session_id`, `class_id`, `tutor_id`, `starts_at`, `ends_at`, `local_date` | `billing` and `notifications` update the session row. A move across a month boundary moves the billing month with it while that month is not yet invoiced; once it is, see flow 1 step 10. |
| `teaching.session.cancelled` | `session_id` | `session_id`, `class_id`, `tutor_id` | `billing` marks the session cancelled, so it is never billable. `notifications` drops it from the digest. |
| `teaching.attendance.marked` | `session_id` | `session_id`, `class_id`, `student_id`, `tutor_id`, `state` (`Present` or `Absent`), `marked_at` | `billing` stores the attendance fact. Only `Present` is billable. |
| `teaching.student.registered` | `student_id` | `student_id`, `tutor_id`, `name` | `billing` stores the student label for invoice lines. The phone number is deliberately not carried: no invoice needs it. |
| `teaching.student.changed` | `student_id` | `student_id`, `tutor_id`, `name` | `billing` updates the label. Already issued invoices keep the name they were rendered with. |
| `teaching.student.removed` | `student_id` | `student_id`, `tutor_id` | `billing` marks the label inactive and never deletes it, because past invoices must still print the name. |
| `teaching.roster.joined` | `class_id` | `class_id`, `student_id`, `tutor_id`, `effective_from` | `billing` opens a roster period. `notifications` increments the class roster count. |
| `teaching.roster.left` | `class_id` | `class_id`, `student_id`, `tutor_id`, `effective_to` | `billing` closes the roster period. `notifications` decrements the count. |

**Published by `billing`**

| Event | Key | Fields | Consumers |
|---|---|---|---|
| `billing.invoice.issued` | `invoice_id` | `invoice_id`, `invoice_number`, `tutor_id`, `student_id`, `period_year`, `period_month`, `total_amount`, `currency`, `issued_at`, `pdf_location` | None today. Published because it is the money fact of the system and later features (an invoice alert, `search`) will want it. |
| `billing.invoice.voided` | `invoice_id` | `invoice_id`, `invoice_number`, `tutor_id`, `voided_at`, `reason` | None today, same reason. |

Paid state and every other purely internal `billing` fact publish nothing until a consumer actually exists. `notifications` publishes nothing; a failed send surfaces through the alert path of feature 9.

### Local copies, and why each one exists

| Service | Copy | Why it cannot be a question asked at read time |
|---|---|---|
| `billing` | Sessions, roster periods, attendance, class labels, student labels, dated rate history | Month end must run entirely on local data, and it must be able to run again months later and produce the same numbers. |
| `notifications` | Recipient (email, display name, timezone, language) | At 6:00 nobody is asking, and there is nobody to ask. |
| `notifications` | Today's sessions, class label, roster count | Same reason. This is the weakest copy in the design: a roster count is a derived number, so it is the one to revisit if it turns out to drift. |
| `teaching` | Nothing | It only ever answers about its own data, and the identity facts it needs arrive in the token. |
| `identity` | Nothing | It is the source of identity, and nothing else about a tutor belongs to it. |

**The timezone note.** `teaching` needs the tutor's timezone to compute a session's `local_date`, and it may neither call `identity` nor keep a copy. The token carries a `tz` claim, so the timezone arrives with the request. `teaching` stores both the instant (`starts_at`) and the computed `local_date`, and publishes both, so `billing` and `notifications` never recompute it. Consequence to accept: changing a timezone later does not retroactively move sessions that already exist.

### Identity propagation

- `identity` signs a token with a private key it alone holds. Claims: `sub` (`tutor_id`), `tz`, `language`, `kid` (which key signed it), `iat`, `exp`. The claim is named `language`, matching the field name on the `identity` events, so one name means one value everywhere. The OIDC convention would call it `locale`; the event field name wins here, because that is the name consumers match on.
- The gateway verifies the token on every request, rejects an unsigned or expired one, and passes the token onward unchanged.
- Each service verifies the signature locally with the public key it holds in configuration, then reads `tutor_id` from the token. Every query filters by it (INV-8). Forwarding a plain trusted header instead was rejected: anything that can reach a service could then impersonate any tutor.
- Token lifetime, refresh, and browser storage belong to feature 7. This spec fixes only the mechanism.

### Writing and publishing safely

- **Outbox**: each service has an `outbox` table. A business change and its events are written in one transaction, so nothing is published that was not saved and nothing saved goes unpublished.
- **Relay**: a small loop in each service reads unsent outbox rows in order, publishes them with `event_id` and `key`, and marks them sent. A crash between publish and mark causes a republish, which INV-5 already makes harmless.
- **Handled events**: each consumer inserts `(consumer_name, event_id)` before it acts, in the same transaction as its own write. A duplicate insert means the message was already handled, so the handler does nothing.
- **Failure**: bounded retries with growing delay, then the dead letter queue plus an alert (feature 9). Billing never stalls because one message is bad.
- **Rebuild**: reset a consumer and replay from retained events (INV-11). This is the intended fix for a projection that a handler bug got wrong, and it is why retention matters more here than it looks.

### Flow 1: month end invoice generation (features 13, 14, 15)

**What a period is**, since every step below turns on it: a period is the pair `(period_year, period_month)`, and a session belongs to the period of its `local_date`, never of its `starts_at` instant. For nearly every session those agree; they differ for one starting late on the last evening of a month. `local_date` is what decides the bill everywhere: the scan in step 4, the `period_year` and `period_month` on `billing.invoice.issued`, and the month shown on the screen. `M` below is one such period.

1. The tutor opens the month end screen. The gateway reads existing invoices for that month from `billing`. Nothing else is consulted.
2. The tutor presses Generate for period `M`. The gateway verifies the token, creates a `request_id`, and sends one command to `billing`.
3. `billing` looks for a run keyed by `(tutor_id, period, generation)` at the current allowed generation for that period. If it exists, it returns that run and its invoices unchanged. Pressing twice can never produce a second invoice.

   **How the generation is stored.** `billing` keeps one `billing_run` row per attempt, holding `tutor_id`, `period_year`, `period_month`, an integer `generation`, `created_at`, and a `superseded_at` that stays empty until a void (step 9), with a unique constraint on `(tutor_id, period_year, period_month, generation)`. The first run for a period is inserted at `generation` 1. The current allowed generation for a period is: 1 when no row exists, the highest `generation` present while its `superseded_at` is empty, and that highest `generation` plus 1 once it is set. So a live run absorbs every further press, and only a void opens the next generation. The unique constraint, not the lookup above it, is what actually makes a double press safe: two presses arriving together both try to insert the same key, and one of them loses.
4. Otherwise, in one transaction, `billing` reads only its own tables: sessions whose `local_date` falls in `M` and that are not cancelled, the roster period covering each session's `local_date`, attendance rows with state `Present`, and the rate in force on that `local_date` (the newest rate history row whose `effective_from` is on or before it).
5. It groups by student, creates one invoice per student with the next invoice number for that tutor, writes the lines (one per attended session: date, class name, rate), writes the run row, and writes `billing.invoice.issued` to the outbox.
6. **It refuses the whole run, writing nothing, if** the invoice profile is incomplete, or any `Present` session has no rate in force on its date. On money, failing loudly beats billing a silent zero. The refusal reaches the tutor through the gateway error shape, with `code` set to `profile_incomplete` (message naming the missing fields) or `rate_missing` (message naming the offending `session_id` and its date), so the screen can send them straight to the screen that fixes it. Nothing is written: no invoice, no `billing_run` row, no event.
7. Each invoice is rendered to PDF and stored in object storage, and the location is saved on the invoice. If rendering fails, the invoice still exists and a re render is available: the invoice is the money truth, the file is a rendering of it.
8. The relay publishes the issued events.
9. **Correction after issue (void and reissue).** The tutor voids invoice `N`, with a reason. The invoice stays in the record as voided forever. In the same transaction, the void stamps `superseded_at` on the newest `billing_run` row for that period, which raises the allowed generation by exactly one and so permits exactly one further run. That run covers only the students who now have a voided invoice and no live one. Other students keep the invoice they were already sent. The new invoice takes a new number and links to the one it replaces. Numbers are never reused.
10. **A session moved across a month boundary after a month was invoiced** is an ordinary correction after issue, not a special case. `billing` updates its local session row from `teaching.session.moved` as always, and the issued invoices for both the month it left and the month it joined stay exactly as they were sent (INV-9). The numbers move only when the tutor voids and reruns, and a rerun of either month then reads the session where it now sits.

Corrected attendance reaches `billing` as an ordinary `teaching.attendance.marked` event, so by the time the reissue runs, the local facts are already right.

### Flow 2: the daily 6:00 digest (feature 17)

1. A scheduler inside `notifications` ticks on a short interval (every 15 minutes, not once a day) because 6:00 is local to each tutor.
2. For each recipient whose local time has just passed 6:00 and who has no digest record for that local date, it reads its own tables: today's sessions for that `local_date`, ordered by start time, with class name and roster count.
3. It inserts the digest record `(tutor_id, local_date)` under a unique constraint before sending. That constraint, not care, is what makes one digest per tutor per day true.
4. No sessions means the record is written and no email is sent.
5. It sends through the email provider, retrying with growing delay on failure. A final failure marks the record failed and raises an alert (feature 9); a replay is a deliberate action, never an endless loop.
6. Every read is local, so a digest still goes out while `teaching` or `identity` is restarting.

### Value sourcing for the hard flows

| Action | Value produced or displayed | Source |
|---|---|---|
| Month end run | Billable session list | `billing` local sessions plus roster periods plus attendance, all from `teaching` events |
| Month end run | Rate applied to a session | `billing` rate history row in force on the session `local_date`, from `teaching.class.rate.changed` |
| Month end run | Session `local_date` | Computed once by `teaching` from the token `tz` claim, carried on the session event |
| Month end run | Student name on a line | `billing` student label copy, frozen onto the invoice line at issue |
| Month end run | Class name on a line | `billing` class label copy, frozen onto the invoice line at issue |
| Month end run | Invoice number | `billing` per tutor sequence, formatted `<year>-<4 digit sequence>`, never reused |
| Month end run | Total amount | Sum of integer VND line amounts. VND is stored as a whole number, so no rounding rule is needed |
| Month end run | Currency | Fixed VND (decided up front in the scope) |
| Month end run | The period a session is billed in | The session `local_date`, carried on the `teaching` session events. A period is `(period_year, period_month)` taken from that date, never from `starts_at` |
| Month end run | Idempotency of the run | Unique constraint on `(tutor_id, period_year, period_month, generation)` in `billing_run`. Generation starts at 1 and rises only when a void stamps `superseded_at` on the newest run |
| Invoice PDF | Tutor legal name, contact line, bank name, account number, account holder | `billing` invoice profile, owned by `billing` and written by the tutor on the feature 12 screen |
| Invoice PDF | Stored file location | Object storage path written back onto the invoice row |
| Digest | Recipient email, display name, language | `notifications` recipient copy, from `identity` events |
| Digest | The tutor's local date and the 6:00 boundary | `notifications` recipient `timezone` copy, from `identity` events |
| Digest | Session times and class names | `notifications` today's sessions and class label copies, from `teaching` events |
| Digest | Student count per session | `notifications` roster count per class, from roster events |
| Digest | One send per tutor per day | Unique constraint on `(tutor_id, local_date)` in the digest record |
| Any request | `tutor_id` | The `sub` claim of the verified token, never an input |

### The contract every service obeys

- `GET /health` for liveness, and a readiness check that also reports its database and broker connection.
- `X-Request-Id`: created at the gateway if absent, logged by every handler, and copied into the `request_id` field of every event published while handling it. This is what makes feature 9 possible across the broker, which is why it is fixed here rather than later.
- One error shape at the gateway boundary: `{ "error": { "code": "...", "message": "...", "request_id": "..." } }`. Internal service errors are mapped to it, never leaked raw.
- Configuration comes from the environment only. No configuration baked into an image, and secrets (private key, database URL, broker URL, email credentials) arrive the same way.
- Every service holds the identity public key in configuration, keyed by `kid`, so a key can be rotated by adding the new one before switching.

### What this requires of feature 2 (capabilities, not products)

Feature 2 chooses the tools. These are the properties it must satisfy, and any candidate that misses one is the wrong choice here.

| Need | Required property |
|---|---|
| Message broker | Ordering per key, independent consumer groups per service, retention long enough to rebuild a projection from the beginning, consumer offset reset, and a dead letter destination with replay |
| Database | One relational database per service, with transactions strong enough for the outbox pattern and unique constraints for idempotency |
| Gateway | Verifies a signed token, creates and forwards `X-Request-Id`, and can fan out a read to two or more services in parallel |
| Object storage | Stores invoice PDFs, addressable by path, reachable only through the owning service |
| Scheduling | An in process scheduler inside `notifications` with a database lock, rather than a cluster level cron, so the digest logic stays testable locally |
| Language and framework | Whatever it picks must make an outbox, a relay, and a consumer with a handled events table straightforward. Four services means this boilerplate is written four times |

### How this design lands across the scope

Ordering only, in the project's Tracer Bullet spirit (prove the whole pipe before thickening any part). It adds no tasks to any feature.

1. Features 2 and 5 stand up the pipe: gateway, one service, its database, the broker, all reachable.
2. Feature 7 adds the token and local verification, so the identity rule is real before any business data exists.
3. Feature 8 runs the first end to end thread: one write in `teaching`, one event through the outbox and relay, one projection updated in another service, one aggregated read on the home screen. This is where the whole design is proved or found wanting.
4. Feature 9 lands early on purpose: without the `request_id` crossing the broker, everything after this is debugged blind.
5. Slices 3 and 4 thicken the same pipe. Nothing after feature 8 introduces a new interaction style.

## Consequences

**Positive**:
- Each service deploys, restarts, and fails alone. Month end billing and the 6:00 digest both work while other services are down.
- Month end is a local read, so it is fast, repeatable, and produces the same numbers when run again later.
- A projection that goes wrong is repaired by replay rather than by hand.
- The boundaries match the way the product is actually described, so a change to teaching rarely touches money.
- It teaches the architecture properly, including the parts that are usually skipped: the outbox, idempotency, and rebuilds.

**Negative / tradeoffs**:
- Real boilerplate before any feature exists: four databases, four outbox tables plus relays, four handled events tables, one migration set per service.
- Eventual consistency is now visible. A student added moments before a month end run may not be in `billing` yet, and tests must wait for a projection rather than assert immediately.
- Duplicated fields must be maintained deliberately, and every new consumer need means a new field on an event.
- Debugging is genuinely harder until feature 9 exists.
- More moving parts on one Azure VM: four services, four databases, a broker, a gateway.
- The `notifications` roster count is a derived value held as a copy, the least comfortable part of the design.
- A timezone change does not retroactively move sessions that already exist.

**Neutral**:
- New patterns to learn: transactional outbox, idempotent consumer, dated rate history, projection rebuild, dead letter replay.
- Invoice numbering is per tutor and gap free only in the sense that numbers are never reused; a void leaves a number spent.
- The service count is capped at six by the placement rule, so features 18 and 19 do not reopen this decision.

## Follow-up

- [ ] Feature 4 (data model and data ownership) inherits the ownership map and the copies table above; it should place every entity accordingly and name each duplicated field with the event that carries it, rather than deciding ownership again.
- [ ] Feature 2 must satisfy the capability table above; if the chosen broker cannot replay or cannot park a poison message, INV-11 and INV-13 fail and this spec needs revisiting.
- [ ] Feature 5 must run the broker locally with retention and a dead letter destination, not a minimal in memory mode.
- [ ] Feature 9 depends on INV-15 and should be treated as a prerequisite for slice 3, not an optional extra.
- [ ] Feature 7 owns token lifetime, refresh, and browser storage; this spec fixes only claims and local verification.
- [ ] Feature 13 must implement the run key with a generation, and feature 15 must implement void with a reason as a deliberate action.
- [ ] Feature 14 renders and stores the invoice PDF inside `billing`; the future `documents` service is only for tutor uploaded session files.
- [ ] There is no `AGENTS.md` yet. Once feature 2 exists, feature 3 (or `/audit`) should record the service contract, the event naming convention, and the outbox and consumer patterns so every service is written the same way.
- [ ] No technology community skills are installed yet. After feature 2 picks the stack, install the matching framework and database skills so later features get specific guidance.
- [ ] Revisit the `notifications` roster count if it drifts. The alternative is a session level snapshot of the count carried on the session events.

## Rationale

Reasoning, the options weighed, and the references: see [rationale.md](rationale.md).
