# Scope: Vermouth

Vermouth is a web app for an independent tutor who wants one place for their classes, their students, their session files, and the monthly tuition bill. It replaces the scattered spreadsheets and the hand made invoice with one tool that counts attendance and produces the bill for you.

**Build approach:** Tracer Bullet (prove the whole pipe works end to end before building any part of it fully).
**Workflow:** Beta (after `$develop`, run `$check verify`, then `$test`). The project default level of rigor. `$architect` is the recommended first stop for a feature with a real decision, but skippable when you already know the build. Any feature can carry its own tag (e.g. `· GA`) to do more or less.
**Target:** friends can test the real money loop by 7 November 2026 (through feature 16), full scope by 5 December 2026, at 10 to 15 hours a week solo. At 20 or more hours a week, pull both in by roughly three weeks (17 October and 14 November).

**Decided up front, so no feature has to ask again:** tutor is the only account and one tutor owns their own data; a student record is just a name and a phone number; every student in a class pays one rate per session and only Present sessions are billed; the tutor sends the invoice PDF themselves, so no parent contact is stored; VND only, Vietnamese and English; a payment QR on the invoice with the tutor marking it paid by hand; the system runs on local Kubernetes on your Azure VM, with a cloud deployment later so friends can test.

_These are recommendations to keep your build orderly, not requirements. Skip anything that does not fit: if you already know how to build a feature, use `$develop` and skip `$architect`. You decide when a feature is `done`._

## At a glance

| # | Feature | Phase | Status |
|---|---------|-------|--------|
| 1 | Service boundaries & communication design | Foundation | done |
| 2 | Stack & scaffold | Foundation | done |
| 3 | Coding standards & tooling | Foundation | done |
| 4 | Data model & data ownership per service | Foundation | done |
| 5 | Local Kubernetes platform & one command startup | Foundation | done |
| 6 | Design system & UI foundation | Foundation | done |
| 7 | Tutor sign in & identity | Slice 1 | done |
| 8 | Core teaching loop | Slice 1 | done |
| 9 | Tracing, central logs & error alerts | Slice 2 | planned |
| 10 | Recurring sessions & exceptions | Slice 3 | planned |
| 11 | Student records & class rosters | Slice 3 | planned |
| 12 | Tutor profile & bank details | Slice 4 | planned |
| 13 | Tuition rate & monthly calculation | Slice 4 | planned |
| 14 | Invoice PDF with payment QR | Slice 4 | planned |
| 15 | Invoice list, share & mark paid | Slice 4 | planned |
| 21 | Rate limit the auth endpoints | Slice 5 | in-progress |
| 16 | Cloud deployment for friend testing | Slice 5 | in-progress |
| 17 | Daily schedule digest email | Slice 6 | planned |
| 18 | Session documents | Slice 7 | planned |
| 19 | Search across students, classes & sessions | Slice 8 | planned |
| 20 | English alongside Vietnamese | Slice 9 | planned |

## Foundations

### 1. Service boundaries & communication design · done
Split Vermouth into services along real business boundaries, and decide how they talk: which calls are synchronous through a gateway, and which facts travel as events through a message broker. This is the learning goal of the project and the ground every later feature stands on, so it comes before any tool choice.
**Done when:** the spec names each service, states in one sentence what it owns and what it must never own, lists the events it publishes and reacts to, marks each interaction synchronous or asynchronous with a reason, and walks the two hard flows (month end invoice generation, the daily digest) step by step across services.
spec [0001](../specs/0001-service-boundaries-and-communication/index.md)
- [x] Design it (spec): `$architect service boundaries & communication design`

### 2. Stack & scaffold · done
Pick the language, the framework per service, the database engine, the message broker, the gateway, and the repository layout, then scaffold a runnable skeleton. One place where tools are chosen, so nothing later has to guess.
**Done when:** the spec records every tool choice with a reason, and the scaffolded skeleton builds and starts locally with at least one service answering a health check through the gateway.
spec [0002](../specs/0002-stack-and-scaffold/index.md) · code in `pkg/vermouth`, `gateway`, `services/*`, `web`, `api/openapi.yaml`, `test/compose.test.yaml`, `Taskfile.yml`
- [x] Decide the stack (spec): `$architect stack & scaffold`
- [x] Scaffold from the decision: `$develop stack & scaffold`

