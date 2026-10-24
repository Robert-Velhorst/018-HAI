# Approval decision deadline reconciliation: 24 October 2026

## Archived requirement and reproduced gap

The retained failed-patch variant `c970061fa5e3` proposed a regression requiring
an approval proof not to outlive its recorded decision. The current service
limited a relative TTL, but the signer added that duration to its later clock
reading. The absolute decision deadline could therefore be exceeded.

A deterministic regression using the existing service, test repository and real
approval-proof signer reproduced the gap before the repair: advancing the
signer's injected clock made the old proof expire approximately 30 seconds after
the recorded decision deadline. No external automation or provider was invoked.

## Repair

- Carry the recorded decision's absolute deadline in the internal issuance request.
- Normalize and cap expiry in the signer before signing or durable reservation.
- Reject elapsed or sub-precision deadline windows before reserving the source.
- Preserve the existing maximum TTL and action, owner, scope, signature,
  single-issuance and consumption controls. A later absolute bound cannot extend
  the requested TTL. Zero bound retains compatibility for direct trusted callers;
  the sole production request construction passes the recorded decision deadline.
- Do not alter an already-signed proof or broaden any account permissions.

## Verification and limits

The historical service requirement was ported as
`TestApprovalProofCannotOutliveRecordedDecision`, with the current reviewed
configuration contract and a deterministic independently advanced signer clock.
A second test covers eight signer cases: absent bound, later bound, earlier bound,
UTC/precision normalization, minimum precision, expired, equal, and sub-precision
bounds. Rejected requests can reuse their source, proving no issuance reservation
was consumed. Valid bounded proofs pass real signature and consumption checks.

Native Go 1.27.3 on Windows, using a four-file source overlay and existing module
cache without downloads, passed 62 selected approval/security test cases with one
skip. The skipped case is
`TestPostgresApprovalProofConsumptionSurvivesRestartAndIsAtomic`; no live PostgreSQL
or database durability acceptance is claimed. Separate `go vet` passed for the
automation package. Formatting and final staged diff are checked separately.

No Docker test environment, provider request, account change or operator database
was introduced. The D: source worktree was not edited. Exact-head hosted CI, full
backend coverage and merging into main remain separate gates. This receipt alone
does not authorize deletion of the original archives or source copies.
