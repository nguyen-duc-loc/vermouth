# Rationale for recurring sessions and exceptions

## Context

The core teaching loop stores each session as a concrete row and publishes scheduled, moved, and
cancelled facts to billing and notifications. The next feature must remove repetitive session entry
without turning a recurrence expression into business truth that every consumer has to interpret.
Attendance already points to a session identifier, billing chooses a month from the session local
date, and issued invoices never change.

The tutor is one person working mostly from a phone. Weekly classes may run on any combination of all
seven weekdays and may use a different time on each day. A new rule may reach back 30 local dates so
recent teaching can be entered, while later changes and early ending do not rewrite older history. A
normal exception must affect one occurrence only. A later timetable change must preserve history and
explicit exceptions. The bounded product scale favors a simple synchronous command over a scheduler
or another runtime component.

The web also needs a durable way to inspect the sessions that generation creates. The supplied
calendar screenshot establishes the useful composition: a compact month picker, clear view controls,
a calm time grid, and colored event cards. Vermouth already owns its typography, color tokens, app
shell, dark theme, and accessible primitives, so the reference should shape hierarchy without
replacing that system.

## Options considered

### Option 1: Versioned normalized rules and concrete sessions

Store rule versions plus one row per weekday slot, then materialize every bounded occurrence as a
normal session. A rule change closes one version and creates another while session exceptions remain
ordinary state on concrete rows.

**Pros**:

1. Existing attendance, billing, notification, event, and replay contracts keep working by session
   identifier.
2. Postgres can validate rule ranges, tenant references, and active time overlap.
3. A single occurrence is easy to move, cancel, restore, inspect, and test.

**Cons**:

1. Creation and schedule changes can write hundreds of rows and events.
2. Rule history and replaced sessions consume more storage than a compact recurrence expression.

### Option 2: Compute occurrences from rules and persist exceptions only

Store the weekly rule and derive ordinary occurrences during reads. Persist only moved or cancelled
exceptions.

**Pros**:

1. The database stores very few rows.
2. Extending an open ended series needs no generation job.

**Cons**:

1. Attendance, billing, and notifications would need stable virtual occurrence identifiers or their
   own recurrence interpretation.
2. Rule edits and time zone changes could rewrite past answers unless every reader reproduces the
   same temporal logic.
3. It contradicts the accepted concrete session ownership model.

### Option 3: Generate a rolling future window

Materialize only the next few months and use a background task to extend each rule before sessions
are needed.

**Pros**:

1. User commands stay smaller for long schedules.
2. The stored session count stays close to the dates currently in use.

**Cons**:

1. A new scheduler, lease, retry path, and alert become required for a bounded solo tutor workload.
2. A failed extension can silently omit sessions from a digest or future calendar.
3. The schedule end is already bounded, so the operational machinery solves a problem the product
   does not have.

### Option 4: Store weekday slots as JSON

Keep one rule row with a JSON array of weekdays and times, then materialize concrete sessions.

**Pros**:

1. The schema uses one fewer table and a whole rule is easy to load as one value.
2. Adding a future slot attribute does not immediately require a column.

**Cons**:

1. Uniqueness, weekday bounds, local time checks, and typed sqlc access move out of the database.
2. Querying or comparing one slot becomes application parsing rather than ordinary relational data.

## Rationale

Option 1 keeps recurrence inside the service that owns schedules while preserving the concrete
session boundary already used everywhere else. The two year maximum makes eager materialization
finite, and a solo tutor cannot create enough valid weekly occurrences for a background generator to
earn its operational cost. Normalized slots fit the relational domain and let sqlc keep the query
surface explicit.

For a recurring class creation, `schedule.valid_from` is the rate effective date. It is explicit,
immutable command input and names when the class schedule begins. The first generated occurrence is
not suitable because its weekday may fall several dates later, while the transaction clock would
make the same class input depend on when the request happened. The existing single session path
continues to use `first_session.local_date`.

The database exclusion constraint is worth the `btree_gist` extension because overlap is a real
invariant under concurrent requests. A query before insert can improve the message but cannot close
the race. Locking the class row gives schedule versions one order, while the tutor wide exclusion
constraint also protects conflicts between different classes. Existing data makes a staged migration
necessary: first expose and reconcile overlaps, then let the guarded constraint migration refuse any
remaining conflict.

Recurring writes wait for that second migration. Shipping generation against only a friendly overlap
query would break the concurrency contract during the exact upgrade window meant to make it safe.
The old application can keep serving after the additive schema phase, so there is no need for a
feature flag or a temporary weaker invariant.

Explicit integer revisions are preferable to timestamps for stale write protection. They have one
meaning, change on every relevant mutation, and remain safe when two writes happen within the same
clock precision. Immutable receipt snapshots solve the other side of command identity: a retry must
return the success originally committed, not a reconstruction from rows that may since have changed.
The command hash therefore covers the path resource and normalized explicit input, while the receipt
keeps the original status, inferred zones, and one transaction instant outside that hash.

Calendar year arithmetic is shared by rule bounds and moves, including the 29 February clamp. Zone
transition resolution is explicit because Go wall clock normalization alone does not prove the
required gap shift or earlier repeated instant. One transaction instant also prevents a command from
crossing a date or state boundary halfway through its own writes.

The global calendar is broader than the narrowest possible class page, but it directly supports the
same invariant by letting the tutor see all classes together. The reference layout works because its
calendar hierarchy is useful. Its generic dashboard statistics, search, AI controls, and unrelated
navigation would dilute the tutor's actual job and are deliberately excluded. One request display
zone keeps its grid coherent. Source rule zones and stored billing dates remain labelled facts rather
than competing calendar axes.