### 3. Coding standards & tooling · done
Capture the conventions from the real scaffolded project, then install lint, format, type checking, and pre commit enforcement. Several services multiply the cost of inconsistent code, so this lands before the code grows.
**Done when:** root `AGENTS.md` reflects the real stack and the shared conventions across services, and lint, format, and pre commit all run clean.
code in `.golangci.yml`, `Taskfile.yml`, `.pre-commit-config.yaml`, `.github/workflows/ci.yml`, `web/biome.jsonc`
- [x] Capture conventions + tooling choices: `$audit`
- [x] Install the tooling: `$develop tooling`

### 4. Data model & data ownership per service · done
The core entities (tutor, student, class, session, attendance, rate, invoice, invoice line, document) and, just as important, which service owns each one and what the others are allowed to keep a copy of. A wrong ownership split is the most expensive thing to redo in this architecture.
**Done when:** every entity has exactly one owning service, each service has its own schema that no other service reads directly, the duplicated fields carried in events are named and justified, and the migration for each service applies cleanly.
spec [0003](../specs/0003-data-model-and-ownership/index.md) · code in `services/*/db/{migrations,queries}`, `services/*/internal/store`, `services/billing/internal/handler/profile.go`, `test/model`
- [x] Design it (spec): `$architect data model & data ownership per service`
- [x] Build it: `$develop data model & data ownership per service`
  - [x] One model migration per service, in the direction the facts flow: `identity` timestamps, then the five `teaching` tables, then `billing` (six projections plus five authoritative records), then the four `notifications` tables · AC-1, AC-2, AC-3, AC-6, AC-9, AC-10, AC-11, AC-12
  - [x] Typed queries: `sqlc.yaml` for `teaching` and `billing`, the first `db/queries/` file per service with every statement filtering by `tutor_id`, then a clean apply and build from empty databases · AC-2, AC-4, AC-5
  - [x] The guard tests: no statement reads a table its service does not own, money is `bigint`, instants are `timestamptz`, and each projection's primary key matches its event key · AC-3, AC-4, AC-6, AC-7, AC-10
  - [x] The behaviour tests against the real Postgres and Redpanda: a duplicate delivery and a full replay change nothing, plus the concurrent run, concurrent number, rejoin, and completeness gate cases · AC-1, AC-7, AC-8, AC-9, AC-11, AC-12
  - [x] Tenant integrity amendment: additive teaching and billing migrations add composite tenant references and `class_rates.updated_at`, with cross tenant guards · AC-2, AC-3, AC-4, AC-6
  - [x] Replay and trusted tenant amendment: consumers use envelope `tutor_id`, replay preserves `digest_runs`, and tests separate business fields from bookkeeping timestamps · AC-3, AC-4, AC-7, AC-8
- [x] Verify it: `$check verify data model & data ownership per service`
- [x] Test it: `$test data model & data ownership per service`

### 5. Local Kubernetes platform & one command startup · done
Bring the whole system up on your Azure VM cluster with one command: every service, its own database, the broker, the gateway. This is the piece that most often eats a week, so it gets its own feature rather than hiding inside another one.
**Done when:** one command brings up every service with its own database and the broker on the local cluster, a request reaches a service through the gateway, and a service restart does not lose data.
spec [0005](../specs/0005-local-kubernetes-platform/index.md) · code in `deploy/`, `Taskfile.yml`, `pkg/vermouth/`, `services/identity/`, `web/`
- [x] Design it (spec): `$architect local kubernetes platform & one command startup`
- [x] Build it: `$develop local kubernetes platform & one command startup`
  - [x] Prove executable platform inputs and generated state schemas, canonical image identities, exact k3d registry and volume identities, unprivileged `127.0.0.1:8080` application transport with no Colima profile change, cold node pull proof, packaged Traefik, two Helm releases, the first identity to notifications thread, security matrices, recovery guards, and `task thread` · AC-1, AC-2, AC-4, AC-8, AC-9, AC-10, AC-11, AC-12, AC-13, AC-17
  - [x] Add teaching, billing, their separate Postgres instances, remaining Secrets, policies, probes, and full readiness · AC-3, AC-9, AC-10, AC-17
  - [x] Add Garage initialization, signed live credential proof, persistent storage, labelled Docker volume ownership, stop, clean, recreate, and persistence proof · AC-5, AC-16
  - [x] Complete service redeploy, remote descriptor handoff, atomic image and Secret state, explicit context targeting, mutation locks, bounded waits, exact Helm recovery, registry aware status, logs, drift detection, and failed Job recovery · AC-7, AC-11, AC-12, AC-17
  - [x] Prove Compose isolation, staged two architecture publication, both architecture web smoke checks, local OAuth disabled behavior, the exact resource formula, and all pinned inputs · AC-6, AC-8, AC-14, AC-15, AC-17
  - [x] Remove project owned host images after publication and add confirmed shared BuildKit cache cleanup · AC-8, AC-17
