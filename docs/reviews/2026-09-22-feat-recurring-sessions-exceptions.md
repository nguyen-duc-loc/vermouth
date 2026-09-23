# Review, feat/recurring-sessions-exceptions, 2026-09-22

**Reviewed by**: gpt 6 astra (author on gpt 5.6 sol)
**Scope**: 177 files, feat/recurring-sessions-exceptions vs main, including uncommitted and untracked work. Findings focus on spec 0013 and interactions that can break tuition calculation.
**Verdict**: Blocked

## Summary

The change adds dated rate commands, projection certification, monthly previews, and atomic invoice issue. The invoice transaction and frozen values are well structured, but the projection proof can certify missing data after consumer or replay failures. Gateway limits also conflict with the new billing contract, and several browser recovery paths are incomplete.

## Blockers

### 🔴 Failure ledger errors can leave an invisible gap in the billing barrier, `pkg/vermouth/consumer.go:340`

**Problem**: The new failure ledger write can fail and return to `drainRecords`, whose error branch at line 179 continues to the next record instead of retrying the failed record. If handling offset 10 and recording its failure both fail during a database outage, then the database recovers for offset 11, `CommitRecords` commits next offset 12. Offset 10 has neither a projection write nor a failure row. The existing drain behavior now invalidates the contiguous progress assumption introduced by `captureBarrier`.

**Why it matters**: Billing sees committed progress beyond its captured end and no unresolved failure, so it can issue invoices while missing an attendance correction, cancellation, or rate change. The same hole can pass replay certification. This violates AC-8 and AC-9.

**Suggested fix**: Keep retrying the same source record, or stop and resume consumption from that record, whenever handling and durable parking have not both reached a safe outcome. Never commit a later offset in that partition across the gap. Add a real broker test where the ledger write fails, the next record succeeds, and billing remains blocked until the earlier record is recovered.

### 🔴 A failed offset reset can still be certified as a completed replay, `pkg/vermouth/cmd/replay/main.go:219`

**Problem**: Replay commits the cleared projection, cleared handled events, and `state = 'replaying'` before calling `CommitAllOffsets`. A crash, failed reset, or partially successful reset leaves the new generation eligible for certification while some old committed offsets remain. `runCertify` checks only that those offsets reach the captured ends. For a previously caught up group, they already do, even if none of that partition's projection has been rebuilt.

**Why it matters**: Running certification after this failure can certify empty or incomplete billing tables. Subsequent previews and issues may silently omit charges. Reporting an error from the reset command does not make the durable certification state safe.

**Suggested fix**: Make reset completion a durable prerequisite for certification. Keep the new generation uncertified until every partition reset is confirmed, and bind replay completion evidence to that generation. Exercise crashes after the database commit and partial broker reset failures, and require certification to refuse both cases.

### 🔴 Retention loss during replay is never checked against the live broker, `pkg/vermouth/cmd/consumerfailures/main.go:303`

**Problem**: `validateRetainedHistory` checks only the earliest offsets captured when replay started. Certification subsequently reads topic identities, partition sets, and committed offsets, but never reads live start offsets. If a partition starts at zero when the manifest is created and old records disappear before the consumer reads them, the consumer's reset policy can advance to the new earliest offset and eventually reach the captured end. The saved zero still passes certification.

**Why it matters**: A rebuild with missing retained facts can become certified and feed immutable invoices. AC-24 explicitly requires retention gaps during rebuild to fail closed.

**Suggested fix**: Read and validate the live earliest offsets for every manifest partition during certification, require complete offset maps, and reject any retained history gap under the specified zero origin policy. Add a real broker scenario that removes an unread prefix after replay preparation and proves certification remains unavailable.

## Major

### 🟠 The gateway times out before billing can return its promised barrier timeout, `services/billing/internal/handler/billing_periods.go:36`

