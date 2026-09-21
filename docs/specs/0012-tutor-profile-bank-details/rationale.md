# Rationale for tutor profile and bank details

## Context

The invoice profile already has an authoritative home in billing, an empty row seeded by identity,
and a database completeness gate. What is missing is the public contract and screen that let the
tutor manage it safely. These values become payment instructions, so a stale browser write or an
ambiguous bank name can produce a wrong invoice even though the form appeared to save.

The product has one tutor account, no admin role, no additional compliance regime, and no need for a
runtime bank provider. The existing gateway contract, Go service layers, Postgres row, and React
design system are enough. The main design pressure is therefore correctness with few moving parts.

The future QR feature needs a stable bank identifier, but it does not yet need QR encoding here.
This decision stores the Vietnamese payment network acquirer identifier now and leaves QR payload
rules to feature 14. The official VietQR v2 banks endpoint is the dated snapshot source, never a
runtime dependency.

## Options considered

### Option 1: One versioned profile with a committed bank catalog

Billing owns one row, validates a whole resource save, and derives the display name from a committed
catalog. A revision rejects stale different writes.

**Pros**:

* One source decides validation, bank identity, completeness, and invoice reads.
* No runtime provider can make profile editing unavailable.
* Lost responses and concurrent tabs have explicit safe outcomes.

**Cons**:

* The repository owns catalog maintenance.
* Revision handling and the filtered bank list add work beyond a simple form.

### Option 2: Keep the existing free text bank name

The tutor types a bank name and feature 14 later maps it to a QR identifier.

**Pros**:

* Smallest change for this feature.
* No catalog needs maintenance yet.

**Cons**:

* Names have aliases and spelling variations, so QR generation would need guessing or another tutor
  correction later.
* Validation could say complete while the stored bank cannot produce a QR.

### Option 3: Use a live external bank catalog

Billing or the browser asks an external provider for bank choices at runtime.

**Pros**:

* Catalog updates arrive without a repository change.
* A specialist provider owns bank naming and identifiers.

**Cons**:

* An external outage can block a local profile edit.
* Startup, caching, timeout, reconciliation, and provider change behavior become new product work.
* The project would depend on a provider before feature 14 has selected its QR contract.

### Option 4: Keep several profiles or profile history

The tutor selects among bank profiles, or every edit creates a retained revision.

**Pros**:

* Switching accounts and reviewing old settings become direct capabilities.
* A full database history can support later audit needs.

**Cons**:

* The product has one tutor and one current payment destination, so selection and retention rules
  solve no present user problem.
* Issued invoices already freeze the values that must remain historically exact.

## Rationale

Option 1 is the smallest design that removes ambiguity before money work begins. It extends the row
billing already owns, keeps the gateway free of rules, and avoids a runtime dependency. The acquirer
identifier gives feature 14 a stable input without deciding QR encoding early.

Partial saves match the existing nullable row and protect work on a phone. Structural completeness
stays in Postgres so the profile page and future month end run cannot disagree. Optimistic revision
checking costs one integer but prevents a stale tab from silently replacing payment instructions.
An identical retry is treated as success so a lost response does not turn an already completed save
into a false conflict.

The runner up was free text because it requires the least code. It was rejected because it moves the
hard decision into the QR feature after the tutor has already entered data. A live provider was also
rejected because availability and reconciliation costs outweigh automatic catalog updates for one
tutor.