- [x] Verify it: `$check verify local kubernetes platform & one command startup`
- [x] Test it: `$test local kubernetes platform & one command startup`

### 6. Design system & UI foundation · done
The visual language and the base components (layout, forms, tables, buttons, empty and error states) in both Vietnamese and English ready shape, so every screen after this is assembly rather than invention. Phone first, since attendance gets marked standing up.
**Done when:** `design.md` covers type, colour, spacing, and the component set; base components have visible focus states, work by keyboard, and read well on a phone screen.
spec [0008](../specs/0008-design-system-ui-foundation/index.md) · code in `web/design.md`, `web/src/{appearance,components,design-system,lib,pages}`, `web/src/{main.tsx,routes.tsx,styles.css}`
- [x] Design it (spec): `$architect design system & UI foundation`
- [x] Build it: `$develop design system & UI foundation`
  - [x] Prove the first end to end design thread: `design.md`, Montserrat, light and dark semantic tokens, appearance boot and storage, one primitive, one class color, and the development gallery · AC-1, AC-2, AC-3, AC-5, AC-8, AC-10
  - [x] Add the seven accents and class colors, appearance panel, shadcn primitives, typed variants, Lucide icons, and every component state · AC-2, AC-3, AC-4, AC-5, AC-10
  - [x] Compose the app shell, responsive table, form and feedback patterns, with phone, keyboard, zoom, and long Vietnamese behavior · AC-4, AC-5, AC-6, AC-7
  - [x] Add date formatting and scoped GSAP motion, rebuild the temporary screens from the foundation, and prove the production bundle excludes the gallery · AC-1, AC-4, AC-5, AC-6, AC-7, AC-8, AC-9
- [x] Verify it: `$check verify design system & UI foundation`
- [x] Test it: `$test design system & UI foundation`

## Slice 1: The core thread

This slice is the walking skeleton. It is narrow on purpose and everything in it is real: real sign in, real databases, real service to service traffic, a real screen.

### 7. Tutor sign in & identity · done
A tutor signs in with their Google account, and every request after that carries an identity the other services can trust. In a split system this is the first real boundary question: who checks the token, the gateway or each service.
**Done when:** a tutor can sign in with Google, a first sign in creates the tutor only for an allowed email, the session survives a reload and sign out ends it, a signed in request is identified at the gateway and trusted downstream, an unsigned request is refused, no password exists anywhere in the repository, and one tutor can never read another tutor's data.
spec [0004](../specs/0004-tutor-sign-in-google-oauth/index.md) · code in `services/identity/{db,internal/handler,internal/http,cmd/devtoken}`, `gateway/internal/{route,aggregate}`, `api/openapi.yaml`, `web/src/{api/session.ts,pages/SignInPage.tsx,routes.tsx}`
- [x] Design it (spec): `$architect tutor sign in & identity`
- [x] Build it: `$develop tutor sign in & identity`
  - [x] Rework identity persistence and the Google callback with canonical emails, browser binding, locked session families, validated URLs, injectable provider adapters, and concurrent first sign in handling · AC-1, AC-2, AC-5, AC-8, AC-9, AC-10, AC-12, AC-14, AC-15
  - [x] Make refresh and sign out one serialized contract through identity and the gateway, including origin checks, cookie clearing, bounded access after revocation, and the one boundary error shape · AC-3, AC-4, AC-6, AC-7, AC-11, AC-15, AC-16
  - [x] Complete the browser session coordinator and sign in route with protected checking, single flight renewal, expiry retry, preserved redirects, exact bilingual refusal copy, and accessible states · AC-1, AC-3, AC-5, AC-6, AC-10, AC-15, AC-16
  - [x] Close the hardened tracer bullet with generated contracts, configuration, session sweeps, `task dev:token`, empty migrations, and `task thread` · AC-1, AC-7, AC-8, AC-12, AC-13
- [x] Verify it: `$check verify tutor sign in & identity`
- [x] Test it: `$test tutor sign in & identity`

