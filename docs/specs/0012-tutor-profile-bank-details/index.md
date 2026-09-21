# 0012. Tutor profile and bank details

**Date**: 2026-09-21
**Status**: In Progress

## Summary

The tutor can keep the identity and Vietnamese bank details that future invoices need. Billing owns
one versioned profile, validates every save, and serves a committed bank catalog with no runtime
network dependency. The web app adds one accessible profile page inside the existing account area.

## Requirements

**User stories**:

* As a tutor, I want to save my invoice identity and bank details so future invoices carry the right
  payment instructions.
* As a tutor, I want to save partial progress and see what remains so I do not lose work.
* As a tutor, I want stale edits and service failures handled without silently overwriting or
  clearing my details.

**Acceptance criteria**:

* **AC-1**: A signed in tutor can open `/profile` from the account panel. The panel shows an
  `Incomplete` status beside the link until the profile is complete, without showing bank values.
* **AC-2**: `GET /api/invoice-profile` returns the tutor's normalized profile, `revision`,
  `is_complete`, `missing_fields`, and `bank_status`. When the identity registration event has not
  seeded the row yet, billing seeds an empty row idempotently and returns it with status `200`.
* **AC-3**: A tutor can save any partial or complete profile through one explicit action. A successful
  save survives reload and returns the canonical stored representation.
* **AC-4**: The one generated database gate reports complete exactly when legal name, contact line,
  bank code, canonical bank name, account number, and account holder are all present. The missing
  field list is empty exactly when that gate is true.
* **AC-5**: `GET /api/banks` returns every active committed catalog entry with its payment network
  acquirer identifier, familiar short name, and official name. The page can search all three without
  case or Vietnamese accent sensitivity. The catalog records a dated snapshot and checksum from the
  official VietQR v2 banks endpoint.
* **AC-6**: Billing trims surrounding whitespace, collapses repeated internal whitespace, treats
  blank text as missing, preserves letter case in names, and enforces the field rules in this spec.
  Invalid values return status `422` through `vermouth.APIError` with safe field codes in `details`.
* **AC-7**: Every changed save compares `expected_revision` atomically and increases the revision
  once. An identical retry against a stale revision returns the current profile as success. A stale
  save with different values returns `409 profile_conflict` and changes nothing.
* **AC-8**: A saved bank that is no longer active remains readable with `bank_status: inactive`, is
  absent from new choices, and must be replaced before any changed save can succeed. No handler
  clears or rewrites it automatically.
* **AC-9**: Only the tutor identified by the verified token can read or write their profile. No
  request accepts `tutor_id`. Bank values appear in no event, log, URL, account panel, or cacheable
  profile response.
* **AC-10**: The page follows `web/design.md`: a focused header and completion status, Invoice
  identity, Bank details, then one Save action. It works by keyboard, at phone width, at 200 percent
  zoom, in both themes, and with long Vietnamese text. Every normal target is at least 44 CSS pixels.
* **AC-11**: A profile load failure shows a page error with Retry and no empty form. Save failures
  preserve every entered value. A bank catalog failure preserves the loaded profile, disables bank
  changes, offers Retry, and permits a save only when the unchanged saved bank is active.
* **AC-12**: Leaving with dirty values asks whether to stay or discard. Saving a previously complete
  profile as incomplete first opens a confirmation that names the cleared fields and explains that
  future invoice creation will be blocked.
* **AC-13**: Billing remains the authoritative reader for invoice creation. An identity event replay
  may recreate a missing row but cannot change any profile field, bank field, or revision.
* **AC-14**: Profile responses use `Cache-Control: no-store`. The authenticated bank catalog uses a
  private one day browser cache. Signing out cancels related requests, clears tutor scoped profile
  and bank query data plus editor state, and prevents a late response from restoring either one.

## Decision

**Chosen option**: Option 1: one versioned billing profile with a committed bank catalog

Billing keeps one authoritative `invoice_profiles` row per tutor. A whole resource `PUT` saves the
editable values, an atomic revision protects against stale writers, and billing derives the bank
name from a project owned catalog keyed by the payment network acquirer identifier. The gateway
proxies the resource without adding business rules, and the browser builds the page from the
existing design system.

