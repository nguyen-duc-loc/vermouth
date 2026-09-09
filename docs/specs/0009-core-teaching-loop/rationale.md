# 0009. Core teaching loop rationale

## Context

Feature 8 is the first product behavior after sign in. It must make a tutor's immediate job useful
while also proving the service design chosen in specs 0001 and 0003. A teaching write must commit
with its event, cross Redpanda, update a rebuildable billing copy, and return through a gateway read
without letting that copy become the source of teaching truth.

The tutor works from a phone and needs one short path, not the full class and student management
surface planned for later features. The accepted teaching schema already separates classes,
sessions, students, roster periods, and attendance. The design system already assigns stored class
color and a tutor override to this feature. The remaining design work is the order of commands, the
source of each date and value, the behavior under partial failure, and the exact home composition.

The system is eventually consistent, which means billing can be behind teaching for a short time.
The browser must expose that honestly without blocking attendance. The two create operations also
need a durable retry answer because records are never deleted and a lost response must not create a
duplicate class or student.

## Options considered

### Option 1: Guided home flow with explicit resources and billing progress

Use small teaching commands in one guided sheet, keep each completed aggregate, publish catalogue
events through the existing outbox, project them into billing, and aggregate identity, teaching, and
billing for home.

**Pros**:

1. Proves every planned boundary through the real user path.
2. Keeps business ownership and recovery rules visible.
3. Grows into later class, roster, and money features without replacing the contracts.

**Cons**:

1. Touches every application layer in one feature.
2. Requires the UI to represent partial setup and projection delay.

### Option 2: Full management pages first

Build separate class, session, student, roster, and attendance pages with broad CRUD contracts before
connecting the end to end thread.

**Pros**:

1. Produces reusable management surfaces immediately.
2. Makes partial setup less visible because each resource has its own page.

**Cons**:

1. Delays proof of the broker and gateway path.
2. Pulls feature 10 and feature 11 behavior into this slice before it is needed.

### Option 3: One setup command

Send class, session, student, roster, and attendance data to one endpoint and treat setup as one
server operation.

**Pros**:

1. Gives the browser one request and one success state.
2. Can appear atomic to the user.

**Cons**:

1. Crosses class and student aggregate boundaries in one transaction.
2. Creates a special command that later resource screens cannot reuse.
3. Hides recovery and encourages compensation when a later step fails.

### Option 4: Product UI with projection proof only in tests

Build the teaching commands and home screen, then verify billing only in integration tests.

**Pros**:

1. Keeps architecture language out of the tutor interface.
2. Makes the home payload and UI smaller.

**Cons**:

1. Gives a friend tester no visible clue when billing has not caught up.
2. Misses the existing contextual panel built for a quiet secondary status.

## Rationale

Option 1 fits the project's Tracer Bullet approach because the first milestone can carry one class
and session through every layer, then thicken the exact path with a student, roster, and attendance.
It also respects the aggregate rule. Class and first session commit together, while student and
roster remain separate commands whose completed facts are never rolled back after another command
fails.

Billing progress belongs in a small secondary panel because the tutor needs confidence that the
future money path is receiving facts, but teaching must remain usable whenever billing is slow. Raw
projection values never supply a class, session, student, or attendance display. The panel reports
only billing's own counts and processing time, which preserves the owner read rule.

Durable command receipts are extra schema, but they are justified by the project's no deletion rule.
Disabling a button cannot solve a response lost after commit. A receipt keyed by tutor, operation,
and browser command key gives the same resources on retry without copying the phone or full response.
The runner up was accepting manual recovery, which is smaller but can create permanent duplicates.

Stored class color follows the accepted design system rather than deriving color only at render
time. Teaching stores the suggestion or explicit override, and no event carries it because no
consumer needs the value. React Hook Form and Zod follow the dependency point already reserved by
spec 0008 for the first real validated form. Controlled React state was the runner up, but it would
duplicate validation and error mapping across a multi step draft.

Cursor paging is more machinery than the expected daily volume needs. It stays because the API
contract should not contain an unbounded list, while a fixed page of 50 keeps the common path to one
request. Identity remains a required home read for tutor identity, while teaching returns the
timezone claim from the exact token used to compute today, setup defaults, and the next midnight
instant. The browser formats session times with that request timezone, so a recent profile change or
the device timezone cannot make display disagree with the stored teaching date.

The initial home response embeds billing progress, but later polling has its own gateway read. This
keeps the optional status fresh without repeatedly loading identity and teaching. One code timeout
keeps a stalled billing call optional, while the two required panels still fail together and visibly.