### 8. Core teaching loop · done
The thinnest real thread through the product: create one class, it has one session, add one student, mark that student Present or Absent, and see today's sessions on the home screen. One narrow path that crosses the gateway, more than one service, their separate databases, and back to the screen.
**Done when:** a signed in tutor can create a class with a single session, add a student, mark attendance, and see today's sessions on the home screen, with the attendance state surviving a reload and at least one cross service read proving the boundary works.
spec [0009](../specs/0009-core-teaching-loop/index.md)
code in `api/openapi.yaml`, `services/teaching/`, `services/billing/`, `gateway/`, `web/src/`, and `test/thread.sh`
- [x] Design it (spec): `$architect core teaching loop`
- [x] Build it: `$develop core teaching loop`
  - [x] Land the class and first session publisher, durable create receipts, billing consumer, and first aggregated home thread · AC-2, AC-3, AC-7 to AC-12, AC-16
  - [x] Add student, roster, and attendance commands with tenant checks, safe retries, events, projections, and canonical reload state · AC-1, AC-4 to AC-6, AC-8, AC-10 to AC-12
  - [x] Build the accessible home, guided setup sheet, session attendance cards, projection panel, draft recovery, paging, polling, and local date refresh · AC-1, AC-3 to AC-5, AC-7, AC-9, AC-13 to AC-15
  - [x] Regenerate the contracts, replace the development thread, update `task thread`, and pass repository checks · AC-11 to AC-13, AC-16
- [x] Verify it: `$check verify core teaching loop`
- [x] Test it: `$test core teaching loop`

## Slice 2: Seeing across services

### 9. Tracing, central logs & error alerts · needs a decision
Follow one request from the gateway through every service it touches, including where it hands off to the broker, and read all service logs in one place. Crashes and failed background jobs raise an alert so a friend's bug report is not your only signal. This lands early because debugging blind across services is what makes people give up on this architecture.
**Done when:** one request id can be followed across every service and through a broker message in a single view, logs from all services are readable in one place, and a thrown error or a failed job produces an alert you actually receive.
- [ ] Design it (spec): `$architect tracing, central logs & error alerts`

## Slice 3: Real schedules

### 10. Recurring sessions & exceptions · needs a decision
A class repeats weekly (for example every Monday and Thursday) and generates its sessions, and a single session can be cancelled or moved without disturbing the rest. The rule versus the exception is a genuinely tricky model, and attendance and billing both read it.
**Done when:** a tutor can define a repeating schedule with an end, see the generated sessions, cancel one session, and move one session to another time, with the change touching only that session and the invoice count reflecting it.
- [ ] Design it (spec): `$architect recurring sessions & exceptions`

### 11. Student records & class rosters
Manage students properly (name and phone number for now) and put them into classes, with a student able to sit in more than one class. Thickens the student strand that slice 1 ran narrowly.
**Done when:** a tutor can create, edit, and remove students, add and remove them from a class, see a class roster and a student's classes, and attendance marking covers a whole roster in one pass.
- [ ] Build it: `$develop student records & class rosters`

## Slice 4: The money loop

### 12. Tutor profile & bank details · Alpha
The tutor's name, contact line, bank name, account number, and account holder name, which are what the invoice and its payment QR are built from.
**Done when:** a tutor can save and edit their profile and bank details, the fields are validated, and the invoice service can read them when it builds an invoice.
- [ ] Build it: `$develop tutor profile & bank details`

### 13. Tuition rate & monthly calculation · needs a decision · GA
A rate per session lives on the class, and at month end the system counts each student's Present sessions and works out what they owe. This is where a silent error sends a wrong bill to a parent, so it gets the heaviest treatment in the project.
**Done when:** a class carries a rate per session, a month end run produces a per student total from Present sessions only, absent and cancelled sessions are excluded, a rate change does not rewrite an already issued invoice, and running the calculation twice does not produce two invoices.
- [ ] Design it (spec): `$architect tuition rate & monthly calculation`

### 14. Invoice PDF with payment QR · needs a decision
Turn a calculated total into a PDF a parent will take seriously: the student's name, the sessions attended with dates, the rate, the total, and a payment QR carrying the tutor's bank details with the amount already filled in. This is the professionalism the product promises.
**Done when:** an invoice renders as a PDF listing the attended sessions and the total in VND, the QR scans correctly in a real banking app with the right account and amount prefilled, the file is stored against the student, and it is not reachable without being signed in.
- [ ] Design it (spec): `$architect invoice PDF with payment QR`