**Problem**: Billing gives its barrier five seconds, but the gateway's shared HTTP client also has a five second timeout in `gateway/internal/aggregate/upstream.go:25`. The gateway timer starts before billing validates the period, reads the existing run, and starts the barrier. When a partition remains behind, the gateway cancels first and returns its generic upstream failure instead of billing's `503 projection_sync_pending` and `Retry-After: 1`.

**Why it matters**: The documented lag response cannot reliably reach the browser, whose retry policy specifically recognizes `projection_sync_pending`. A nearly complete barrier also leaves no gateway time for calculation or invoice writes.

**Suggested fix**: Give billing requests an outer deadline that exceeds the complete barrier and calculation budget. Preserve request cancellation and test the delayed response through the actual gateway client, rather than returning an immediate mocked 503.

### 🟠 Supported monthly responses can be truncated by the gateway, `gateway/internal/route/billing_routes.go:157`

**Problem**: The new billing proxy uses `aggregate.Client`, which reads at most 4 MiB with `io.LimitReader` and silently returns the truncated body. A valid preview with 500 distinct students, 20 sessions each, and a 120 character class name can exceed that limit. An in memory encoding using a valid three byte character produced 5,664,662 bytes for 10,000 distinct candidate pairs, against a limit of 4,194,304 bytes.

**Why it matters**: A month inside the advertised AC-20 limits returns incomplete JSON through the public API. Preview cannot be reviewed, and an issued run of that size cannot be read reliably, including after a lost issue response.

**Suggested fix**: Set a bounded response allowance that covers the worst valid billing payload, or implement a contract that safely divides it. Detect an exceeded limit explicitly instead of forwarding truncated JSON. Verify the supported volume boundary through the gateway with maximum valid UTF 8 labels.

### 🟠 Switching months during issue stores the run under the wrong month, `web/src/pages/BillingPage.tsx:109`

**Problem**: The issue mutation has no immutable variables. Its success callback writes to the current `selected` month, and the month input remains enabled while issue is pending. TanStack updates the pending mutation's options after a render. Starting August issue, switching to July, then resolving the August request writes August's run into July's period key and removes July's preview. This was reproduced in memory with the installed `MutationObserver` implementation.

**Why it matters**: July can display August's invoices as its issued result and lose its issue action until a later refetch. Error recovery can likewise invalidate the wrong month.

**Suggested fix**: Pass tutor, year, month, and fingerprint as mutation variables captured at the explicit issue action. Use those variables and the returned run period for cache updates and recovery, regardless of the current route. Add a deferred response test that changes the selected month while issue is pending.

### 🟠 Missing rate amounts are accepted as a free rate, `gateway/internal/route/billing_routes.go:100`

**Problem**: The request is decoded directly into a generated struct with a plain `int64` amount. Bodies `{}`, `null`, and `{"rate_amount":null}` therefore produce zero without a decoding error. The gateway forwards an explicit zero, and teaching accepts it because zero is a valid rate. The teaching boundary has the same presence problem. OpenAPI requires a nonnull `rate_amount`.

**Why it matters**: A malformed request can persist a free tuition rate, advance the revision, and publish a dated money fact instead of returning 400. The server cannot distinguish a deliberate zero from an omitted amount after this decoding step.

**Suggested fix**: Validate required field presence and nullability at the gateway and teaching boundary before converting to the domain input. Keep an explicit integer zero valid. Add cases for missing, null, empty object, and explicit zero bodies without hand editing generated types.

### 🟠 Archived classes cannot reach the rate correction interface, `web/src/pages/ClassDetailPage.tsx:177`

**Problem**: The whole page, including tuition rates and the rate dialog, depends on a successful roster read. `ReadClassRoster` in `services/teaching/internal/handler/class_rosters.go:134` deliberately returns 404 for every archived class. An archived class with retained sessions can therefore have a successful rate response while the page renders only the roster error.

**Why it matters**: The archived correction flow promised by AC-3 is unavailable in the browser. A billing `rate_missing` recovery link for such a class reaches the same dead end.

