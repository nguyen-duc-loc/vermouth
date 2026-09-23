# 0013. Tuition rate and monthly calculation rationale

## Context

Feature 13 turns attendance and dated rates into money. A quiet omission is worse than an obvious
failure because the tutor may send the result to a parent before noticing it. The calculation must
therefore prove that its local event projection has consumed a fixed published cut with no known
failure, show every input to the tutor, and preserve exactly what was issued.

The accepted architecture already places current rates and attendance in teaching, dated rate history
and invoices in billing, and all cross service facts on Redpanda. Billing must remain independent of
teaching at request time. The existing schema also fixes integer VND, one invoice per student, dated
rate lookup by session `local_date`, immutable invoice values, generation based retry safety, and
transactional outbox events.

The product serves one independent tutor. A synchronous month calculation bounded at 500 students and
10,000 candidate rows is simpler and safer than background job machinery. The browser must still
tolerate event lag, an uncertain network response, and a second tab changing an input during review.
This is a private tuition statement, not a statutory tax or electronic invoice.

One code inspection changed the initial design. The current consumer commits an offset after parking a
failed record. Committed offsets alone therefore prove progress, not successful projection. A durable
unresolved failure ledger and a verified rollout replay are required before a broker barrier can
honestly protect money. The barrier still cannot prove that every committed teaching outbox row is
already published. It is a minimum published cut, and the product must say so.

## Options considered

### Option 1: Reviewed synchronous issue from a certified published projection

Billing certifies the projection, captures a fixed broker barrier, checks unresolved failures inside a
repeatable read snapshot, calculates a temporary preview, and later repeats that work in the issue
transaction.

**Pros**:

1. It respects service ownership and still gives the tutor a precise review and retry contract.
2. It adds no external component and makes every failure state observable.
3. The fingerprint catches relevant changes without storing stale drafts.

**Cons**:

1. Preview and issue repeat the calculation and can each wait five seconds.
2. Shared consumer failure state and operator recovery add work outside the billing handler.

### Option 2: Issue immediately from the current billing projection

One command reads whatever billing has and writes invoices without a separate review or readiness
proof.

**Pros**:

1. It is the smallest handler and fastest happy path.
2. It needs no preview fingerprint or broker administration read.

**Cons**:

1. Consumer lag or a parked rate event can silently undercharge a student.
2. The tutor cannot catch an incorrect attendance mark or dated rate before issue.

### Option 3: Persist mutable monthly drafts

Preview creates a draft document that is edited or refreshed before it becomes an invoice run.

**Pros**:

1. The tutor can leave and return to the exact review state.
2. The draft can serve as an explicit audit snapshot.

**Cons**:

1. A draft duplicates derived projection data and needs invalidation, retention, ownership, and state
   transitions of its own.
2. A stale draft still needs a fresh projection comparison before issue, so it does not remove the
   hard correctness problem.

### Option 4: Read teaching synchronously during month end

Billing asks teaching for current sessions, attendance, and rates while issuing.

**Pros**:

1. It avoids projection lag for the immediate request.
2. It can read teaching's latest state directly.

**Cons**:

1. It breaks the accepted service boundary and makes billing unavailable whenever teaching is down.
2. It creates a distributed snapshot problem because teaching and billing cannot share one database
   transaction.

## Rationale

Option 1 is the only choice that satisfies both accepted service independence and the feature's money
correctness without pretending the asynchronous boundary is stronger than it is. A fixed broker
barrier names a minimum published cut. The failure ledger closes the dead letter gap that committed
offsets leave open, while certification prevents old failures or a rebuilt database from passing as
ready. Structural validation then catches projection relationships that cannot form a valid invoice.

The preview remains temporary because every issued value is already frozen on invoices and lines.
Versioned canonical JSON gives the browser one compact comparison token, while recomputation ensures
the server never trusts client money. Repeatable read supplies one database snapshot. Monotonic rate
revisions prevent an old replay from undoing a correction. The live run, invoice, and replacement
constraints settle concurrent writers, and a loser rolls back before reading the winner in a fresh
transaction. Serializable isolation remains the runner up if later writes escape those explicit
constraints.

The shared consumer failure table is deliberately fail closed. A decoded failure blocks only its
tutor, while an unreadable record blocks all billing. That operational cost is smaller than issuing
money while the system knows it may have skipped an input.