### 15. Invoice list, share & mark paid
See invoices by month and by student, open or download the PDF to send it yourself, and mark one paid when the transfer lands. Closes the loop the tutor used to run by hand.
**Done when:** a tutor can list invoices by month and student, see paid and unpaid at a glance, download or share the PDF, and mark an invoice paid or unpaid.
- [ ] Build it: `$develop invoice list, share & mark paid`

## Slice 5: Friends can use it

### 21. Rate limit the auth endpoints · in-progress · from spec 0004
Bound `start`, `callback` and `refresh` so nobody can spam the sign in path or grow `login_attempts` without limit. Spec 0004 leaves them unbounded on purpose, which is fine behind localhost and not fine once feature 16 gives them a public address, so this lands before that one ships.
**Done when:** each protected auth endpoint refuses a caller past configured caller and global rates through its existing browser or API boundary, a flood cannot grow the pending login table without bound, and one evidence target exercises the limit.
spec [0007](../specs/0007-auth-endpoint-rate-limits/index.md) · code in `gateway/{internal/ratelimit,internal/route,cmd/ratelimitevidence}`, `api/openapi.yaml`, `web/src/{api/session.ts,pages/SignInPage.tsx,routes.tsx}`, `deploy/helm/vermouth/`, and `test/authratelimit/`
- [x] Design it (spec): `$architect rate limit the auth endpoints`
- [x] Build it: `$develop rate limit the auth endpoints`
  - [x] Build the bounded gateway registry, paired token buckets, required configuration, trusted proxy parser, and concurrent guard tests · AC-2, AC-3, AC-6, AC-8, AC-9
  - [x] Prove the first limited `start` thread through gateway, identity, deployment configuration, unchanged auth state, and sampled logs · AC-1, AC-4, AC-5, AC-9, AC-10
  - [x] Extend the policy to callback and refresh, close cookie and `HEAD` edges, update OpenAPI, and add the accessible manual browser retry · AC-1, AC-3, AC-4, AC-5, AC-7, AC-11, AC-12, AC-13
  - [x] Add `task test:auth-rate-limit`, the pending attempt bounds, race coverage, and schema version 1 launch evidence · AC-3, AC-5, AC-6, AC-7, AC-8, AC-10, AC-13, AC-14
- [x] Verify it: `$check verify rate limit the auth endpoints`
- [ ] Test it: `$test rate limit the auth endpoints`

### 16. Cloud deployment for friend testing · in-progress
Put the running system somewhere your friends can open in a browser, with a real address and a certificate. Separate from the local cluster on purpose, so deployment never leaks into earlier features.
**Done when:** the whole system runs on a reachable address over a secure connection, the sign in and the invoice flow both work there, secrets are not baked into images, and you can push an update without wiping the data.
spec [0006](../specs/0006-cloud-deployment-friend-testing/index.md)
- [x] Design it (spec): `$architect cloud deployment for friend testing`
- [ ] Build it: `$develop cloud deployment for friend testing`
  - [ ] Align local Traefik, then bootstrap the exact Azure VM, locked static storage, free DNS, HTTPS, restricted SSH, production Secrets, and private pulls · AC-1, AC-2, AC-3, AC-5, AC-8, AC-11, AC-12
  - [ ] Promote two architecture Docker Hub digests through manual GitHub Actions, require rate limit and migration evidence, and prove the first secure production thread · AC-4, AC-6, AC-9, AC-10, AC-13
  - [ ] Complete the full topology, capacity gate, status, logs, retention, and guarded application rollback · AC-7, AC-14, AC-15, AC-16, AC-19, AC-20
  - [ ] Add encrypted export and full stopped state restore, then prove every stateful marker and external path · AC-17, AC-18
- [ ] Verify it: `$check verify cloud deployment for friend testing`
- [ ] Test it: `$test cloud deployment for friend testing`

## Slice 6: The daily digest

### 17. Daily schedule digest email · needs a decision
At 6:00 in the tutor's timezone, an email listing today's sessions with times, class names, and student counts. The second asynchronous flow in the system, and the one that teaches scheduled work plus a service reacting to a message.
**Done when:** a scheduled run at 6:00 local time emails each tutor their own sessions for that day, a tutor with no sessions gets no email or a clearly empty one, a failed send is retried and visible in the logs, and one run never sends the same digest twice.
- [ ] Design it (spec): `$architect daily schedule digest email`

## Slice 7: Session documents

