# 0001. Service boundaries and event driven communication: rationale

The decision record for [index.md](index.md). Read by humans and by `/architect` on an update. A build does not need it.

## Context

> ⚠️ Premise note: on its merits this product does not need four services. One tutor, one account, a handful of screens, and a solo builder at 10 to 15 hours a week is the textbook case for a single well structured application, and splitting it multiplies the operational work several times over before a single screen exists. The reason to do it anyway is stated plainly in the scope: learning this architecture is the goal of the project, and feature 1 is called its learning goal. That changes the standard of judgement but not the physics. So the design below optimises for the thing that usually kills a solo distributed build: it must stay operable and debuggable by one person. Every service must be able to do its own job with nobody else awake, and the split must stop at the point where it stops teaching anything new. If at any point the goal shifts from learning to shipping fast, the honest move is a single application, and this spec becomes the record of a road not taken.

Vermouth replaces a tutor's spreadsheets and hand made invoices with one tool that counts attendance and produces the monthly bill. The scope already decided the shape of the world: one tutor account owning their own data, a student is a name and a phone number, one rate per session per class, only `Present` sessions are billed, the tutor sends the invoice PDF themselves, VND only, Vietnamese and English, running on local Kubernetes on an Azure VM with a cloud deployment later.

The decision here is the ground every later feature stands on: how many services there are, what each one owns, and how a fact gets from the service that knows it to the service that needs it. It comes before any tool choice, because the tools must serve the boundaries and not the other way round. Two flows put real pressure on it. Month end invoice generation reads a month of sessions, roster membership, attendance, and rates, then produces money that a parent will see, and the scope demands that running it twice does not produce two invoices and that a rate change never rewrites an issued invoice. The daily 6:00 digest needs today's sessions and the tutor's email at a moment when no user is present and no request is in flight.

The forces that shaped the answer: money correctness matters more than anything else, because a silent error sends a wrong bill to a parent; the builder is alone, so anything requiring a platform team is out; the whole system runs on one VM, so the moving parts must be few enough to bring up with one command; a deadline exists (friends testing the money loop by 7 November 2026); and debugging across services is the known reason people abandon this architecture, which is why the request identifier crossing the broker is treated as load bearing rather than as an observability nicety.

The consequence of not deciding is the worst outcome available: services drawn along technical layers instead of business capabilities, each one calling the others for data it should own, which is a single application with network calls in the middle. That shape is harder to operate than a monolith and teaches the opposite of the intended lesson.

## Options considered

### Option 1: one well structured application (modular monolith)

A single deployable with clear internal module boundaries (teaching, billing, notifications), one database, in process calls, and a background job runner for the digest.

**Pros**:
- Dramatically less work: no broker, no outbox, no projections, no duplicated fields, one database, one deployment.
- Month end is a single transaction over consistent data, which is the strongest possible position for money correctness.
- The genuinely correct engineering answer for this product's size and team (basis: monolith first, extract only when a real bottleneck or ownership boundary forces it).

**Cons**:
- It does not teach the architecture the project exists to learn, which the scope names as feature 1's purpose.
- Module boundaries with no network between them erode quietly, so the ownership discipline is never actually tested.

### Option 2: four event driven services behind an aggregating gateway (chosen)

`identity`, `teaching`, `billing`, `notifications`, each with its own database. No synchronous service to service calls at all. Every cross boundary fact travels as a fat event through the broker, and each service keeps the local copy it needs. The gateway does routing, token verification, and read only aggregation for multi service screens.

**Pros**:
- Every service can do its own job alone, which is what makes month end and the 6:00 digest survive a neighbour being down (basis: database per service, plus event carried state transfer).
- The no calls rule forces genuine ownership thinking, so the boundaries are tested rather than assumed.
- Four services map onto the three real business capabilities plus one asynchronous worker, and give both hard flows something to cross.
- Read aggregation at the gateway keeps a phone first home screen to one round trip without creating a fifth thing to build (basis: API gateway with read aggregation).

**Cons**:
- Eventual consistency becomes visible in the product and in every test.
- Four copies of the same infrastructure: outbox, relay, handled events, migrations.
- Duplicated fields must be maintained on purpose, and a new consumer need means changing an event.
- Debugging is hard until tracing exists, and money bugs now have more places to hide.

### Option 3: four services that call each other synchronously

The same split, but a service may call another for data it needs, with events used only where an asynchronous reaction is natural.

**Pros**:
- Much easier to start: no projections, no duplicated fields, and every read is fresh.
- The mental model transfers straight from single application thinking.

**Cons**:
- It produces a distributed monolith: nothing deploys or restarts alone, and a month end run fails when `teaching` is down.
- Failures cascade along call chains, and latency compounds on the paths a user waits for.
- It teaches the failure mode rather than the architecture, which defeats the stated purpose.

### Option 4: three event driven services, with notifications folded into billing

The same event rules, but `billing` also owns email and the digest schedule.