**Implementation skills**: `openapi` (`oakoss/agent-skills`, `.agents/skills/openapi/`) ·
`golang-database` (`samber/cc-skills-golang`, `.agents/skills/golang-database/`) ·
`golang-security` (`samber/cc-skills-golang`, `.agents/skills/golang-security/`) ·
`frontend-design` (`anthropics/skills`, `.agents/skills/frontend-design/`) ·
`tailwindcss-accessibility` (`josiahsiegel/claude-plugin-marketplace`,
`.agents/skills/tailwindcss-accessibility/`) · `tanstack-query-best-practices`
(`deckardger/tanstack-agent-skills`, `.agents/skills/tanstack-query-best-practices/`) ·
`tanstack-router-best-practices` (`deckardger/tanstack-agent-skills`,
`.agents/skills/tanstack-router-best-practices/`)

## Rationale

Reasoning and options: see [rationale.md](rationale.md).

## Feature design

### Data model

The existing authoritative row gains two columns and a stronger generated gate.

| Column | Type | Rule |
|---|---|---|
| `tutor_id` | `uuid PRIMARY KEY` | From the verified token, never request input |
| `legal_name` | `text` nullable | Required for completeness, at most 120 characters |
| `contact_line` | `text` nullable | Required for completeness, at most 200 characters |
| `bank_code` | `text` nullable | Active acquirer identifier selected from the catalog |
| `bank_name` | `text` nullable | Familiar short name derived from `bank_code` by billing |
| `bank_account_number` | `text` nullable | Required for completeness, 3 to 34 uppercase ASCII letters or digits |
| `bank_account_holder` | `text` nullable | Required for completeness, at most 120 characters |
| `revision` | `bigint NOT NULL DEFAULT 0` | Increases once for each changed save, never below zero |
| `created_at` | `timestamptz NOT NULL DEFAULT now()` | Existing row creation time |
| `updated_at` | `timestamptz NOT NULL DEFAULT now()` | Changes with a changed save |
| `is_complete` | generated `boolean` | True only when the six profile and bank values are present |

The migration adds `bank_code` and `revision`, adds `CHECK (revision >= 0)`, and recreates
`is_complete` to include `bank_code`. A row that has an old free text bank name but no bank code
becomes incomplete. The migration preserves that name and does not guess a code from it. Every new
handler write keeps `bank_code` and `bank_name` paired.

The six completeness values are `legal_name`, `contact_line`, `bank_code`, `bank_name`,
`bank_account_number`, and `bank_account_holder`. SQL treats null, an empty string, and a string that
becomes empty after trimming as absent. `missing_fields` uses this fixed editable order:
`legal_name`, `contact_line`, `bank_code`, `bank_account_number`, `bank_account_holder`. It reports
`bank_code` when either the stored code or canonical name is absent.

The catalog is a committed reviewed snapshot, not a table and not a runtime call. Its source is
`GET https://api.vietqr.io/v2/banks`. The snapshot records the capture instant, raw response SHA256,
and source identifier beside the approved mapping. VietQR `bin` becomes the six digit string
`code`, `shortName` becomes `short_name`, and `name` becomes `official_name`. Only entries with
`transferSupported = 1` start active. The build reviews the snapshot before this feature can be
accepted. Catalog tests require unique codes, unique active short names, nonempty values, and output
sorted by code.

Retirement changes `active` to false. A retired entry stays in the catalog forever and its code is
never reused, so a saved profile always has a stable status and label. A source refresh is an
explicit repository change that preserves every retired entry rather than replacing the file
blindly.

### State transitions

```text
profile
  missing row -> seeded incomplete revision 0
  incomplete -> incomplete or complete through a valid save
  complete -> complete through a valid save
  complete -> incomplete only after the tutor confirms the warning

bank status
  missing -> active through a catalog selection
  active -> inactive when a later committed catalog version retires it
  inactive -> active only when the tutor selects a current bank

editor
  clean -> dirty -> saving -> clean
  saving -> field error or service error -> dirty
  saving -> conflict -> dirty while current server values load separately
```

### Validation and normalization

| Field | Request rule | Stored value |
|---|---|---|
| `legal_name` | Optional while incomplete, at most 120 Unicode code points after normalization, no controls or line breaks | NFC normalize, trim outer Unicode whitespace, collapse internal Unicode whitespace, preserve case, blank becomes null |
| `contact_line` | Optional while incomplete, at most 200 Unicode code points after normalization, no controls or line breaks | Same normalization |
| `bank_code` | Optional while incomplete, exact active catalog code | Trim outer whitespace, then store code and derive `bank_name` from the same catalog entry |
| `bank_account_number` | Optional while incomplete, otherwise `[A-Za-z0-9]{3,34}` | Trim outer whitespace, reject internal whitespace and punctuation, uppercase ASCII, blank becomes null |
| `bank_account_holder` | Optional while incomplete, at most 120 Unicode code points after normalization, no controls or line breaks | Same name normalization, independent from legal name |

