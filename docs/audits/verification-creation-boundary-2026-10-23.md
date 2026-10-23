# Verification creation acknowledgement

The verification reconciliation identified creation acknowledgement binding as
remaining work after finalization was hardened. The service previously trusted
the value returned by `CreateRun` immediately. A nil acknowledgement could panic
during memory opt-in auditing. A missing identity or substituted owner, question
or status could reach later verification processing.

Five regression fixtures demonstrated those failures before the implementation
change. Their repository double deliberately mutates the same pointer supplied
to creation, so comparing only against that mutated pointer would not be enough.

The service now captures the intended initial run as an independent value before
calling storage. The returned run must have a nonempty identity and match all
requested fields except the database-assigned ID and creation/update timestamps.
The comparison happens before opt-in auditing, evidence collection and memory
promotion. A separate positive fixture retains database-managed timestamps.
After the change, the entire native verification package passed locally: 159
named test/subtest PASS events, zero failures and zero skips. This result does
not establish live database, provider or production acceptance.

A rejected acknowledgement does not establish whether the database actually
inserted the initial row. The change does not automatically retry or promise to
roll that row back. It stops the downstream pipeline and reports a bounded error
instead of returning an apparently successful verification result.

This covers the run-creation contract only. Per-evidence/per-claim acknowledgements,
secondary audit failures, provider acceptance and the separate global-control
owner-policy decision remain outside this fix. No production database, account
permissions, credentials, archives or original source copies are changed.

A subsequent [record-acknowledgement follow-up](verification-record-acknowledgements-2026-10-23.md)
adds evidence/claim content and linkage checks. The 159-result figure above
remains the creation-only checkpoint, not a claim about that subsequent revision.
