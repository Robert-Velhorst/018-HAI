# Runtime and framework archive reconciliation: 24 October 2026

## Scope

This follows the failed-patch reconciliation receipt. It covers the two remaining
runtime/framework variants, not the full retained transcript corpus. Private
receipts retain full source hashes; this public report uses short references.
An implemented local repair is not proof of publication, merge, deployment, or
permission to delete archives.

## Framework provenance variant a5a7e12aae53

The nine historical hunks were compared with current workflow source:

- Completion persistence now uses attested `CompleteClaimedItem` instead of the
  earlier `UpdateClaimedItem`; both error branches retain framework selection.
- `normalizeFrameworkSelection` replaces the older helper name and also handles
  operating-contract and selector-risk metadata.
- JSON provenance remains in decision Reason, readable policy metadata remains in
  RuleApplied, and the actor and event trigger retain workflow-worker identities.
- The stable framework-selection source URI remains. Repeated event writes now
  compare both payload and rule, stronger than the older message-only check.
- The framework-selection record's Approved flag differs from the historical
  draft. It does not substitute for execution approval: `workflowApprovalProof`
  explicitly requires decision type `approval`, the approved decision value and
  an actor matching the workflow owner. No approval policy was changed here.

One historical compatibility path was genuinely missing: the old producer format places
`catalog <version> digest <digest>; constitution v<version> digest <digest>` in
Reason and JSON in RuleApplied. The current decoder only read RuleApplied when
Reason was empty. A new deterministic regression failed on that old descriptive
format before the repair. No operator database was searched to establish whether
such historical rows currently exist.

The repair recognizes only the historical catalog-summary prefix and requires
the entire summary to equal the catalog/constitution metadata in its decoded
payload. Existing normalization, schema validation and decision-ID consistency
checks still apply. Unknown prose, mismatched metadata and malformed primary JSON
do not become trusted through fallback. Current primary JSON remains authoritative
even if the legacy field is invalid. No owner scope or permission was broadened.

Verification: native Go 1.27.3 on Windows passed 27 selected framework test cases
and separate workflow-package vet. One real PostgreSQL round-trip test was
skipped; no database durability acceptance is claimed. The new safeguard table
covers metadata conflicts, invalid JSON, decision identity, unknown prose and
compatibility with empty summaries and valid primary payloads.

## Historical runtime test-file variant 37d7d98e1590

The historical file is not suitable for copying over current runtime tests:

| Historical expectation | Current disposition |
| --- | --- |
| Approval boolean permits registry execution | Existing tests additionally require owner-bound provenance and final-effect proof. |
| Cancellation establishes a stopped/blocked outcome | Current tests require owner-bound stop and report an indeterminate downstream outcome until delivery is verified. |
| Duplicate active runtime task ID is rejected | Existing regression retains this protection. |
| Odysseus agent HTTP request completes with selected tool flags | Superseded by tests that block unmediated tools before contacting the runner. Direct execution availability is NOT claimed. |
| Hermes direct CLI invocation with safe-looking flags | Superseded by policy-block tests preventing process creation without per-tool mediation. No live Hermes execution acceptance is claimed. |
| OpenClaw direct CLI invocation | Superseded by the run-bound gateway policy boundary; CLI opt-in cannot bypass it. No live gateway acceptance is claimed. |
| Switching an upload path deletes the previous archive | Deliberately superseded by `TestOpenClawSetEcosystemPathPreservesPreviousArchives` and managed-archive rollback/restart tests. Restoring filename-based deletion would defeat recoverability. |
| Workspace containment, truncated streams, skill routing, personal-work envelopes and ZIP inventory | Current source retains these checks and assertions, with additional provenance and containment guards. |

Verification: 23 explicitly selected existing runtime tests passed natively on
Windows, with no skips. They used local fakes, temporary fixtures and in-process
HTTP handlers, not live external agents or new Docker containers. This is selected
runtime coverage, not a claim that every old test body or every runtime works live.

## Updated failed-patch subset status

All 18 unique variants now have an explicit source-level disposition:

- 16 variants (65 failed entries): preserved source intent or explicitly documented
  supersession on the reviewed main/source baseline.
- Two variants (five failed entries): archive requirements exposed actual gaps;
  approval expiry and descriptive framework-record compatibility have tested
  local repairs. They still require publication, exact-head CI and main integration.

These totals apply only to the 70 failed patch entries. The thousands of successful
patch variants, remaining source copies and non-patch archive content are separate
work. No whole archive or source checkout was deleted during this review.

## Combined repair verification

Both prepared repairs were then tested together through the same six-file Go
overlay. The selected automation/workflow run passed 96 cases and skipped three
real PostgreSQL cases: proof consumption across restart, provenance round-trip,
and durable owner-scoped workflow approval lookup. Combined package vet passed.
All six tested source files were verified byte-for-byte against the prepared Git
tree. This does not establish real PostgreSQL, provider or deployed acceptance.
