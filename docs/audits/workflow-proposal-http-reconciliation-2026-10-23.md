# Workflow proposal and HTTP boundary reconciliation

## Reviewed sources

Reviewed the retained proposal-resolution transaction test and both retained
workflow HTTP handler variants. The handler variants have the same text with
different line endings. Keep the stronger current implementation rather than
restoring either older version wholesale.

The proposal tests preserve four distinct database behaviors: a proposal waits
for concurrent source retraction and sees its committed quarantine; opposing
resolvers commit exactly one decision and audit event; an audit write failure
rolls back the item, proposal, transition and decision together; and graph
projection happens only after a successful commit. Keep the newer typed audit
failure assertion instead of matching internal database error wording.

The current handlers retain verified owner and actor binding, owner-scoped reads
and writes, pending pursuit intake, and approval provenance separate from generic
state transitions. Keep newer bounded whole-document action requests,
cancellation-aware execution/recovery/follow-up interfaces, result redaction,
exact returned record identity checks and project dossiers. Do not restore
ignored JSON errors, implicit unbounded requests or legacy non-contextual worker
fallbacks. Older handlers add no missing behavior that should override these
protections.

## PostgreSQL acceptance gap and correction

The four proposal transaction tests existed but were not selected by the
explicit PostgreSQL CI commands. Ordinary local package checks without their
dedicated DSN skip them. Their presence and a green ordinary package test were
therefore not proof of real transaction or lock behavior.

Add all four to the existing disposable PostgreSQL job with the existing
destructive-test opt-in and dedicated migration database. Run them serially
with race detection and a bounded timeout. Require a separate exact top-level
PASS marker for each test, so missing or skipped tests cannot pass acceptance.
No database service, permission, migration or production behavior is changed.

A source-enumerating workflow contract checks selection and PASS markers for
every PostgreSQL test in the reviewed file and verifies that its database helper
checks the dedicated DSN before opening the connection. It failed before the
workflow wiring was added.

## Verification and preservation

The full workflow package passed uncached locally on Windows and Go 1.27.3
before this CI-only change. No dedicated local PostgreSQL DSN was configured,
so SQL cases were skipped, not claimed as executed. Real SQL acceptance remains
pending the new CI run. Local checks do not prove external-account acceptance.

Private receipts retain exact original fingerprints and review decisions.
Original worktrees and transcript archives remain intact. Publication alone
does not establish main preservation or authorize their deletion.