**Pros**:
- One less service, database, and deployment to operate on a single VM.
- Fewer copies: the digest data would sit beside billing's data.

**Cons**:
- `billing` grows two unrelated responsibilities, money and delivery, which is exactly the boundary erosion the design is trying to prevent.
- It removes the clearest example of a service that exists only to react to events on a schedule, which feature 17 was put in the scope to teach.

## Rationale

Option 1 is the better engineering answer for this product and was recommended as such; it loses only because the project's stated goal is to learn this architecture, and a monolith cannot teach it. That tradeoff is accepted knowingly, and the premise note records it so a future reader does not mistake the split for a scale requirement.

Between the distributed options, the deciding force is month end correctness under a solo operator. Option 3 makes `billing` depend on `teaching` being awake at exactly the moment money is produced, and it makes a rerun months later dependent on `teaching` still answering the same way. Option 2 turns that into a purely local read: `billing` accumulates the facts as they happen and derives the bill from its own tables, so the run is fast, repeatable, and survives a neighbour restarting (basis: event carried state transfer, and idempotency keys for money operations). The same argument decides the digest at 6:00, when there is nobody to ask.

Forbidding synchronous calls outright rather than allowing read only ones is the other load bearing call. The softer rule is more comfortable on day one, and it reliably grows: one fresh read becomes five, and the services stop being independently deployable without anyone deciding that they should. Since the whole point is to learn the discipline, the strict rule is the one worth paying for, and the price (duplicated fields and data that catches up a moment later) is exactly the lesson.

Given no synchronous calls, several choices stop being open. A consumer that cannot ask a follow up question must receive what it needs, which forces fat events with named fields per event rather than thin identifier only notifications. A publisher that saves and then publishes separately will eventually do one without the other, and on this data that means a wrong bill, so the outbox in the same transaction is not optional (basis: transactional outbox). Since the relay can republish after a crash, and no broker delivers exactly once end to end once your own database write is part of the story, consumers record the event identifiers they have handled (basis: idempotent consumer with a handled message store). Ordering is then only promised per key, so projections store facts and derive answers at read time instead of accumulating in arrival order, which removes cross key ordering as a concern entirely.

Two engineer decisions shaped the money model against the recommendation, and both are recorded as chosen deliberately. Adjustment lines on the next invoice were recommended for a correction after issue; void and reissue was chosen instead, so the model carries voided invoices, never reused numbers, and a link from a replacement to what it replaced (basis: immutable financial documents, corrected by reversal rather than edit). That pulls against a run keyed by tutor and period, which is what prevents a double press producing two bills, so the reconciliation is a generation on the run key: an accidental rerun returns the existing invoice, and only a deliberate void permits one further run, limited to the students whose invoices were voided.

The gateway keeps read only aggregation and nothing else. A full backend for frontend service would shape every screen more cleanly, but it is a fifth deployable that reliably accumulates business logic, and a phone first home screen only needs the round trips reduced, not a new home for rules. Reading a fact only from its owning service is the rule that keeps all of this invisible to the tutor: copies exist so a service can work alone, never so a screen can take a shortcut, which is what stops two screens disagreeing about the same session.

## References

**Project sources** (verifiable, in this repo):
- `docs/scope/scope.md`, the "Decided up front" block: one tutor account, `Present` sessions only, VND only, the tutor sends the invoice, local Kubernetes on an Azure VM.
- `docs/scope/scope.md`, feature 1: the boundaries decision comes before any tool choice, and its "Done when" requires the service list, the events, the interaction styles, and both hard flows walked step by step.
- `docs/scope/scope.md`, feature 13 "Done when": a rate change must not rewrite an issued invoice, and running the calculation twice must not produce two invoices.
- `docs/scope/scope.md`, feature 17 "Done when": a 6:00 local send per tutor, retried on failure, and never twice for the same run.
- `docs/scope/scope.md`, feature 9: one request identifier followed across services and through a broker message.
- `docs/scope/scope.md`, build approach Tracer Bullet, and the Beta workflow tier.
- No `AGENTS.md` exists yet, so no project conventions constrained this decision.

**Practices & standards**:
- Database per service, and service boundaries drawn on business capabilities rather than technical layers.
- Event carried state transfer (fat events), and the tolerant reader rule for evolving them.
- Transactional outbox with a publishing relay.
- Idempotent consumer with a handled message store, and idempotency keys for money operations.
- Per key partition ordering, with projections that derive rather than accumulate.
- Dead letter queue with bounded retry and growing delay, plus replay.
- Projection rebuild by replaying retained events.
- API gateway with read only aggregation, with backend for frontend as the runner up.
- Token based identity propagation with local signature verification, rather than a trusted internal header.
- Immutable financial documents, corrected by void and reissue rather than edit.
- Correlation identifier propagation across process and broker boundaries.
- Monolith first, extract services only when a bottleneck or an ownership boundary forces it (the practice this decision knowingly sets aside, and why).
