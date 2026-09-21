# 0011. Student records and class rosters rationale

## Context

The core teaching loop proves one student, one membership, and one attendance mark, but those facts
are reachable only through setup and Home. There is no way to find or edit a student, close a roster
period, inspect membership history, or commit one complete session roster. The current single mark
route can leave a partially saved attendance pass when a network or browser failure interrupts a
sequence of writes.

The existing model already carries the right business history. `students` retains removed rows,
`roster_periods` carries inclusive effective dates, `attendance` has one row per session and student,
and the event catalogue already names every cross service fact this feature needs. Billing and
notifications depend on those exact facts, while the gateway must remain free of business rules.

Student phone is private contact data but no formal compliance program applies yet. It must remain
inside the teaching boundary and the owning tutor's current record surfaces. The web app is phone
first, uses a settled design system, and generates all public types from one OpenAPI contract.

## Options considered

### Option 1: Extend the temporal model with atomic aggregate commands

Keep the current entities and event names. Add complete resource reads, one dated class roster delta,
and one whole session attendance pass, with row locks, database constraints, command receipts, and
generated contracts.

**Pros**:

1. Preserves one teaching truth and every existing consumer contract.
2. Makes retries and concurrency explicit at the same aggregate boundaries the repository already
   uses.
3. Adds no service, provider, or new entity.

**Cons**:

1. Requires a careful additive constraint migration and replacement of two existing routes.
2. Whole roster attendance produces more writes and events than changing one student at a time.

### Option 2: Keep one request per membership and attendance row

Add list and detail pages but keep the existing narrow commands. The browser would coordinate many
requests and infer when a roster or attendance pass is complete.

**Pros**:

1. Changes less handler code and preserves current routes.
2. Lets each successful row appear immediately.

**Cons**:

1. Network failure leaves partial business state with no reliable resume boundary.
2. Concurrent tabs can silently overwrite a tutor's work one row at a time.
3. The browser becomes responsible for a business transaction it cannot make atomic.

### Option 3: Add roster snapshots and attendance batch entities

Create parent records for every roster edit and attendance pass, then derive current membership and
marks from those parents.

**Pros**:

1. Makes each user submission an explicit audit object.
2. Gives batch revisions and status their own stored identity.

**Cons**:

1. Duplicates history already represented by roster periods, attendance rows, receipts, and events.
2. Forces new identifiers into contracts and consumers that do not need them.
3. Adds migration and query complexity without a compliance or audit requirement.

## Rationale

Option 1 fixes the actual gap, which is incomplete commands and missing product reads, rather than
replacing a sound data model. The class row is the natural serialisation point for one roster delta,
and the session row is the natural point for one attendance pass. Existing receipt and outbox
patterns then provide safe retry and cross service publication without a second coordinator.

Option 2 is the runner up because it has the smallest code diff, but its partial failure is visible
to the tutor and can affect money. Option 3 offers a stronger audit shape than the product currently
needs, while creating another representation of membership and attendance that could disagree with
the rows billing already understands.

The migration stays additive and guarded. It refuses invalid history rather than repairing dates
silently, because a guessed correction could change who is billed. The web uses resource routes and
targeted query invalidation, with no optimistic teaching state, so eventual projection delay never
changes the authoritative screen.

The aggregate locks need one shared order even though commands still modify only one aggregate.
Class, session, then student locks let roster, attendance, archive, and session state commands
revalidate against one committed view without creating a lock cycle. Canonical reads use one SQL
statement or a repeatable snapshot, because a revision assembled from separate moving reads would
not describe any real state.

The temporal model accepts one deliberate limitation. It never deletes a mistaken same day roster
period and cannot insert an open backdated period before a later retained period. Making those
corrections fully general would require a void fact or another dated end input, neither of which the
agreed product flow includes. The UI therefore explains the boundary and the service rejects an
ambiguous correction instead of guessing.