Text processing rejects controls and line breaks before whitespace collapse. It then applies Unicode
NFC, trims, collapses whitespace, and counts Unicode code points. The account number follows its own
rule and never applies Unicode folding.

The request never accepts `bank_name`. That prevents a caller from pairing a valid code with a false
display name. A changed save carrying the current inactive code fails validation. An identical retry
still succeeds because it changes nothing.

A migrated free text `bank_name` with no code appears as a read only previous bank name hint. The
handler preserves it while `bank_code` remains null. Selecting an active code replaces it with that
entry's short name. When a saved code stays unchanged, a later catalog rename does not rewrite the
stored name or revision. Only selecting a different code derives a new name.

### API surface

Every operation is authenticated by the existing bearer token. The gateway verifies it and passes it
unchanged to billing, where billing verifies it again.

| Endpoint | Method | Key inputs | Key outputs | Key errors |
|---|---|---|---|---|
| `/api/invoice-profile` | GET | none | nullable fields, `revision`, `is_complete`, `missing_fields`, `bank_status` | `401`, `500` |
| `/api/invoice-profile` | PUT | `expected_revision`, all five nullable editable keys as a complete representation | normalized profile response | `400`, `401`, `409 profile_conflict`, `422 invalid_profile`, `500` |
| `/api/banks` | GET | none | bounded `banks` array with `code`, `short_name`, `official_name` | `401`, `500` |

The bank list is finite committed reference data, so it is deliberately unpaged. The `PUT` schema
requires `expected_revision` and all five editable keys, permits null for each editable value, sets
`additionalProperties: false`, and requires a nonnegative revision. The HTTP decoder also rejects
unknown properties. Malformed JSON, missing keys, wrong JSON types, a negative revision, and unknown
properties return `400`. A valid shape with invalid field values returns `422`.

Both profile methods seed the row idempotently before their transaction continues. One short
transaction locks the row with `SELECT FOR UPDATE`, normalizes the five editable values, and compares
them with the stored editable state. An identical state returns `200` without a revision change.
Otherwise a revision mismatch returns `409`. Only then does the handler validate values and active
bank status, update fields and timestamp, and increase revision once. A differing stale request
therefore gets `409` before field validation. Conflict details contain exactly `current_revision`.

Status `422` details use this shape:

```json
{
  "fields": [
    { "field": "legal_name", "reason": "too_long" }
  ]
}
```

Known field values are `legal_name`, `contact_line`, `bank_code`, `bank_account_number`, and
`bank_account_holder`. Known reason values are `too_long`, `invalid_format`, and `inactive_bank`.
The browser owns Vietnamese copy for these stable codes. Partial values are valid, so no field uses a
`required` error.

### Page and client behavior

The account sheet adds one Profile and bank details link. It reads completion from the profile query
and shows only the `Incomplete` status when needed. While completion is unknown it shows no status;
a failed account panel read shows a retryable unavailable cue. The `/profile` page uses the current
`AppShell`, cards, form fields, inputs, button, dialog, toast, and status badge. One product component
provides a search input followed by a native radio group of matching banks, using the existing
primitives and no bank logos.

The profile and catalog queries load in parallel. Profile data is immediately stale and refetches on
page entry. Catalog data has a one day stale time. A successful mutation writes its returned profile
into the exact profile query key. No optimistic update is used because server normalization and
revision checks decide the canonical row. Sign out clears both query keys.

The draft initializes only from the first successful load or an explicit discard and reload action.
A refetch may update cached server state but never replaces a dirty draft. Dirty state compares the
normalized five editable draft values with that loaded baseline. While saving, fields and duplicate
submission are disabled, so a response cannot erase text entered after the request began.

After `409`, the page keeps the draft and its original expected revision, then fetches the current
profile separately. It offers Keep editing, which leaves the draft and stale revision untouched, or
Discard my edits and load latest, which requires confirmation and adopts the fetched row. It never
silently updates the draft revision and offers no forced overwrite or merge action.

If the catalog fails after the tutor changed the bank draft, the draft remains visible, the picker is
disabled, and Save is allowed only when the draft code equals the saved code and the profile reports
that code active. A server `inactive_bank` response preserves the draft, invalidates and refetches the
catalog, and requires an active replacement.