**Suggested fix**: Allow the owned archived class and its rate editor to render independently of active roster availability. Keep roster mutations disabled where appropriate. Add a component scenario with an archived rate response, retained sessions, and a rejected active roster read.

### 🟠 An uncertain issue response never triggers the required authoritative recovery read, `web/src/pages/BillingPage.tsx:121`

**Problem**: `onError` handles only `preview_stale`. When the server commits but the response is lost, the existing period query remains `unissued` and the page continues to show the old preview plus an error. It does not read the period to find the committed run before offering another issue action. The preview result's `already_issued` variant is also not reconciled into `run`, which is taken only from `periodQuery` at line 135.

**Why it matters**: The user cannot tell whether invoices exist after a network failure, despite the authoritative read being available without Redpanda. A concurrent issue discovered by preview can leave a disabled zero invoice review instead of the committed invoices. AC-17 and AC-18 require these recovery states.

**Suggested fix**: After an uncertain issue failure, read the submitted period and display its committed run when present. Only offer another explicit issue when that read confirms it is unissued. Reconcile `already_issued` preview responses into the same issued view. Cover response loss and an issue committed between the initial period read and preview.

### 🟠 The production issue and certification protocols lack regression coverage, `services/billing/internal/handler/billing_periods_test.go:139`

**Problem**: The new handler tests exercise date validation, blocker helpers, readiness JSON decoding, and one fingerprint vector. No Go test invokes `BillingService.Preview`, `BillingService.Issue`, the actual barrier, or the new certification protocol. Existing money tests exercise lower level SQL, and the thread script covers a sequential issue path. They do not prove transaction rollback after each issue write, concurrent production issue, ledger failure handling, certification failures, or repeatable snapshot behavior.

**Why it matters**: These are the new branches that protect immutable money records, and the review found failures precisely in those untested paths. Marking AC-24 covered by schema inspection and helper tests leaves the required real Postgres and Redpanda behavior unverified by the committed suite.

**Suggested fix**: Add focused integration tests around the actual production handler and recovery commands using the repository infrastructure. Include failure injection at each money write, concurrent issue and fresh winner reads, fixed barriers, ledger failures and resolution, failed resets, retention gaps, stale fingerprints, and the exact volume boundaries.

## Minor

### 🟡 Backdated corrections leave the history status permanently syncing, `gateway/internal/route/billing_routes.go:75`

**Problem**: The gateway compares the projected revision on the current effective date with the class's global `rate_revision`. A backdated command increments that global revision without changing the current effective date. For example, after current September revision 3 and an August correction at revision 4, billing correctly holds September revision 3 and August revision 4, but this check can never report `synced`.

**Why it matters**: The public response reports outstanding projection lag after all facts have arrived, contradicting AC-19. The browser's pending command check correctly uses its exact date, so the two status sources can disagree.

**Suggested fix**: Distinguish overall class command progress from the revision of the current dated row. Determine overall history readiness from the appropriate class progress evidence, while retaining exact effective date checks for a pending correction. Add the backdated sequence to the gateway tests.

## Strengths

* The issue transaction keeps the run, number allocation, frozen invoice values, lines, and outbox events together. Issued reads use authoritative tables and do not require broker access.
* Candidate calculation retains missing attendance and labels, uses inclusive roster dates, preserves zero rate lines, and keeps ordering deterministic for the typed fingerprint.
* Rate receipts retain the original response, and positive revision projection updates reject stale corrections for the same effective date.

## Test coverage

The committed tests give useful coverage of sequential rate revisions, receipt replay, basic tenant isolation, SQL candidate visibility, stale rate rejection, browser ready and blocked states, and one canonical digest. The end to end thread also exercises a changed rate, stale preview refusal, successful issue, retry, and immutable reload.

This review did not run database or broker mutations or modify tests. It used source and contract inspection plus two read only in memory checks: the installed query library reproduced the month cache race, and JSON encoding demonstrated the supported response size exceeding the gateway cap. The integration and browser gaps described above remain necessary before merge.
