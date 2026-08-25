# 0003. Data model and data ownership per service, rationale

The build spec is [index.md](index.md). This file holds the reasoning, the options weighed, and what
the decision rests on.

## Context

> ⚠️ Premise note: this spec designs tables for features that are not yet specced. Reaching to
> everything the accepted specs already fix means `invoices`, `billing_runs`, and `digest_runs` land
> now while features 13 to 17 are still planned. The failure mode to avoid is the opposite one though:
> the month end run is the most expensive thing in this system to redo, spec 0001 already pins its
> inputs down to the field, and a projection whose key is wrong is only fixable by a migration plus a
> replay. So the reach is deliberate, with one guard rail: a later feature adds its own migration
> rather than editing one of these four, and a real change to this model routes back through
> `$architect`. Two smaller notes. This spec refines spec 0001's wording in two places rather than
> only applying it: the `notifications` roster count becomes membership rows counted at read time, and
> the run table is written `billing_runs`. Both are recorded in Follow-up so the two specs do not
> disagree. And spec 0001's catalogue leaves three values without a source (which roster period a
> `roster.left` closes, when a removal or a cancellation happened, and the year part of an invoice
> number); all three are resolved in the Value sourcing table rather than left for the build to invent.

Spec 0001 decided who owns what, in sentences. It named four services, the events between them, the
copy each service keeps, and it walked both hard flows step by step. What it did not do is turn any of
that into tables. Today the repository holds three real tables: `identity.tutors`,
`notifications.recipients`, and the shared kit (`outbox`, `handled_events`) copied into all four
services. Two `db/queries/README.md` files sit empty saying feature 4 decides the entities.

Four forces shape the choices below.

**A replay is the repair tool, so the schema has to make replay safe.** INV-11 and STK-22 make
resetting a consumer and replaying from the beginning the intended fix for a projection a handler bug
got wrong. That is only true if a replayed event lands on the row it landed on the first time, and if
nothing authoritative is in reach of a consumer. Both of those are schema properties, not handler
discipline: they are decided by what the primary key is and by which package writes which table.

**Month end has to produce the same numbers again months later.** The run reads only local tables, so
every input it needs must be a row in `billing`, and every value it renders onto an issued invoice must
survive a later edit to the thing it came from. A rate that changes, a student who is renamed, a bank
account that moves: none of them may reach backwards into an invoice already sent (INV-9).

**Two write paths into one database.** In `billing` and `notifications`, consumers write copies while
handlers write records, into the same Postgres instance. Nothing in the code stops a handler touching a
projection or a consumer touching an invoice. If the line between them is not written down per table,
it will be crossed, and the first symptom will be a replay that quietly rewrites money.

**One person, ten to fifteen hours a week, on a small VM.** Nine entities across four databases is
already a lot of surface for a solo build. Anything that adds a moving part has to pay for itself
immediately, and anything that can be a constraint rather than a convention should be, because a
constraint holds while nobody is looking.

## Options considered

### Option 1: one shared schema for the tutor's data

Keep one database with all nine entities in it, and let the four services read it. The events still
flow for the things that need them.

**Pros**:

- No duplicated fields, no eventual consistency, and the month end run is one join.
- Roughly half the migrations and none of the projection tables.

**Cons**:

- It contradicts INV-2 and STK-5 outright, which means superseding spec 0001 rather than implementing
  it, and it throws away the learning goal the project exists for.
- The independence 0001 bought disappears: month end can no longer run while `teaching` is restarting.

### Option 2: one owning table per entity, projections keyed by the id their events carry

Place every entity in the service 0001 gives it. Key each projection table on the identifying field
its own events already carry, so a consumer upsert is idempotent by construction. Write down, per
table, whether it is rebuildable or authoritative. Forbid deletion in favour of a nullable end
timestamp, and count derived numbers at read time.

**Pros**:

- A replay is safe because of the keys, not because of care taken in a handler. Idempotency stops
  being a thing to remember.
- The projection and record split is readable from the migration, so `task replay:` is safe to run
  without reading Go.
- Every invariant that can be a database constraint is one: the partial unique index on the open roster
  period, the unique run generation, the unique invoice number, the unique digest per day.

**Cons**:

- Composite primary keys taken from event fields, rather than surrogate ids, are unusual to read and
  make a future "one more period on the same day" case a migration.
- The duplication is real: `classes` in three databases, `sessions` in three, and the frozen payee
  block repeated on every invoice row.
- The schema reaches ahead of the features that use it.

### Option 3: surrogate keyed projections with stored derived values

Give every projection table its own `uuid` primary key plus a unique index on the natural key, and
store the numbers reads want: a roster count integer on the class, a `billing_month` column on the
session.

**Pros**:

- Familiar shape, and every read is cheaper: no counting, no date range scan.
- A surrogate key survives a change to what identifies the thing, without a migration.

**Cons**:

- The stored numbers are exactly what INV-7 forbids, for a reason this project will actually hit: a
  redelivered `roster.joined` double counts, a replay from the beginning multiplies the count, and a
  `session.moved` across a month boundary leaves `billing_month` wrong. None of those failures
  announces itself.
- The surrogate key buys nothing here, because the consumer only ever has the natural key to upsert on,
  so the unique index does all the work and the id is dead weight.

### Option 4: event sourced billing, no projection tables

Keep no copies in `billing`. Rebuild the state it needs from the retained event log each time the month
end run happens.

**Pros**:

- One source of truth, and no projection can ever be stale or wrong.
- Auditing is free: the log is the whole history.

**Cons**:

- Every month end run becomes a full topic scan, and retention is now load bearing for correctness
  rather than just for repair.
- Invoices are authoritative records that must never be derived, so the log would have to be mixed with
  real tables anyway, which is the complexity of both models and the simplicity of neither.
- It is a much bigger idea to learn on top of the four this project already introduces.

## Rationale

Option 2 wins on the first force. Because a projection's primary key is the identifying field its
events already carry, an upsert is the same row every time, so a duplicate delivery and a full replay
are both harmless before `handled_events` is even consulted (basis: idempotent consumer with an
idempotency key taken from the message, rather than from the consumer's own bookkeeping). That is worth
more here than the readability Option 3's surrogate keys would buy, because the failure Option 3
invites is the silent kind: nothing in a doubled roster count or a stale `billing_month` looks wrong
until an invoice is already sent.

The roster period key is where this pays off most visibly, and it started as a gap. No event carries a
`roster_period_id`, so Option 3's surrogate key would have to be minted by the consumer, which means a
replay mints a second one for the same period. Keying on `(class_id, student_id, effective_from)`,
which every roster event does carry, removes the problem instead of managing it, and the partial unique
index on the open row turns "at most one open period per pair" from a hope into something Postgres
enforces. That index is also what makes `teaching.roster.left` well defined, since it carries
`effective_to` but no `effective_from`.

The second force, a repeatable month end, is why the freezing goes further than the invoice line. The
run refuses unless the profile is complete, so the payee fields are guaranteed present at issue, and
copying them onto the invoice costs five text columns per row. What it buys is that a re render years
later prints the file the tutor actually sent, which is the whole content of INV-9. The alternative,
rendering the payee block from the live profile, would mean a tutor changing bank accounts silently
rewrites every past invoice, which is the exact class of bug the void and reissue rule exists to
prevent (basis: immutable financial documents, corrected by reissue rather than edit).

The third force is why the projection and record split is a table in the spec rather than a sentence in
a handler. `invoice_profiles` is the one row that straddles the line, so it gets the explicit rule: the
consumer inserts with `ON CONFLICT DO NOTHING` and writes no field, which makes a replay able to
recreate a missing row and unable to touch bank details.

The fourth force decided the smaller calls. The completeness gate is a generated column rather than a
predicate written twice, because Postgres recomputes it and two copies of a field list would drift the
first week feature 12 changes a label (basis: spec 0001's flow 1 step 6, where the refusal must name
the missing fields). The invoice number comes from a counter row rather than `max(sequence) + 1`,
because a voided invoice must keep its number spent and a `max` over live invoices would hand it out
again. And nothing is deleted, because attendance, invoices, and past sessions all point at these rows,
and a row that vanishes takes the explanation with it. The honest cost of that last one is a
`WHERE ... IS NULL` on every list query, and a forgotten one shows removed data rather than failing, so
it is a convention the partial indexes support but do not enforce.

## Cross check

A read only critique pass on another model read the finished spec and wrote nothing. Its verdict was
that the model is buildable and that the load bearing part holds: every projection keyed on the id its
own events carry, and the projection versus authoritative split matching spec 0001 without reopening
ownership. What it found was unnamed value sources rather than a design to rethink, and the seven it
named are now closed in the spec: who writes `teaching.classes.rate_amount` on a rate change, what
happens to a roster student with nothing billable, what `effective_to` means on the day itself and the
one predicate both services share, which rate a reissue takes, the generation read that was missing
from the query list, what a missing attendance row means, and how the tutor's local day resolves. It
also caught a real syntax error: `CHECK (voided_at IS NULL) = (void_reason IS NULL)`, which Postgres
will not parse, now written with the outer parentheses it needs.

Two of its findings were checked and not accepted. It asked for a descending index on
`class_rates (class_id, effective_from DESC)` for `RateInForceOn`, but the primary key on that same
pair already serves a backwards scan, so the index would be a duplicate. And it reported that this
file conflates the two foreign key cases, which it does not: there is no foreign key argument here at
all, and the invariant in `index.md` already limits a foreign key to two authoritative tables in the
same context.

## Verification boundary amendment

On 2026-08-25, verification exposed a mismatch between the build boundary and `verify.md`. The value
sourcing table deliberately reaches into future teaching, billing, invoice, and digest actions, but
the verification refresh had made all eighteen rows mandatory for feature 4. That made a completed
schema slice depend on features 8 through 17 even though this spec says those actions are not built
here.

Three treatments were considered. Building every action now would collapse several roadmap features
into this foundation. Removing the future rows would reopen the unnamed value problem when those
features arrive. Keeping the rows as design obligations while gating feature 4 only on its migrations,
typed queries, replay path, and store behavior preserves both boundaries. The third treatment is the
chosen one. Each later feature must verify its row when it builds the action.

The independent cross check then found a separate tenant integrity gap. Filtering every query by
`tutor_id` does not stop a child row for tutor A from referencing a globally unique parent id owned by
tutor B. Composite same service foreign keys close that path at the database boundary. Because the
original model migrations have already run in development, additive teaching and billing migrations
are safer than rewriting history. They preserve data, fail visibly if a mismatch already exists, and
reverse without touching the original tables.

The same pass clarified three sources that the first draft left implicit. Handler tenant identity
comes from the verified token, consumer tenant identity from the event envelope, and scheduler tenant
identity from the selected recipient. Teaching derives a session calendar day from its start instant
in the verified token timezone. Projection bookkeeping timestamps come from the consumer transaction
clock and are excluded from business state replay comparisons.

## References

**Project sources** (verifiable, in this repo):

- `docs/specs/0001-service-boundaries-and-communication/index.md`, the service map and the local copies
  table: the ownership this spec applies rather than reopens, including the placement rule that sends
  `document` to its own service.
- Spec 0001's event catalogue: every projection column name and every copied field traces to a field
  named there, and the three values it leaves unsourced are closed in the Value sourcing table.
- Spec 0001, flow 1: the run key with a generation, the `superseded_at` rule, the two refusal codes,
  and the invoice number format.
- Spec 0001, flow 2: the unique digest per tutor per local date, and the local reads it depends on.
- Spec 0001, INV-2, INV-5, INV-6, INV-7, INV-8, INV-9, INV-11, INV-12: the invariants that decided the
  keys, the constraints, and the projection and record split.
- Spec 0001's follow up asking feature 4 to place every entity against the ownership map and name each
  duplicated field with the event that carries it, and the one asking for the roster count to be
  revisited.
- `docs/specs/0002-stack-and-scaffold/index.md`, STK-3, STK-5, STK-7, STK-15, STK-22, STK-23: hand
  written SQL through `sqlc`, one database per service, `bigint` dong, integration tests against the
  real infrastructure, the replay target, and the single replica rule behind `digest_runs`.
- `docs/scope/scope.md`, feature 4 "Done when": one owning service per entity, a schema no other
  service reads, duplicated fields named and justified, and each migration applying cleanly.
- `docs/scope/scope.md`, the "Decided up front" block: a student is a name and a phone number, one rate
  per session with only `Present` billed, VND only, and the tutor marks an invoice paid by hand.
- `services/identity/db/migrations/00002_tutors.sql` and
  `services/notifications/db/migrations/00002_recipients.sql`: the existing table shape, the
  `recorded_at` and `updated_at` convention on a projection, and both comments deferring the rest to
  feature 4.
- `services/identity/sqlc.yaml`: the `uuid` and `timestamptz` overrides the other services copy.
- `pkg/vermouth/envelope.go` and `pkg/vermouth/catalogue.go`: the envelope fields, including the
  `occurred_at` this spec uses as the source of `removed_at` and `cancelled_at`, and the five key kinds.
- `services/billing/AGENTS.md`, the projections versus authoritative records gotcha, which this spec
  turns into a per table list.

**Practices & standards**:

- Idempotent consumer keyed by the identity the message already carries, rather than by a key the
  consumer mints.
- Projections that store facts and derive answers at read time, never accumulate in arrival order.
- Immutable financial documents, corrected by void and reissue, with the rendered values frozen at
  issue.
- A counter row taken inside the writing transaction for gap tolerant, never reused numbering.
- Database constraints over conventions: partial unique indexes for "at most one open", composite keys
  from natural identity, `CHECK` for a closed set of states.
- Append only dated history, read as the newest row in force on a date.
- Explicit end timestamps in place of deletion, so referencing rows keep their explanation.
- Generated columns for a gate two callers must agree on.
- Tenant scoping on every table and every query, taken from the verified token.
- Event sourcing as the runner up for `billing`, set aside deliberately: the reasons are in Option 4.