Dirty state uses the TanStack Router navigation blocker for app navigation and `beforeunload` for a
browser reload or close. A blocking load failure uses an alert. Field errors connect through
`aria-invalid` and `aria-describedby`, and focus moves to the first invalid field. Save success uses
a polite live announcement. The incomplete confirmation dialog traps focus and returns it to Save.
That confirmation is a browser protection, not server authorization. Billing accepts any valid
partial `PUT` with the correct revision.

Search applies Unicode decomposition, removes combining marks, lowercases, maps Vietnamese `đ` and
`Đ` to `d`, and then performs substring matching across the folded official name, short name, and
code. Tests cover composed and decomposed accents plus both forms of `đ`.

Query keys include the authenticated tutor id. User initiated sign out first confirms discard when
the editor is dirty, then cancels profile and catalog requests, removes both queries, clears editor
state, and unmounts the protected tree. Forced session expiry skips the prompt and clears private
state immediately. A cancelled or late response cannot repopulate a new session's query key.

### Value sourcing

| Action | Value produced or displayed | Source |
|---|---|---|
| Read or save profile | Tutor ownership | `sub` from the verified token |
| Read missing profile | Empty row and revision zero | `SeedInvoiceProfile`, called idempotently before the read |
| Display profile fields | Canonical values | `billing.invoice_profiles` for the trusted tutor |
| Save legal name, contact line, account number, holder | Canonical values | Normalized `PUT` inputs under the validation table |
| Save bank code | Stable bank identity | `bank_code` input matched to one active committed catalog entry |
| Save bank name | Familiar short name | `short_name` on the matched catalog entry, never request text |
| Decide completeness | Structural profile readiness | Generated `invoice_profiles.is_complete` |
| Display missing fields | Editable fields still absent | Null checks on the same stored row, with emptiness tested against `is_complete` |
| Display bank status | `missing`, `active`, or `inactive` | Stored `bank_code` looked up in the current committed catalog |
| Display a migrated bank hint | Previous free text bank name | Stored `bank_name` when `bank_code` is null |
| Build the bank catalog | Code, names, and initial activity | Dated official VietQR v2 banks snapshot: `bin`, `shortName`, `name`, and `transferSupported` |
| Display bank choices | Active banks | Committed catalog entries where `active` is true |
| Search bank choices | Matching entries | The exact Vietnamese folding rule applied to returned `official_name`, `short_name`, and `code` |
| Detect a stale save | Current revision | Locked `invoice_profiles.revision` compared with `expected_revision` |
| Treat a repeated save as success | Whether desired values already landed | Normalized five editable inputs compared with the locked current row before the revision check |
| Show account panel status | Incomplete cue only | `is_complete` from the profile query |
| Show unknown account status | No badge or unavailable cue | Profile query loading or error state |
| Confirm a complete profile becoming incomplete | Cleared field labels | Difference between the loaded complete profile and the normalized draft |
| Explain a boundary failure | Request identity | Gateway request id carried in `vermouth.APIError` |

### Key invariants

* Billing owns every profile value and no other service stores a copy.
* One tutor has exactly one profile row, selected only from the verified token.
* A catalog selection writes code and name as one pair. A request never supplies the name.
* A retired catalog code remains reserved forever and cannot be reused for another bank.
* An unknown nonnull stored code is treated as inactive. Its stored label and all values remain
  unchanged until the tutor chooses an active replacement.
* `missing_fields` is empty exactly when `is_complete` is true.
* A changed save increases revision once. A failed, identical, or rejected save does not.
* A stale writer never overwrites a newer different row.
* Identity replay may insert the empty row and may not update any profile column.
* Issued invoices remain immutable. This feature changes no existing invoice row.
* The browser never invents validation, bank names, completeness, or revision outcomes. It mirrors
  server results and uses matching early validation only to improve feedback.
* Structural completeness and bank activity are separate. The page never hides an inactive warning
  behind a Complete status.

### Security model

The profile is private bank data for one tutor, with no additional regulatory scope. Existing
database, cluster, backup, and access controls apply. Application field encryption is not added.

Every query filters by trusted `tutor_id`. No bank value enters Redpanda, logs, URLs, error details,
or the account panel. A save log may contain request id, tutor id, outcome, changed field codes,
revision before and after, completeness before and after, and no values or account fragments.
Profile responses carry `Cache-Control: no-store`. The full account number appears only inside the
owner's editing input. Sign out clears the related TanStack Query memory.