### 18. Session documents · needs a decision
Attach files to a specific session (the worksheet, the slides, the homework) and open them again later from that session. Replaces the third spreadsheet and the messy folder.
**Done when:** a tutor can upload, list, download, and delete files on a session, file size and type are limited, a file is only reachable by the tutor who owns it, and deleting a session does not leave orphan files.
- [ ] Design it (spec): `$architect session documents`

## Slice 8: Search

### 19. Search across students, classes & sessions · needs a decision
One search box that finds a student, a class, or a session. Interesting in this architecture because the data being searched lives in more than one service's database.
**Done when:** searching a student name, a phone number, or a class name returns matching results grouped by type, results are limited to the signed in tutor's own data, a result opens the right screen, and no match shows a clear empty state.
- [ ] Design it (spec): `$architect search across students, classes & sessions`

## Slice 9: English

### 20. English alongside Vietnamese · needs a decision
Every screen and the invoice PDF available in Vietnamese and English, with the tutor choosing. Currency stays VND.
**Done when:** a tutor can switch language and every screen follows, the choice persists across sessions, dates and amounts format correctly per language, the invoice PDF renders in the chosen language, and no text is left hard coded.
- [ ] Design it (spec): `$architect english alongside vietnamese`

## Deferred
Out of scope for this build pass, kept here so the plan stays honest.
- **Parent contacts & emailing invoices directly**: store a parent contact and send the invoice from Vermouth · needs a decision
- **Automatic payment confirmation**: reconcile paid invoices from a bank feed or payment provider · needs a decision
- **Per student rates & flat monthly classes**: pricing beyond one rate per class · needs a decision
- **Excused absences**: a third attendance state that is not billed · needs a decision
- **Parent portal**: parents sign in to see attendance and invoices · needs a decision
- **Student data export & delete**: export or fully remove one student's data · needs a decision
- **Metrics dashboards**: throughput and latency graphs across services · needs a decision
- **Product analytics**: which features get used · needs a decision
- **Teaching centers with several tutors**: shared students, owner and tutor roles · needs a decision
- **Vermouth subscription plans**: charging tutors to use the product · needs a decision
- **Session level student count snapshot**: carry the roster count on the session events if the digest count drifts, instead of `notifications` counting roster events · from spec 0001
- **Retention for the digest tables**: prune old `notifications.sessions` and `digest_runs` rows once the tables are big enough to notice · from spec 0003
- **A stub Google provider for tests**: drive the browser sign in path end to end without a real Google round trip, what the empty `test/e2e/` will want · from spec 0004
- **Sign out everywhere**: revoke every session of one tutor at once, for a lost or shared phone · from spec 0004
- **Account linking & a second sign in provider**: one tutor with more than one way in · from spec 0004
- **Scheduled external HTTPS checks**: add the deferred 15 minute outside health probe and notification · from spec 0006
- **Automated production backup**: replace manual Mac export before production data becomes unacceptable to lose · from spec 0006
- **Azure OIDC deployment**: replace the temporary restricted SSH key when the subscription permits Entra application creation · from spec 0006
- **Shared auth rate state for several gateway replicas**: replace or divide the in memory budgets before gateway runs more than one replica · from spec 0007

## Legend

**The decision box.** Every feature carries exactly one, the sub task whose label ends with `(spec)`. Its wording varies (`Design it (spec)` normally, `Decide the stack (spec)` on Stack & scaffold), so skills locate it by that `(spec)` suffix, never by an exact label. Every other box is an execution box and `$architect` never ticks one.

- **Next step** = the first unticked box (always a command or a tracked milestone).
- **needs a decision** = run `$architect` first; otherwise straight to `$develop` (or `$audit` for standards & tooling). The tag drops once the spec is captured.
- **Atomic build tasks live in the spec's `## Build plan`, not here**: the scope carries only the milestone rollup, filled in by `$architect` when the spec is captured.
- **Status** `planned` then `in-progress` then `done`, plus `existing` (predates this workflow) and `dropped` (de scoped, kept for history).
- **Workflow tier tag** beside a heading (e.g. `· GA`, `· Alpha`) sets that one feature's rigor above or below the project default; no tag inherits the default Beta.
- **Workflow** (header line) is the project default, what runs after `$develop`: **Prototype** = nothing; **Alpha** = `$check verify`; **Beta** = `$check verify` then `$test`; **GA** = adds a fresh model `$check review` then `$document`.
- **Pointer line** (`spec <n> · code in <path>`): the spec link added by `$architect`, the code path by `$develop`.
