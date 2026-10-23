# Task engine source reconciliation

## Scope and method

Two retained `backend/internal/task/service.go` variants, 145,594 and 142,040
bytes, were compared with published commit
`13933b0a744b37f671d39ac2651767b10845e3d1`. Their complete contents are identical
after CRLF-to-LF normalization. Original byte hashes were checked separately;
line-ending equivalence is not an exact-byte preservation claim.

The Go AST/token comparison accounts for all 160 retained declarations, including
comments, literal text and statement order: 125 are token-identical, 33 changed,
and two were removed. The current file has 175 declarations, including 17 added
declarations. These counts include replacement of the import group; every old
import is retained. The only removed function is `usesAdmittedEmbeddedRuntime`.

The complete 748-added/130-removed-line diff and changed trust-boundary contexts
were manually read. Token-identical bodies were not all manually reread. The
comparison does not prove shared dependency behavior, initialization order or
external runtime safety.

## Integration decision

Retain the current engine rather than overwrite it with either older variant:

- Caller-owned execution context and bounded storage operations preserve request
  cancellation and permit separately bounded outcome recording after dispatch.
- Approval execution uses the stored review configuration and binding digest,
  rather than trusting a response fingerprint or a browser-supplied claim.
- Uncertain execution outcomes retain receipts for review instead of becoming
  automatic retries, completed-result reuse or learned success.
- Retry policy distinguishes a potentially fixable answer from evidence rejected
  solely because of untrusted provenance, and carries MCP traces across attempts.
- Opt-in, bounded, read-only chat-history MCP context remains untrusted evidence,
  not instructions or action authority. Planning does not speculatively query it.
- Brain-skill selection checks the raw request owner, not only a normalized plan.
- Required framework participants need fresh verified role evidence. The removed
  embedded-runtime helper bypassed that requirement; restoring it would undo the
  current admission policy rather than recover a missing feature.

The participant gate can block even low-risk work until the required participants
are configured and verified. This review does not claim those participants are
operational. Registry declarations alone are rejected, but the helper is not a
formal proof of a trusted runtime probe or a complete provenance allowlist.

No production engine code was changed by this review. Useful newer behavior is
already present in the compared published file.

## Verification and remaining gates

The task package passed `go test -p 1 ./internal/task -count=1` at the same
published commit. Local fixtures and mock provider/runtime boundaries do not
prove real provider compliance, database concurrency, connected-account behavior
or end-to-end cancellation through every dependency.

Separate helper implementations, other retained source files and transcript
archives still require their own review. Review completion, CI, PR publication,
preservation on `main` and local deletion remain separate gates. Both retained
engine source files remain intact and are not deletion-eligible from this review.
