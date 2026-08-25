# 0007. Rationale: auth endpoint rate limits

## Context

Spec 0004 deliberately left `start`, `callback`, and `refresh` without a rate limit while Vermouth
was reachable only on localhost. `start` inserts one ten minute `login_attempts` row, callback can
reach the Google exchange, and refresh reads and writes session rows. A public caller can therefore
spend memory, database, and external work without first proving an identity.

Spec 0006 puts the application on a public HTTPS address and makes verified rate limit evidence a
launch prerequisite. That deployment has one gateway replica and one identity replica on a small
two CPU VM. The right design must protect that real shape, keep the gateway free of business rules,
preserve the existing `vermouth.APIError` contract, and avoid adding an operational service for a
handful of friend testers.

The gateway is the earliest repository owned point that has the public route, request id, refresh
cookie, and proxy metadata together. It has no database. The public edge differs between local and
production environments, so a policy placed in Envoy or Traefik would need two implementations.

## Options considered

### Option 1: Gateway memory with dual token buckets

The gateway keeps bounded caller entries and fixed global entries in memory. Each entry contains a
minute and an hour token bucket from `golang.org/x/time/rate`.

**Pros**:

1. Refusal happens before any identity, Postgres, or Google work.
2. No database migration, network dependency, or new service is needed.
3. One implementation serves local Envoy, production Traefik, and direct development.
4. The gateway can produce the repository error shape and preserve auth redirects.

**Cons**:

1. Restart restores full budgets.
2. Replicas do not coordinate, so the current one replica constraint is load bearing.
3. Correct trusted proxy parsing and bounded concurrent state become gateway responsibilities.

### Option 2: Identity with Postgres counters

Identity stores window counters in its own database and checks them in each handler.

**Pros**:

1. Counters survive process restart and coordinate any future identity replicas.
2. The state can be inspected with familiar SQL and no new infrastructure service.

**Cons**:

1. Every abusive request reaches the service and creates database work on the path being protected.
2. Client address trust still has to cross the gateway boundary correctly.
3. Counter cleanup, contention, and write amplification add schema and operational cost to a small
   deployment.

### Option 3: Envoy and Traefik local limits

Each public edge product applies its own native local rate policy before the Go gateway.

**Pros**:

1. Traffic is refused at the earliest possible point.
2. The application carries no caller registry.

**Cons**:

1. Local and production use different edge products, so policy and tests would be duplicated.
2. Native error bodies and redirect behavior do not naturally match `vermouth.APIError` and the
   existing sign in screen.
3. Changing an application auth limit becomes a platform rollout as well as an application change.

### Option 4: Shared Redis compatible limiter

A new network service stores counters for all gateway replicas.

**Pros**:

1. Restarts and replica changes keep one coordinated budget.
2. The pattern scales beyond the current VM without changing request semantics.

**Cons**:

1. It adds deployment, memory, persistence, readiness, monitoring, and failure policy for one small
   feature.
2. A network or store failure becomes part of every public auth request.
3. The current one replica deployment receives no practical benefit from the extra operation.

## Rationale

Option 1 matches the system that will actually ship. The gateway is already the public request
boundary, the deployment fixes it at one replica, and the chosen global budgets bound callers who
rotate address or token keys. Postgres and Redis coordination solve a replica problem Vermouth does
not have, while making the attack path more expensive or less reliable.

Paired continuous refill token buckets were chosen over fixed or exact rolling windows. They allow
short normal bursts, enforce a longer average, and do not store one timestamp per request. The
official Go supplemental rate package is the smallest proven primitive. The registry mutex protects
one probe of every `TokensAt(now)` value followed by all `AllowN(now, 1)` calls only when every probe
has a token. This closes the partial consumption race without relying on cancellation semantics.
The runner up was an exact rolling window, which gives more literal counts but needs more memory and
cleanup for no user benefit here.

One process mutex is intentional. The decision touches several bucket objects and least recently
used state as one short memory operation, which is easier to reason about than sharded locks. No I/O
or background goroutine runs under it. If measurement later shows contention, that evidence can
justify partitioning. Designing that complexity before friend testing would be premature.

The client IP rule treats forwarding metadata as untrusted unless the direct peer is configured.
Using the rightmost untrusted address handles several trusted proxy hops without accepting a public
caller supplied leftmost value. Exact IPv4 and IPv6 `/64` keys balance shared network fairness
against trivial IPv6 address rotation. Global endpoint budgets remain the final availability bound
when caller grouping is imperfect.

The global pair is checked before a new caller entry is allocated. When it already refuses, existing
caller state may be inspected only to compute a truthful retry delay. Rotating keys therefore cannot
fill the registry or evict a legitimate caller while the whole service is already closed.

The browser redirects keep a tutor inside the existing sign in experience. Refresh keeps the API
error shape because it is fetched by code. A limited refresh does not mean the session ended, so the
browser preserves its current view and waits for a manual retry. Automatic retries would add load at
the exact moment the gateway is asking callers to stop.

Node's built in test runner is enough for the pure browser outcome and countdown state. Adding
Vitest or Playwright for this one reducer would reopen the web testing decision and add a package
before the planned browser test feature needs it. The runner up is the later Playwright path, which
will still verify the complete screen during `$check verify` and the money path work.
