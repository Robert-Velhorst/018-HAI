# Verification evidence and claim acknowledgements

After run creation and finalization were bound to their storage responses, the
record-level path still checked only that returned evidence/claims had IDs.
Six negative fixtures demonstrated that a repository could return another run's
record, changed evidence, changed evidence usage, changed claim text or a changed
claim status without stopping finalization or downstream memory processing.

Each record's intended value is now captured before the repository call. Its
run ID must already match the run being finalized. A nonempty returned ID is
still required. Database-managed IDs are accepted only when the input had none;
an explicitly supplied ID may not be replaced. Creation/update timestamps are
excluded from equality, but all content, provenance, scoring, review and linkage
fields must match the independent snapshot.

The negative fixtures mutate the same pointer supplied to storage, exercising
the snapshot boundary rather than merely returning a detached bad object. The
transaction fixture requires rollback of staged evidence, claims and completion
audit, leaves the initial run uncertain, and prevents memory promotion. Separate
cases reject foreign-run inputs before writes and accept database-generated
record metadata.

After these changes the complete native verification package passed locally:
170 named test/subtest PASS events, zero failures and zero skips. All six
negative acknowledgement cases had failed before the implementation change.
These are local repository-double and package results, not live database proof.

This is a storage-contract check, not independent confirmation of a committed
database transaction. Compatibility repositories without transaction support
cannot promise rollback of earlier successful writes. No automatic retry is
introduced. Completion-audit acknowledgement binding, advisory audit failure
reporting, database acceptance and external provider/source acceptance remain
separate review items. No schema, account permission, credential, original file,
transcript archive or database is changed by this code review.

A later [audit-boundary review](verification-audit-boundary-2026-10-23.md) covers
completion acknowledgement, memory opt-in acknowledgement and advisory audit
failure disclosure. This document's 170-result count is its earlier checkpoint.