No new environment variable, secret, external account, or third party credential is required.

### Critical test scenarios

* Happy path: save partial progress, reload it, finish every field through an active bank selection,
  and observe the page and account panel become complete, verifies **AC-1** to **AC-6**, **AC-10**.
* Lost response: commit a save, retry the same old revision and values, and receive the current row
  without another revision increase, verifies **AC-7**.
* Conflict: two clients save different values from one revision, one succeeds and the other gets
  `409` without data loss, verifies **AC-7**, **AC-11**.
* Retired bank: load a saved inactive bank, keep it readable, exclude it from choices, then require a
  new active selection before a changed save, verifies **AC-5**, **AC-8**.
* Replay: replay the registration event after a complete profile exists and compare every field and
  revision byte for byte, verifies **AC-13**.
* Permission: tutor B cannot read or change tutor A's row, and a body supplied `tutor_id` is rejected
  by the generated contract, verifies **AC-9**.
* Browser recovery: fail profile load, catalog load, and save separately, then exercise dirty
  navigation and complete to incomplete confirmation, verifies **AC-11**, **AC-12**.
* Accessibility: use keyboard only at phone width and 200 percent zoom to search a bank, fix an
  announced validation error, confirm an incomplete save, and retain visible focus, verifies
  **AC-10**.
* Request and editor edges: cover `PUT` before `GET`, a current revision no op, concurrent identical
  saves, migrated free text and blank values, a renamed or unknown catalog code, dirty refetch,
  disabled editing during save, and sign out with requests in flight, verifies **AC-2**, **AC-4**,
  **AC-7**, **AC-8**, **AC-11**, **AC-14**.

## Build plan

Tracer Bullet means the first milestone proves one partial value from browser to billing and back
before the full form grows.

1. [x] Add the billing migration for `bank_code`, `revision`, and the recreated completeness gate. Add
   the OpenAPI 3.1 profile schemas and `GET` plus `PUT` routes, regenerate both clients, then carry
   one partial legal name through billing, gateway, `/profile`, and reload. Add the account panel
   entry and its completion cue, satisfies **AC-1**, **AC-2**, **AC-3**, **AC-4**, **AC-9**,
   **AC-13**.
2. [x] Add the reviewed committed bank catalog, `GET /api/banks`, full normalization and validation, the
   canonical bank pair, every form field, completion cue, searchable bank component, and complete
   responsive page composition, satisfies **AC-1**, **AC-3** to **AC-6**, **AC-8**, **AC-10**,
   **AC-14**.
3. [x] Add atomic revision saves, identical retry recognition, conflict responses, inactive bank rules,
   dirty navigation, complete to incomplete confirmation, and independent load and save recovery,
   satisfies **AC-7**, **AC-8**, **AC-11**, **AC-12**.
4. [x] Close the thread with migration reversal, generated contract checks, tenant isolation, replay
   preservation, safe log assertions, cache headers, browser query clearing, phone, keyboard, zoom,
   dark theme, and long Vietnamese evidence. Extend `task thread` through profile save and read,
   satisfies **AC-2**, **AC-4**, **AC-6**, **AC-7**, **AC-9** to **AC-14**.

## Consequences

**Positive**:

* Future invoice work reads one local billing row with a stable bank identifier.
* Partial saves preserve work while one database gate still decides structural readiness.
* Revision checks prevent a stale tab from silently replacing newer payment instructions.
* A committed catalog keeps the page and later invoice work independent from a third party outage.

**Negative and tradeoffs**:

* The project now owns catalog review and retirement updates.
* A filtered radio list is simpler than a custom combobox, but the full bank list needs a contained
  scroll region and careful focus behavior on a phone.
* A retired catalog entry can leave a structurally complete profile whose bank status is inactive.
  Feature 14 must decide whether QR generation refuses that state.
* Whole resource saves make conflict behavior clear but send unchanged values on every update.

**Neutral**:

* No event changes and no service gains another service's bank data.
* The legal invoice name stays independent from Google's display name.
* The account holder may differ from the legal invoice name.

## Follow-up

* [ ] Feature 13 should freeze `bank_code` beside the existing payee fields when it issues an invoice,
  so a later bank change cannot alter a re rendered invoice.
* [ ] Feature 14 should verify the committed acquirer identifiers against its chosen Vietnamese QR
  encoding, decide how an inactive saved bank affects generation, and render only the frozen code.
